package apps

import (
	"fmt"
	"strings"

	"ezssh/internal/sshhub"
)

// FirewallManager 通过目标机 ufw 命令管理防火墙。
// 支持：状态检测、打开/关闭、规则列表（禁IP/允许IP/禁端口/允许端口/端口范围）。
type FirewallManager struct {
	hub *sshhub.Hub
}

func NewFirewallManager(hub *sshhub.Hub) *FirewallManager {
	return &FirewallManager{hub: hub}
}

// FirewallStatus 防火墙整体状态。
type FirewallStatus struct {
	Supported bool   `json:"supported"` // 目标机是否可用受支持的防火墙后端
	Backend   string `json:"backend"`   // 当前后端，如 "ufw"
	Active    bool   `json:"active"`    // 是否已启用（生效）
	Version   string `json:"version"`   // 后端版本（如 ufw 0.36.1）
	SSHPort   string `json:"sshPort"`   // 检测到的 ssh 监听端口，启用前会放行
}

// FirewallRule 一条防火墙规则（由 /etc/ufw/user.rules 的 tuple 解析而来）。
type FirewallRule struct {
	ID          string `json:"id"`   // 稳定标识，用于删除定位
	Action      string `json:"action"` // allow | deny | reject
	Proto       string `json:"proto"`  // tcp | udp | any
	Port        string `json:"port"`   // "8080"、"49000:50000"，无端口时为 "any"
	From        string `json:"from"`   // 来源 IP，无来源时为 "any"
	Description string `json:"description"`
}

// run 在目标机执行命令，失败（非零退出码）时返回错误并带上输出。
func (m *FirewallManager) run(hostID, cmd string) (string, error) {
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
		return "", fmt.Errorf("firewall: %s", msg)
	}
	return string(out), nil
}

func shellQuoteFW(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// platform 返回目标机平台，默认 linux（与既有逻辑零变化）。
func (m *FirewallManager) platform(hostID string) string {
	p, err := m.hub.Platform(hostID)
	if err != nil || p == "" {
		return "linux"
	}
	return p
}

// resolveUFW 定位目标机上的 ufw 可执行文件（非交互 PATH 常不含 /usr/sbin）。
// 返回空串表示未安装 ufw。
func (m *FirewallManager) resolveUFW(hostID string) (string, error) {
	for _, path := range []string{"ufw", "/usr/sbin/ufw", "/sbin/ufw"} {
		out, err := m.run(hostID, fmt.Sprintf(`command -v %s 2>/dev/null || echo __NOT_FOUND__`, path))
		if err != nil {
			return "", err
		}
		out = strings.TrimSpace(out)
		if out != "" && out != "__NOT_FOUND__" {
			return out, nil
		}
	}
	return "", nil
}

// Status 返回防火墙状态。
func (m *FirewallManager) Status(hostID string) (FirewallStatus, error) {
	if m.platform(hostID) == "windows" {
		return m.statusWindows(hostID)
	}
	ufw, err := m.resolveUFW(hostID)
	if err != nil {
		return FirewallStatus{}, err
	}
	st := FirewallStatus{Backend: "ufw"}
	if ufw == "" {
		st.Supported = false
		return st, nil
	}
	st.Supported = true

	// ufw status 输出含 "Status: active" 或 "Status: inactive"
	out, err := m.run(hostID, ufw+" status")
	if err != nil {
		// 未启用时 ufw status 也可能返回非零？实测 inactive 返回 0，active 返回 0。
		// 失败则直接视为不可用，交由上层提示。
		return st, err
	}
	if strings.Contains(out, "Status: active") {
		st.Active = true
	}
	// 提取版本号（ufw 0.36.1）
	verOut, err := m.run(hostID, ufw+" version")
	if err == nil {
		for _, line := range strings.Split(verOut, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "ufw") {
				st.Version = line
				break
			}
		}
	}
	// 检测 ssh 端口
	if sshPort, err := m.sshPort(hostID); err == nil && sshPort != "" {
		st.SSHPort = sshPort
	}
	return st, nil
}

// sshPort 读取 sshd 实际监听端口。
// 用 sed 提取（避免在双引号命令中出现 $2 等被外层 shell 展开）。
func (m *FirewallManager) sshPort(hostID string) (string, error) {
	out, err := m.run(hostID, `sshd -T 2>/dev/null | sed -n 's/^port //p'`)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// SetEnabled 打开或关闭防火墙。
// 打开前自动放行 ssh 端口，避免把自己锁在外面。
func (m *FirewallManager) SetEnabled(hostID string, enabled bool) error {
	if m.platform(hostID) == "windows" {
		return m.setEnabledWindows(hostID, enabled)
	}
	ufw, err := m.resolveUFW(hostID)
	if err != nil {
		return err
	}
	if ufw == "" {
		return fmt.Errorf("目标机未安装 ufw 防火墙")
	}
	if !enabled {
		_, err := m.run(hostID, ufw+" disable")
		return err
	}

	// 先确保 ssh 端口放行，再启用
	sshPort, err := m.sshPort(hostID)
	if err != nil || sshPort == "" {
		sshPort = "22"
	}
	if !m.hasPortAllowRule(hostID, sshPort) {
		if _, err := m.run(hostID, fmt.Sprintf(`%s allow proto tcp from any to any port %s`, ufw, sshPort)); err != nil {
			return fmt.Errorf("自动放行 ssh 端口 %s 失败: %w", sshPort, err)
		}
	}
	_, err = m.run(hostID, ufw+" --force enable")
	return err
}

// hasPortAllowRule 判断是否存在指定端口的放行规则（不区分来源/协议，够用于 ssh 自保）。
func (m *FirewallManager) hasPortAllowRule(hostID, port string) bool {
	// 直接查 user.rules 中该端口的 allow 条目
	cmd := fmt.Sprintf(`grep -q "### tuple ### allow .* %s " /etc/ufw/user.rules 2>/dev/null`, port)
	if _, err := m.run(hostID, cmd); err == nil {
		return true
	}
	return false
}

// ListRules 解析 /etc/ufw/user.rules 中的 tuple 行得到规则列表。
func (m *FirewallManager) ListRules(hostID string) ([]FirewallRule, error) {
	if m.platform(hostID) == "windows" {
		return m.listRulesWindows(hostID)
	}
	out, err := m.run(hostID, `cat /etc/ufw/user.rules 2>/dev/null`)
	if err != nil {
		return nil, err
	}
	return m.parseRules(out), nil
}

// parseRules 解析 user.rules 内容中的 tuple 行。
// tuple 格式：### tuple ### <action> <proto> <dport> <dstnet> <sport> <src> <dir>
// src 字段为 0.0.0.0/0 或 any 时表示无来源限制；dport 为 any 时表示无端口限制。
func (m *FirewallManager) parseRules(content string) []FirewallRule {
	var rules []FirewallRule
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "### tuple ###") {
			continue
		}
		fields := strings.Fields(line)
		// fields: ["###","tuple","###", action, proto, dport, dstnet, sport, src, dir]
		if len(fields) < 10 {
			continue
		}
		action := fields[3]
		proto := fields[4]
		if proto == "any" || proto == "" {
			proto = "any"
		}
		dport := fields[5]
		if dport == "" || dport == "any" {
			dport = "any"
		}
		src := fields[8]
		if src == "0.0.0.0/0" || src == "::/0" || src == "any" {
			src = "any"
		}
		rules = append(rules, FirewallRule{
			ID:     fmt.Sprintf("%s|%s|%s|%s", action, proto, dport, src),
			Action: action,
			Proto:  proto,
			Port:   dport,
			From:   src,
		})
	}
	// 按文件顺序返回（即添加顺序）
	return rules
}

// RuleSpec 一条待添加/删除的规则。
type RuleSpec struct {
	Action string `json:"action"` // allow | deny | reject
	Proto  string `json:"proto"`  // tcp | udp | any（any 时省略）
	Port   string `json:"port"`   // 端口或端口范围 "49000:50000"，空表示不限端口
	From   string `json:"from"`   // 来源 IP，空表示不限来源
}

// buildSpec 按规则生成 ufw 子命令（不含 "ufw " 前缀），供 add/delete 共用。
// 已验证的语法：
//   端口+协议:        allow 80/tcp            → ufw allow 80/tcp
//   端口范围:         allow 49000:50000/tcp   → ufw allow 49000:50000/tcp
//   来源IP:           allow from 1.2.3.4      → ufw allow from 1.2.3.4
//   来源IP+单端口:     allow from 1.2.3.4 to any port 8080
//   来源IP+端口范围:    allow proto tcp from 1.2.3.4 to any port 49000:50000
func (m *FirewallManager) buildSpec(s RuleSpec) (string, error) {
	action := strings.ToLower(strings.TrimSpace(s.Action))
	switch action {
	case "allow", "deny", "reject":
	default:
		return "", fmt.Errorf("不支持的规则动作：%s", s.Action)
	}
	proto := strings.ToLower(strings.TrimSpace(s.Proto))
	port := strings.TrimSpace(s.Port)
	from := strings.TrimSpace(s.From)

	// 端口范围必须显式指定协议
	if strings.Contains(port, ":") && proto == "" {
		return "", fmt.Errorf("端口范围操作必须指定协议（tcp 或 udp）")
	}

	var parts []string
	if from != "" {
		parts = append(parts, action, "from", from)
		if port != "" {
			parts = append(parts, "to", "any", "port", port)
			// 有端口时若指定了协议，用带 proto 的写法（端口范围必须带 proto）
			if proto != "" {
				// 把 proto 插到 from 前面
				parts = []string{action, "proto", proto, "from", from, "to", "any", "port", port}
			}
		}
	} else if port != "" {
		p := port
		if proto != "" && proto != "any" {
			p = port + "/" + proto
		}
		parts = []string{action, p}
	} else {
		// 既无来源也无端口 → 无效规则
		return "", fmt.Errorf("必须指定来源 IP 或端口")
	}
	return strings.Join(parts, " "), nil
}

// AddRule 添加一条防火墙规则。
func (m *FirewallManager) AddRule(hostID string, s RuleSpec) (string, error) {
	if m.platform(hostID) == "windows" {
		return m.addRuleWindows(hostID, s)
	}
	ufw, err := m.resolveUFW(hostID)
	if err != nil {
		return "", err
	}
	if ufw == "" {
		return "", fmt.Errorf("目标机未安装 ufw 防火墙")
	}
	spec, err := m.buildSpec(s)
	if err != nil {
		return "", err
	}
	out, err := m.run(hostID, ufw+" "+spec)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// RemoveRule 删除一条规则，spec 与添加时一致。
func (m *FirewallManager) RemoveRule(hostID string, s RuleSpec) (string, error) {
	if m.platform(hostID) == "windows" {
		return m.removeRuleWindows(hostID, s)
	}
	ufw, err := m.resolveUFW(hostID)
	if err != nil {
		return "", err
	}
	if ufw == "" {
		return "", fmt.Errorf("目标机未安装 ufw 防火墙")
	}
	spec, err := m.buildSpec(s)
	if err != nil {
		return "", err
	}
	out, err := m.run(hostID, ufw+" delete "+spec)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ---- Windows 防火墙（NetSecurity）----

// winFwStatusScript 采集 Windows 防火墙状态，逐行输出 ACTIVE/VER/PORT（\t 分隔）。
const winFwStatusScript = "$profiles = Get-NetFirewallProfile -ErrorAction SilentlyContinue\n" +
	"$on = ($profiles | Where-Object { $_.Enabled } | Measure-Object).Count\n" +
	"if ($on -gt 0) { Write-Output 'ACTIVE\tTrue' } else { Write-Output 'ACTIVE\tFalse' }\n" +
	"$mod = Get-Module NetSecurity -ListAvailable -ErrorAction SilentlyContinue | Select-Object -First 1\n" +
	"$ver = ''\n" +
	"if ($mod) { $ver = [string]$mod.Version }\n" +
	"Write-Output ('VER\t' + $ver)\n" +
	"$p = '22'\n" +
	"if (Test-Path \"$env:ProgramData\\ssh\\sshd_config\") {\n" +
	"  $m = Select-String -Path \"$env:ProgramData\\ssh\\sshd_config\" -Pattern '^\\s*Port\\s+(\\d+)'\n" +
	"  if ($m) { $p = $m.Matches[0].Groups[1].Value }\n" +
	"}\n" +
	"Write-Output ('PORT\t' + $p)"

// winFwSetEnabledScript 打开/关闭三个配置文件（Domain/Private/Public）。
func winFwSetEnabledScript(enabled bool) string {
	v := "False"
	if enabled {
		v = "True"
	}
	return "Set-NetFirewallProfile -Profile Domain,Private,Public -Enabled " + v + " -ErrorAction Stop\n" +
		"Write-Output 'OK'"
}

// winFwPortScript 读取 Win32-OpenSSH sshd_config 的 Port（未配置默认 22）。
const winFwPortScript = "$p = '22'\n" +
	"if (Test-Path \"$env:ProgramData\\ssh\\sshd_config\") {\n" +
	"  $m = Select-String -Path \"$env:ProgramData\\ssh\\sshd_config\" -Pattern '^\\s*Port\\s+(\\d+)'\n" +
	"  if ($m) { $p = $m.Matches[0].Groups[1].Value }\n" +
	"}\n" +
	"Write-Output $p"

// winFwListScript 列出本工具创建的规则（DisplayName 以 EZSSH- 开头），避免把系统内置的
// 上千条 Windows 防火墙规则铺到前端。输出行：R\tDisplayName\tAction\tProtocol\tLocalPort\tRemoteAddress。
const winFwListScript = "Get-NetFirewallRule -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName -like 'EZSSH-*' } | ForEach-Object {\n" +
	"  $r = $_\n" +
	"  $pf = $r | Get-NetFirewallPortFilter -ErrorAction SilentlyContinue\n" +
	"  $af = $r | Get-NetFirewallAddressFilter -ErrorAction SilentlyContinue\n" +
	"  $proto = ''; if ($pf) { $proto = [string]$pf.Protocol }\n" +
	"  $lp = ''; if ($pf) { $lp = [string]($pf.LocalPort -join ',') }\n" +
	"  $ra = ''; if ($af) { $ra = [string]($af.RemoteAddress -join ',') }\n" +
	"  Write-Output ('R\t' + $r.DisplayName + '\t' + $r.Action + '\t' + $proto + '\t' + $lp + '\t' + $ra)\n" +
	"}"

// windowsRuleName 由规则 spec 生成幂等 DisplayName。
// 前端删除规则时按 spec 还原（与 Linux buildSpec 对称），故 DisplayName 用确定性拼接而非随机后缀。
func windowsRuleName(s RuleSpec) string {
	action := strings.ToLower(strings.TrimSpace(s.Action))
	if action != "deny" && action != "reject" {
		action = "allow"
	} else {
		action = "deny"
	}
	proto := strings.ToLower(strings.TrimSpace(s.Proto))
	if proto != "tcp" && proto != "udp" {
		proto = "any"
	}
	port := strings.TrimSpace(s.Port)
	if port == "" {
		port = "any"
	}
	from := strings.TrimSpace(s.From)
	if from == "" {
		from = "any"
	}
	return "EZSSH-" + action + "-" + proto + "-" + port + "-" + from
}

// winFwErr 为 Windows 防火墙权限类错误补充"需要管理员权限"提示。
func (m *FirewallManager) winFwErr(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "access is denied") || strings.Contains(msg, "0x80070005") ||
		strings.Contains(msg, "拒绝") || strings.Contains(msg, "privilege") {
		return fmt.Errorf("%s（可能需要管理员权限）", err.Error())
	}
	return err
}

func (m *FirewallManager) statusWindows(hostID string) (FirewallStatus, error) {
	out, err := m.run(hostID, winPS(winFwStatusScript))
	if err != nil {
		return FirewallStatus{}, err
	}
	st := FirewallStatus{Backend: "windows-fw", Supported: true}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "ACTIVE\t"):
			st.Active = strings.EqualFold(strings.TrimSpace(line[len("ACTIVE\t"):]), "true")
		case strings.HasPrefix(line, "VER\t"):
			st.Version = strings.TrimSpace(line[len("VER\t"):])
		case strings.HasPrefix(line, "PORT\t"):
			st.SSHPort = strings.TrimSpace(line[len("PORT\t"):])
		}
	}
	return st, nil
}

func (m *FirewallManager) winFwPort(hostID string) (string, error) {
	out, err := m.run(hostID, winPS(winFwPortScript))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ensureWindowsSSHAllow 启用防火墙前确保 ssh 端口入站放行，避免把自己锁在外面。
func (m *FirewallManager) ensureWindowsSSHAllow(hostID, port string) error {
	script := "if (-not (Get-NetFirewallRule -DisplayName 'EZSSH-ssh-autoallow' -ErrorAction SilentlyContinue)) {\n" +
		"  New-NetFirewallRule -DisplayName 'EZSSH-ssh-autoallow' -Direction Inbound -Action Allow -Protocol TCP -LocalPort " + port + " -Profile Any | Out-Null\n" +
		"}"
	_, err := m.run(hostID, winPS(script))
	return m.winFwErr(err)
}

func (m *FirewallManager) setEnabledWindows(hostID string, enabled bool) error {
	if !enabled {
		_, err := m.run(hostID, winPS(winFwSetEnabledScript(false)))
		return m.winFwErr(err)
	}
	sshPort := "22"
	if p, err := m.winFwPort(hostID); err == nil && p != "" {
		sshPort = p
	}
	if err := m.ensureWindowsSSHAllow(hostID, sshPort); err != nil {
		return fmt.Errorf("自动放行 ssh 端口 %s 失败: %w", sshPort, err)
	}
	_, err := m.run(hostID, winPS(winFwSetEnabledScript(true)))
	return m.winFwErr(err)
}

func (m *FirewallManager) listRulesWindows(hostID string) ([]FirewallRule, error) {
	out, err := m.run(hostID, winPS(winFwListScript))
	if err != nil {
		return nil, m.winFwErr(err)
	}
	return m.parseWindowsRules(out), nil
}

// parseWindowsRules 解析 winFwListScript 输出。
func (m *FirewallManager) parseWindowsRules(out string) []FirewallRule {
	var rules []FirewallRule
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "R\t") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 6 {
			continue
		}
		action := "deny"
		switch strings.ToLower(f[2]) {
		case "allow":
			action = "allow"
		case "block":
			action = "deny"
		default:
			continue // NotConfigured 等非用户规则
		}
		proto := strings.ToLower(strings.TrimSpace(f[3]))
		if proto != "tcp" && proto != "udp" {
			proto = "any"
		}
		port := strings.ReplaceAll(strings.TrimSpace(f[4]), "-", ":")
		if port == "" {
			port = "any"
		}
		from := strings.TrimSpace(f[5])
		if from == "" || strings.EqualFold(from, "any") {
			from = "any"
		}
		rules = append(rules, FirewallRule{
			ID:          fmt.Sprintf("%s|%s|%s|%s", action, proto, port, from),
			Action:      action,
			Proto:       proto,
			Port:        port,
			From:        from,
			Description: f[1],
		})
	}
	return rules
}

func (m *FirewallManager) addRuleWindows(hostID string, s RuleSpec) (string, error) {
	name := windowsRuleName(s)
	action := "Allow"
	if strings.ToLower(strings.TrimSpace(s.Action)) == "deny" || strings.ToLower(strings.TrimSpace(s.Action)) == "reject" {
		action = "Block"
	}
	proto := strings.ToLower(strings.TrimSpace(s.Proto))
	port := strings.TrimSpace(s.Port)
	from := strings.TrimSpace(s.From)

	// 协议为空但有端口时按 ufw 语义放行 TCP+UDP 两条；协议 Any 且无端口则一条不限协议。
	type combo struct{ proto, port string }
	var combos []combo
	switch proto {
	case "tcp", "udp":
		combos = []combo{{proto: strings.ToUpper(proto), port: port}}
	case "":
		if port != "" {
			combos = []combo{{"TCP", port}, {"UDP", port}}
		} else {
			combos = []combo{{"", ""}}
		}
	default:
		combos = []combo{{"", ""}}
	}

	var lines []string
	for _, c := range combos {
		lines = append(lines, "$a = @{ DisplayName = "+psLiteral(name)+"; Direction = 'Inbound'; Action = '"+action+"'; Profile = 'Any' }")
		if c.proto != "" {
			lines = append(lines, "$a.Protocol = '"+c.proto+"'")
		}
		if c.port != "" {
			lines = append(lines, "$a.LocalPort = '"+strings.ReplaceAll(c.port, ":", "-")+"'")
		}
		if from != "" {
			lines = append(lines, "$a.RemoteAddress = "+psLiteral(from))
		}
		lines = append(lines, "New-NetFirewallRule @a | Out-Null")
	}
	lines = append(lines, "Write-Output "+psLiteral(name))
	out, err := m.run(hostID, winPS(strings.Join(lines, "\n")))
	if err != nil {
		return "", m.winFwErr(err)
	}
	return strings.TrimSpace(out), nil
}

func (m *FirewallManager) removeRuleWindows(hostID string, s RuleSpec) (string, error) {
	name := windowsRuleName(s)
	script := "$rules = Get-NetFirewallRule -DisplayName " + psLiteral(name) + " -ErrorAction SilentlyContinue\n" +
		"if (-not $rules) { Write-Output 'no rule'; exit 0 }\n" +
		"$n = ($rules | Measure-Object).Count\n" +
		"$rules | Remove-NetFirewallRule\n" +
		"Write-Output ('removed ' + $n)"
	out, err := m.run(hostID, winPS(script))
	if err != nil {
		return "", m.winFwErr(err)
	}
	return strings.TrimSpace(out), nil
}
