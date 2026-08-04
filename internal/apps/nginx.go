package apps

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	"github.com/pkg/sftp"

	"ezssh/internal/sshhub"
	"ezssh/internal/store"
)

// NginxStatus nginx 安装状态。
type NginxStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Running   bool   `json:"running"`
}

// DeployResult 站点部署结果（含 nginx -t 与 reload 输出）。
type DeployResult struct {
	Output  string `json:"output"`
	Warning string `json:"warning,omitempty"` // 如 SSL 已开但证书未装，降级为 http 时的提示
}

// NginxManager 管理远程服务器的 Nginx：安装、站点配置生成与部署。
type NginxManager struct {
	hub  *sshhub.Hub
	sftp *SFTPManager
}

func NewNginxManager(hub *sshhub.Hub, sftp *SFTPManager) *NginxManager {
	return &NginxManager{hub: hub, sftp: sftp}
}

// exec 在目标机执行命令并返回合并输出；非零退出码返回错误并带输出。
// 失败时也返回输出文本（不再丢弃），供调用方展示真实报错。
func (m *NginxManager) exec(hostID, cmd string) (string, error) {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return "", err
	}
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	out, err := sess.CombinedOutput(cmd)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return msg, fmt.Errorf("%s", msg)
	}
	return string(out), nil
}

// execFallback 同 exec，但失败时附加回退命令的输出，用于诊断无法直连默认 shell 的环境。
func (m *NginxManager) execFallback(hostID, cmd, fallbackCmd string) (string, error) {
	out, err := m.exec(hostID, cmd)
	if err != nil {
		fallbackOut, ferr := m.exec(hostID, fallbackCmd)
		combined := out
		if ferr != nil {
			combined += "\n[fallback] " + ferr.Error()
		} else if strings.TrimSpace(fallbackOut) != "" {
			combined += "\n[fallback] " + strings.TrimSpace(fallbackOut)
		}
		return "", fmt.Errorf("%s", combined)
	}
	return out, nil
}

// runScript 通过 `sh -s` 在目标机执行多行脚本，并逐行流式回调 stdout/stderr。
func (m *NginxManager) runScript(hostID, script string, onLine func(string)) error {
	client, err := m.hub.GetClient(hostID)
	if err != nil {
		return err
	}
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return err
	}
	sess.Stdin = strings.NewReader(script)
	if err := sess.Start("sh -s"); err != nil {
		return err
	}

	var wg sync.WaitGroup
	stream := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
		sc.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
			if atEOF && len(data) == 0 {
				return 0, nil, nil
			}
			if i := bytes.IndexAny(data, "\n\r"); i >= 0 {
				return i + 1, data[:i], nil
			}
			if atEOF {
				return len(data), data, nil
			}
			return 0, nil, nil
		})
		for sc.Scan() {
			if onLine != nil {
				onLine(sc.Text())
			}
		}
	}
	wg.Add(2)
	go stream(stdout)
	go stream(stderr)
	wg.Wait()

	if err := sess.Wait(); err != nil {
		return fmt.Errorf("脚本执行失败: %w", err)
	}
	return nil
}

// writeHostFile 通过 SFTP 将内容写入目标机的指定路径（自动创建父目录）。
func (m *NginxManager) writeHostFile(hostID, filePath string, content []byte) error {
	sshc, err := m.hub.GetClient(hostID)
	if err != nil {
		return err
	}
	client, err := sftp.NewClient(sshc)
	if err != nil {
		return fmt.Errorf("sftp 连接失败: %w", err)
	}
	defer client.Close()

	dir := path.Dir(filePath)
	if err := client.MkdirAll(dir); err != nil {
		return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
	}
	f, err := client.Create(filePath)
	if err != nil {
		return fmt.Errorf("创建文件 %s 失败: %w", filePath, err)
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("写入文件 %s 失败: %w", filePath, err)
	}
	return nil
}

// removeHostFile 删除目标机上的文件（不存在时忽略）。
func (m *NginxManager) removeHostFile(hostID, filePath string) error {
	sshc, err := m.hub.GetClient(hostID)
	if err != nil {
		return err
	}
	client, err := sftp.NewClient(sshc)
	if err != nil {
		return fmt.Errorf("sftp 连接失败: %w", err)
	}
	defer client.Close()
	if err := client.Remove(filePath); err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return nil
		}
		return fmt.Errorf("删除文件 %s 失败: %w", filePath, err)
	}
	return nil
}

// CheckNginx 检测目标机是否已安装 nginx，返回安装状态、版本与运行状态。
func (m *NginxManager) CheckNginx(hostID string) (NginxStatus, error) {
	out, err := m.exec(hostID, `if command -v nginx >/dev/null 2>&1; then nginx -v 2>&1; else echo "__NGINX_NOT_FOUND__"; fi`)
	if err != nil {
		return NginxStatus{}, err
	}
	out = strings.TrimSpace(out)
	if strings.Contains(out, "__NGINX_NOT_FOUND__") {
		return NginxStatus{Installed: false}, nil
	}
	running := false
	if ro, err := m.exec(hostID, `pgrep -x nginx >/dev/null 2>&1 && echo RUNNING || echo STOPPED`); err == nil {
		running = strings.Contains(ro, "RUNNING")
	}
	return NginxStatus{Installed: true, Version: out, Running: running}, nil
}

// InstallNginx 一键安装 nginx：检测包管理器 → 安装 → 启动并设开机自启 → 放行 80/443。
// 每一行输出通过 onLine 回调流式返回。
func (m *NginxManager) InstallNginx(hostID string, onLine func(string)) error {
	script := `set -e
if command -v nginx >/dev/null 2>&1; then
  echo "Nginx 已安装: $(nginx -v 2>&1)"
  exit 0
fi
echo "==> 检测系统包管理器"
PM=""
if command -v apt-get >/dev/null 2>&1; then PM=apt
elif command -v dnf >/dev/null 2>&1; then PM=dnf
elif command -v yum >/dev/null 2>&1; then PM=yum
elif command -v apk >/dev/null 2>&1; then PM=apk
fi
if [ -z "$PM" ]; then
  echo "错误：不支持的系统（未检测到 apt/dnf/yum/apk）"
  exit 1
fi
echo "==> 安装 nginx"
case "$PM" in
  apt) apt-get update -y && apt-get install -y nginx ;;
  dnf) dnf install -y nginx ;;
  yum) yum install -y nginx ;;
  apk) apk add --no-cache nginx ;;
esac
echo "==> 启动并设置开机自启"
if command -v systemctl >/dev/null 2>&1; then
  systemctl enable --now nginx || systemctl start nginx || true
else
  service nginx start || true
fi
echo "==> 放行 80/443 端口"
if command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld 2>/dev/null; then
  firewall-cmd --permanent --add-service=http || true
  firewall-cmd --permanent --add-service=https || true
  firewall-cmd --reload || true
elif command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q 'Status: active'; then
  ufw allow 80/tcp || true
  ufw allow 443/tcp || true
fi
echo "==> 安装完成"
nginx -v 2>&1 || true`
	return m.runScript(hostID, script, onLine)
}

// siteConfPath 返回站点配置文件路径（conf.d 同时兼容 Debian / RHEL 系）。
func siteConfPath(siteID string) string {
	return "/etc/nginx/conf.d/" + siteID + ".conf"
}

// GenerateConfig 生成站点 nginx 配置文本。
// useSSL=true 时生成 443 段 + 80→443 跳转（需证书已安装到稳定路径）。
func GenerateConfig(site *store.Website, useSSL bool) string {
	var b strings.Builder
	serverName := strings.Join(site.AllDomains(), " ")

	// 主站点段（443 或 80）
	b.WriteString("# auto-generated by EZssh, site " + site.ID + "\n")
	writeServerBlock(&b, site, serverName, useSSL)

	if useSSL {
		// 80 → 443 跳转段
		b.WriteString("\nserver {\n")
		b.WriteString("    listen 80;\n")
		b.WriteString("    server_name " + serverName + ";\n")
		b.WriteString("    return 301 https://$host$request_uri;\n")
		b.WriteString("}\n")
	}
	return b.String()
}

func writeServerBlock(b *strings.Builder, site *store.Website, serverName string, useSSL bool) {
	b.WriteString("server {\n")
	if useSSL {
		b.WriteString("    listen 443 ssl;\n")
		b.WriteString("    ssl_certificate /etc/nginx/ssl/" + site.PrimaryDomain() + "/fullchain.pem;\n")
		b.WriteString("    ssl_certificate_key /etc/nginx/ssl/" + site.PrimaryDomain() + "/key.pem;\n")
	} else {
		b.WriteString("    listen 80;\n")
	}
	b.WriteString("    server_name " + serverName + ";\n")

	switch site.SiteType {
	case "proxy":
		b.WriteString("    location / {\n")
		b.WriteString("        proxy_pass " + strings.TrimSpace(site.ProxyPass) + ";\n")
		b.WriteString("        proxy_http_version 1.1;\n")
		b.WriteString("        proxy_set_header Host $host;\n")
		b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
		b.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
		if useSSL {
			b.WriteString("        proxy_set_header X-Forwarded-Proto https;\n")
		} else {
			b.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
		}
		b.WriteString("    }\n")
	case "redirect":
		b.WriteString("    return 301 " + strings.TrimSpace(site.RedirectURL) + "$request_uri;\n")
	default: // static
		b.WriteString("    root " + staticRoot(site) + ";\n")
		b.WriteString("    index index.html index.htm;\n")
		b.WriteString("    location / {\n")
		b.WriteString("        try_files $uri $uri/ =404;\n")
		b.WriteString("    }\n")
	}
	b.WriteString("}\n")
}

// staticRoot 返回静态网站的根目录：未填写时使用默认路径。
func staticRoot(site *store.Website) string {
	root := strings.TrimSpace(site.RootDir)
	if root == "" {
		root = "/opt/ezssh/apps/WebManager/www/" + site.PrimaryDomain()
	}
	return root
}

// defaultIndexHTML 预制的静态站点首页：根目录不存在时自动创建并写入，可直接访问看到效果。
const defaultIndexHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>站点已创建成功</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  html, body { height: 100%; }
  body { min-height: 100vh; display: flex; flex-direction: column; justify-content: center; align-items: center;
    background: #ffffff; color: #1a1a1a; font-family: -apple-system, "Segoe UI", "Microsoft YaHei", "PingFang SC", sans-serif; }
  h1 { font-size: 30px; font-weight: 600; letter-spacing: 1px; color: #000000; }
  .badge { margin-top: 14px; padding: 5px 16px; border-radius: 999px; background: #f2f2f2;
    color: #333333; font-size: 13px; }
  footer { position: fixed; bottom: 22px; width: 100%; text-align: center; color: #888888; font-size: 13px; }
</style>
</head>
<body>
  <h1>站点已创建成功！</h1>
  <div class="badge">EZSSH · 默认站点</div>
  <footer>Powered by EZSSH</footer>
</body>
</html>
`

// defaultIndexHTML_en 预制的静态站点首页（英文版）：根目录不存在时自动创建并写入。
const defaultIndexHTML_en = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Site Created Successfully</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  html, body { height: 100%; }
  body { min-height: 100vh; display: flex; flex-direction: column; justify-content: center; align-items: center;
    background: #ffffff; color: #1a1a1a; font-family: -apple-system, "Segoe UI", "Microsoft YaHei", "PingFang SC", sans-serif; }
  h1 { font-size: 30px; font-weight: 600; letter-spacing: 1px; color: #000000; }
  .badge { margin-top: 14px; padding: 5px 16px; border-radius: 999px; background: #f2f2f2;
    color: #333333; font-size: 13px; }
  footer { position: fixed; bottom: 22px; width: 100%; text-align: center; color: #888888; font-size: 13px; }
</style>
</head>
<body>
  <h1>Your site is ready!</h1>
  <div class="badge">EZSSH · Default Site</div>
  <footer>Powered by EZSSH</footer>
</body>
</html>
`

// ensureStaticRoot 确保静态网站根目录存在，并在 index.html 缺失时写入预制首页。
// 已存在时保留用户内容，不覆盖。返回创建提示（无则空串）。
// lang 用于选择预制首页语言："en" 写英文版，其余写中文版。
func (m *NginxManager) ensureStaticRoot(hostID string, site *store.Website, lang string) (string, error) {
	root := staticRoot(site)
	idx := root + "/index.html"
	has, err := m.exec(hostID, `test -f `+sshQuote(idx)+` && echo YES || echo NO`)
	if err == nil && strings.Contains(has, "YES") {
		return "", nil // index.html 已存在，不覆盖
	}
	html := defaultIndexHTML
	if lang == "en" {
		html = defaultIndexHTML_en
	}
	if err := m.writeHostFile(hostID, idx, []byte(html)); err != nil {
		return "", err
	}
	return "已创建网站根目录 " + root + " 并写入默认 index.html", nil
}

// certInstalled 检查某域名证书是否已安装到稳定路径。
func (m *NginxManager) certInstalled(hostID, domain string) bool {
	out, err := m.exec(hostID, `test -f `+sshQuote("/etc/nginx/ssl/"+domain+"/fullchain.pem")+` && echo YES || echo NO`)
	return err == nil && strings.Contains(out, "YES")
}

// DeploySite 将站点配置写入 conf.d 并 reload。SSL 已开但证书未安装时降级为 http 并返回提示。
// lang 用于静态站缺失 index.html 时预制首页的语言（"en" 为英文）。
func (m *NginxManager) DeploySite(hostID string, site *store.Website, lang string) (DeployResult, error) {
	if !site.Enabled {
		// 站点停用：移除配置
		if err := m.removeHostFile(hostID, siteConfPath(site.ID)); err != nil {
			return DeployResult{}, err
		}
		return m.reload(hostID)
	}

	useSSL := site.SSL
	var warning string
	if useSSL && !m.certInstalled(hostID, site.PrimaryDomain()) {
		useSSL = false
		warning = "SSL 已开启但证书尚未签发/安装（/etc/nginx/ssl/" + site.PrimaryDomain() + "/fullchain.pem 不存在），本次以 HTTP 方式部署。请先签发证书后再部署。"
	}

	conf := GenerateConfig(site, useSSL)
	if err := m.writeHostFile(hostID, siteConfPath(site.ID), []byte(conf)); err != nil {
		return DeployResult{}, err
	}
	// 静态站点：确保根目录存在，index.html 缺失时写入预制首页
	var notes []string
	if site.SiteType == "static" {
		if note, err := m.ensureStaticRoot(hostID, site, lang); err != nil {
			return DeployResult{}, err
		} else if note != "" {
			notes = append(notes, note)
		}
	}
	res, err := m.reload(hostID)
	if len(notes) > 0 {
		if res.Output != "" {
			res.Output += "\n"
		}
		res.Output += strings.Join(notes, "\n")
	}
	res.Warning = warning
	return res, err
}

// RemoveSite 删除站点配置并 reload。
func (m *NginxManager) RemoveSite(hostID, siteID string) (DeployResult, error) {
	if err := m.removeHostFile(hostID, siteConfPath(siteID)); err != nil {
		return DeployResult{}, err
	}
	return m.reload(hostID)
}

// reload 执行 nginx -t 校验，然后 reload（若 nginx 未运行则直接启动）。
// 多种方式逐级回退（-s reload → systemctl reload → service reload → 重启/启动），
// 实际执行输出写入 /tmp/ezssh_nginx_reload.log 并整体回显；最后确认 master 存活。
func (m *NginxManager) reload(hostID string) (DeployResult, error) {
	tOut, err := m.exec(hostID, `nginx -t 2>&1`)
	output := strings.TrimSpace(tOut)
	if err != nil {
		return DeployResult{Output: output}, fmt.Errorf("nginx -t 校验失败: %s", output)
	}

	// 已运行 → 依次尝试 reload；全部失败则 quit 后重启。
	// 未运行（如一键安装时启动被吞掉）→ 直接启动。
	// 启动失败的真实原因会写入 logfile 并随输出回显（此前被 `|| true` 吞掉）。
	script := `logfile=/tmp/ezssh_nginx_reload.log
: > "$logfile"
start_nginx() {
  systemctl reset-failed nginx 2>/dev/null || true
  if systemctl start nginx >"$logfile" 2>&1; then
    return 0
  fi
  if service nginx start >"$logfile" 2>&1; then
    return 0
  fi
  nginx >"$logfile" 2>&1
}
reload_ok=""
if pgrep -x nginx >/dev/null 2>&1; then
  if nginx -s reload >"$logfile" 2>&1; then
    reload_ok=1
  elif systemctl reload nginx >"$logfile" 2>&1; then
    reload_ok=1
  elif service nginx reload >"$logfile" 2>&1; then
    reload_ok=1
  else
    echo "标准 reload 均失败，尝试重启 nginx 加载新配置…"
    cat "$logfile"
    nginx -s quit 2>/dev/null || true
    sleep 1
    start_nginx
  fi
else
  echo "nginx 未运行，正在启动…"
  start_nginx
fi
cat "$logfile"
pgrep -x nginx >/dev/null 2>&1 && echo "__NGINX_RUNNING__"`
	rOut, rerr := m.exec(hostID, script)
	rOut = strings.TrimSpace(rOut)
	if rOut != "" {
		output += "\n" + rOut
	}
	if rerr != nil || !strings.Contains(rOut, "__NGINX_RUNNING__") {
		// 诊断辅助：进程 / systemd / 最近日志，帮助定位“为何起不来”
		diag, _ := m.execFallback(hostID,
			`echo __SHELL__=$0; command -v nginx; pgrep -x nginx >/dev/null 2>&1 && echo __MASTER_RUNNING__ || echo __MASTER_STOPPED__; if command -v systemctl >/dev/null 2>&1; then systemctl is-active nginx 2>&1 | head -1; fi; if command -v journalctl >/dev/null 2>&1; then journalctl -u nginx -n 6 --no-pager 2>&1 | tail -6; fi`,
			`echo __SHELL_FALLBACK__=$0; command -v nginx; pgrep -x nginx >/dev/null 2>&1 && echo __MASTER_RUNNING__ || echo __MASTER_STOPPED__; if command -v systemctl >/dev/null 2>&1; then systemctl is-active nginx 2>&1 | head -1; fi`)
		if diag != "" {
			output += "\n[diag] " + strings.TrimSpace(diag)
		}
		return DeployResult{Output: output}, fmt.Errorf("nginx reload 失败: %s", rOut)
	}
	return DeployResult{Output: output}, nil
}
