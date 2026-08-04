package api

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	pkgSftp "github.com/pkg/sftp"

	"ezssh/internal/auth"
	"ezssh/internal/sshhub"
	"ezssh/internal/store"
	"ezssh/internal/vault"
)

const (
	testUser     = "tester"
	testPassword = "secret-pass-123"
)

// startTestSSHServer 启动一个本地 SSH 服务器作为目标主机。
func startTestSSHServer(t *testing.T) (host, port string) {
	t.Helper()
	return startTestSSHServerUsers(t, map[string]string{
		testUser: testPassword,
	})
}

// startTestSSHServerUsers 启动一个接受多组「用户名:密码」的本地 SSH 服务器，
// 用于验证编辑主机凭据后能基于新凭据重连。
func startTestSSHServerUsers(t *testing.T, users map[string]string) (host, port string) {
	t.Helper()

	_, signer, err := makeSSHKey()
	if err != nil {
		t.Fatalf("make key: %v", err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if want, ok := users[c.User()]; ok && string(pass) == want {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	// SFTP 根目录（pkg/sftp 服务器在临时目录上操作）
	sftpRoot := t.TempDir()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSSHConn(conn, cfg, sftpRoot)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", fmt.Sprintf("%d", addr.Port)
}

func makeSSHKey() (ssh.PublicKey, ssh.Signer, error) {
	priv, err := generateED25519()
	if err != nil {
		return nil, nil, err
	}
	signer, err := ssh.ParsePrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	return signer.PublicKey(), signer, nil
}

func generateED25519() ([]byte, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "ezssh-test")
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(block), nil
}

func handleSSHConn(conn net.Conn, cfg *ssh.ServerConfig, sftpRoot string) {
	sConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		conn.Close()
		return
	}
	defer sConn.Close()
	go ssh.DiscardRequests(reqs)

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		ch, chReqs, err := newChan.Accept()
		if err != nil {
			continue
		}
		go handleSessionChannel(ch, chReqs, sftpRoot)
	}
}

func handleSessionChannel(ch ssh.Channel, reqs <-chan *ssh.Request, sftpRoot string) {
	defer ch.Close()
	shellStarted := false
	for req := range reqs {
		switch req.Type {
		case "subsystem":
			// 支持 sftp 子系统
			if string(req.Payload[4:]) == "sftp" {
				req.Reply(true, nil)
				go func() {
					server, err := pkgSftp.NewServer(
						ch,
						pkgSftp.WithServerWorkingDirectory(sftpRoot),
					)
					if err != nil {
						ch.Close()
						return
					}
					_ = server.Serve()
				}()
				continue
			}
			req.Reply(false, nil)
		case "pty-req":
			// 终端尺寸：payload 前 4 字节 + term 字符串后 8 字节为 cols/rows（大端）
			req.Reply(true, nil)
		case "shell":
			req.Reply(true, nil)
			if shellStarted {
				continue
			}
			shellStarted = true
			_, _ = ch.Write([]byte("test-shell>\n"))
			// 简单回显，供 shell 场景使用
			go func() {
				buf := make([]byte, 1024)
				for {
					n, err := ch.Read(buf)
					if err != nil {
						return
					}
					if n > 0 {
						_, _ = ch.Write(append([]byte("echo: "), buf[:n]...))
					}
				}
			}()
		case "exec":
			payload := strings.TrimSpace(string(req.Payload[4:])) // skip command length
			out, status := execReply(payload, sftpRoot)
			_, _ = ch.Write([]byte(out))
			_, _ = ch.SendRequest("exit-status", false, []byte{
				byte(status >> 24), byte(status >> 16), byte(status >> 8), byte(status),
			})
			req.Reply(true, nil)
			return
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

// newTestServer 构建完整后端并返回 httptest 服务器与临时 DB 路径。
// newTestServerWithStore 同 newTestServer 但额外返回 Store，便于测试播种内置主机与断言状态。
func newTestServerWithStore(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	v := vault.New()
	am := auth.NewManager()
	hub := sshhub.New(st, v)
	t.Cleanup(hub.CloseAll)

	srv := New(st, v, am, hub)
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	return ts, st
}

func newTestServer(t *testing.T) *httptest.Server {
	ts, _ := newTestServerWithStore(t)
	return ts
}

// seedRemoteFile 用测试账号向 mock SSH 服务器的 SFTP 工作目录写入原始字节
// （相对路径解析到 mock 工作目录）。用于播种 GBK/UTF-16/zip 等无法经 JSON 字符串传的二进制内容。
// 注意：不调用 pkg/sftp 的 client.Close() —— 其 Close() 可能因 recv goroutine
// 阻塞而挂起（SFTPManager.CloseAll 为此加了超时保护），直接关闭整个 SSH 连接即可，
// 通道随之断开，所有 goroutine 自动退出。
func seedRemoteFile(t *testing.T, host, port, relPath string, data []byte) {
	t.Helper()
	cfg := &ssh.ClientConfig{
		User:            testUser,
		Auth:            []ssh.AuthMethod{ssh.Password(testPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	conn, err := ssh.Dial("tcp", net.JoinHostPort(host, port), cfg)
	if err != nil {
		t.Fatalf("seed dial: %v", err)
	}
	defer conn.Close()
	client, err := pkgSftp.NewClient(conn)
	if err != nil {
		t.Fatalf("seed sftp: %v", err)
	}
	if dir := filepath.Dir(relPath); dir != "." && dir != "/" {
		if err := client.MkdirAll(dir); err != nil {
			t.Fatalf("seed mkdir: %v", err)
		}
	}
	f, err := client.Create(relPath)
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		t.Fatalf("seed write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}
}

// mockExecOverrides 让测试按整条命令覆盖 mock SSH server 的 exec 响应。
// 值 "\x00FAIL" 表示命令执行失败（非零退出码）；其余值作为 stdout 原样返回。
var mockExecOverrides = map[string]string{}

// execReply 返回测试服务器对常见命令的模拟输出与退出码。
// sftpRoot 为 mock SFTP 工作目录，用于真实模拟 cp -a 的同机复制（其产物需可被
// 后续 stat/read 校验）。
func execReply(payload, sftpRoot string) (string, uint32) {
	if v, ok := mockExecOverrides[payload]; ok {
		if v == "\x00FAIL" {
			return "command not found\n", 1
		}
		return v, 0
	}
	switch {
	case strings.HasPrefix(payload, "cp -a "):
		// 模拟远端 cp -a：在 mock SFTP 目录里真实复制，数据落盘供后续校验。
		simulateRemoteCopy(payload, sftpRoot)
		return "", 0
	case strings.Contains(payload, "uname -a"):
		return "Linux test-host 6.1.0 x86_64 GNU/Linux\n", 0
	case strings.HasPrefix(payload, "cat /proc/stat"):
		return "cpu  100 0 50 800 10 0 5 0 0 0\ncpu0 50 0 25 400 5 0 2 0 0 0\n" +
			"===\nMemTotal:       2000000 kB\nMemFree:        500000 kB\nMemAvailable:   1200000 kB\nBuffers:         100000 kB\nCached:          400000 kB\nSwapTotal:      1000000 kB\nSwapFree:        800000 kB\n" +
			"===\n0.05 0.10 0.15 1/2 345\n" +
			"===\n/dev/vda1  20000 8000 12000  40% /\ntmpfs  1000 10 990  1% /run\n" +
			"===\neth0: 1000000 0 0 0 0 0 0 0 200000 0 0 0 0 0 0 0\nlo: 500 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0\n", 0
	case strings.Contains(payload, "setsid sh '/tmp/ezssh_bg_") && strings.Contains(payload, "echo $!"):
		// 后台任务分离式启动：回显远端 PID
		return "12345\n", 0
	case strings.Contains(payload, "unzip -o ") || strings.Contains(payload, "tar -xf "):
		// 文件解压：在 mock 工作目录真实解压，供后续 sftpList 校验
		simulateExtract(payload, sftpRoot)
		return "", 0
	case strings.HasPrefix(payload, "grep MemTotal /proc/meminfo"):
		// 进程采集命令（ps 基础信息 + awk /proc stat 时间）
		return "MemTotal:       2000000 kB\n" +
			"  1   0 root      1000   2000 /sbin/init\n" +
			" 42   1 root      3000   4000 /usr/sbin/sshd\n" +
			" 99   1 testuser  2000   3000 nginx: master process\n" +
			"===T\n" +
			"T\t1\t1000\t500\t600000\n" +
			"T\t42\t2000\t1000\t610000\n" +
			"T\t99\t3000\t1500\t620000\n" +
			"===U\n" +
			"UPTIME\t123456.50\n", 0
	case strings.Contains(payload, "docker ps"):
		return `{"Command":"/sbin/init","CreatedAt":"2026-06-01 00:00:00 +0000 UTC","ID":"abc123def456","Image":"nginx:latest","Names":"web","Ports":"0.0.0.0:80->80/tcp","RunningFor":"2 weeks","State":"running","Status":"Up 2 weeks"}` + "\n" +
			`{"Command":"bash","CreatedAt":"2026-06-02 00:00:00 +0000 UTC","ID":"def456abc789","Image":"redis:7","Names":"cache","Ports":"","RunningFor":"1 week","State":"exited","Status":"Exited (0) 3 days ago"}` + "\n", 0
	case strings.Contains(payload, "docker stats"):
		return `{"BlockIO":"10MB / 20MB","CPUPerc":"0.5%","Container":"abc123def456...","ID":"abc123def456","MemPerc":"1.2%","MemUsage":"10MiB / 1GiB","Name":"web","NetIO":"1MB / 2MB","PIDs":"5"}` + "\n", 0
	case strings.Contains(payload, "docker images"):
		return `{"Containers":"1","CreatedAt":"2026-06-01 00:00:00 +0000 UTC","ID":"img123","Repository":"nginx","Size":"50MB","Tag":"latest"}` + "\n" +
			`{"Containers":"0","CreatedAt":"2026-06-02 00:00:00 +0000 UTC","ID":"img456","Repository":"redis","Size":"40MB","Tag":"7"}` + "\n", 0
	case strings.Contains(payload, "docker logs"):
		return "started server\nlistening on :80\n", 0
	default:
		return "Hello from EZSSH test server. Command: " + payload + "\n", 0
	}
}

// simulateRemoteCopy 在 mock 的 SFTP 工作目录（sftpRoot）上模拟远端 cp -a 语义，
// 使同机 localCopy 产生的文件真实落盘，供后续 stat/read 校验。
// 支持两类命令：文件 `cp -a '<src>' '<dst>'` 与目录合并 `mkdir -p <dst> && cp -a '<src>/.' '<dst>/'`。
func simulateRemoteCopy(payload, sftpRoot string) {
	body := payload
	if i := strings.Index(body, "cp -a "); i >= 0 {
		body = body[i+len("cp -a "):]
	}
	// 参数由 sshQuote 单引号包裹，按 ' 切分取奇数索引即为各参数内容
	parts := strings.Split(body, "'")
	var args []string
	for i := 1; i < len(parts); i += 2 {
		if strings.TrimSpace(parts[i]) != "" {
			args = append(args, parts[i])
		}
	}
	if len(args) < 2 {
		return
	}
	src, dst := args[0], args[1]
	merge := strings.HasSuffix(dst, "/") || strings.HasSuffix(src, "/.")
	src = strings.TrimSuffix(strings.TrimSuffix(src, "/."), "/")
	dst = strings.TrimSuffix(dst, "/")
	if merge {
		// 合并复制：dst 目录内逐个复制 src 下的条目（≈ cp -a src/. dst/）
		srcAbs := filepath.Join(sftpRoot, filepath.FromSlash(src))
		dstAbs := filepath.Join(sftpRoot, filepath.FromSlash(dst))
		entries, err := os.ReadDir(srcAbs)
		if err != nil {
			return
		}
		if err := os.MkdirAll(dstAbs, 0o755); err != nil {
			return
		}
		for _, e := range entries {
			copyMockPath(filepath.Join(srcAbs, e.Name()), filepath.Join(dstAbs, e.Name()))
		}
		return
	}
	copyMockPath(filepath.Join(sftpRoot, filepath.FromSlash(src)), filepath.Join(sftpRoot, filepath.FromSlash(dst)))
}

// copyMockPath 递归复制 mock 文件系统内的一个文件/目录到目标路径。
func copyMockPath(src, dst string) {
	fi, err := os.Stat(src)
	if err != nil {
		return
	}
	if fi.IsDir() {
		if err := os.MkdirAll(dst, fi.Mode().Perm()); err != nil {
			return
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return
		}
		for _, e := range entries {
			copyMockPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
		}
		return
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(dst, data, fi.Mode().Perm())
}

func doJSON(t *testing.T, method, url, cookie string, body any) (int, map[string]any, []*http.Cookie) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out, resp.Cookies()
}

func cookieOf(cs []*http.Cookie) string {
	for _, c := range cs {
		if c.Name == sessionCookie {
			return c.Name + "=" + c.Value
		}
	}
	return ""
}

func listHosts(t *testing.T, base, cookie string) (int, []map[string]any) {
	t.Helper()
	req, err := http.NewRequest("GET", base+"/api/hosts", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Cookie", cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list hosts: %v", err)
	}
	defer resp.Body.Close()
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestE2E 全链路：初始化 → 登录 → 新增主机 → 建连 → 执行命令。
func TestE2E(t *testing.T) {
	ts := newTestServer(t)

	// 0. 初始状态未初始化
	code, initStatus, _ := doJSON(t, "GET", ts.URL+"/api/init-status", "", nil)
	if code != 200 || initStatus["initialized"] != false {
		t.Fatalf("expected uninitialized, got %d %v", code, initStatus)
	}

	// 1. 初始化
	code, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	if code != 200 {
		t.Fatalf("init failed: %d", code)
	}
	cookie := cookieOf(cookies)
	if cookie == "" {
		t.Fatal("no session cookie after init")
	}

	// 2. 重复初始化应拒绝
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	if code != 400 {
		t.Fatalf("expected 400 for re-init, got %d", code)
	}

	// 3. 启动本地 SSH 目标机
	host, portStr := startTestSSHServer(t)
	port, _ := strconv.Atoi(portStr)

	// 4. 新增主机（密码认证）
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "test-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
		"group_name": "dev", "remark": "e2e",
	})
	if code != 201 {
		t.Fatalf("create host failed: %d %v", code, hostResp)
	}
	hostID, _ := hostResp["id"].(string)
	if hostID == "" {
		t.Fatal("no host id returned")
	}

	// 5. 主机列表（裸数组）
	code, hosts := listHosts(t, ts.URL, cookie)
	if code != 200 {
		t.Fatalf("list hosts failed: %d", code)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0]["id"] != hostID {
		t.Fatalf("unexpected host id %v", hosts[0]["id"])
	}

	// 6. 建连预热
	code, connResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts/"+hostID+"/connect", cookie, nil)
	if code != 200 {
		t.Fatalf("connect failed: %d %v", code, connResp)
	}
	if fp, _ := connResp["fingerprint"].(string); fp == "" {
		t.Fatal("no fingerprint returned")
	}

	// 7. 连接状态
	code, statusResp, _ := doJSON(t, "GET", ts.URL+"/api/hosts/"+hostID+"/status", cookie, nil)
	if code != 200 || statusResp["connected"] != true {
		t.Fatalf("status not connected: %d %v", code, statusResp)
	}

	// 8. 执行 uname -a
	code, execResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts/"+hostID+"/exec", cookie, map[string]string{
		"command": "uname -a",
	})
	if code != 200 {
		t.Fatalf("exec failed: %d %v", code, execResp)
	}
	if !strings.Contains(execResp["output"].(string), "Linux") {
		t.Fatalf("unexpected output: %v", execResp["output"])
	}

	// 9. 无凭证访问主机详情应无 credential 泄露
	if _, ok := hostResp["credential"]; ok {
		t.Fatal("credential leaked in host response")
	}

	// 10. 登出后访问需认证接口应 401
	doJSON(t, "POST", ts.URL+"/api/logout", cookie, nil)
	code, _, _ = doJSON(t, "GET", ts.URL+"/api/hosts", cookie, nil)
	if code != 401 {
		t.Fatalf("expected 401 after logout, got %d", code)
	}

	// 11. 错误口令登录应 401 且触发限流计数
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/login", "", map[string]string{
		"username": "admin", "password": "wrong-pass",
	})
	if code != 401 {
		t.Fatalf("expected 401 for bad login, got %d", code)
	}
}
