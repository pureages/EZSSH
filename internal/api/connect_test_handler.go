package api

import (
	"net/http"
	"strings"

	"ezssh/internal/store"
)

// POST /api/test-connect 使用表单中的明文凭据测试 SSH 连通性。
// 不持久化主机、不缓存连接、不记录 TOFU。
func (s *Server) handleTestConnect(w http.ResponseWriter, r *http.Request) {
	var req hostReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if msg, ok := req.validate(true); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}

	host := &store.Host{
		Host:     strings.TrimSpace(req.Host),
		Port:     req.Port,
		Username: strings.TrimSpace(req.Username),
		AuthType: req.AuthType,
	}
	fp, platform, err := s.hub.TestConnect(host, []byte(req.credentialPlain()))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "连接失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"connected":   "true",
		"fingerprint": fp,
		"platform":    platform,
	})
}
