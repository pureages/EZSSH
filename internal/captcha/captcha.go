package captcha

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// 排除易混淆字符 0/O/1/l/I
const chars = "23456789abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ"

// Manager 内存态验证码存储（单用户场景足够）。
type Manager struct {
	mu    sync.Mutex
	items map[string]captchaItem
}

type captchaItem struct {
	answer  string
	expires time.Time
}

func NewManager() *Manager {
	return &Manager{items: make(map[string]captchaItem)}
}

func randN(max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(n.Int64())
}

func randHex() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Create 生成一张验证码，返回 id 与 SVG 内容。
func (m *Manager) Create() (id, svg string) {
	// 4 位随机字符
	var sb strings.Builder
	for i := 0; i < 4; i++ {
		sb.WriteByte(chars[randN(len(chars))])
	}
	answer := sb.String()

	id = randHex()
	m.mu.Lock()
	m.items[id] = captchaItem{answer: answer, expires: time.Now().Add(5 * time.Minute)}
	// 顺手清理过期项
	now := time.Now()
	for k, v := range m.items {
		if now.After(v.expires) {
			delete(m.items, k)
		}
	}
	m.mu.Unlock()

	return id, renderSVG(answer)
}

// Verify 校验答案（不区分大小写），无论成败都作废该验证码（防重放）。
func (m *Manager) Verify(id, answer string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return false
	}
	delete(m.items, id)
	if time.Now().After(item.expires) {
		return false
	}
	return strings.EqualFold(item.answer, strings.TrimSpace(answer))
}

// renderSVG 用 SVG 绘制验证码：4 个字符 + 干扰线与噪点。
func renderSVG(code string) string {
	const w, h = 140, 48
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, w, h, w, h))
	b.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" rx="8" fill="#111a2e"/>`, w, h))

	// 背景噪点
	for i := 0; i < 60; i++ {
		x, y := randN(w), randN(h)
		b.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="1" fill="#334155"/>`, x, y))
	}

	// 干扰线
	for i := 0; i < 3; i++ {
		x1, y1 := randN(w), randN(h)
		x2, y2 := randN(w), randN(h)
		b.WriteString(fmt.Sprintf(`<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#475569" stroke-width="1.5"/>`, x1, y1, x2, y2))
	}

	// 字符（随机偏移、角度、颜色）
	colors := []string{"#60a5fa", "#c084fc", "#34d399", "#fbbf24", "#fb923c", "#38bdf8"}
	step := w / (len(code) + 1)
	for i, ch := range code {
		x := step*(i+1) + randN(8) - 4
		y := 32 + randN(8) - 4
		rot := randN(24) - 12
		color := colors[randN(len(colors))]
		b.WriteString(fmt.Sprintf(
			`<text x="%d" y="%d" font-size="28" font-family="Consolas,monospace" font-weight="bold" fill="%s" transform="rotate(%d %d %d)">%c</text>`,
			x, y, color, rot, x, y, ch,
		))
	}
	b.WriteString(`</svg>`)
	return b.String()
}
