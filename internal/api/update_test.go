package api

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.0.1", "0.0.1", 0},
		{"v0.0.1", "0.0.1", 0},
		{"0.0.1", "0.0.2", -1},
		{"0.1.0", "0.0.9", 1},
		{"1.0.0", "0.9.9", 1},
		{"1.0", "1.0.0", 0},
		{"1.0.1", "1.0", 1},
		{"1.10", "1.9", 1},
		{"", "0.0.1", -1},
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		if got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
