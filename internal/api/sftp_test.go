package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"testing"
)

func sftpReq(t *testing.T, method, url, cookie string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Cookie", cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// doPaste 发送粘贴请求并解析 NDJSON 流响应，返回最后一个事件与过程事件数。
func doPaste(t *testing.T, base, cookie, hostID string, body any) (int, map[string]any, int) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", base+"/api/hosts/"+hostID+"/sftp/paste", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	events := 0
	var last map[string]any
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		events++
		var ev map[string]any
		if json.Unmarshal([]byte(ln), &ev) == nil {
			last = ev
		}
	}
	return resp.StatusCode, last, events
}

func sftpList(t *testing.T, base, cookie, hostID, dir string) (int, []map[string]any) {
	t.Helper()
	u := fmt.Sprintf("%s/api/hosts/%s/sftp/list?path=%s", base, hostID, url.QueryEscape(dir))
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Cookie", cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestEditHostCredsInvalidatesSFTP 复现：编辑主机用户名/密码后，文件管理器
// 不应复用旧连接上已失效的 SFTP 客户端（否则持续报 "list failed: connection lost"）。
func TestEditHostCredsInvalidatesSFTP(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)

	// 目标机同时接受旧（普通用户）与新的（root）凭据
	host, portStr := startTestSSHServerUsers(t, map[string]string{
		testUser:     testPassword,
		"rooter":     "root-pass-456",
	})
	port, _ := strconv.Atoi(portStr)

	// 1. 以普通用户创建主机
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "creds-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
	})
	if code != 201 {
		t.Fatalf("create host: %d", code)
	}
	hostID := hostResp["id"].(string)

	// 2. 首次列表：构建 SFTP 缓存
	code, _ = sftpList(t, ts.URL, cookie, hostID, ".")
	if code != 200 {
		t.Fatalf("first list: %d", code)
	}

	// 3. 编辑主机：改用 root 用户与新密码
	code, _, _ = doJSON(t, "PUT", ts.URL+"/api/hosts/"+hostID, cookie, map[string]any{
		"name": "creds-vm", "host": host, "port": port,
		"username": "rooter", "auth_type": "password", "password": "root-pass-456",
	})
	if code != 200 {
		t.Fatalf("edit host: %d", code)
	}

	// 4. 再次列表：必须成功（基于新凭据重建 SSH 连接与 SFTP 客户端）
	code, entries := sftpList(t, ts.URL, cookie, hostID, ".")
	if code != 200 {
		t.Fatalf("list after edit failed: %d %v", code, entries)
	}
}

func TestSFTPFlow(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)
	if cookie == "" {
		t.Fatal("no cookie")
	}

	host, portStr := startTestSSHServer(t)
	port, _ := strconv.Atoi(portStr)
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "sftp-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
	})
	if code != 201 {
		t.Fatalf("create host failed: %d", code)
	}
	hostID := hostResp["id"].(string)

	// 1. 列出根目录（应为空或包含基础文件）
	code, entries := sftpList(t, ts.URL, cookie, hostID, ".")
	if code != 200 {
		t.Fatalf("list failed: %d", code)
	}

	// 2. 创建目录（相对路径：pkg/sftp 的 Windows 实现不解析绝对路径到 workdir）
	code, mkdirResp := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostID+"/sftp/mkdir", cookie,
		map[string]string{"path": "subdir"})
	if code != 200 {
		t.Fatalf("mkdir failed: %d %v", code, mkdirResp)
	}

	// 3. 写文件
	code, _ = sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostID+"/sftp/write", cookie,
		map[string]any{"path": "subdir/hello.txt", "content": "hello sftp"})
	if code != 200 {
		t.Fatalf("write failed: %d", code)
	}

	// 4. 列表应能看到子目录
	code, entries = sftpList(t, ts.URL, cookie, hostID, ".")
	if code != 200 {
		t.Fatalf("list failed: %d", code)
	}
	t.Logf("entries after mkdir: %v", entries)
	foundDir := false
	for _, e := range entries {
		if e["name"] == "subdir" {
			foundDir = true
			break
		}
	}
	if !foundDir {
		t.Fatal("subdir not listed")
	}

	// 5. 读取文件内容
	code, readResp := sftpReq(t, "GET",
		ts.URL+"/api/hosts/"+hostID+"/sftp/read?path="+url.QueryEscape("subdir/hello.txt"),
		cookie, nil)
	if code != 200 {
		t.Fatalf("read failed: %d", code)
	}
	if readResp["content"] != "hello sftp" {
		t.Fatalf("unexpected content: %v", readResp["content"])
	}

	// 6. chmod 为 600
	code, _ = sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostID+"/sftp/chmod", cookie,
		map[string]any{"path": "subdir/hello.txt", "mode": 0o600})
	if code != 200 {
		t.Fatalf("chmod failed: %d", code)
	}

	// 7. 重命名
	code, _ = sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostID+"/sftp/rename", cookie,
		map[string]string{"old_path": "subdir/hello.txt", "new_path": "subdir/world.txt"})
	if code != 200 {
		t.Fatalf("rename failed: %d", code)
	}

	// 8. 上传（multipart）
	code = uploadMultipart(t, ts.URL, cookie, hostID, "subdir/upload.txt", "uploaded data")
	if code != 200 {
		t.Fatalf("upload failed: %d", code)
	}
	code, readResp = sftpReq(t, "GET",
		ts.URL+"/api/hosts/"+hostID+"/sftp/read?path="+url.QueryEscape("subdir/upload.txt"),
		cookie, nil)
	if code != 200 || readResp["content"] != "uploaded data" {
		t.Fatalf("upload content mismatch: %v", readResp)
	}

	// 9. 下载（应返回原始内容）
	dlReq, _ := http.NewRequest("GET",
		ts.URL+"/api/hosts/"+hostID+"/sftp/download?path="+url.QueryEscape("subdir/upload.txt"),
		nil)
	dlReq.Header.Set("Cookie", cookie)
	dlResp, err := http.DefaultClient.Do(dlReq)
	if err != nil {
		t.Fatal(err)
	}
	defer dlResp.Body.Close()
	body, _ := io.ReadAll(dlResp.Body)
	if dlResp.StatusCode != 200 || string(body) != "uploaded data" {
		t.Fatalf("download mismatch: %d %q", dlResp.StatusCode, string(body))
	}

	// 10. 删除目录（递归）
	code, _ = sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostID+"/sftp/remove", cookie,
		map[string]string{"path": "subdir"})
	if code != 200 {
		t.Fatalf("remove failed: %d", code)
	}
	code, entries = sftpList(t, ts.URL, cookie, hostID, ".")
	if code != 200 {
		t.Fatalf("list after remove failed: %d", code)
	}
	for _, e := range entries {
		if e["name"] == "subdir" {
			t.Fatal("subdir should be removed")
		}
	}
}

// TestSftpPaste 覆盖文件/目录的复制粘贴：
// 同服务器复制、跨服务器（中转）复制/剪切、目录递归复制。
func TestSftpPaste(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)
	if cookie == "" {
		t.Fatal("no cookie")
	}

	// 两台独立主机（各自独立临时目录，模拟不同服务器）
	create := func(name string) string {
		host, portStr := startTestSSHServer(t)
		port, _ := strconv.Atoi(portStr)
		code, resp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
			"name": name, "host": host, "port": port,
			"username": testUser, "auth_type": "password", "password": testPassword,
		})
		if code != 201 {
			t.Fatalf("create host %s failed: %d", name, code)
		}
		return resp["id"].(string)
	}
	hostA := create("host-a")
	hostB := create("host-b")

	// 准备源数据：hostA 下 src/hello.txt
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostA+"/sftp/mkdir", cookie, map[string]string{"path": "src"}); code != 200 {
		t.Fatalf("mkdir src failed: %d", code)
	}
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostA+"/sftp/write", cookie,
		map[string]any{"path": "src/hello.txt", "content": "hello paste"}); code != 200 {
		t.Fatalf("write failed: %d", code)
	}

	// 1. 同服务器复制：hostA src/hello.txt → hostA .（local, copy）
	code, last, _ := doPaste(t, ts.URL, cookie, hostA, map[string]any{
		"src_host_id": hostA, "src_path": "src/hello.txt", "dst_dir": ".",
		"mode": "copy", "transport": "local",
	})
	if code != 200 || last["ok"] != "true" {
		t.Fatalf("local copy failed: %d %v", code, last)
	}
	code, readResp := sftpReq(t, "GET", ts.URL+"/api/hosts/"+hostA+"/sftp/read?path="+url.QueryEscape("hello.txt"), cookie, nil)
	if code != 200 || readResp["content"] != "hello paste" {
		t.Fatalf("local copy content mismatch: %d %v", code, readResp)
	}

	// 2. 跨服务器中转复制（大文件）：hostA src/big.bin → hostB .（relay, copy），应产生多个进度事件
	big := strings.Repeat("x", 2<<20) // 2MB
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostA+"/sftp/write", cookie,
		map[string]any{"path": "src/big.bin", "content": big}); code != 200 {
		t.Fatalf("write big failed: %d", code)
	}
	code, last, events := doPaste(t, ts.URL, cookie, hostB, map[string]any{
		"src_host_id": hostA, "src_path": "src/big.bin", "dst_dir": ".",
		"mode": "copy", "transport": "relay",
	})
	if code != 200 || last["ok"] != "true" {
		t.Fatalf("relay copy failed: %d %v", code, last)
	}
	if events < 2 {
		t.Fatalf("expected progress events for big file relay, got %d", events)
	}
	// 大文件用 download 接口回读校验（read 接口限制 1MB）
	dlReq, _ := http.NewRequest("GET", ts.URL+"/api/hosts/"+hostB+"/sftp/download?path="+url.QueryEscape("big.bin"), nil)
	dlReq.Header.Set("Cookie", cookie)
	dlResp, err := http.DefaultClient.Do(dlReq)
	if err != nil {
		t.Fatal(err)
	}
	dlBody, _ := io.ReadAll(dlResp.Body)
	dlResp.Body.Close()
	if dlResp.StatusCode != 200 || string(dlBody) != big {
		t.Fatalf("relay copy content mismatch: %d len=%d", dlResp.StatusCode, len(dlBody))
	}

	// 3. 跨服务器中转剪切：hostA src/hello.txt → hostB dst/，源应被删除（relay, move）
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostB+"/sftp/mkdir", cookie, map[string]string{"path": "dst"}); code != 200 {
		t.Fatalf("mkdir dst failed: %d", code)
	}
	code, last, _ = doPaste(t, ts.URL, cookie, hostB, map[string]any{
		"src_host_id": hostA, "src_path": "src/hello.txt", "dst_dir": "dst",
		"mode": "move", "transport": "relay",
	})
	if code != 200 || last["ok"] != "true" {
		t.Fatalf("relay move failed: %d %v", code, last)
	}
	code, readResp = sftpReq(t, "GET", ts.URL+"/api/hosts/"+hostB+"/sftp/read?path="+url.QueryEscape("dst/hello.txt"), cookie, nil)
	if code != 200 || readResp["content"] != "hello paste" {
		t.Fatalf("relay move dest mismatch: %d %v", code, readResp)
	}
	if code, _ := sftpReq(t, "GET", ts.URL+"/api/hosts/"+hostA+"/sftp/read?path="+url.QueryEscape("src/hello.txt"), cookie, nil); code != 400 {
		t.Fatalf("source should be removed after move, got %d", code)
	}

	// 4. 目录递归中转复制：hostA tree/ → hostB .（relay, copy）
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostA+"/sftp/mkdir", cookie, map[string]string{"path": "tree"}); code != 200 {
		t.Fatalf("mkdir tree failed: %d", code)
	}
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostA+"/sftp/write", cookie,
		map[string]any{"path": "tree/a.txt", "content": "A"}); code != 200 {
		t.Fatalf("write a.txt failed: %d", code)
	}
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostA+"/sftp/mkdir", cookie, map[string]string{"path": "tree/sub"}); code != 200 {
		t.Fatalf("mkdir tree/sub failed: %d", code)
	}
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostA+"/sftp/write", cookie,
		map[string]any{"path": "tree/sub/b.txt", "content": "B"}); code != 200 {
		t.Fatalf("write b.txt failed: %d", code)
	}
	code, last, _ = doPaste(t, ts.URL, cookie, hostB, map[string]any{
		"src_host_id": hostA, "src_path": "tree", "dst_dir": ".",
		"mode": "copy", "transport": "relay",
	})
	if code != 200 || last["ok"] != "true" {
		t.Fatalf("relay dir copy failed: %d %v", code, last)
	}
	code, readResp = sftpReq(t, "GET", ts.URL+"/api/hosts/"+hostB+"/sftp/read?path="+url.QueryEscape("tree/sub/b.txt"), cookie, nil)
	if code != 200 || readResp["content"] != "B" {
		t.Fatalf("relay dir copy content mismatch: %d %v", code, readResp)
	}

	// 5. 同服务器剪切到子目录（local, move）：移动 hostB 上第 4 步复制来的 tree/a.txt
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostB+"/sftp/mkdir", cookie, map[string]string{"path": "move-to"}); code != 200 {
		t.Fatalf("mkdir move-to failed: %d", code)
	}
	code, last, _ = doPaste(t, ts.URL, cookie, hostB, map[string]any{
		"src_host_id": hostB, "src_path": "tree/a.txt", "dst_dir": "move-to",
		"mode": "move", "transport": "local",
	})
	if code != 200 || last["ok"] != "true" {
		t.Fatalf("local move failed: %d %v", code, last)
	}
	code, readResp = sftpReq(t, "GET", ts.URL+"/api/hosts/"+hostB+"/sftp/read?path="+url.QueryEscape("move-to/a.txt"), cookie, nil)
	if code != 200 || readResp["content"] != "A" {
		t.Fatalf("local move dest mismatch: %d %v", code, readResp)
	}
}

// TestSftpDirDownload 验证「下载文件夹」：目录被递归打包为 tar.gz 流式下载，
// 符号链接保留、子目录与文件内容正确。
func TestSftpDirDownload(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)
	if cookie == "" {
		t.Fatal("no cookie")
	}

	host, portStr := startTestSSHServer(t)
	port, _ := strconv.Atoi(portStr)
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "dl-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
	})
	if code != 201 {
		t.Fatalf("create host failed: %d", code)
	}
	hostID := hostResp["id"].(string)

	// 准备目录结构：downloads/a.txt、downloads/sub/b.txt
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostID+"/sftp/mkdir", cookie, map[string]string{"path": "downloads"}); code != 200 {
		t.Fatalf("mkdir downloads failed: %d", code)
	}
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostID+"/sftp/write", cookie,
		map[string]any{"path": "downloads/a.txt", "content": "AAA"}); code != 200 {
		t.Fatalf("write a.txt failed: %d", code)
	}
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostID+"/sftp/mkdir", cookie, map[string]string{"path": "downloads/sub"}); code != 200 {
		t.Fatalf("mkdir sub failed: %d", code)
	}
	if code, _ := sftpReq(t, "POST", ts.URL+"/api/hosts/"+hostID+"/sftp/write", cookie,
		map[string]any{"path": "downloads/sub/b.txt", "content": "BBB"}); code != 200 {
		t.Fatalf("write b.txt failed: %d", code)
	}

	req, _ := http.NewRequest("GET",
		ts.URL+"/api/hosts/"+hostID+"/sftp/download?path="+url.QueryEscape("downloads"), nil)
	req.Header.Set("Cookie", cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("download dir failed: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/gzip" {
		t.Fatalf("unexpected content-type: %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "downloads.tar.gz") {
		t.Fatalf("unexpected content-disposition: %q", cd)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	contents := map[string]string{}
	var types = map[string]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read failed: %v", err)
		}
		types[hdr.Name] = hdr.Typeflag
		if hdr.Typeflag == tar.TypeReg {
			buf := make([]byte, hdr.Size)
			if _, err := io.ReadFull(tr, buf); err != nil {
				t.Fatalf("read %s failed: %v", hdr.Name, err)
			}
			contents[hdr.Name] = string(buf)
		}
	}

	if types["downloads/"] != tar.TypeDir {
		t.Fatalf("missing dir entry downloads/: %v", types)
	}
	if types["downloads/sub/"] != tar.TypeDir {
		t.Fatalf("missing dir entry downloads/sub/: %v", types)
	}
	if contents["downloads/a.txt"] != "AAA" {
		t.Fatalf("a.txt mismatch: %v", contents["downloads/a.txt"])
	}
	if contents["downloads/sub/b.txt"] != "BBB" {
		t.Fatalf("b.txt mismatch: %v", contents["downloads/sub/b.txt"])
	}
}

func uploadMultipart(t *testing.T, base, cookie, hostID, p, content string) int {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", path.Base(p))
	_, _ = fw.Write([]byte(content))
	_ = mw.Close()

	req, _ := http.NewRequest("POST",
		base+"/api/hosts/"+hostID+"/sftp/upload?path="+url.QueryEscape(p),
		&buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Cookie", cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}
