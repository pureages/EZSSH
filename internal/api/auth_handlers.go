package api

import (
	"net/http"
	"strings"

	"ezssh/internal/vault"
)

func (s *Server) initialized() bool {
	n, err := s.st.CountUsers()
	return err == nil && n > 0
}

// GET /api/init-status → { initialized, unlocked, login_route, lang, version }
func (s *Server) handleInitStatus(w http.ResponseWriter, r *http.Request) {
	route, _ := s.st.GetSetting(settingLoginRoute)
	if route == "" {
		route = "/login"
	}
	lang, _ := s.st.GetSetting(settingLang)
	if lang == "" {
		lang = "en"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"initialized": s.initialized(),
		"unlocked":    s.v.IsUnlocked(),
		"login_route": route,
		"lang":        lang,
		"version":     s.Version,
	})
}

// POST /api/init 首次初始化：创建管理员口令并解锁保险库。
func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) {
	if s.initialized() {
		writeErr(w, http.StatusBadRequest, "already initialized")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Username) > 64 {
		writeErr(w, http.StatusBadRequest, "username must be 1-64 chars")
		return
	}
	if len(req.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 chars")
		return
	}

	salt, err := vault.NewSalt()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "rng failure")
		return
	}
	hash := vault.HashPassword(req.Password, salt)
	if err := s.st.CreateUser(req.Username, hash, salt); err != nil {
		writeErr(w, http.StatusInternalServerError, "create user failed")
		return
	}
	// 用同一口令派生 KEK 解锁保险库
	if err := s.v.Unlock(req.Password, salt, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "vault unlock failed")
		return
	}
	token, err := s.am.CreateSession()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session create failed")
		return
	}
	setSessionCookie(w, token)
	_ = s.st.AddAudit("auth.init", "", req.Username)

	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}

// loginFail 返回统一的登录失败响应（携带是否需要验证码标记）。
func (s *Server) loginFail(w http.ResponseWriter, ip string, msg string) {
	s.am.RecordFail(ip)
	writeJSON(w, http.StatusUnauthorized, map[string]any{
		"error":         msg,
		"need_captcha": s.am.FailCount(ip) >= 1,
	})
}

// POST /api/login 登录：校验口令，必要时顺带解锁保险库，颁发会话。
// 同一 IP 已失败过时要求携带验证码。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.am.CanLogin(ip) {
		writeErr(w, http.StatusTooManyRequests, "too many attempts, locked for 5 minutes")
		return
	}
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		CaptchaID   string `json:"captcha_id"`
		CaptchaCode string `json:"captcha_code"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 该 IP 之前失败过 → 必须通过验证码
	if s.am.FailCount(ip) >= 1 {
		if !s.captcha.Verify(req.CaptchaID, req.CaptchaCode) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error":         "验证码错误或已过期",
				"need_captcha": true,
			})
			return
		}
	}

	u, err := s.st.GetUser(strings.TrimSpace(req.Username))
	if err != nil {
		s.loginFail(w, ip, "账号或口令错误")
		return
	}

	// 登录口令同时用于解锁保险库（若尚未解锁）
	if !s.v.IsUnlocked() {
		if err := s.v.Unlock(req.Password, u.Salt, u.PasswordHash); err != nil {
			s.loginFail(w, ip, "账号或口令错误")
			return
		}
	} else if !vault.Verify(req.Password, u.Salt, u.PasswordHash) {
		s.loginFail(w, ip, "账号或口令错误")
		return
	}

	// 登录成功：清空失败记录
	s.am.ClearFail(ip)
	token, err := s.am.CreateSession()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session create failed")
		return
	}
	setSessionCookie(w, token)
	_ = s.st.AddAudit("auth.login", "", ip)

	writeJSON(w, http.StatusOK, map[string]string{"username": u.Username})
}

// POST /api/logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.am.Destroy(c.Value)
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// GET /api/me
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	// 单用户场景：返回唯一账号信息
	var username string
	if n, err := s.st.CountUsers(); err == nil && n > 0 {
		rows, err := s.st.ListUsers()
		if err == nil && len(rows) > 0 {
			username = rows[0]
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username":       username,
		"vaultUnlocked": s.v.IsUnlocked(),
	})
}
