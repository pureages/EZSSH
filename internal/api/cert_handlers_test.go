package api

import (
	"strings"
	"testing"
)

func TestIssueReqValidate(t *testing.T) {
	cases := []struct {
		name    string
		req     issueReq
		wantOK  bool
		wantDom string // 归一化后的 domain（仅校验通过时断言）
	}{
		{
			name:    "single domain lowercased",
			req:     issueReq{HostID: "h1", Domain: "Example.COM", Method: "dns", DNSAccountID: "d1"},
			wantOK:  true,
			wantDom: "example.com",
		},
		{
			name:    "multi domain with mixed separators",
			req:     issueReq{HostID: "h1", Domain: "a.com, *.a.com\n b.com;a.com", Method: "dns", DNSAccountID: "d1"},
			wantOK:  true,
			wantDom: "a.com,*.a.com,b.com",
		},
		{
			name:    "wildcard with dns",
			req:     issueReq{HostID: "h1", Domain: "*.wyj.me", Method: "dns", DNSAccountID: "d1"},
			wantOK:  true,
			wantDom: "*.wyj.me",
		},
		{
			name:   "wildcard with http rejected",
			req:    issueReq{HostID: "h1", Domain: "*.wyj.me", Method: "http", Webroot: "/w"},
			wantOK: false,
		},
		{
			name:   "wildcard on TLD rejected",
			req:    issueReq{HostID: "h1", Domain: "*.com", Method: "dns", DNSAccountID: "d1"},
			wantOK: false,
		},
		{
			name:   "wildcard not leftmost rejected",
			req:    issueReq{HostID: "h1", Domain: "a.*.com", Method: "dns", DNSAccountID: "d1"},
			wantOK: false,
		},
		{
			name:   "empty domain rejected",
			req:    issueReq{HostID: "h1", Domain: "  ,, ", Method: "dns", DNSAccountID: "d1"},
			wantOK: false,
		},
		{
			name:   "missing host rejected",
			req:    issueReq{Domain: "a.com", Method: "dns", DNSAccountID: "d1"},
			wantOK: false,
		},
		{
			name:   "dns without account rejected",
			req:    issueReq{HostID: "h1", Domain: "a.com", Method: "dns"},
			wantOK: false,
		},
		{
			name:   "unknown method rejected",
			req:    issueReq{HostID: "h1", Domain: "a.com", Method: "tls-alpn"},
			wantOK: false,
		},
	}

	for _, c := range cases {
		req := c.req
		msg, ok := req.validate()
		if ok != c.wantOK {
			t.Errorf("%s: ok=%v (msg=%q) want ok=%v", c.name, ok, msg, c.wantOK)
			continue
		}
		if !c.wantOK {
			continue
		}
		if req.Domain != c.wantDom {
			t.Errorf("%s: domain=%q want %q", c.name, req.Domain, c.wantDom)
		}
	}
}

func TestIssueReqValidateTooManyDomains(t *testing.T) {
	var parts []string
	for i := 0; i < maxCertDomains+1; i++ {
		parts = append(parts, "d"+string(rune('a'+i))+".com")
	}
	req := issueReq{HostID: "h1", Domain: strings.Join(parts, ","), Method: "dns", DNSAccountID: "d1"}
	if _, ok := req.validate(); ok {
		t.Fatalf("expected too-many-domains to be rejected")
	}
}

func TestValidSiteDomainAllowsLeadingWildcard(t *testing.T) {
	ok := []string{"example.com", "www.example.com", "*.example.com", "*.sub.example.com", "localhost"}
	bad := []string{"", "*", "*.com", "a.*.com", "*.", "*example.com", "exa mple.com"}
	for _, d := range ok {
		if !validSiteDomain(d) {
			t.Errorf("validSiteDomain(%q) = false, want true", d)
		}
	}
	for _, d := range bad {
		if validSiteDomain(d) {
			t.Errorf("validSiteDomain(%q) = true, want false", d)
		}
	}
}
