package api

import (
	"strconv"
	"testing"

	"ezssh/internal/store"
)

// TestBuiltinHostFeatures 覆盖内置主机 hide/show-all 往返 + 可删除（删除后不再重播）+ 普通主机仍可删。
func TestBuiltinHostFeatures(t *testing.T) {
	ts, st := newTestServerWithStore(t)
	code, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	if code != 200 {
		t.Fatalf("init: %d", code)
	}
	cookie := cookieOf(cookies)
	if err := st.EnsureBuiltinHost(); err != nil {
		t.Fatalf("seed builtin: %v", err)
	}

	// 隐藏内置 → 列表 hidden=true
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/hosts/"+store.BuiltinHostID+"/hide", cookie, map[string]bool{"hidden": true})
	if code != 200 {
		t.Fatalf("hide: %d", code)
	}
	code, hosts := listHosts(t, ts.URL, cookie)
	if code != 200 {
		t.Fatalf("list: %d", code)
	}
	builtin := findHost(hosts, store.BuiltinHostID)
	if builtin == nil || builtin["builtin"] != true || builtin["hidden"] != true {
		t.Fatalf("builtin after hide: %v", builtin)
	}

	// 显示全部 → hidden=false
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/hosts/show-all", cookie, nil)
	if code != 200 {
		t.Fatalf("show-all: %d", code)
	}
	_, hosts = listHosts(t, ts.URL, cookie)
	if builtin := findHost(hosts, store.BuiltinHostID); builtin["hidden"] != false {
		t.Fatalf("builtin after show-all: %v", builtin)
	}

	// 删除内置 → 200，标记已删除，列表不再有该主机
	code, _, _ = doJSON(t, "DELETE", ts.URL+"/api/hosts/"+store.BuiltinHostID, cookie, nil)
	if code != 200 {
		t.Fatalf("delete builtin: got %d", code)
	}
	if v, _ := st.GetSetting(store.SettingBuiltinDeleted); v != "1" {
		t.Fatalf("deleted flag not set: %q", v)
	}
	_, hosts = listHosts(t, ts.URL, cookie)
	if findHost(hosts, store.BuiltinHostID) != nil {
		t.Fatalf("builtin still in list after delete: %v", hosts)
	}

	// 模拟重启重新播种 → 应被跳过（尊重删除意愿）
	if err := st.EnsureBuiltinHost(); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	_, hosts = listHosts(t, ts.URL, cookie)
	if findHost(hosts, store.BuiltinHostID) != nil {
		t.Fatalf("builtin re-seeded after delete: %v", hosts)
	}

	// 普通主机仍可删
	host, portStr := startTestSSHServer(t)
	port, _ := strconv.Atoi(portStr)
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "normal-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
	})
	if code != 201 {
		t.Fatalf("create normal: %d %v", code, hostResp)
	}
	normalID, _ := hostResp["id"].(string)
	code, _, _ = doJSON(t, "DELETE", ts.URL+"/api/hosts/"+normalID, cookie, nil)
	if code != 200 {
		t.Fatalf("delete normal: %d", code)
	}
}

func findHost(hosts []map[string]any, id string) map[string]any {
	for _, h := range hosts {
		if h["id"] == id {
			return h
		}
	}
	return nil
}
