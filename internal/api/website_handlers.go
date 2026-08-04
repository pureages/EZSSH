package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"ezssh/internal/store"
)

// ---- 通用 ----

// newRandomID 生成指定前缀的随机 ID（如 ws_xxxxxxxxxx）。
func newRandomID(prefix string) (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(b), nil
}

var domainRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

// validateDomains 校验逗号分隔的域名列表；返回首个非法域名。
func validateDomains(domains string) (string, bool) {
	for _, d := range strings.Split(domains, ",") {
		d = strings.TrimSpace(d)
		if d == "" || !domainRe.MatchString(d) {
			return d, false
		}
	}
	return "", true
}

// ndjson 构造 NDJSON 流式响应 writer；客户端断开后 send 自动忽略，不会 panic。
func ndjson(w http.ResponseWriter) (send func(any), ok bool) {
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, fok := w.(http.Flusher)
	if !fok {
		return nil, false
	}
	var sendMu sync.Mutex
	return func(v any) {
		sendMu.Lock()
		defer sendMu.Unlock()
		defer func() { _ = recover() }()
		_ = json.NewEncoder(w).Encode(v)
		flusher.Flush()
	}, true
}

// ---- DTO ----

type websiteDTO struct {
	ID          string `json:"id"`
	HostID      string `json:"hostId"`
	HostName    string `json:"hostName"`
	Name        string `json:"name"`
	GroupName   string `json:"group_name"`
	Domains     string `json:"domains"`
	SiteType    string `json:"site_type"`
	RootDir     string `json:"root_dir"`
	ProxyPass   string `json:"proxy_pass"`
	RedirectURL string `json:"redirect_url"`
	SSL         bool   `json:"ssl"`
	CertID      string `json:"cert_id"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func (s *Server) websiteToDTO(w *store.Website) websiteDTO {
	name := ""
	if h, err := s.st.GetHost(w.HostID); err == nil {
		name = h.Name
	}
	return websiteDTO{
		ID: w.ID, HostID: w.HostID, HostName: name, Name: w.Name,
		GroupName: w.GroupName, Domains: w.Domains, SiteType: w.SiteType,
		RootDir: w.RootDir, ProxyPass: w.ProxyPass, RedirectURL: w.RedirectURL,
		SSL: w.SSL, CertID: w.CertID, Enabled: w.Enabled,
		CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt,
	}
}

type websiteReq struct {
	HostID      string `json:"hostId"`
	Name        string `json:"name"`
	GroupName   string `json:"group_name"`
	Domains     string `json:"domains"`
	SiteType    string `json:"site_type"`
	RootDir     string `json:"root_dir"`
	ProxyPass   string `json:"proxy_pass"`
	RedirectURL string `json:"redirect_url"`
	SSL         bool   `json:"ssl"`
	Enabled     bool   `json:"enabled"`
}

// validate 校验建站公共字段，并规范化输入。
func (r *websiteReq) validate() (string, bool) {
	r.HostID = strings.TrimSpace(r.HostID)
	r.Name = strings.TrimSpace(r.Name)
	r.GroupName = strings.TrimSpace(r.GroupName)
	r.Domains = strings.TrimSpace(r.Domains)
	r.RootDir = strings.TrimSpace(r.RootDir)
	r.ProxyPass = strings.TrimSpace(r.ProxyPass)
	r.RedirectURL = strings.TrimSpace(r.RedirectURL)

	if r.HostID == "" {
		return "hostId is required", false
	}
	if r.Name == "" {
		return "name is required", false
	}
	if r.Domains == "" {
		return "domains is required", false
	}
	if bad, ok := validateDomains(r.Domains); !ok {
		return "invalid domain: " + bad, false
	}
	switch r.SiteType {
	case "static":
		// root_dir 可为空，部署时默认 /var/www/<主域名>
	case "proxy":
		if r.ProxyPass == "" || !(strings.HasPrefix(r.ProxyPass, "http://") || strings.HasPrefix(r.ProxyPass, "https://")) {
			return "proxy_pass must start with http:// or https://", false
		}
	case "redirect":
		if r.RedirectURL == "" || !(strings.HasPrefix(r.RedirectURL, "http://") || strings.HasPrefix(r.RedirectURL, "https://")) {
			return "redirect_url must start with http:// or https://", false
		}
	default:
		return "site_type must be static, proxy or redirect", false
	}
	return "", true
}

func (r *websiteReq) toWebsite(id string) *store.Website {
	return &store.Website{
		ID: id, HostID: r.HostID, Name: r.Name, GroupName: r.GroupName,
		Domains: r.Domains, SiteType: r.SiteType, RootDir: r.RootDir,
		ProxyPass: r.ProxyPass, RedirectURL: r.RedirectURL,
		SSL: r.SSL, Enabled: r.Enabled,
	}
}

// ---- 网站 CRUD ----

// GET /api/websites?host_id=&group=
func (s *Server) handleListWebsites(w http.ResponseWriter, r *http.Request) {
	hostID := r.URL.Query().Get("host_id")
	group := r.URL.Query().Get("group")
	sites, err := s.st.ListWebsites(hostID, group)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list websites failed")
		return
	}
	out := make([]websiteDTO, 0, len(sites))
	for _, ws := range sites {
		out = append(out, s.websiteToDTO(ws))
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/websites/groups?host_id=
func (s *Server) handleListWebsiteGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.st.ListWebsiteGroups(r.URL.Query().Get("host_id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list groups failed")
		return
	}
	if groups == nil {
		groups = []string{}
	}
	writeJSON(w, http.StatusOK, groups)
}

// POST /api/websites 建站（仅落库；部署走 POST /deploy 或创建后的自动部署）。
func (s *Server) handleCreateWebsite(w http.ResponseWriter, r *http.Request) {
	var req websiteReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg, ok := req.validate(); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if _, err := s.st.GetHost(req.HostID); err != nil {
		writeErr(w, http.StatusBadRequest, "host not found")
		return
	}
	id, err := newRandomID("ws")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "id generation failed")
		return
	}
	ws := req.toWebsite(id)
	if err := s.st.CreateWebsite(ws); err != nil {
		writeErr(w, http.StatusInternalServerError, "create website failed")
		return
	}
	_ = s.st.AddAudit("website.create", ws.HostID, ws.Name)
	saved, err := s.st.GetWebsite(id)
	if err != nil {
		saved = ws
	}
	writeJSON(w, http.StatusCreated, s.websiteToDTO(saved))
}

// PUT /api/websites/{id}
func (s *Server) handleUpdateWebsite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	old, err := s.st.GetWebsite(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "website not found")
		return
	}
	var req websiteReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg, ok := req.validate(); !ok {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	// 编辑时 hostId 允许留空（沿用原服务器）
	if req.HostID == "" {
		req.HostID = old.HostID
	}
	ws := req.toWebsite(id)
	if err := s.st.UpdateWebsite(ws); err != nil {
		writeErr(w, http.StatusInternalServerError, "update website failed")
		return
	}
	_ = s.st.AddAudit("website.update", ws.HostID, ws.Name)
	writeJSON(w, http.StatusOK, s.websiteToDTO(ws))
}

// DELETE /api/websites/{id}
// 删除网站 = 删记录 + 远端删 conf 并 reload + 清理该域名证书（acme.sh --remove + 删证书记录）。
func (s *Server) handleDeleteWebsite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ws, err := s.st.GetWebsite(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "website not found")
		return
	}

	var warnings []string
	// 远端清理（尽力而为：主机离线/连接失败不阻断删除）
	if res, err := s.nginx.RemoveSite(ws.HostID, ws.ID); err != nil {
		warnings = append(warnings, "移除站点配置失败: "+err.Error())
	} else if res.Warning != "" {
		warnings = append(warnings, res.Warning)
	}
	if err := s.removeRemoteCert(ws.HostID, ws.PrimaryDomain(), &warnings); err != nil {
		warnings = append(warnings, err.Error())
	}

	if err := s.st.DeleteWebsite(id); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete website failed")
		return
	}
	_ = s.st.AddAudit("website.delete", ws.HostID, ws.Name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": "true", "warnings": warnings})
}

// removeRemoteCert 删除某服务器上某域名的证书：清理 DB 记录 + 远端 acme.sh 证书。
func (s *Server) removeRemoteCert(hostID, domain string, warnings *[]string) error {
	certs, err := s.st.ListCertificates(hostID)
	if err == nil {
		for _, c := range certs {
			if c.Domain != domain {
				continue
			}
			if err := s.cert.RemoveCert(hostID, domain, func(string) {}); err != nil {
				*warnings = append(*warnings, "移除证书 "+domain+" 失败: "+err.Error())
			}
			_ = s.st.DeleteCertificate(c.ID)
		}
	}
	// 站点若引用了该证书，清空关联
	sites, _ := s.st.ListWebsites(hostID, "")
	for _, ws := range sites {
		if ws.PrimaryDomain() == domain && ws.CertID != "" {
			_ = s.st.UpdateWebsiteCertID(ws.ID, "")
		}
	}
	return nil
}

// POST /api/websites/{id}/deploy 写配置 + nginx -t + reload。
// 失败也返回 200（ok=false + output），便于前端展示完整输出。
// lang 查询参数（"en"）决定静态站缺失 index.html 时预制首页的语言。
func (s *Server) handleDeployWebsite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ws, err := s.st.GetWebsite(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "website not found")
		return
	}
	res, derr := s.nginx.DeploySite(ws.HostID, ws, r.URL.Query().Get("lang"))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": derr == nil, "output": res.Output, "warning": res.Warning, "error": errString(derr),
	})
}

// POST /api/websites/{id}/enable 切换启用/停用（停用会移除配置）。
func (s *Server) handleToggleWebsite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ws, err := s.st.GetWebsite(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "website not found")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ws.Enabled = req.Enabled
	if err := s.st.UpdateWebsite(ws); err != nil {
		writeErr(w, http.StatusInternalServerError, "update website failed")
		return
	}
	res, derr := s.nginx.DeploySite(ws.HostID, ws, r.URL.Query().Get("lang"))
	_ = s.st.AddAudit("website.toggle", ws.HostID, ws.Name)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": derr == nil, "output": res.Output, "warning": res.Warning, "error": errString(derr),
	})
}

// ---- Nginx ----

// GET /api/nginx/status?host_id=
func (s *Server) handleNginxStatus(w http.ResponseWriter, r *http.Request) {
	hostID := r.URL.Query().Get("host_id")
	if hostID == "" {
		writeErr(w, http.StatusBadRequest, "host_id is required")
		return
	}
	st, err := s.nginx.CheckNginx(hostID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "check nginx failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// POST /api/nginx/install 一键安装 nginx（NDJSON 流式进度）。
func (s *Server) handleNginxInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HostID string `json:"host_id"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.HostID) == "" {
		writeErr(w, http.StatusBadRequest, "host_id is required")
		return
	}
	send, ok := ndjson(w)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	err := s.nginx.InstallNginx(req.HostID, func(line string) {
		send(map[string]string{"line": line})
	})
	if err != nil {
		send(map[string]string{"error": err.Error()})
		return
	}
	send(map[string]string{"ok": "true"})
}
