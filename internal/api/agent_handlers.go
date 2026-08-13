package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"ezssh/internal/store"
)

// settingAgentToken settings 键：pissh 等本地 agent 调 API 用的访问令牌。
const settingAgentToken = "agent_token"

// requireAgent 校验 X-Agent-Token 头（网关本机的 pissh 等 agent 进程调用专用）。
// token 与登录会话相互独立，仅用于服务器本地脚本/扩展访问主机执行能力。
func (s *Server) requireAgent(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, _ := s.st.GetSetting(settingAgentToken)
		if tok == "" || r.Header.Get("X-Agent-Token") != tok {
			writeErr(w, http.StatusUnauthorized, "invalid agent token")
			return
		}
		next(w, r)
	}
}

// POST /api/agent/token 生成（或返回已有）agent token。需登录态，仅管理员可见。
func (s *Server) handleAgentToken(w http.ResponseWriter, r *http.Request) {
	tok, _ := s.st.GetSetting(settingAgentToken)
	if tok == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		tok = hex.EncodeToString(b)
		if err := s.st.SetSetting(settingAgentToken, tok); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
}

// GET /api/agent/hosts 返回主机清单（不含任何凭证）。agent token 认证。
// 供 pissh 的 list_hosts 工具使用，让 AI 了解网关中纳管了哪些服务器。
func (s *Server) handleAgentHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.st.ListHosts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type item struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Platform string `json:"platform"`
		Group    string `json:"group,omitempty"`
	}
	out := make([]item, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, item{h.ID, h.Name, h.Host, h.Port, h.Username, h.Platform, h.GroupName})
	}
	writeJSON(w, http.StatusOK, map[string]any{"hosts": out})
}

type agentExecReq struct {
	Host    string `json:"host"`
	Command string `json:"command"`
}

// POST /api/agent/exec 按主机名或 ID 在目标主机执行一条命令并返回输出。agent token 认证。
// 供 pissh 的 ssh_exec 工具使用：AI 说"查看 home99 的硬件信息"时，由扩展调用本接口。
func (s *Server) handleAgentExec(w http.ResponseWriter, r *http.Request) {
	var req agentExecReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Host = strings.TrimSpace(req.Host)
	if req.Host == "" || strings.TrimSpace(req.Command) == "" {
		writeErr(w, http.StatusBadRequest, "host and command are required")
		return
	}
	h, err := s.findHostByRef(req.Host)
	if err != nil {
		writeErr(w, http.StatusNotFound, "host not found: "+req.Host)
		return
	}
	client, err := s.hub.GetClient(h.ID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "connect failed: "+err.Error())
		return
	}
	sess, err := client.NewSession()
	if err != nil {
		writeErr(w, http.StatusBadGateway, "open session failed: "+err.Error())
		return
	}
	defer sess.Close()
	out, runErr := sess.CombinedOutput(req.Command)
	writeJSON(w, http.StatusOK, map[string]string{"output": string(out), "error": errString(runErr)})
}

// findHostByRef 按 ID 精确匹配；否则按名称匹配（不区分大小写，先全等后包含）。
func (s *Server) findHostByRef(ref string) (*store.Host, error) {
	hosts, err := s.st.ListHosts()
	if err != nil {
		return nil, err
	}
	for _, h := range hosts {
		if h.ID == ref {
			return h, nil
		}
	}
	lower := strings.ToLower(ref)
	for _, h := range hosts {
		if strings.ToLower(h.Name) == lower {
			return h, nil
		}
	}
	for _, h := range hosts {
		if strings.Contains(strings.ToLower(h.Name), lower) {
			return h, nil
		}
	}
	return nil, store.ErrNotFound
}
