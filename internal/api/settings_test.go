package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// 从响应中读取验证码字段
func getCaptcha(t *testing.T, ts *httptestServer) (string, string) {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/captcha")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		ID  string `json:"id"`
		SVG string `json:"svg"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.ID == "" || out.SVG == "" {
		t.Fatal("captcha empty")
	}
	if !strings.Contains(out.SVG, "<svg") {
		t.Fatal("captcha not svg")
	}
	return out.ID, out.SVG
}

func TestLoginRouteSetting(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)

	// 读取设置默认
	code, settings, _ := doJSON(t, "GET", ts.URL+"/api/settings", cookie, nil)
	if code != 200 || settings["login_route"] != "/login" {
		t.Fatalf("default settings: %d %v", code, settings)
	}

	// 修改登录路由
	code, settings, _ = doJSON(t, "PUT", ts.URL+"/api/settings", cookie, map[string]string{
		"login_route": "/secret-admin",
	})
	if code != 200 || settings["login_route"] != "/secret-admin" {
		t.Fatalf("update settings: %d %v", code, settings)
	}

	// init-status 应返回新路由
	code, initStatus, _ := doJSON(t, "GET", ts.URL+"/api/init-status", "", nil)
	if code != 200 || initStatus["login_route"] != "/secret-admin" {
		t.Fatalf("init-status login_route: %d %v", code, initStatus)
	}

	// 非法路由应拒绝
	code, _, _ = doJSON(t, "PUT", ts.URL+"/api/settings", cookie, map[string]string{
		"login_route": "no-slash",
	})
	if code != 400 {
		t.Fatalf("invalid route should be 400, got %d", code)
	}
}

func TestLangSetting(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)

	// 默认语言 en
	code, settings, _ := doJSON(t, "GET", ts.URL+"/api/settings", cookie, nil)
	if code != 200 || settings["lang"] != "en" {
		t.Fatalf("default lang: %d %v", code, settings)
	}

	// 切换为英文
	code, settings, _ = doJSON(t, "PUT", ts.URL+"/api/settings", cookie, map[string]string{
		"lang": "en",
	})
	if code != 200 || settings["lang"] != "en" {
		t.Fatalf("update lang: %d %v", code, settings)
	}
	if settings["login_route"] != "/login" {
		t.Fatalf("login_route should be preserved: %v", settings)
	}

	// init-status 应返回新语言（登录前即可读）
	code, initStatus, _ := doJSON(t, "GET", ts.URL+"/api/init-status", "", nil)
	if code != 200 || initStatus["lang"] != "en" {
		t.Fatalf("init-status lang: %d %v", code, initStatus)
	}

	// 非法语言应拒绝
	code, _, _ = doJSON(t, "PUT", ts.URL+"/api/settings", cookie, map[string]string{
		"lang": "fr",
	})
	if code != 400 {
		t.Fatalf("invalid lang should be 400, got %d", code)
	}

	// 只改 login_route 不应影响 lang
	code, settings, _ = doJSON(t, "PUT", ts.URL+"/api/settings", cookie, map[string]string{
		"login_route": "/secret-admin",
	})
	if code != 200 || settings["lang"] != "en" || settings["login_route"] != "/secret-admin" {
		t.Fatalf("mixed update: %d %v", code, settings)
	}

	// 空 body 应拒绝
	code, _, _ = doJSON(t, "PUT", ts.URL+"/api/settings", cookie, map[string]string{})
	if code != 400 {
		t.Fatalf("empty update should be 400, got %d", code)
	}
}

func TestHideFmUsernameSetting(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)

	// 默认 0（显示用户名）
	code, settings, _ := doJSON(t, "GET", ts.URL+"/api/settings", cookie, nil)
	if code != 200 || settings["hide_fm_username"] != "0" {
		t.Fatalf("default hide_fm_username: %d %v", code, settings)
	}

	// 设为 1（隐藏）
	code, settings, _ = doJSON(t, "PUT", ts.URL+"/api/settings", cookie, map[string]string{
		"hide_fm_username": "1",
	})
	if code != 200 || settings["hide_fm_username"] != "1" {
		t.Fatalf("update hide_fm_username: %d %v", code, settings)
	}
	if settings["lang"] != "en" || settings["login_route"] != "/login" {
		t.Fatalf("other settings should be preserved: %v", settings)
	}

	// 非法值应拒绝
	code, _, _ = doJSON(t, "PUT", ts.URL+"/api/settings", cookie, map[string]string{
		"hide_fm_username": "yes",
	})
	if code != 400 {
		t.Fatalf("invalid hide_fm_username should be 400, got %d", code)
	}

	// 改回 0
	code, settings, _ = doJSON(t, "PUT", ts.URL+"/api/settings", cookie, map[string]string{
		"hide_fm_username": "0",
	})
	if code != 200 || settings["hide_fm_username"] != "0" {
		t.Fatalf("reset hide_fm_username: %d %v", code, settings)
	}
}

func TestChangePasswordReencrypt(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)

	// 建一台测试主机
	host, portStr := startTestSSHServer(t)
	port := int(portStr[0]) - '0' // 占位，实际用下面转换
	_ = port
	port = parseIntSafe(t, portStr)
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "pw-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
	})
	if code != 201 {
		t.Fatalf("create host failed: %d", code)
	}
	hostID := hostResp["id"].(string)

	// 建连成功（旧密码下凭据可用）
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/hosts/"+hostID+"/connect", cookie, nil)
	if code != 200 {
		t.Fatalf("connect before change failed: %d", code)
	}

	// 错误旧口令改密码应失败
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/change-password", cookie, map[string]string{
		"old_password": "wrong-old", "new_password": "new-pass-456",
	})
	if code != 401 {
		t.Fatalf("wrong old password should be 401, got %d", code)
	}

	// 正确改密码
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/change-password", cookie, map[string]string{
		"old_password": "admin-pass-123", "new_password": "new-pass-456",
	})
	if code != 200 {
		t.Fatalf("change password failed: %d", code)
	}

	// 旧口令登录失败
	code, loginResp, _ := doJSON(t, "POST", ts.URL+"/api/login", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	if code != 401 {
		t.Fatalf("old password login should fail: %d", code)
	}
	if need, _ := loginResp["need_captcha"].(bool); !need {
		t.Fatalf("need_captcha should be true after fail, got %v", loginResp)
	}

	// 新口令无验证码登录应失败（此前有失败记录，需验证码）
	code, loginResp2, _ := doJSON(t, "POST", ts.URL+"/api/login", "", map[string]string{
		"username": "admin", "password": "new-pass-456",
	})
	if code != 401 {
		t.Fatalf("login without captcha after prior fail should fail: %d", code)
	}
	if need, _ := loginResp2["need_captcha"].(bool); !need {
		t.Fatalf("need_captcha should be true, got %v", loginResp2)
	}

	// 携带错误验证码也应失败（验证码正确路径在 captcha 包内单测）
	capID, _ := getCaptcha(t, ts)
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/login", "", map[string]any{
		"username": "admin", "password": "new-pass-456",
		"captcha_id": capID, "captcha_code": "wrong",
	})
	if code != 401 {
		t.Fatalf("wrong captcha should fail: %d", code)
	}
}

func parseIntSafe(t *testing.T, s string) int {
	t.Helper()
	var n int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
