package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type wsResp struct {
	Type      string `json:"type"`
	ChannelID string `json:"channelId"`
	Payload   struct {
		Data string `json:"data"`
	} `json:"payload"`
}

func dialWS(t *testing.T, ts *httptestServer, cookie string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	hdr := map[string][]string{"Cookie": {cookie}}
	conn, resp, err := websocket.DefaultDialer.Dial(url, hdr)
	if err != nil {
		if resp != nil {
			t.Fatalf("ws dial failed: %v (status %d)", err, resp.StatusCode)
		}
		t.Fatalf("ws dial failed: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// 简化别名：避免重复 import httptest
type httptestServer = httptest.Server

func TestWSTerminal(t *testing.T) {
	ts := newTestServer(t)

	// 初始化并获取 cookie
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)
	if cookie == "" {
		t.Fatal("no cookie")
	}

	// 启动本地 SSH 目标机
	host, portStr := startTestSSHServer(t)
	port, _ := strconv.Atoi(portStr)

	// 新增主机
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "term-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
	})
	if code != 201 {
		t.Fatalf("create host failed: %d %v", code, hostResp)
	}
	hostID := hostResp["id"].(string)

	conn := dialWS(t, ts, cookie)

	// open 终端
	openPayload, _ := json.Marshal(map[string]any{"cols": 80, "rows": 24})
	if err := conn.WriteJSON(map[string]any{
		"type": "terminal.open", "hostId": hostID, "appId": "terminal",
		"channelId": "ch_test", "payload": json.RawMessage(openPayload),
	}); err != nil {
		t.Fatal(err)
	}

	// 收到 shell 欢迎输出（可能先收到 open 结果确认，循环读取直到出现 output）
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	foundWelcome := false
	for i := 0; i < 5; i++ {
		var m wsResp
		if err := conn.ReadJSON(&m); err != nil {
			t.Fatalf("read welcome: %v", err)
		}
		if m.Type != "terminal.output" {
			t.Logf("received %s msg", m.Type)
			continue
		}
		out, err := base64.StdEncoding.DecodeString(m.Payload.Data)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out), "test-shell") {
			foundWelcome = true
			break
		}
		t.Logf("output: %q", string(out))
	}
	if !foundWelcome {
		t.Fatal("did not receive shell welcome output")
	}

	// 输入命令并收到回显（测试服务器 shell 模式回显 "echo: ..."）
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.WriteJSON(map[string]any{
		"type": "terminal.input", "hostId": hostID, "appId": "terminal",
		"channelId": "ch_test",
		"payload":   map[string]string{"data": base64.StdEncoding.EncodeToString([]byte("ls\n"))},
	}); err != nil {
		t.Fatal(err)
	}
	var m2 wsResp
	if err := conn.ReadJSON(&m2); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	echo, err := base64.StdEncoding.DecodeString(m2.Payload.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(echo), "echo: ls") {
		t.Fatalf("unexpected echo: %q", string(echo))
	}

	// resize 不报错
	if err := conn.WriteJSON(map[string]any{
		"type": "terminal.resize", "hostId": hostID, "appId": "terminal",
		"channelId": "ch_test",
		"payload":   map[string]any{"cols": 120, "rows": 40},
	}); err != nil {
		t.Fatal(err)
	}

	// close 后无异常
	if err := conn.WriteJSON(map[string]any{
		"type": "terminal.close", "hostId": hostID, "appId": "terminal",
		"channelId": "ch_test", "payload": nil,
	}); err != nil {
		t.Fatal(err)
	}
}
