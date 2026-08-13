package apps

import (
	"bytes"
	"testing"
)

func TestNormalizeWinInput(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare LF -> CR", "dir /b\n", "dir /b\r"},
		{"already CRLF stays single CR", "dir /b\r\n", "dir /b\r"},
		{"no newline unchanged", "echo hi", "echo hi"},
		{"multi-line LF -> CR", "cd /x\nrun\n", "cd /x\rrun\r"},
		{"mixed CR and LF", "a\rb\nc\r\nd", "a\rb\rc\rd"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalizeWinInput([]byte(c.in))
			if !bytes.Equal(got, []byte(c.want)) {
				t.Fatalf("normalizeWinInput(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
