package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// newAuthedTest 初始化网关并返回带会话 cookie 的测试服务器。
func newAuthedTest(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	ts := newTestServer(t)
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
	return ts, cookie
}

// doJSONArr 同 doJSON 但响应为 JSON 数组。
func doJSONArr(t *testing.T, method, url, cookie string, body any) (int, []map[string]any) {
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
	var out []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestCommandsCRUD 覆盖保存命令的 401 / CRUD 往返 / 重复名 / 空值 / 404。
func TestCommandsCRUD(t *testing.T) {
	ts, cookie := newAuthedTest(t)

	// 未登录 401
	code, _, _ := doJSON(t, "GET", ts.URL+"/api/commands", "", nil)
	if code != 401 {
		t.Fatalf("unauth: got %d", code)
	}

	// 空 name / 空 command → 400
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/commands", cookie, map[string]string{"name": "", "command": "x"})
	if code != 400 {
		t.Fatalf("empty name: got %d", code)
	}
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/commands", cookie, map[string]string{"name": "x", "command": "  "})
	if code != 400 {
		t.Fatalf("empty command: got %d", code)
	}

	// 创建（多行命令）
	code, resp, _ := doJSON(t, "POST", ts.URL+"/api/commands", cookie, map[string]string{
		"name": "deploy", "command": "echo a\necho b",
	})
	if code != 201 {
		t.Fatalf("create: got %d %v", code, resp)
	}
	id, _ := resp["id"].(float64)

	// 重复名 → 400
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/commands", cookie, map[string]string{"name": "deploy", "command": "x"})
	if code != 400 {
		t.Fatalf("duplicate name: got %d", code)
	}

	// 列表
	code, list := doJSONArr(t, "GET", ts.URL+"/api/commands", cookie, nil)
	if code != 200 || len(list) != 1 || list[0]["name"] != "deploy" {
		t.Fatalf("list: got %d %v", code, list)
	}

	// 更新
	code, resp, _ = doJSON(t, "PUT", ts.URL+"/api/commands/"+itoa(id), cookie, map[string]string{
		"name": "deploy2", "command": "echo c",
	})
	if code != 200 || resp["name"] != "deploy2" {
		t.Fatalf("update: got %d %v", code, resp)
	}

	// 更新成重复名 → 400
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/commands", cookie, map[string]string{"name": "other", "command": "x"})
	if code != 201 {
		t.Fatalf("create other: got %d", code)
	}
	code, _, _ = doJSON(t, "PUT", ts.URL+"/api/commands/"+itoa(id), cookie, map[string]string{"name": "other", "command": "y"})
	if code != 400 {
		t.Fatalf("update dup: got %d", code)
	}

	// 删除
	code, _, _ = doJSON(t, "DELETE", ts.URL+"/api/commands/"+itoa(id), cookie, nil)
	if code != 200 {
		t.Fatalf("delete: got %d", code)
	}
	// 删除不存在 → 404
	code, _, _ = doJSON(t, "DELETE", ts.URL+"/api/commands/"+itoa(id), cookie, nil)
	if code != 404 {
		t.Fatalf("delete missing: got %d", code)
	}
}

func itoa(f float64) string {
	return strconv.FormatInt(int64(f), 10)
}
