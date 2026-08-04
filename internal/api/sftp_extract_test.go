package api

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestSftpExtractZip 覆盖 zip 就地解压与不支持格式 400。
func TestSftpExtractZip(t *testing.T) {
	ts, cookie := newAuthedTest(t)
	host, portStr := startTestSSHServer(t)
	port, _ := strconv.Atoi(portStr)
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "extract-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
	})
	if code != 201 {
		t.Fatalf("create host: %d", code)
	}
	hostID, _ := hostResp["id"].(string)

	// 内存构造 zip（含一个文件 hello.txt）
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, _ := zw.Create("hello.txt")
	_, _ = fw.Write([]byte("hello extract"))
	_ = zw.Close()
	seedRemoteFile(t, host, portStr, "x.zip", buf.Bytes())

	// 解压
	code, resp, _ := doJSON(t, "POST", ts.URL+"/api/hosts/"+hostID+"/sftp/extract", cookie, map[string]string{"path": "x.zip"})
	if code != 200 {
		t.Fatalf("extract zip: %d %v", code, resp)
	}

	// 列表校验解压产物（mock 走相对路径解析到 sftpRoot，同 TestSFTPFlow 约定）
	code, list := doJSONArr(t, "GET", ts.URL+"/api/hosts/"+hostID+"/sftp/list?path="+url.QueryEscape("."), cookie, nil)
	if code != 200 {
		t.Fatalf("list: %d", code)
	}
	if !nameInList(list, "hello.txt") {
		t.Fatalf("extracted file missing: %v", list)
	}

	// tar.gz 解压
	var tbuf bytes.Buffer
	tw := tar.NewWriter(&tbuf)
	hdr := &tar.Header{Name: "sub/readme.md", Mode: 0o644, Size: int64(len("hi")), Typeflag: tar.TypeReg}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte("hi"))
	_ = tw.Close()
	var gz bytes.Buffer
	gzw := gzip.NewWriter(&gz)
	_, _ = gzw.Write(tbuf.Bytes())
	_ = gzw.Close()
	seedRemoteFile(t, host, portStr, "b.tar.gz", gz.Bytes())
	code, resp, _ = doJSON(t, "POST", ts.URL+"/api/hosts/"+hostID+"/sftp/extract", cookie, map[string]string{"path": "b.tar.gz"})
	if code != 200 {
		t.Fatalf("extract tar.gz: %d %v", code, resp)
	}
	code, list = doJSONArr(t, "GET", ts.URL+"/api/hosts/"+hostID+"/sftp/list?path="+url.QueryEscape("sub"), cookie, nil)
	if code != 200 || !nameInList(list, "readme.md") {
		t.Fatalf("tar.gz result: %d %v", code, list)
	}

	// 不支持格式 → 400
	seedRemoteFile(t, host, portStr, "a.rar", []byte("x"))
	code, resp, _ = doJSON(t, "POST", ts.URL+"/api/hosts/"+hostID+"/sftp/extract", cookie, map[string]string{"path": "a.rar"})
	if code != 400 {
		t.Fatalf("rar should 400: %d %v", code, resp)
	}
}

func nameInList(list []map[string]any, name string) bool {
	for _, e := range list {
		if e["name"] == name {
			return true
		}
	}
	return false
}

// simulateExtract 在 mock 的 SFTP 工作目录（sftpRoot）上模拟远端 unzip/tar 解压，
// 供 execReply 对解压命令做真实落盘，后续 sftpList 可校验产物。
func simulateExtract(payload, sftpRoot string) {
	var archive, dest string
	if strings.Contains(payload, "unzip -o ") {
		archive = quoteArg(payload, "unzip -o ")
		dest = quoteArg(payload, " -d ")
	} else {
		archive = quoteArg(payload, "tar -xf ")
		dest = quoteArg(payload, " -C ")
	}
	if archive == "" || dest == "" {
		return
	}
	base := filepath.Join(sftpRoot, filepath.FromSlash(strings.TrimPrefix(archive, "/")))
	to := filepath.Join(sftpRoot, filepath.FromSlash(strings.TrimPrefix(dest, "/")))
	if strings.HasSuffix(strings.ToLower(archive), ".zip") {
		extractZipMock(base, to)
	} else {
		extractTarMock(base, to)
	}
}

// quoteArg 从命令串中取 '<marker>'<quote>path</quote> 的路径（mock 解析用）。
func quoteArg(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	rest := s[i+len(marker):]
	if !strings.HasPrefix(rest, "'") {
		return ""
	}
	rest = rest[1:]
	if j := strings.Index(rest, "'"); j >= 0 {
		return rest[:j]
	}
	return ""
}

func safeJoin(dest, name string) string {
	clean := filepath.Clean(dest)
	target := filepath.Join(clean, name)
	if !strings.HasPrefix(target, clean+string(os.PathSeparator)) && target != clean {
		return ""
	}
	return target
}

func extractZipMock(zipPath, dest string) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return
	}
	defer r.Close()
	_ = os.MkdirAll(dest, 0o755)
	for _, f := range r.File {
		target := safeJoin(dest, f.Name)
		if target == "" {
			continue
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0o755)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		src, err := f.Open()
		if err != nil {
			continue
		}
		out, err := os.Create(target)
		if err != nil {
			src.Close()
			continue
		}
		_, _ = io.Copy(out, src)
		src.Close()
		out.Close()
	}
}

func extractTarMock(tarPath, dest string) {
	f, err := os.Open(tarPath)
	if err != nil {
		return
	}
	defer f.Close()
	_ = os.MkdirAll(dest, 0o755)
	var rd io.Reader = f
	if strings.HasSuffix(strings.ToLower(tarPath), ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return
		}
		defer gz.Close()
		rd = gz
	}
	tr := tar.NewReader(rd)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}
		target := safeJoin(dest, hdr.Name)
		if target == "" {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			out, err := os.Create(target)
			if err != nil {
				continue
			}
			_, _ = io.Copy(out, tr)
			out.Close()
		}
	}
}
