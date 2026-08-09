package api

import "net/http"

// GET /api/hosts/{id}/quick-access 文件管理器快速访问路径列表（后端持久化，
// 任意浏览器/电脑登录同一网关都能看到；不含默认根目录）。
func (s *Server) handleGetQuickAccess(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	paths, err := s.st.ListQuickAccess(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load quick access failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paths": paths})
}

// PUT /api/hosts/{id}/quick-access 覆盖保存快速访问路径列表。
func (s *Server) handleSaveQuickAccess(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Paths == nil {
		req.Paths = []string{}
	}
	if err := s.st.SaveQuickAccess(id, req.Paths); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}
