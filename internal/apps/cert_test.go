package apps

import (
	"strings"
	"testing"
)

// 多域名（SAN）+ 通配符：每个域名一个 -d，且域名一律单引号包裹（防止 shell 展开 *.a.com）。
func TestBuildIssueArgsMultiDomainWildcard(t *testing.T) {
	args, pre, primary, err := buildIssueArgs([]string{"wyj.me", "*.wyj.me", "static.wyj.me"}, "dns", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary != "wyj.me" {
		t.Errorf("primary = %q, want %q", primary, "wyj.me")
	}
	if pre != "" {
		t.Errorf("dns 方式不应需要前置脚本，got %q", pre)
	}
	for _, want := range []string{
		"--issue",
		"--dns dns_cf",
		"-d 'wyj.me'",
		"-d '*.wyj.me'",
		"-d 'static.wyj.me'",
		"--keylength ec-256",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args 缺少 %q: %s", want, args)
		}
	}
}

// HTTP-01（webroot）：多域名 + 预建 webroot 目录；缺少 webroot / 空域名应报错。
func TestBuildIssueArgsHTTPWebroot(t *testing.T) {
	args, pre, primary, err := buildIssueArgs([]string{"a.com", "b.com"}, "http", "/var/www/a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary != "a.com" {
		t.Errorf("primary = %q, want %q", primary, "a.com")
	}
	if !strings.Contains(args, "--webroot '/var/www/a'") {
		t.Errorf("args 缺少 --webroot: %s", args)
	}
	if !strings.Contains(args, "-d 'b.com'") {
		t.Errorf("args 缺少第二域名 -d: %s", args)
	}
	if !strings.Contains(pre, "mkdir -p '/var/www/a'") {
		t.Errorf("前置脚本应创建 webroot，got %q", pre)
	}

	if _, _, _, err := buildIssueArgs([]string{"a.com"}, "http", "  "); err == nil {
		t.Error("http 缺少 webroot 时应报错")
	}
	if _, _, _, err := buildIssueArgs(nil, "dns", ""); err == nil {
		t.Error("空域名列表应报错")
	}
}
