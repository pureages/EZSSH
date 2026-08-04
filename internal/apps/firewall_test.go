package apps

import (
	"testing"
)

// TestBuildSpec 锁定 ufw 规则命令构造。这些命令均已在真实 ufw 0.36.1 / 0.36.2 主机上验证生效。
func TestBuildSpec(t *testing.T) {
	m := &FirewallManager{}
	cases := []struct {
		name string
		in   RuleSpec
		want string
	}{
		{"单端口无协议", RuleSpec{Action: "allow", Proto: "", Port: "80"}, "allow 80"},
		{"单端口tcp", RuleSpec{Action: "allow", Proto: "tcp", Port: "80"}, "allow 80/tcp"},
		{"端口范围tcp", RuleSpec{Action: "allow", Proto: "tcp", Port: "49000:50000"}, "allow 49000:50000/tcp"},
		{"端口udp", RuleSpec{Action: "deny", Proto: "udp", Port: "25"}, "deny 25/udp"},
		{"仅来源IP", RuleSpec{Action: "allow", Proto: "", Port: "", From: "1.2.3.4"}, "allow from 1.2.3.4"},
		{"仅来源IP禁止", RuleSpec{Action: "deny", Proto: "", Port: "", From: "5.6.7.8"}, "deny from 5.6.7.8"},
		{"来源+单端口无协议", RuleSpec{Action: "allow", Proto: "", Port: "8080", From: "1.2.3.4"}, "allow from 1.2.3.4 to any port 8080"},
		{"来源+单端口带协议", RuleSpec{Action: "allow", Proto: "tcp", Port: "8080", From: "1.2.3.4"}, "allow proto tcp from 1.2.3.4 to any port 8080"},
		{"来源+端口范围", RuleSpec{Action: "allow", Proto: "tcp", Port: "49000:50000", From: "162.1.2.3"}, "allow proto tcp from 162.1.2.3 to any port 49000:50000"},
		{"来源+端口范围udp", RuleSpec{Action: "deny", Proto: "udp", Port: "49000:50000", From: "9.9.9.9"}, "deny proto udp from 9.9.9.9 to any port 49000:50000"},
		{"来源+端口reject", RuleSpec{Action: "reject", Proto: "", Port: "8080", From: "7.7.7.7"}, "reject from 7.7.7.7 to any port 8080"},
	}
	for _, c := range cases {
		got, err := m.buildSpec(c.in)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestBuildSpecRangeRequiresProto 端口范围必须显式指定协议。
func TestBuildSpecRangeRequiresProto(t *testing.T) {
	m := &FirewallManager{}
	_, err := m.buildSpec(RuleSpec{Action: "allow", Proto: "", Port: "49000:50000"})
	if err == nil {
		t.Error("expected error for port range without protocol")
	}
}

// TestParseRules 解析 user.rules 的 tuple 行（内容为真实主机上抓取的结果）。
func TestParseRules(t *testing.T) {
	m := &FirewallManager{}
	content := `# ifupdown has been replaced by netplan(5) on this system.  See
# /etc/netplan/*.yaml for all interface configuration.
*filter
:ufw-before-input - [0:0]
:ufw-before-output - [0:0]
:ufw-before-forward - [0:0]
:ufw-not-local - [0:0]
:ufw-after-input - [0:0]
:ufw-after-output - [0:0]
:ufw-after-forward - [0:0]
:ufw-reject-input - [0:0]
:ufw-reject-output - [0:0]
:ufw-reject-forward - [0:0]
:ufw-track-input - [0:0]
:ufw-track-output - [0:0]
:ufw-track-forward - [0:0]
:ufw-logging-deny - [0:0]
:ufw-logging-allow - [0:0]
:ufw-user-input - [0:0]
:ufw-user-output - [0:0]
:ufw-user-forward - [0:0]
-A ufw-user-input -p tcp --dport 80 -j ACCEPT
### tuple ### allow tcp 80 0.0.0.0/0 any 0.0.0.0/0 in
-A ufw-user-input -p tcp --dport 8080 -s 1.2.3.4 -j ACCEPT
### tuple ### allow tcp 8080 0.0.0.0/0 any 1.2.3.4 in
-A ufw-user-input -p udp --dport 49000:50000 -s 162.1.2.3 -j ACCEPT
### tuple ### allow udp 49000:50000 0.0.0.0/0 any 162.1.2.3 in
-A ufw-user-input -s 5.6.7.8 -j DROP
### tuple ### deny any any 0.0.0.0/0 any 5.6.7.8 in
-A ufw-user-input -p udp --dport 25 -j DROP
### tuple ### deny udp 25 0.0.0.0/0 any 0.0.0.0/0 in
COMMIT
`
	m.hub = nil // ListRules 只用到本地解析，不执行远程命令
	// ListRules 本身需要 SSH，这里直接调用私有解析逻辑验证字段。
	rules := m.parseRules(content)
	if len(rules) != 5 {
		t.Fatalf("expected 5 rules, got %d: %+v", len(rules), rules)
	}
	wants := []FirewallRule{
		{Action: "allow", Proto: "tcp", Port: "80", From: "any"},
		{Action: "allow", Proto: "tcp", Port: "8080", From: "1.2.3.4"},
		{Action: "allow", Proto: "udp", Port: "49000:50000", From: "162.1.2.3"},
		{Action: "deny", Proto: "any", Port: "any", From: "5.6.7.8"},
		{Action: "deny", Proto: "udp", Port: "25", From: "any"},
	}
	for i, want := range wants {
		r := rules[i]
		if r.Action != want.Action || r.Proto != want.Proto || r.Port != want.Port || r.From != want.From {
			t.Errorf("rule %d: got %+v, want %+v", i, r, want)
		}
	}
}
