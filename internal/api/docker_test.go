package api

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type dockerMsg struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

func wsRequest(t *testing.T, conn *websocket.Conn, msgType, hostID, channelID string, payload any) map[string]any {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{
		"type": msgType, "hostId": hostID, "channelId": channelID, "payload": payload,
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read %s: %v", msgType, err)
		}
		var m dockerMsg
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Type == msgType {
			return m.Payload
		}
		if m.Type == "error" {
			t.Fatalf("%s error: %v", msgType, m.Payload)
		}
	}
	t.Fatalf("timeout waiting %s", msgType)
	return nil
}

func TestDockerFlow(t *testing.T) {
	ts := newTestServer(t)
	_, _, cookies := doJSON(t, "POST", ts.URL+"/api/init", "", map[string]string{
		"username": "admin", "password": "admin-pass-123",
	})
	cookie := cookieOf(cookies)

	host, portStr := startTestSSHServer(t)
	port, _ := strconv.Atoi(portStr)
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "docker-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
	})
	if code != 201 {
		t.Fatalf("create host failed: %d", code)
	}
	hostID := hostResp["id"].(string)

	conn := dialWS(t, ts, cookie)
	defer conn.Close()

	// 容器列表
	p := wsRequest(t, conn, "docker.list", hostID, "ch_dl", nil)
	containers := p["containers"].([]any)
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}
	first := containers[0].(map[string]any)
	if first["state"] != "running" || first["image"] != "nginx:latest" {
		t.Fatalf("unexpected container: %v", first)
	}

	// stats
	p = wsRequest(t, conn, "docker.stats", hostID, "ch_ds", nil)
	stats := p["stats"].([]any)
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
	st := stats[0].(map[string]any)
	if st["CPUPerc"] != "0.5%" || st["MemUsage"] != "10MiB / 1GiB" {
		t.Fatalf("unexpected stats: %v", st)
	}

	// images
	p = wsRequest(t, conn, "docker.images", hostID, "ch_di", nil)
	images := p["images"].([]any)
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	img := images[0].(map[string]any)
	if img["Repository"] != "nginx" || img["Tag"] != "latest" {
		t.Fatalf("unexpected image: %v", img)
	}

	// logs
	p = wsRequest(t, conn, "docker.logs", hostID, "ch_dlog", map[string]any{
		"id": "abc123def456", "tail": 100,
	})
	if logs := p["logs"].(string); !containsStr(logs, "listening on :80") {
		t.Fatalf("unexpected logs: %q", logs)
	}

	// action: start
	p = wsRequest(t, conn, "docker.action", hostID, "ch_da", map[string]any{
		"action": "start", "id": "abc123def456",
	})
	if p["ok"] != true {
		t.Fatalf("action failed: %v", p)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
