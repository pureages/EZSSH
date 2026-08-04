package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ezssh/internal/apps"
	"ezssh/internal/auth"
	"ezssh/internal/captcha"
	"ezssh/internal/sshhub"
	"ezssh/internal/store"
	"ezssh/internal/vault"
)

// Server 聚合各依赖并提供 HTTP 路由。
type Server struct {
	st          *store.Store
	v           *vault.Vault
	am          *auth.Manager
	// Version 网关版本号（由 main 注入，可用 ldflags 覆盖）。
	Version     string
	hub         *sshhub.Hub
	sftp        *apps.SFTPManager
	copyMgr     *apps.CopyManager
	monitor     *apps.Monitor
	procs       *apps.ProcessManager
	docker      *apps.DockerManager
	firewall    *apps.FirewallManager
	download    *apps.DownloadManager
	bg          *apps.BackgroundManager
	nginx       *apps.NginxManager
	cert        *apps.CertManager
	captcha     *captcha.Manager
	muSub       sync.Mutex
	// monitorSubs: hostID -> subID -> 订阅连接。subID 区分同一连接上的不同订阅者
	//（如桌面图标订阅、监控窗口订阅），全部取消后才停止该主机的采集。
	monitorSubs map[string]map[string]*wsConn
}

func New(st *store.Store, v *vault.Vault, am *auth.Manager, hub *sshhub.Hub) *Server {
	s := &Server{
		st:      st,
		v:       v,
		am:      am,
		hub:     hub,
		sftp:    apps.NewSFTPManager(hub),
		monitor: apps.NewMonitor(hub),
		procs:   apps.NewProcessManager(hub),
		docker:  apps.NewDockerManager(hub),
		firewall: apps.NewFirewallManager(hub),
		captcha: captcha.NewManager(),
	}
	s.copyMgr = apps.NewCopyManager(hub, s.sftp)
	s.download = apps.NewDownloadManager(hub)
	s.bg = apps.NewBackgroundManager(hub, s.sftp, s.procs, st)
	s.nginx = apps.NewNginxManager(hub, s.sftp)
	s.cert = apps.NewCertManager(hub, s.sftp)
	// 监控数据通过各 WS 连接分发
	s.monitor.SetOnData(func(hostID string, snap apps.Snapshot) {
		s.broadcastMonitor(hostID, snap)
	})
	// 证书自动续签兜底任务
	s.startRenewalLoop()
	return s
}

// Close 释放连接资源。
func (s *Server) Close() {
	s.sftp.CloseAll()
	s.monitor.CloseAll()
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// 公开端点
	mux.HandleFunc("GET /api/init-status", s.handleInitStatus)
	mux.HandleFunc("POST /api/init", s.handleInit)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("GET /api/captcha", s.handleCaptcha)

	// 需认证
	mux.HandleFunc("POST /api/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("GET /api/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("GET /ws", s.requireAuth(s.handleWS))
	mux.HandleFunc("GET /api/settings", s.requireAuth(s.handleGetSettings))
	mux.HandleFunc("PUT /api/settings", s.requireAuth(s.handleUpdateSettings))
	mux.HandleFunc("POST /api/change-password", s.requireAuth(s.handleChangePassword))
	mux.HandleFunc("GET /api/update-check", s.requireAuth(s.handleUpdateCheck))
	mux.HandleFunc("POST /api/update", s.requireAuth(s.handleUpdate))

	mux.HandleFunc("GET /api/hosts", s.requireAuth(s.handleListHosts))
	mux.HandleFunc("POST /api/hosts", s.requireAuth(s.handleCreateHost))
	mux.HandleFunc("PUT /api/hosts/{id}", s.requireAuth(s.handleUpdateHost))
	mux.HandleFunc("DELETE /api/hosts/{id}", s.requireAuth(s.handleDeleteHost))
	mux.HandleFunc("POST /api/hosts/{id}/connect", s.requireAuth(s.handleConnectHost))
	mux.HandleFunc("POST /api/hosts/{id}/hide", s.requireAuth(s.handleHideHost))
	mux.HandleFunc("POST /api/hosts/show-all", s.requireAuth(s.handleShowAllHosts))
	mux.HandleFunc("GET /api/hosts/{id}/status", s.requireAuth(s.handleHostStatus))
	mux.HandleFunc("POST /api/hosts/{id}/exec", s.requireAuth(s.handleHostExec))
	mux.HandleFunc("POST /api/test-connect", s.requireAuth(s.handleTestConnect))
	mux.HandleFunc("GET /api/geo", s.requireAuth(s.handleGeo))

	// SFTP 文件操作
	mux.HandleFunc("GET /api/hosts/{id}/sftp/list", s.requireAuth(s.handleSftpList))
	mux.HandleFunc("POST /api/hosts/{id}/sftp/mkdir", s.requireAuth(s.handleSftpMkdir))
	mux.HandleFunc("POST /api/hosts/{id}/sftp/rename", s.requireAuth(s.handleSftpRename))
	mux.HandleFunc("POST /api/hosts/{id}/sftp/remove", s.requireAuth(s.handleSftpRemove))
	mux.HandleFunc("POST /api/hosts/{id}/sftp/chmod", s.requireAuth(s.handleSftpChmod))
	mux.HandleFunc("GET /api/hosts/{id}/sftp/read", s.requireAuth(s.handleSftpRead))
	mux.HandleFunc("POST /api/hosts/{id}/sftp/write", s.requireAuth(s.handleSftpWrite))
	mux.HandleFunc("GET /api/hosts/{id}/sftp/download", s.requireAuth(s.handleSftpDownload))
	mux.HandleFunc("POST /api/hosts/{id}/sftp/upload", s.requireAuth(s.handleSftpUpload))
	mux.HandleFunc("POST /api/hosts/{id}/sftp/paste", s.requireAuth(s.handleSftpPaste))
	mux.HandleFunc("POST /api/hosts/{id}/sftp/extract", s.requireAuth(s.handleSftpExtract))

	// 一键命令：保存的命令 CRUD
	mux.HandleFunc("GET /api/commands", s.requireAuth(s.handleListCommands))
	mux.HandleFunc("POST /api/commands", s.requireAuth(s.handleCreateCommand))
	mux.HandleFunc("PUT /api/commands/{id}", s.requireAuth(s.handleUpdateCommand))
	mux.HandleFunc("DELETE /api/commands/{id}", s.requireAuth(s.handleDeleteCommand))

	// 一键命令：后台长期运行任务
	mux.HandleFunc("POST /api/background/start", s.requireAuth(s.handleBackgroundStart))
	mux.HandleFunc("GET /api/background", s.requireAuth(s.handleBackgroundList))
	mux.HandleFunc("POST /api/background/{id}/kill", s.requireAuth(s.handleBackgroundKill))
	mux.HandleFunc("GET /api/background/{id}/logs", s.requireAuth(s.handleBackgroundLogs))

	// 网站管理：Nginx 建站
	mux.HandleFunc("GET /api/websites", s.requireAuth(s.handleListWebsites))
	mux.HandleFunc("GET /api/websites/groups", s.requireAuth(s.handleListWebsiteGroups))
	mux.HandleFunc("POST /api/websites", s.requireAuth(s.handleCreateWebsite))
	mux.HandleFunc("PUT /api/websites/{id}", s.requireAuth(s.handleUpdateWebsite))
	mux.HandleFunc("DELETE /api/websites/{id}", s.requireAuth(s.handleDeleteWebsite))
	mux.HandleFunc("POST /api/websites/{id}/deploy", s.requireAuth(s.handleDeployWebsite))
	mux.HandleFunc("POST /api/websites/{id}/enable", s.requireAuth(s.handleToggleWebsite))
	mux.HandleFunc("GET /api/nginx/status", s.requireAuth(s.handleNginxStatus))
	mux.HandleFunc("POST /api/nginx/install", s.requireAuth(s.handleNginxInstall))

	// 网站管理：Let's Encrypt 证书
	mux.HandleFunc("GET /api/certificates", s.requireAuth(s.handleListCertificates))
	mux.HandleFunc("GET /api/certificates/check", s.requireAuth(s.handleCheckCertificate))
	mux.HandleFunc("POST /api/certificates/issue", s.requireAuth(s.handleIssueCertificate))
	mux.HandleFunc("POST /api/certificates/{id}/renew", s.requireAuth(s.handleRenewCertificate))
	mux.HandleFunc("POST /api/certificates/{id}/sync", s.requireAuth(s.handleSyncCertificate))
	mux.HandleFunc("DELETE /api/certificates/{id}", s.requireAuth(s.handleDeleteCertificate))

	// 网站管理：DNS 验证账户
	mux.HandleFunc("GET /api/dns-accounts", s.requireAuth(s.handleListDnsAccounts))
	mux.HandleFunc("POST /api/dns-accounts", s.requireAuth(s.handleCreateDnsAccount))
	mux.HandleFunc("PUT /api/dns-accounts/{id}", s.requireAuth(s.handleUpdateDnsAccount))
	mux.HandleFunc("DELETE /api/dns-accounts/{id}", s.requireAuth(s.handleDeleteDnsAccount))

	// 前端静态资源（未构建时返回提示页）
	mux.Handle("/", s.staticHandler())
	return mux
}

// webDistCandidates 返回前端 web/dist 的候选目录，按优先级排列：
// 1. EZSSH_WEB 环境变量（显式指定，最高优先）
// 2. 工作目录下的 web/dist（开发态 / 源码内运行）
// 3. 可执行文件同目录下的 web/dist（单目录部署，如 tar 包解压后同一目录）
// 4. ~/.ezssh/web/dist（一键安装脚本安装的固定位置）
func webDistCandidates() []string {
	cands := []string{}
	if d := os.Getenv("EZSSH_WEB"); d != "" {
		cands = append(cands, d)
	}
	cands = append(cands, filepath.Join("web", "dist"))
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), "web", "dist"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		cands = append(cands, filepath.Join(home, ".ezssh", "web", "dist"))
	}
	return cands
}

func (s *Server) staticHandler() http.Handler {
	for _, dist := range webDistCandidates() {
		if fi, err := os.Stat(dist); err == nil && fi.IsDir() {
			if _, err := os.Stat(filepath.Join(dist, "index.html")); err == nil {
				return http.FileServer(http.Dir(dist))
			}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<h1>EZSSH 网关已启动</h1><p>前端尚未构建，请先执行 <code>npm install &amp;&amp; npm run build</code>（见 README）。</p>")
	})
}

// ---- HTTP 辅助 ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
