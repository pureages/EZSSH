package api

import (
	"net/http"
	"strings"

	"ezssh/internal/vault"
)

const (
	settingLoginRoute      = "login_route"
	settingLang            = "lang"
	settingHideFmUsername  = "hide_fm_username"
)

// GET /api/settings 读取可配置项（登录路由、界面语言等）。
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	route, _ := s.st.GetSetting(settingLoginRoute)
	if route == "" {
		route = "/login"
	}
	lang, _ := s.st.GetSetting(settingLang)
	if lang == "" {
		lang = "en"
	}
	hideFmUser, _ := s.st.GetSetting(settingHideFmUsername)
	if hideFmUser == "" {
		hideFmUser = "0"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"login_route":      route,
		"lang":             lang,
		"hide_fm_username": hideFmUser,
	})
}

// PUT /api/settings 更新配置（login_route / lang / hide_fm_username 均可选，至少一项）。
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LoginRoute     string `json:"login_route"`
		Lang           string `json:"lang"`
		HideFmUsername string `json:"hide_fm_username"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.LoginRoute == "" && req.Lang == "" && req.HideFmUsername == "" {
		writeErr(w, http.StatusBadRequest, "no settings provided")
		return
	}

	if req.Lang != "" {
		if req.Lang != "zh" && req.Lang != "en" {
			writeErr(w, http.StatusBadRequest, "lang 仅支持 zh/en")
			return
		}
		if err := s.st.SetSetting(settingLang, req.Lang); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存失败")
			return
		}
		_ = s.st.AddAudit("settings.lang", "", req.Lang)
	}

	if req.HideFmUsername != "" {
		if req.HideFmUsername != "0" && req.HideFmUsername != "1" {
			writeErr(w, http.StatusBadRequest, "hide_fm_username 仅支持 0/1")
			return
		}
		if err := s.st.SetSetting(settingHideFmUsername, req.HideFmUsername); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存失败")
			return
		}
		_ = s.st.AddAudit("settings.hide_fm_username", "", req.HideFmUsername)
	}

	if req.LoginRoute != "" {
		route := strings.TrimSpace(req.LoginRoute)
		// 校验：必须以 / 开头、不含空白、不含 # 与 ?
		if !strings.HasPrefix(route, "/") ||
			strings.ContainsAny(route, " #?") {
			writeErr(w, http.StatusBadRequest, "login_route 必须以 / 开头，且不含空格/#/?")
			return
		}
		if len(route) > 64 {
			writeErr(w, http.StatusBadRequest, "login_route 过长")
			return
		}
		if err := s.st.SetSetting(settingLoginRoute, route); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存失败")
			return
		}
		_ = s.st.AddAudit("settings.login_route", "", route)
	}

	// 返回各配置项当前值
	route, _ := s.st.GetSetting(settingLoginRoute)
	if route == "" {
		route = "/login"
	}
	lang, _ := s.st.GetSetting(settingLang)
	if lang == "" {
		lang = "en"
	}
	hideFmUser, _ := s.st.GetSetting(settingHideFmUsername)
	if hideFmUser == "" {
		hideFmUser = "0"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"login_route":      route,
		"lang":             lang,
		"hide_fm_username": hideFmUser,
	})
}

// POST /api/change-password 修改登录口令：
// 校验旧口令 → 用新口令派生新 KEK → 重新加密全部主机凭据 → 更新口令哈希。
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.NewPassword) < 8 {
		writeErr(w, http.StatusBadRequest, "新口令至少 8 位")
		return
	}
	if req.OldPassword == req.NewPassword {
		writeErr(w, http.StatusBadRequest, "新口令不能与旧口令相同")
		return
	}

	// 获取当前账号（单用户）
	users, err := s.st.ListUsers()
	if err != nil || len(users) == 0 {
		writeErr(w, http.StatusInternalServerError, "账号不存在")
		return
	}
	username := users[0]
	u, err := s.st.GetUser(username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取账号失败")
		return
	}

	// 校验旧口令（与保险库解锁共用派生逻辑）
	if !vault.Verify(req.OldPassword, u.Salt, u.PasswordHash) {
		writeErr(w, http.StatusUnauthorized, "旧口令不正确")
		return
	}
	if !s.v.IsUnlocked() {
		writeErr(w, http.StatusForbidden, "保险库未解锁，无法修改口令")
		return
	}

	// 旧 KEK 解密 → 新 KEK 重加密所有主机凭据
	oldKEK := s.currentKEK()
	if oldKEK == nil {
		writeErr(w, http.StatusInternalServerError, "读取密钥失败")
		return
	}
	hosts, err := s.st.ListHosts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取主机失败")
		return
	}

	// 生成新盐与新哈希，派生新 KEK
	newSalt, err := vault.NewSalt()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "rng failure")
		return
	}
	newHash := vault.HashPassword(req.NewPassword, newSalt)
	newKEK := vault.DeriveKey(req.NewPassword, newSalt)

	// 用新 KEK 重加密所有主机凭据
	for _, h := range hosts {
		plain, err := vault.DecryptWith(oldKEK, h.Credential)
		if err != nil {
			continue // 个别损坏凭据跳过，避免整批失败
		}
		enc, err := vault.EncryptWith(newKEK, plain)
		if err != nil {
			continue
		}
		_ = s.st.UpdateHostCredential(h.ID, enc)
	}

	// 更新口令哈希与盐
	if err := s.st.UpdateUserPassword(username, newHash, newSalt); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新口令失败")
		return
	}
	// 切换内存 KEK
	s.v.SetKey(newKEK)
	_ = s.st.AddAudit("auth.change_password", "", username)

	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// currentKEK 导出当前内存 KEK 的副本（用于重加密）。
func (s *Server) currentKEK() []byte {
	// 通过加密-解密往返获取实际 KEK 的等效能力不现实，
	// 直接要求 vault 提供。此处以 SetKey 配对使用。
	kek := s.v.GetKey()
	return kek
}

// GET /api/captcha 生成验证码，返回 id 与 SVG。
func (s *Server) handleCaptcha(w http.ResponseWriter, r *http.Request) {
	id, svg := s.captcha.Create()
	writeJSON(w, http.StatusOK, map[string]string{
		"id":  id,
		"svg": svg,
	})
}
