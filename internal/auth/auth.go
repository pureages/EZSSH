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
)

// Manager 管理会话令牌与登录限流（内存态，单用户场景足够）。
type Manager struct {
	mu       sync.Mutex
	sessions map[string]time.Time
	fails    map[string][]time.Time
}

func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]time.Time),
		fails:    make(map[string][]time.Time),
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

// pruneFails 清理超出窗口的失败记录并返回剩余记录（需持锁调用）。
func (m *Manager) pruneFails(ip string) []time.Time {
	now := time.Now()
	recent := m.fails[ip][:0]
	for _, t := range m.fails[ip] {
		if now.Sub(t) < lockWindow {
			recent = append(recent, t)
		}
	}
	m.fails[ip] = recent
	return recent
}
