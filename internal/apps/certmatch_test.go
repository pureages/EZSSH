package apps

import (
	"strings"
	"testing"

	"ezssh/internal/store"
)

func TestCertDirNameCandidates(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"www.wyj.me", []string{"www.wyj.me", "*.wyj.me"}},
		{"wyj.me", []string{"wyj.me"}},
		{"*.wyj.me", []string{"*.wyj.me"}},
		{"a.b.wyj.me", []string{"a.b.wyj.me", "*.b.wyj.me"}},
		{"WWW.WYJ.ME", []string{"www.wyj.me", "*.wyj.me"}},
		{" www.wyj.me ", []string{"www.wyj.me", "*.wyj.me"}},
		{"www.wyj.me.", []string{"www.wyj.me", "*.wyj.me"}},
		{"", nil},
	}
	for _, c := range cases {
		got := CertDirNameCandidates(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("%q: got %v, want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%q: got %v, want %v", c.in, got, c.want)
			}
		}
	}
}

// fakeResolver 只依据「命令中引用的证书目录名是否在 have 中」回答 YES/NO。
type fakeResolver struct{ have map[string]bool }

func (f fakeResolver) exec(_ string, cmd string) (string, error) {
	const pre, suf = "/etc/nginx/ssl/", "/fullchain.pem"
	i := strings.Index(cmd, pre)
	j := strings.Index(cmd, suf)
	if i < 0 || j < 0 || j < i {
		return "NO\n", nil
	}
	if f.have[cmd[i+len(pre):j]] {
		return "YES\n", nil
	}
	return "NO\n", nil
}

func TestResolveCertDirName(t *testing.T) {
	// 泛域名证书覆盖其下一级域名
	wild := fakeResolver{have: map[string]bool{"*.wyj.me": true}}
	if name, ok := resolveCertDirName(wild, "h1", []string{"www.wyj.me"}); !ok || name != "*.wyj.me" {
		t.Fatalf("wildcard: got %q ok=%v", name, ok)
	}
	// 精确目录优先于泛域名目录
	both := fakeResolver{have: map[string]bool{"*.wyj.me": true, "www.wyj.me": true}}
	if name, ok := resolveCertDirName(both, "h1", []string{"www.wyj.me"}); !ok || name != "www.wyj.me" {
		t.Fatalf("exact-first: got %q ok=%v", name, ok)
	}
	// 多域名：任一域名命中即可（wyj.me 无证书，www.wyj.me 命中泛域名）
	if name, ok := resolveCertDirName(wild, "h1", []string{"wyj.me", "www.wyj.me"}); !ok || name != "*.wyj.me" {
		t.Fatalf("multi-domain: got %q ok=%v", name, ok)
	}
	// 泛域名不覆盖裸域，也不覆盖二级子域（TLS 通配只覆盖一级）
	if _, ok := resolveCertDirName(wild, "h1", []string{"wyj.me"}); ok {
		t.Fatal("apex should not match wildcard")
	}
	if _, ok := resolveCertDirName(wild, "h1", []string{"a.b.wyj.me"}); ok {
		t.Fatal("second-level subdomain should not match *.wyj.me")
	}
	// 无任何证书
	if _, ok := resolveCertDirName(fakeResolver{have: map[string]bool{}}, "h1", []string{"www.wyj.me"}); ok {
		t.Fatal("no cert should not resolve")
	}
}

func TestGenerateConfigUsesCertName(t *testing.T) {
	site := &store.Website{ID: "s1", Domains: "www.wyj.me", SiteType: "static", SSL: true}
	conf := GenerateConfig(site, true, "*.wyj.me")
	if !strings.Contains(conf, "/etc/nginx/ssl/*.wyj.me/fullchain.pem") {
		t.Fatalf("missing wildcard cert path:\n%s", conf)
	}
	if !strings.Contains(conf, "/etc/nginx/ssl/*.wyj.me/key.pem") {
		t.Fatalf("missing wildcard key path:\n%s", conf)
	}
	if !strings.Contains(conf, "listen 443 ssl;") {
		t.Fatalf("missing 443 block:\n%s", conf)
	}
	// 未指定证书目录名时退回站点主域名（保持原行为）
	if conf2 := GenerateConfig(site, true, ""); !strings.Contains(conf2, "/etc/nginx/ssl/www.wyj.me/fullchain.pem") {
		t.Fatalf("fallback to primary domain failed:\n%s", conf2)
	}
}
