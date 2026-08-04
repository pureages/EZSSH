package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// TestPlatformProbeWindows 验证：uname -s 失败 → 裸 ver 命中 → platform=windows，
// 且探测结果持久化（GET /api/hosts 与 connect 响应均带 platform）。
func TestPlatformProbeWindows(t *testing.T) {
	// 复位 overrides（测试可能并行执行；此处单测，直接清空）
	mockExecOverrides = map[string]string{}
	mockExecOverrides["uname -s"] = "\x00FAIL"
	mockExecOverrides["ver"] = "Microsoft Windows [Version 10.0.22631]\n"

	ts, cookie, hostID := setupHostAndConnect(t)
	defer ts.Close()

	// connect 响应应带 platform=windows
	code, connResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts/"+hostID+"/connect", cookie, nil)
	if code != http.StatusOK {
		t.Fatalf("connect failed: %d %v", code, connResp)
	}
	if p, _ := connResp["platform"].(string); p != "windows" {
		t.Fatalf("expected platform windows in connect response, got %q", connResp["platform"])
	}

	// host 列表应持久化 platform=windows
	code, hosts := listHosts(t, ts.URL, cookie)
	if code != http.StatusOK || len(hosts) != 1 {
		t.Fatalf("list hosts failed: %d %v", code, hosts)
	}
	if p, _ := hosts[0]["platform"].(string); p != "windows" {
		t.Fatalf("expected persisted platform windows, got %q", hosts[0]["platform"])
	}
}

// TestPlatformProbeLinux 验证：uname -s 成功且非 windows 输出 → platform=linux 并持久化。
func TestPlatformProbeLinux(t *testing.T) {
	mockExecOverrides = map[string]string{}

	ts, cookie, hostID := setupHostAndConnect(t)
	defer ts.Close()

	code, connResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts/"+hostID+"/connect", cookie, nil)
	if code != http.StatusOK {
		t.Fatalf("connect failed: %d %v", code, connResp)
	}
	if p, _ := connResp["platform"].(string); p != "linux" {
		t.Fatalf("expected platform linux in connect response, got %q", connResp["platform"])
	}

	code, hosts := listHosts(t, ts.URL, cookie)
	if code != http.StatusOK || len(hosts) != 1 {
		t.Fatalf("list hosts failed: %d %v", code, hosts)
	}
	if p, _ := hosts[0]["platform"].(string); p != "linux" {
		t.Fatalf("expected persisted platform linux, got %q", hosts[0]["platform"])
	}
}

// TestPlatformExplicitNotOverridden 验证：用户显式指定 platform 时不再探测覆盖。
func TestPlatformExplicitNotOverridden(t *testing.T) {
	mockExecOverrides = map[string]string{}
	mockExecOverrides["uname -s"] = "Linux test-host 6.1.0 x86_64 GNU/Linux\n"

	ts := newTestServer(t)
	code, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	if code != http.StatusOK {
		t.Fatalf("init failed: %d", code)
	}
	cookie := cookieOf(cookies)

	host, portStr := startTestSSHServer(t)
	port, _ := strconv.Atoi(portStr)
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "win-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
		"platform": "windows",
	})
	if code != http.StatusCreated {
		t.Fatalf("create host failed: %d %v", code, hostResp)
	}
	hostID, _ := hostResp["id"].(string)

	code, connResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts/"+hostID+"/connect", cookie, nil)
	if code != http.StatusOK {
		t.Fatalf("connect failed: %d %v", code, connResp)
	}
	if p, _ := connResp["platform"].(string); p != "windows" {
		t.Fatalf("expected explicit platform windows preserved, got %q", connResp["platform"])
	}
}

// setupHostAndConnect 初始化后端 + 启动 mock SSH + 创建主机（不建连，由调用方 connect 触发探测）。
func setupHostAndConnect(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	ts := newTestServer(t)

	code, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	if code != http.StatusOK {
		t.Fatalf("init failed: %d", code)
	}
	cookie := cookieOf(cookies)
	if cookie == "" {
		t.Fatal("no session cookie after init")
	}

	host, portStr := startTestSSHServer(t)
	port, _ := strconv.Atoi(portStr)
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "probe-host", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
	})
	if code != http.StatusCreated {
		t.Fatalf("create host failed: %d %v", code, hostResp)
	}
	hostID, _ := hostResp["id"].(string)
	if hostID == "" {
		t.Fatal("no host id returned")
	}
	return ts, cookie, hostID
}
