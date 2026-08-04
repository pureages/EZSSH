package api

import (
	"net/http"
	"strings"
	"time"

	"ezssh/internal/store"
)

// certDTO 证书对外视图。
type certDTO struct {
	ID           string `json:"id"`
	HostID       string `json:"hostId"`
	HostName     string `json:"hostName"`
	Domain       string `json:"domain"`
	Method       string `json:"method"` // http | dns
	DNSAccountID string `json:"dns_account_id"`
	Email        string `json:"email"`
	Status       string `json:"status"` // issuing | active | renewing | error
	ExpiresAt    string `json:"expires_at"`
	LastRenew    string `json:"last_renew"`
	Error        string `json:"error"`
	CreatedAt    string `json:"created_at"`
}

func (s *Server) certToDTO(c *store.Certificate) certDTO {
	name := ""
	if h, err := s.st.GetHost(c.HostID); err == nil {
		name = h.Name
	}
	return certDTO{
		ID: c.ID, HostID: c.HostID, HostName: name, Domain: c.Domain,
		Method: c.Method, DNSAccountID: c.DNSAccountID, Email: c.Email,
		Status: c.Status, ExpiresAt: c.ExpiresAt, LastRenew: c.LastRenew,
		Error: c.Error, CreatedAt: c.CreatedAt,
	}
}

// GET /api/certificates?host_id=
func (s *Server) handleListCertificates(w http.ResponseWriter, r *http.Request) {
	certs, err := s.st.ListCertificates(r.URL.Query().Get("host_id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list certificates failed")
		return
	}
	out := make([]certDTO, 0, len(certs))
	for _, c := range certs {
		out = append(out, s.certToDTO(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// issueReq 证书签发请求。
type issueReq struct {
	HostID        string `json:"host_id"`
	WebsiteID     string `json:"website_id"` // 可选：签发成功后回填站点 cert_id
	Domain        string `json:"domain"`
	Method        string `json:"method"` // http | dns
	DNSAccountID  string `json:"dns_account_id"`
	Email         string `json:"email"`
	Webroot       string `json:"webroot"` // method=http 时使用
}

func (r *issueReq) validate() (string, bool) {
	r.HostID = strings.TrimSpace(r.HostID)
	r.Domain = strings.TrimSpace(strings.ToLower(r.Domain))
	r.Method = strings.TrimSpace(r.Method)
	if r.HostID == "" {
		return "host_id is required", false
	}
	if bad, ok := validateDomains(r.Domain); !ok {
		return "invalid domain: " + bad, false
	}
	if r.Method != "http" && r.Method != "dns" {
		return "method must be http or dns", false
	}
	if r.Method == "dns" && strings.TrimSpace(r.DNSAccountID) == "" {
		return "dns_account_id is required for dns method", false
	}
	return "", true
}

// POST /api/certificates/issue 签发证书（NDJSON 流式进度）。
func (s *Server) handleIssueCertificate(w http.ResponseWriter, r *http.Request) {
	var req issueReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg, ok := req.validate(); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}

	id, err := newRandomID("cert")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "id generation failed")
		return
	}
	cert := &store.Certificate{
		ID: id, HostID: req.HostID, Domain: req.Domain, Method: req.Method,
		DNSAccountID: req.DNSAccountID, Email: strings.TrimSpace(req.Email), Status: "issuing",
	}
	if err := s.st.CreateCertificate(cert); err != nil {
		writeErr(w, http.StatusInternalServerError, "create certificate failed")
		return
	}

	send, ok := ndjson(w)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	line := func(l string) { send(map[string]string{"line": l}) }

	fail := func(errMsg string) {
		_ = s.st.UpdateCertificateState(id, "error", "", "", errMsg)
		send(map[string]string{"error": errMsg})
	}

	if err := s.cert.EnsureAcmeSh(req.HostID, line); err != nil {
		fail("安装 acme.sh 失败: " + err.Error())
		return
	}

	var cfToken string
	if req.Method == "dns" {
		acct, err := s.st.GetDnsAccount(req.DNSAccountID)
		if err != nil {
			fail("dns account not found")
			return
		}
		if !s.v.IsUnlocked() {
			fail("vault locked, please login to unlock")
			return
		}
		token, derr := s.v.Decrypt(acct.TokenEnc)
		if derr != nil {
			fail("解密 DNS 账户 Token 失败: " + derr.Error())
			return
		}
		cfToken = string(token)
	}

	if err := s.cert.Issue(req.HostID, req.Domain, req.Method, cfToken, req.Webroot, line); err != nil {
		fail(err.Error())
		return
	}

	// 签发成功：读取到期时间并更新记录；读取失败说明证书未安装到位，标记 error 提示
	expires, serr := s.cert.CertStatus(req.HostID, req.Domain)
	if serr != nil {
		fail("证书已签发但未安装到 /etc/nginx/ssl/" + req.Domain + "/: " + serr.Error())
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if err := s.st.UpdateCertificateState(id, "active", expires, now, ""); err != nil {
		send(map[string]string{"error": err.Error()})
		return
	}
	if req.WebsiteID != "" {
		_ = s.st.UpdateWebsiteCertID(req.WebsiteID, id)
	}
	_ = s.st.AddAudit("certificate.issue", req.HostID, req.Domain)
	send(map[string]string{"ok": "true", "cert_id": id})
}

// POST /api/certificates/{id}/renew 强制续签（NDJSON 流式进度）。
func (s *Server) handleRenewCertificate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cert, err := s.st.GetCertificate(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "certificate not found")
		return
	}
	send, ok := ndjson(w)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	line := func(l string) { send(map[string]string{"line": l}) }

	_ = s.st.UpdateCertificateState(id, "renewing", cert.ExpiresAt, cert.LastRenew, "")
	if err := s.cert.Renew(cert.HostID, cert.Domain, true, line); err != nil {
		_ = s.st.UpdateCertificateState(id, "error", cert.ExpiresAt, cert.LastRenew, "续签失败: "+err.Error())
		send(map[string]string{"error": err.Error()})
		return
	}
	// 续签成功后重读到期时间；读取失败时保留原到期时间，避免到期被清空
	expires, serr := s.cert.CertStatus(cert.HostID, cert.Domain)
	if serr != nil || strings.TrimSpace(expires) == "" {
		expires = cert.ExpiresAt
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	if err := s.st.UpdateCertificateState(id, "active", expires, now, ""); err != nil {
		send(map[string]string{"error": err.Error()})
		return
	}
	_ = s.st.AddAudit("certificate.renew", cert.HostID, cert.Domain)
	send(map[string]string{"ok": "true"})
}

// GET /api/certificates/check?host_id=&domain= 检测某域名证书是否已安装到稳定路径。
// 返回 { installed, expires_at }，供建站表单勾选 SSL 时显示证书可用性。
func (s *Server) handleCheckCertificate(w http.ResponseWriter, r *http.Request) {
	hostID := strings.TrimSpace(r.URL.Query().Get("host_id"))
	domain := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("domain")))
	if hostID == "" || domain == "" {
		writeErr(w, http.StatusBadRequest, "host_id and domain are required")
		return
	}
	expires, err := s.cert.CertStatus(hostID, domain)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"installed": false, "expires_at": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"installed": true, "expires_at": expires})
}

// POST /api/certificates/{id}/sync 从服务器重读证书到期时间，刷新记录。
func (s *Server) handleSyncCertificate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cert, err := s.st.GetCertificate(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "certificate not found")
		return
	}
	expires, serr := s.cert.CertStatus(cert.HostID, cert.Domain)
	if serr != nil {
		// 证书文件不存在 → 标记为未安装（error 状态），提示先重新签发/续签安装
		_ = s.st.UpdateCertificateState(id, "error", cert.ExpiresAt, cert.LastRenew, "证书未安装到 /etc/nginx/ssl/"+cert.Domain+"/（点击「续签」可强制重装）: "+serr.Error())
		updated, _ := s.st.GetCertificate(id)
		writeJSON(w, http.StatusOK, s.certToDTO(updated))
		return
	}
	if err := s.st.UpdateCertificateState(id, "active", expires, cert.LastRenew, ""); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	updated, err := s.st.GetCertificate(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "get certificate failed")
		return
	}
	writeJSON(w, http.StatusOK, s.certToDTO(updated))
}

// DELETE /api/certificates/{id} 仅删记录（远端 acme.sh 证书保留；删除网站时会一并清理）。
func (s *Server) handleDeleteCertificate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cert, err := s.st.GetCertificate(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "certificate not found")
		return
	}
	// 若站点引用该证书，清空关联
	sites, _ := s.st.ListWebsites(cert.HostID, "")
	for _, ws := range sites {
		if ws.CertID == id {
			_ = s.st.UpdateWebsiteCertID(ws.ID, "")
		}
	}
	if err := s.st.DeleteCertificate(id); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete certificate failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// ---- 自动续签 ----

// renewalInterval 网关兜底续签检查周期（acme.sh 服务器端 cron 为主路径）。
const renewalInterval = 12 * time.Hour

// renewalThreshold 到期前多少天触发自动续签。
const renewalThreshold = 30 * 24 * time.Hour

// startRenewalLoop 启动自动续签后台循环：每 12h 检查一遍 active 证书，
// 到期前 30 天内调用 acme.sh --renew（不加 force，未到期自动跳过）。
func (s *Server) startRenewalLoop() {
	go func() {
		// 首次检查延后，避免网关启动瞬间对大量主机建连
		time.Sleep(5 * time.Minute)
		ticker := time.NewTicker(renewalInterval)
		defer ticker.Stop()
		for range ticker.C {
			s.checkRenewals()
		}
	}()
}

func (s *Server) checkRenewals() {
	if !s.v.IsUnlocked() {
		return
	}
	certs, err := s.st.ListActiveCertificates()
	if err != nil {
		return
	}
	for _, c := range certs {
		expires, ok := parseSQLiteTime(c.ExpiresAt)
		if !ok {
			continue // 到期未知，跳过（依赖手动签发/续签后写入）
		}
		if time.Until(expires) > renewalThreshold {
			continue
		}
		// 触发自动续签（后台执行，逐台异步避免阻塞循环）
		go s.renewQuietly(c.ID)
	}
}

// renewQuietly 后台静默续签单张证书：成功/失败都回写状态，不打断主循环。
func (s *Server) renewQuietly(certID string) {
	defer func() { _ = recover() }()
	cert, err := s.st.GetCertificate(certID)
	if err != nil {
		return
	}
	if err := s.cert.Renew(cert.HostID, cert.Domain, false, func(string) {}); err != nil {
		_ = s.st.UpdateCertificateState(cert.ID, "error", cert.ExpiresAt, cert.LastRenew, "自动续签失败: "+err.Error())
		return
	}
	expires, serr := s.cert.CertStatus(cert.HostID, cert.Domain)
	if serr != nil || strings.TrimSpace(expires) == "" {
		expires = cert.ExpiresAt // 读取失败保留原到期时间
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_ = s.st.UpdateCertificateState(cert.ID, "active", expires, now, "")
}

// parseSQLiteTime 解析 SQLite 时间字符串（UTC，"2006-01-02 15:04:05"）。
func parseSQLiteTime(s string) (time.Time, bool) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}
