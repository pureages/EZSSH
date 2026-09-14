package apps

import "strings"

// certResolver 抽象「在目标机执行命令」：NginxManager 与 CertManager 都实现了 exec。
// 抽成接口便于对证书目录解析做单元测试（无需真实 SSH）。
type certResolver interface {
	exec(hostID, cmd string) (string, error)
}

// CertDirNameCandidates 返回该域名可能使用的证书目录名，按优先级排列：
//
//	1. 域名本身（精确匹配，也是多域名 SAN 证书的主域名目录）
//	2. 上一级泛域名（*.parent）：TLS 泛域名只覆盖一级，www.wyj.me → *.wyj.me
//
// 例：www.wyj.me → ["www.wyj.me", "*.wyj.me"]；wyj.me → ["wyj.me"]。
func CertDirNameCandidates(domain string) []string {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.TrimSuffix(d, ".")
	if d == "" {
		return nil
	}
	out := []string{d}
	if i := strings.Index(d, "."); i > 0 {
		parent := d[i+1:]
		// 上级至少形如 a.b，避免为裸后缀构造 *.me 这类无意义探测；
		// 输入本身就是泛域名（*.wyj.me）时无需重复追加。
		if w := "*." + parent; strings.Contains(parent, ".") && w != d {
			out = append(out, w)
		}
	}
	return out
}

// certInstalledIn 判断目标机是否已把证书安装到 /etc/nginx/ssl/<name>/。
func certInstalledIn(r certResolver, hostID, name string) bool {
	out, err := r.exec(hostID, `test -f `+sshQuote("/etc/nginx/ssl/"+name+"/fullchain.pem")+` && echo YES || echo NO`)
	return err == nil && strings.Contains(out, "YES")
}

// resolveCertDirName 在目标机上解析覆盖 domains 的证书目录名：
// 先按「精确域名」找，再按「上一级泛域名」找，首个命中即返回。
// 因此泛域名证书（*.wyj.me）可直接用于其下一级域名（www.wyj.me）。
// ok=false 表示没有任何可用证书。
func resolveCertDirName(r certResolver, hostID string, domains []string) (string, bool) {
	var exact, wild []string
	seen := map[string]bool{}
	for _, d := range domains {
		for i, name := range CertDirNameCandidates(d) {
			if seen[name] {
				continue
			}
			seen[name] = true
			if i == 0 {
				exact = append(exact, name)
			} else {
				wild = append(wild, name)
			}
		}
	}
	// 精确目录整体优先于泛域名目录：只要站点任一域名有专属证书就优先使用
	cands := make([]string, 0, len(exact)+len(wild))
	cands = append(cands, exact...)
	cands = append(cands, wild...)
	for _, name := range cands {
		if certInstalledIn(r, hostID, name) {
			return name, true
		}
	}
	return "", false
}
