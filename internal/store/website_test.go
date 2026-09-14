package store

import (
	"reflect"
	"testing"
)

func TestSplitDomainList(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "example.com", []string{"example.com"}},
		{"comma separated", "a.com,b.com", []string{"a.com", "b.com"}},
		{"mixed separators", " a.com, *.a.com\n b.com ;c.com ", []string{"a.com", "*.a.com", "b.com", "c.com"}},
		{"dedup and lowercase", "A.com, a.COM", []string{"a.com"}},
		{"wildcard", "*.wyj.me,wyj.me", []string{"*.wyj.me", "wyj.me"}},
		{"blank entries ignored", ",, a.com ,,", []string{"a.com"}},
	}
	for _, c := range cases {
		got := SplitDomainList(c.in)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestCertificateDomains(t *testing.T) {
	c := &Certificate{Domain: "wyj.me, *.wyj.me, static.wyj.me"}
	if got := c.PrimaryDomain(); got != "wyj.me" {
		t.Errorf("PrimaryDomain: got %q want %q", got, "wyj.me")
	}
	want := []string{"wyj.me", "*.wyj.me", "static.wyj.me"}
	if got := c.Domains(); !reflect.DeepEqual(got, want) {
		t.Errorf("Domains: got %v want %v", got, want)
	}
	empty := &Certificate{}
	if empty.PrimaryDomain() != "" || len(empty.Domains()) != 0 {
		t.Errorf("empty certificate should have no domains")
	}
}
