package captcha

import (
	"strings"
	"testing"
)

func TestCreateReturnsSVG(t *testing.T) {
	m := NewManager()
	id, svg := m.Create()
	if id == "" {
		t.Fatal("empty id")
	}
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "</svg>") {
		t.Fatal("svg malformed")
	}
}

func TestVerifyWrongAndUnknown(t *testing.T) {
	m := NewManager()
	id, _ := m.Create()

	// 错误答案
	if m.Verify(id, "wrong") {
		t.Fatal("wrong answer should fail")
	}
	// 一次性消费：再次验证同一 id 失败
	if m.Verify(id, "wrong") {
		t.Fatal("consumed captcha should be invalid")
	}
	// 未知 id
	if m.Verify("no-such-id", "xxxx") {
		t.Fatal("unknown id should fail")
	}
}

func TestDistinct(t *testing.T) {
	m := NewManager()
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id, _ := m.Create()
		if seen[id] {
			t.Fatalf("duplicate captcha id: %s", id)
		}
		seen[id] = true
	}
}
