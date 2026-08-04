package api

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

type psListMsg struct {
	Type    string `json:"type"`
	Payload struct {
		Processes []map[string]any `json:"processes"`
	} `json:"payload"`
}

func TestMonitorAndProcess(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)

	host, portStr := startTestSSHServer(t)
	port, _ := strconv.Atoi(portStr)
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "mon-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
	})
	if code != 201 {
		t.Fatalf("create host failed: %d", code)
	}
	hostID := hostResp["id"].(string)

	conn := dialWS(t, ts, cookie)
	defer conn.Close()

	// 订阅监控
	if err := conn.WriteJSON(map[string]any{
		"type": "monitor.subscribe", "hostId": hostID, "appId": "monitor",
		"channelId": "ch_mon", "payload": nil,
	}); err != nil {
		t.Fatal(err)
	}

	// 收集 monitor.data
	conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	var snap map[string]any
	found := false
	for i := 0; i < 10; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Type == "monitor.data" {
			snap = m.Payload
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no monitor.data received")
	}
	snapshot := snap["snapshot"].(map[string]any)
	if snapshot["load1"].(float64) != 0.05 {
		t.Fatalf("unexpected load1: %v", snapshot["load1"])
	}
	if snapshot["mem_total"].(float64) <= 0 {
		t.Fatalf("mem_total should be positive: %v", snapshot["mem_total"])
	}

	// 请求进程列表
	if err := conn.WriteJSON(map[string]any{
		"type": "ps.list", "hostId": hostID, "appId": "process",
		"channelId": "ch_ps", "payload": nil,
	}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var pl psListMsg
	gotPS := false
	for i := 0; i < 5; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read ps: %v", err)
		}
		if err := json.Unmarshal(data, &pl); err != nil {
			continue
		}
		if pl.Type == "ps.list" {
			gotPS = true
			break
		}
	}
	if !gotPS {
		t.Fatal("no ps.list received")
	}
	if len(pl.Payload.Processes) < 3 {
		t.Fatalf("expected >=3 processes, got %v", pl.Payload.Processes)
	}
	// 首次采集 CPU 差值无样本，断言列表包含 sshd(42) 且用户映射正确
	foundSSHD := false
	for _, p := range pl.Payload.Processes {
		if p["command"] == "/usr/sbin/sshd" && int(p["pid"].(float64)) == 42 && p["user"] == "root" {
			foundSSHD = true
		}
	}
	if !foundSSHD {
		t.Fatalf("missing sshd(42): %v", pl.Payload.Processes)
	}

	// kill（信号 15）
	if err := conn.WriteJSON(map[string]any{
		"type": "ps.kill", "hostId": hostID, "appId": "process",
		"channelId": "ch_ps", "payload": map[string]any{"pid": 42, "signal": 15},
	}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for i := 0; i < 5; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read kill: %v", err)
		}
		var m struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(data, &m)
		if m.Type == "ps.killed" {
			return
		}
	}
	t.Fatal("no ps.killed received")
}
