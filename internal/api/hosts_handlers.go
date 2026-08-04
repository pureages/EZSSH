package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"ezssh/internal/store"
)

type hostDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	AuthType    string `json:"auth_type"`
	GroupName   string `json:"group_name"`
	Remark      string `json:"remark"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	Connected   bool   `json:"connected"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Hidden      bool   `json:"hidden"`
	Builtin     bool   `json:"builtin"`
	Platform    string `json:"platform,omitempty"`
}

func toDTO(h *store.Host, connected bool, fp string) hostDTO {
	return hostDTO{
		ID: h.ID, Name: h.Name, Host: h.Host, Port: h.Port, Username: h.Username,
		AuthType: h.AuthType, GroupName: h.GroupName, Remark: h.Remark,
		CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt,
		Connected: connected, Fingerprint: fp, Hidden: h.Hidden, Builtin: h.Builtin,
		Platform: h.Platform,
	}
}

func newHostID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "h_" + hex.EncodeToString(b), nil
}

// GET /api/hosts
func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.st.ListHosts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list hosts failed")
		return
	}
	out := make([]hostDTO, 0, len(hosts))
	for _, h := range hosts {
		connected, fp := s.hub.Status(h.ID)
		out = append(out, toDTO(h, connected, fp))
	}
	writeJSON(w, http.StatusOK, out)
}

type hostReq struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthType   string `json:"auth_type"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	GroupName  string `json:"group_name"`
	Remark     string `json:"remark"`
	Platform   string `json:"platform,omitempty"`
}

// validate 校验公共字段。requireCredential=true 时强制要求提供凭据（创建场景），
// false 时允许留空（编辑场景，留空表示保留原凭据）。
func (r *hostReq) validate(requireCredential bool) (string, bool) {
	if strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Host) == "" || strings.TrimSpace(r.Username) == "" {
		return "name/host/username are required", false
	}
	if r.Port <= 0 || r.Port > 65535 {
		r.Port = 22
	}
	switch r.AuthType {
	case "password":
		if requireCredential && r.Password == "" {
			return "password is required", false
		}
	case "privatekey":
		if requireCredential && strings.TrimSpace(r.PrivateKey) == "" {
			return "private_key is required", false
		}
	default:
		return "auth_type must be password or privatekey", false
	}
	// platform 收敛到 ''（未知/自动检测）| linux | windows
	switch r.Platform {
	case "", "linux", "windows":
	default:
		return "platform must be empty, linux or windows", false
	}
	return "", true
}

// credentialPlain 返回待加密的明文凭据。
func (r *hostReq) credentialPlain() string {
	if r.AuthType == "password" {
		return r.Password
	}
	return strings.TrimSpace(r.PrivateKey)
}

// POST /api/hosts
func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	if !s.v.IsUnlocked() {
		writeErr(w, http.StatusForbidden, "vault locked, please login to unlock")
		return
	}
	var req hostReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg, ok := req.validate(true); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	id, err := newHostID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "id generation failed")
		return
	}
	enc, err := s.v.Encrypt([]byte(req.credentialPlain()))
	if err != nil {
		writeErr(w, http.StatusForbidden, "vault locked")
		return
	}
	h := &store.Host{
		ID: id, Name: strings.TrimSpace(req.Name), Host: strings.TrimSpace(req.Host),
		Port: req.Port, Username: strings.TrimSpace(req.Username),
		AuthType: req.AuthType, Credential: enc,
		GroupName: strings.TrimSpace(req.GroupName), Remark: strings.TrimSpace(req.Remark),
		Platform: req.Platform,
	}
	if err := s.st.CreateHost(h); err != nil {
		writeErr(w, http.StatusInternalServerError, "create host failed")
		return
	}
	_ = s.st.AddAudit("host.create", id, h.Name)

	// 回读以获得数据库生成的 created_at/updated_at
	saved, err := s.st.GetHost(id)
	if err != nil {
		saved = h
	}
	connected, fp := s.hub.Status(id)
	writeJSON(w, http.StatusCreated, toDTO(saved, connected, fp))
}

// PUT /api/hosts/{id}
func (s *Server) handleUpdateHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	old, err := s.st.GetHost(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "host not found")
		return
	}
	var req hostReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg, ok := req.validate(false); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	h := &store.Host{
		ID: id, Name: strings.TrimSpace(req.Name), Host: strings.TrimSpace(req.Host),
		Port: req.Port, Username: strings.TrimSpace(req.Username),
		AuthType: req.AuthType,
		GroupName: strings.TrimSpace(req.GroupName), Remark: strings.TrimSpace(req.Remark),
		Platform: req.Platform,
	}
	if err := s.st.UpdateHost(h); err != nil {
		writeErr(w, http.StatusInternalServerError, "update host failed")
		return
	}
	// 凭据有变更时重新加密；留空表示保留原凭据
	if s.v.IsUnlocked() && req.credentialPlain() != "" {
		enc, err := s.v.Encrypt([]byte(req.credentialPlain()))
		if err == nil {
			_ = s.st.UpdateHostCredential(id, enc)
		}
	}
	_ = s.st.AddAudit("host.update", id, h.Name)
	// 凭据/地址变更后旧连接不可信，强制断开并丢弃缓存的 SFTP 客户端
	// （否则文件管理器会复用旧连接上失效的 SFTP 客户端，报 "connection lost"）
	if old.Host != h.Host || old.Port != h.Port || old.Username != h.Username || old.AuthType != h.AuthType {
		s.hub.CloseHost(id)
		s.sftp.CloseHost(id)
	}

	saved, err := s.st.GetHost(id)
	if err != nil {
		saved = h
	}
	connected, fp := s.hub.Status(id)
	writeJSON(w, http.StatusOK, toDTO(saved, connected, fp))
}

// DELETE /api/hosts/{id}
func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	saved, err := s.st.GetHost(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "host not found")
		return
	}
	s.hub.CloseHost(id)
	s.sftp.CloseHost(id)
	if err := s.st.DeleteHost(id); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete host failed")
		return
	}
	// 删除内置网关主机后标记，重启时不再重新播种
	if saved.Builtin {
		_ = s.st.SetSetting(store.SettingBuiltinDeleted, "1")
	}
	_ = s.st.AddAudit("host.delete", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// POST /api/hosts/{id}/hide 设置桌面图标隐藏标记（{hidden: true|false}）。
func (s *Server) handleHideHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.st.GetHost(id); err != nil {
		writeErr(w, http.StatusNotFound, "host not found")
		return
	}
	var req struct {
		Hidden bool `json:"hidden"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.st.UpdateHostHidden(id, req.Hidden); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	state := "shown"
	if req.Hidden {
		state = "hidden"
	}
	_ = s.st.AddAudit("host.hide", id, state)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// POST /api/hosts/show-all 清除全部主机的桌面隐藏标记。
func (s *Server) handleShowAllHosts(w http.ResponseWriter, r *http.Request) {
	if err := s.st.ShowAllHosts(); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	_ = s.st.AddAudit("host.show_all", "", "")
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// POST /api/hosts/{id}/connect 建立 SSH 连接（预热），返回主机指纹。
func (s *Server) handleConnectHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := s.hub.GetClient(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "host not found")
			return
		}
		if strings.Contains(err.Error(), "vault locked") {
			writeErr(w, http.StatusForbidden, "vault locked")
			return
		}
		writeErr(w, http.StatusBadGateway, "connect failed: "+err.Error())
		return
	}
	_ = s.st.AddAudit("host.connect", id, "")
	platform := ""
	if p, perr := s.hub.Platform(id); perr == nil {
		platform = p
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"connected":   "true",
		"fingerprint": s.hub.Fingerprint(id),
		"platform":    platform,
	})
}

// GET /api/hosts/{id}/status
func (s *Server) handleHostStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	saved, err := s.st.GetHost(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "host not found")
		return
	}
	connected, fp := s.hub.Status(id)
	// 平台优先取 store 持久化值；连接中且未知时从活跃连接取（不触发新连接）
	platform := saved.Platform
	if platform == "" && connected {
		if p, perr := s.hub.Platform(id); perr == nil {
			platform = p
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connected": connected, "fingerprint": fp, "platform": platform,
	})
}

type execReq struct {
	Command string `json:"command"`
}

// POST /api/hosts/{id}/exec 在目标主机执行一条命令并返回输出（M1 验证用）。
func (s *Server) handleHostExec(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req execReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeErr(w, http.StatusBadRequest, "command is required")
		return
	}
	client, err := s.hub.GetClient(id)
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

	type result struct {
		Output string `json:"output"`
		Error  string `json:"error,omitempty"`
	}
	out, runErr := sess.CombinedOutput(req.Command)
	// 命令执行失败（非零退出码）也返回输出，便于排错
	writeJSON(w, http.StatusOK, result{Output: string(out), Error: errString(runErr)})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
