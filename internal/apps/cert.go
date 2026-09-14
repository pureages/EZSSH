package apps

import (
	"fmt"
	"strings"
	"time"

	"ezssh/internal/sshhub"
	"ezssh/internal/store"
)

// CertManager 通过远程服务器上的 acme.sh 签发/续签 Let's Encrypt 证书。
// 支持 HTTP-01（--webroot）与 DNS-01（--dns dns_cf，Cloudflare API Token）：
//   - 多域名（SAN）：一条命令可携带多个 -d，第 1 个域名为主域名；
//   - 通配符域名（*.example.com）：只能走 DNS-01（HTTP-01 无法签发泛域名）。
//
// 证书经 --install-cert 安装到稳定的 /etc/nginx/ssl/<主域名>/ 路径供 nginx 引用，
// 并写入 reloadcmd（续签时自动 reload nginx）；acme.sh 自带 cron 兜底自动续签。
type CertManager struct {
	hub  *sshhub.Hub
	sftp *SFTPManager
}

// certPrimaryDomain 取域名串（逗号/空白分隔）中的主域名——acme.sh 以首个 -d 命名证书目录。
func certPrimaryDomain(domains string) string {
	if ds := store.SplitDomainList(domains); len(ds) > 0 {
		return ds[0]
	}
	return ""
}

func NewCertManager(hub *sshhub.Hub, sftp *SFTPManager) *CertManager {
	return &CertManager{hub: hub, sftp: sftp}
}

// CheckAcmeSh 检测目标机是否已安装 acme.sh。
func (m *CertManager) CheckAcmeSh(hostID string) (bool, error) {
	out, err := m.exec(hostID, `if command -v acme.sh >/dev/null 2>&1 || test -x "$HOME/.acme.sh/acme.sh"; then echo YES; else echo NO; fi`)
	if err != nil {
		return false, err
	}
	return strings.Contains(out, "YES"), nil
}

// EnsureAcmeSh 确保目标机安装 acme.sh（缺 curl 时先装）。
func (m *CertManager) EnsureAcmeSh(hostID string, onLine func(string)) error {
	installed, err := m.CheckAcmeSh(hostID)
	if err != nil {
		return err
	}
	if installed {
		onLine("acme.sh 已安装")
		return nil
	}
	script := `set -e
echo "==> 安装 acme.sh"
if ! command -v curl >/dev/null 2>&1; then
  PM=""
  if command -v apt-get >/dev/null 2>&1; then PM=apt
  elif command -v dnf >/dev/null 2>&1; then PM=dnf
  elif command -v yum >/dev/null 2>&1; then PM=yum
  elif command -v apk >/dev/null 2>&1; then PM=apk
  fi
  case "$PM" in
    apt) apt-get update -y && apt-get install -y curl ;;
    dnf) dnf install -y curl ;;
    yum) yum install -y curl ;;
    apk) apk add --no-cache curl ;;
    *) echo "错误：无法安装 curl" ; exit 1 ;;
  esac
fi
curl -fsSL https://get.acme.sh | sh
echo "==> acme.sh 安装完成"`
	return m.runScript(hostID, script, onLine)
}

// hasAcmeCert 判断远端 acme.sh 中是否已存在该域名的证书记录。
// 存在且未到期时，acme.sh --issue 会默认跳过（输出 "Skipping. Next renewal time..." 并以退出码 2 结束），
// 用户显式点击「签发」即为期望重新申请，需追加 --force。
func (m *CertManager) hasAcmeCert(hostID, domain string) bool {
	cmd := `test -d "$HOME/.acme.sh/` + domain + `_ecc" -o -d "$HOME/.acme.sh/` + domain + `" && echo YES || echo NO`
	out, err := m.exec(hostID, cmd)
	return err == nil && strings.Contains(out, "YES")
}

// buildIssueArgs 构建 acme.sh --issue 的验证参数（纯函数，便于测试）。
// 每个域名生成一个 -d（多域名写入同一张证书的 SAN 列表）；域名一律单引号包裹，
// 避免 shell 把通配符 *.example.com 当作文件名展开。
// 返回 issueArgs（含 --issue）、pre（前置脚本，httpwebroot 需要预建目录）与主域名（首个域名）。
func buildIssueArgs(domains []string, method, webroot string) (issueArgs, pre, primary string, err error) {
	if len(domains) == 0 {
		return "", "", "", fmt.Errorf("域名不能为空")
	}
	primary = domains[0]
	dArgs := ""
	for _, d := range domains {
		if d = strings.TrimSpace(d); d != "" {
			dArgs += " -d " + sshQuote(d)
		}
	}
	switch method {
	case "dns":
		// DNS-01：通配符域名只能走此方式
		return "--issue --dns dns_cf" + dArgs + " --keylength ec-256", "", primary, nil
	default:
		webroot = strings.TrimSpace(webroot)
		if webroot == "" {
			return "", "", "", fmt.Errorf("HTTP 验证需要提供网站根目录（webroot）")
		}
		// 确保 webroot 目录存在（acme.sh 会在其下创建 .well-known/acme-challenge）
		return "--issue --webroot " + sshQuote(webroot) + dArgs + " --keylength ec-256",
			"mkdir -p " + sshQuote(webroot) + "\n", primary, nil
	}
}

// Issue 签发证书，支持多域名（SAN）与通配符域名。
// domains 为域名列表（首项为主域名）；method=dns 时需传 Cloudflare API Token（cfToken），
// method=http 时用 webroot。签发成功自动安装到 /etc/nginx/ssl/<主域名>/ 并写入 reloadcmd。
// 若 acme.sh 已存在该主域名证书（如面板记录被删后再次签发），自动追加 --force 强制重新签发。
func (m *CertManager) Issue(hostID string, domains []string, method, cfToken, webroot string, onLine func(string)) error {
	issueArgs, pre, primary, err := buildIssueArgs(domains, method, webroot)
	if err != nil {
		return err
	}
	env := ""
	if method == "dns" {
		cfToken = strings.TrimSpace(cfToken)
		if cfToken == "" {
			return fmt.Errorf("DNS 验证需要提供 Cloudflare API Token")
		}
		// 凭据经环境变量注入（acme.sh dns_cf 读取），避免写盘
		env = "CF_Token=" + shellQuote(cfToken) + " "
	}
	if m.hasAcmeCert(hostID, primary) {
		onLine("==> 检测到 acme.sh 已存在该域名证书，追加 --force 强制重新签发")
		issueArgs += " --force"
	}

	cmd := env + `sh "$HOME/.acme.sh/acme.sh" ` + issueArgs +
		` --server letsencrypt 2>&1`
	// 注意：不能追加 `--log` —— acme.sh 的 --log 作为最后一个参数时
	// （后面没有值）会在参数解析处 shift 越界报 "shift: can't shift that many"，
	// 且我们已用 2>&1 流式捕获全部输出，无需 --log。
	script := "set -e\n" + pre + cmd + "\n"
	if err := m.runScript(hostID, script, onLine); err != nil {
		return fmt.Errorf("acme.sh 签发失败: %w", err)
	}

	if err := m.installCert(hostID, primary, onLine); err != nil {
		return err
	}
	onLine("==> 证书签发并安装完成")
	return nil
}

// installCert 安装证书到稳定路径并写入 reloadcmd（自动续签时 reload nginx）。
// 注意：acme.sh 的 --install-cert 不会自动创建目标目录（会报 touch: cannot touch ... No such file or directory），
// 必须先 mkdir -p 目标目录。
func (m *CertManager) installCert(hostID, domain string, onLine func(string)) error {
	dir := "/etc/nginx/ssl/" + domain
	onLine("==> 安装证书到 " + dir + "/")
	cmd := `mkdir -p ` + sshQuote(dir) + ` && ` +
		`sh "$HOME/.acme.sh/acme.sh" --install-cert -d ` + sshQuote(domain) +
		` --ecc --fullchain-file ` + sshQuote(dir+"/fullchain.pem") +
		` --key-file ` + sshQuote(dir+"/key.pem") +
		` --reloadcmd "nginx -s reload || systemctl reload nginx" 2>&1`
	script := "set -e\n" + cmd + "\n"
	if err := m.runScript(hostID, script, onLine); err != nil {
		return fmt.Errorf("安装证书失败: %w", err)
	}
	return nil
}

// Renew 续签证书。domains 可为逗号分隔的域名列表（多域名证书按主域名续签，SAN 会一并续期）。
// force=true 无条件续签（手动按钮）；自动续签不加 force，未到期时 acme.sh 自动跳过。
// 续签成功会自动重装证书到稳定路径并 reload nginx。
// 注意：命令末尾不追加 --log，原因同 Issue（--log 作末参触发 shift 越界 bug）。
// 续签前先 mkdir -p 证书目录，保证 acme.sh 重放保存的 --install-cert 钩子时目标目录存在。
func (m *CertManager) Renew(hostID, domains string, force bool, onLine func(string)) error {
	domain := certPrimaryDomain(domains)
	if domain == "" {
		return fmt.Errorf("域名不能为空")
	}
	args := "--renew -d " + sshQuote(domain) + " --ecc --server letsencrypt"
	if force {
		args += " --force"
	}
	pre := "mkdir -p " + sshQuote("/etc/nginx/ssl/"+domain) + "\n"
	cmd := `sh "$HOME/.acme.sh/acme.sh" ` + args + ` 2>&1`
	script := "set -e\n" + pre + cmd + "\n"
	return m.runScript(hostID, script, onLine)
}

// CertStatus 读取已安装证书的到期时间，返回 SQLite 可存格式（"2006-01-02 15:04:05"）。
// domains 可为逗号分隔的域名列表，取主域名定位安装目录；证书未安装时返回空串与错误。
func (m *CertManager) CertStatus(hostID, domains string) (string, error) {
	domain := certPrimaryDomain(domains)
	if domain == "" {
		return "", fmt.Errorf("域名不能为空")
	}
	return m.CertStatusByDir(hostID, domain)
}

// ResolveCertName 返回覆盖 domains、且已安装到 /etc/nginx/ssl/<name>/ 的证书目录名：
// 精确匹配优先，其次上一级泛域名目录（如 www.wyj.me → *.wyj.me）。ok=false 表示无可用证书。
func (m *CertManager) ResolveCertName(hostID string, domains []string) (string, bool) {
	return resolveCertDirName(m, hostID, domains)
}

// CertStatusByDir 读取指定证书目录名（/etc/nginx/ssl/<name>/）下证书的到期时间。
func (m *CertManager) CertStatusByDir(hostID, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("域名不能为空")
	}
	pem := "/etc/nginx/ssl/" + name + "/fullchain.pem"
	out, err := m.exec(hostID, `openssl x509 -enddate -noout -in `+sshQuote(pem)+` 2>/dev/null`)
	if err != nil || strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("证书文件不存在或无法读取")
	}
	out = strings.TrimSpace(out)
	idx := strings.Index(out, "notAfter=")
	if idx < 0 {
		return "", fmt.Errorf("无法解析证书到期时间: %s", out)
	}
	dateStr := strings.TrimSpace(out[idx+len("notAfter="):])
	t, err := time.Parse("Jan 2 15:04:05 2006 MST", dateStr)
	if err != nil {
		t, err = time.Parse("Jan _2 15:04:05 2006 MST", dateStr)
		if err != nil {
			return "", fmt.Errorf("解析到期时间 %q 失败: %v", dateStr, err)
		}
	}
	return t.UTC().Format("2006-01-02 15:04:05"), nil
}

// RemoveCert 移除 acme.sh 中的证书记录并删除安装目录。
// domains 可为逗号分隔的域名列表，取主域名为准（多域名证书在 acme.sh 中按主域名索引）。
func (m *CertManager) RemoveCert(hostID, domains string, onLine func(string)) error {
	domain := certPrimaryDomain(domains)
	if domain == "" {
		return fmt.Errorf("域名不能为空")
	}
	if installed, _ := m.CheckAcmeSh(hostID); installed {
		cmd := `sh "$HOME/.acme.sh/acme.sh" --remove -d ` + sshQuote(domain) + ` --ecc 2>&1`
		_ = m.runScript(hostID, "set -e\n"+cmd+"\n", onLine)
	}
	_, err := m.exec(hostID, `rm -rf `+sshQuote("/etc/nginx/ssl/"+domain))
	return err
}

// exec 在目标机执行命令并返回合并输出（复用 NginxManager 的实现）。
func (m *CertManager) exec(hostID, cmd string) (string, error) {
	return NewNginxManager(m.hub, m.sftp).exec(hostID, cmd)
}

// runScript 复用 NginxManager 的流式脚本执行。
func (m *CertManager) runScript(hostID, script string, onLine func(string)) error {
	return NewNginxManager(m.hub, m.sftp).runScript(hostID, script, onLine)
}
