package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	sessionTTL = 12 * time.Hour
	lockWindow = 5 * time.Minute
	lockMax    = 5

	// 登录入口探测（/api/route-check）限流：故意与「登录失败」分开计数，
	// 避免攻击者用错误路径刷请求把管理员自己锁在登录之外。
	routeProbeWindow = 5 * time.Minute
)

// RouteProbeMax 一个窗口内允许的登录入口探测失败次数上限（超出后 API 返回 429）。
const RouteProbeMax = 30

// Manager 管理会话令牌与登录限流（内存态，单用户场景足够）。
type Manager struct {
	mu         sync.Mutex
	sessions   map[string]time.Time
	fails      map[string][]time.Time
	routeFails map[string][]time.Time
}

func NewManager() *Manager {
	return &Manager{
		sessions:   make(map[string]time.Time),
		fails:      make(map[string][]time.Time),
		routeFails: make(map[string][]time.Time),
	}
}

func (m *Manager) CreateSession() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	m.mu.Lock()
	m.sessions[token] = time.Now().Add(sessionTTL)
	m.mu.Unlock()
	return token, nil
}

func (m *Manager) Validate(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(m.sessions, token)
		return false
	}
	return true
}

func (m *Manager) Destroy(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

// CanLogin 返回该 IP 是否允许尝试登录（最近 5 分钟失败 <5 次）。
func (m *Manager) CanLogin(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pruneFails(ip)) < lockMax
}

func (m *Manager) RecordFail(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fails[ip] = append(m.pruneFails(ip), time.Now())
}

// FailCount 返回该 IP 当前窗口内的失败次数。
func (m *Manager) FailCount(ip string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pruneFails(ip))
}

// ClearFail 登录成功时清空该 IP 的失败记录。
func (m *Manager) ClearFail(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.fails, ip)
}

// CanProbeRoute 返回该 IP 是否仍可探测登录入口（最近 5 分钟失败 < RouteProbeMax 次）。
// 用于 /api/route-check：安全路由的值不下发给前端，只能「按路径询问能否登录」，
// 因此需要限流防止被暴力枚举。
func (m *Manager) CanProbeRoute(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(pruneWindow(m.routeFails, ip, routeProbeWindow)) < RouteProbeMax
}

// RecordRouteProbeFail 记录一次登录入口探测失败。
func (m *Manager) RecordRouteProbeFail(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routeFails[ip] = append(pruneWindow(m.routeFails, ip, routeProbeWindow), time.Now())
}

// ClearRouteProbe 探测命中时清空该 IP 的探测失败记录。
func (m *Manager) ClearRouteProbe(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.routeFails, ip)
}

// pruneFails 清理超出窗口的失败记录并返回剩余记录（需持锁调用）。
func (m *Manager) pruneFails(ip string) []time.Time {
	return pruneWindow(m.fails, ip, lockWindow)
}

// pruneWindow 清理 m[key] 中超出 window 的时间戳并返回剩余记录（需持锁调用）。
func pruneWindow(m map[string][]time.Time, key string, window time.Duration) []time.Time {
	now := time.Now()
	recent := m[key][:0]
	for _, t := range m[key] {
		if now.Sub(t) < window {
			recent = append(recent, t)
		}
	}
	m[key] = recent
	return recent
}
