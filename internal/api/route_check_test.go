package api

import (
	"strconv"
	"testing"

	"ezssh/internal/auth"
)

// 默认 /login 的探测失败不计入限流：否则用户反复刷新首页就会被自己限流。
func TestRouteCheckDefaultPathNotCounted(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)
	if code, _, _ := doJSON(t, "PUT", ts.URL+"/api/settings", cookie, map[string]string{
		"login_route": "/gate-9f3k",
	}); code != 200 {
		t.Fatalf("set login_route failed: %d", code)
	}

	for i := 0; i < auth.RouteProbeMax+5; i++ {
		code, res, _ := doJSON(t, "POST", ts.URL+"/api/route-check", "", map[string]string{"path": "/login"})
		if code != 200 || res["ok"] != false {
			t.Fatalf("probe %d: code=%d res=%v", i, code, res)
		}
	}
	// 未被限流：安全路由仍可命中
	code, res, _ := doJSON(t, "POST", ts.URL+"/api/route-check", "", map[string]string{"path": "/gate-9f3k"})
	if code != 200 || res["ok"] != true {
		t.Fatalf("security route should still match: code=%d res=%v", code, res)
	}
}

func TestNormalizeRoutePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/login", "/login"},
		{"  /login  ", "/login"},
		{"/login/", "/login"},
		{"/Login", "/login"},
		{"/gate-9f3k/?a=1", "/gate-9f3k"},
		{"/gate-9f3k#x", "/gate-9f3k"},
		{"login", "/login"},
		{"/", "/"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := normalizeRoutePath(c.in); got != c.want {
			t.Errorf("normalizeRoutePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRouteMatches(t *testing.T) {
	cases := []struct {
		path, route string
		want        bool
	}{
		{"/login", "/login", true},
		{"/login/", "/login", true},
		{"/LOGIN", "/login", true},
		{"/login", "/gate-9f3k", false},
		{"/gate", "/gate-9f3k", false},
		{"", "/login", false},
		{"/login", "", false},
		{"/login?x=1", "/login", true},
	}
	for _, c := range cases {
		if got := routeMatches(c.path, c.route); got != c.want {
			t.Errorf("routeMatches(%q, %q) = %v, want %v", c.path, c.route, got, c.want)
		}
	}
}

// /api/route-check：不泄露路由值，仅按路径判定；默认 /login 命中，切换到安全路由后旧路径失效。
func TestRouteCheckEndpoint(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)

	check := func(path string) (int, bool) {
		code, res, _ := doJSON(t, "POST", ts.URL+"/api/route-check", "", map[string]string{"path": path})
		ok, _ := res["ok"].(bool)
		return code, ok
	}

	// 默认路由：/login 命中，其它路径不命中
	if code, ok := check("/login"); code != 200 || !ok {
		t.Fatalf("default route check: code=%d ok=%v", code, ok)
	}
	if code, ok := check("/gate-9f3k"); code != 200 || ok {
		t.Fatalf("unknown path should not match: code=%d ok=%v", code, ok)
	}

	// 切到安全路由
	code, settings, _ := doJSON(t, "PUT", ts.URL+"/api/settings", cookie, map[string]string{
		"login_route": "/gate-9f3k",
	})
	if code != 200 || settings["login_route"] != "/gate-9f3k" {
		t.Fatalf("set login_route: %d %v", code, settings)
	}

	// 默认路径不再命中，安全路由命中（大小写与末尾斜杠无关）
	if _, ok := check("/login"); ok {
		t.Fatal("default /login must not match after security route is set")
	}
	if _, ok := check("/gate-9f3k/"); !ok {
		t.Fatal("security route should match")
	}
	if _, ok := check("/GATE-9F3K"); !ok {
		t.Fatal("security route match should ignore case")
	}
	if _, ok := check("/gate-9f3j"); ok {
		t.Fatal("wrong route must not match")
	}
}

// 探测失败限流：超过上限后返回 429（防止枚举安全路由）。
func TestRouteCheckRateLimit(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)
	code, _, _ := doJSON(t, "PUT", ts.URL+"/api/settings", cookie, map[string]string{
		"login_route": "/gate-9f3k",
	})
	if code != 200 {
		t.Fatalf("set login_route failed: %d", code)
	}

	// 前 auth.RouteProbeMax 次失败仍返回 200（ok=false），之后被限流为 429
	for i := 0; i < auth.RouteProbeMax; i++ {
		code, res, _ := doJSON(t, "POST", ts.URL+"/api/route-check", "", map[string]string{"path": "/guess-" + strconv.Itoa(i)})
		if code != 200 {
			t.Fatalf("probe %d: unexpected code %d (%v)", i, code, res)
		}
	}
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/route-check", "", map[string]string{"path": "/guess-final"})
	if code != 429 {
		t.Fatalf("expected 429 after too many probes, got %d", code)
	}
	// 命中正确路径同样被限流（限流优先于匹配，避免用响应差异绕过）
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/route-check", "", map[string]string{"path": "/gate-9f3k"})
	if code != 429 {
		t.Fatalf("expected 429 even for the correct route while limited, got %d", code)
	}
}
