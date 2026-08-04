package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"ezssh/internal/store"
)

const bgStartTimeout = 60 * time.Second

// POST /api/background/start 在若干台服务器上后台启动同一条命令。
// 逐台执行，返回每台的结果（部分失败也 200，便于前端逐台展示）。
func (s *Server) handleBackgroundStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HostIDs []string `json:"host_ids"`
		Command string   `json:"command"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.HostIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "至少选择一台服务器")
		return
	}
	if commandTrimmed(req.Command) == "" {
		writeErr(w, http.StatusBadRequest, "命令内容不能为空")
		return
	}
	command := commandTrimmed(req.Command)

	type result struct {
		HostID   string            `json:"hostId"`
		HostName string            `json:"hostName"`
		OK       bool              `json:"ok"`
		Error    string            `json:"error,omitempty"`
		Task     map[string]any    `json:"task,omitempty"`
	}
	results := make([]result, 0, len(req.HostIDs))
	for _, hostID := range req.HostIDs {
		res := result{HostID: hostID}
		host, err := s.st.GetHost(hostID)
		if err != nil {
			if err == store.ErrNotFound {
				res.Error = "主机不存在"
			} else {
				res.Error = "读取主机失败"
			}
			results = append(results, res)
			continue
		}
		res.HostName = host.Name
		ctx, cancel := context.WithTimeout(r.Context(), bgStartTimeout)
		task, err := s.bg.Start(ctx, hostID, host.Name, command)
		cancel()
		if err != nil {
			res.Error = err.Error()
		} else {
			res.OK = true
			res.Task = map[string]any{"id": task.ID, "pid": task.PID}
			_ = s.st.AddAudit("background.start", hostID, command)
		}
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, results)
}

// GET /api/background 后台任务列表（含实时进程统计）。
func (s *Server) handleBackgroundList(w http.ResponseWriter, r *http.Request) {
	views, err := s.bg.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load background tasks failed")
		return
	}
	writeJSON(w, http.StatusOK, views)
}

// POST /api/background/{id}/kill 结束后台任务进程。
func (s *Server) handleBackgroundKill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.bg.Kill(id); err != nil {
		if err == store.ErrNotFound {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.st.AddAudit("background.kill", "", id)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// GET /api/background/{id}/logs?lines=N 读取后台任务输出日志尾部。
func (s *Server) handleBackgroundLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	lines := 500
	if v := r.URL.Query().Get("lines"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			lines = n
		}
	}
	if lines < 50 {
		lines = 50
	}
	if lines > 5000 {
		lines = 5000
	}
	out, err := s.bg.Logs(r.Context(), id, lines)
	if err != nil {
		if err == store.ErrNotFound {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": out})
}

// commandTrimmed 去掉首尾空白（\n \t \r 空格）。
func commandTrimmed(s string) string {
	trim := func(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
	for len(s) > 0 && trim(s[0]) {
		s = s[1:]
	}
	for len(s) > 0 && trim(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	return s
}
