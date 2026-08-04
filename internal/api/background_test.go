package api

import (
	"strconv"
	"testing"
)

// TestBackgroundStartListKill 后台任务全链路：启动 → 列表 → 日志 → 停止 → 404。
func TestBackgroundStartListKill(t *testing.T) {
	ts, cookie := newAuthedTest(t)

	// 校验：无主机 / 空命令
	code, _, _ := doJSON(t, "POST", ts.URL+"/api/background/start", cookie, map[string]any{
		"host_ids": []string{}, "command": "sleep 100",
	})
	if code != 400 {
		t.Fatalf("no hosts: got %d", code)
	}
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/background/start", cookie, map[string]any{
		"host_ids": []string{"h_any"}, "command": "  ",
	})
	if code != 400 {
		t.Fatalf("empty command: got %d", code)
	}

	// 不存在的 host → 该台 ok:false，接口仍 200
	code, res := doJSONArr(t, "POST", ts.URL+"/api/background/start", cookie, map[string]any{
		"host_ids": []string{"h_bogus"}, "command": "sleep 100",
	})
	if code != 200 || len(res) != 1 || res[0]["ok"] != false {
		t.Fatalf("bogus host: got %d %v", code, res)
	}

	// 建立真实目标机并后台启动
	host, portStr := startTestSSHServer(t)
	port, _ := strconv.Atoi(portStr)
	code, hostResp, _ := doJSON(t, "POST", ts.URL+"/api/hosts", cookie, map[string]any{
		"name": "bg-vm", "host": host, "port": port,
		"username": testUser, "auth_type": "password", "password": testPassword,
	})
	if code != 201 {
		t.Fatalf("create host: got %d %v", code, hostResp)
	}
	hostID, _ := hostResp["id"].(string)

	code, res = doJSONArr(t, "POST", ts.URL+"/api/background/start", cookie, map[string]any{
		"host_ids": []string{hostID}, "command": "sleep 100",
	})
	if code != 200 || len(res) != 1 || res[0]["ok"] != true {
		t.Fatalf("start: got %d %v", code, res)
	}
	task := res[0]["task"].(map[string]any)
	taskID, _ := task["id"].(string)
	if taskID == "" || task["pid"] != float64(12345) {
		t.Fatalf("task: %v", task)
	}

	// 列表：任务在列（mock 进程表无 PID 12345 → exited）
	code, views := doJSONArr(t, "GET", ts.URL+"/api/background", cookie, nil)
	if code != 200 {
		t.Fatalf("list: got %d", code)
	}
	found := false
	for _, v := range views {
		if v["id"] == taskID {
			found = true
			if v["pid"] != float64(12345) {
				t.Fatalf("view pid: %v", v)
			}
		}
	}
	if !found {
		t.Fatalf("task %s not in list: %v", taskID, views)
	}

	// 日志
	code, logsResp, _ := doJSON(t, "GET", ts.URL+"/api/background/"+taskID+"/logs", cookie, nil)
	if code != 200 {
		t.Fatalf("logs: got %d", code)
	}
	if logsResp["logs"] == "" || logsResp["logs"] == nil {
		t.Fatalf("empty logs: %v", logsResp)
	}

	// 停止
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/background/"+taskID+"/kill", cookie, nil)
	if code != 200 {
		t.Fatalf("kill: got %d", code)
	}

	// 未知任务 → 404
	code, _, _ = doJSON(t, "POST", ts.URL+"/api/background/bg_nope/kill", cookie, nil)
	if code != 404 {
		t.Fatalf("kill unknown: got %d", code)
	}
	code, _, _ = doJSON(t, "GET", ts.URL+"/api/background/bg_nope/logs", cookie, nil)
	if code != 404 {
		t.Fatalf("logs unknown: got %d", code)
	}
}
