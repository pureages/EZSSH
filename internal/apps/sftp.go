package apps

import (
	"fmt"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"ezssh/internal/sshhub"
)

// SFTPManager 缓存每台主机的 SFTP 客户端（基于 ConnectionHub 的连接）。
type SFTPManager struct {
	hub        *sshhub.Hub
	mu         sync.Mutex
	clients    map[string]*sftp.Client
	sshClients map[string]*ssh.Client // 构建 SFTP 客户端所用的底层 SSH 连接
}

func NewSFTPManager(hub *sshhub.Hub) *SFTPManager {
	return &SFTPManager{
		hub:        hub,
		clients:    make(map[string]*sftp.Client),
		sshClients: make(map[string]*ssh.Client),
	}
}

// Client 返回指定主机的 SFTP 客户端，必要时基于已有 SSH 连接创建。
// 每次都会与 hub 当前连接比对底层 SSH Client：凭据被编辑（hub.CloseHost 后重连）
// 或网络断线惰性重连时，SSH 连接会被整体替换，旧 SFTP 客户端已失效必须重建，
// 否则文件管理器/复制粘贴会持续报 "connection lost"。
func (m *SFTPManager) Client(hostID string) (*sftp.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sshc, err := m.hub.GetClient(hostID)
	if err != nil {
		return nil, err
	}
	if c, ok := m.clients[hostID]; ok && m.sshClients[hostID] == sshc {
		return c, nil
	}
	// 底层 SSH 连接已更换：丢弃旧 SFTP 客户端并基于新连接重建
	if c, ok := m.clients[hostID]; ok {
		delete(m.clients, hostID)
		delete(m.sshClients, hostID)
		go closeSFTP(c)
	}
	c, err := sftp.NewClient(sshc)
	if err != nil {
		return nil, fmt.Errorf("sftp connect: %w", err)
	}
	m.clients[hostID] = c
	m.sshClients[hostID] = sshc
	return c, nil
}

// CloseHost 丢弃某主机的缓存 SFTP 客户端（主机被编辑/删除、连接被主动关闭时调用，
// 让下一个 SFTP 操作基于新凭据重建，而不是复用旧连接上失效的客户端）。
func (m *SFTPManager) CloseHost(hostID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[hostID]; ok {
		delete(m.clients, hostID)
		delete(m.sshClients, hostID)
		go closeSFTP(c)
	}
}

func closeSFTP(c *sftp.Client) {
	done := make(chan struct{})
	go func() {
		_ = c.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

// CloseAll 关闭所有 SFTP 客户端。pkg/sftp 的 Close() 可能因未完成的
// goroutine 阻塞，这里用超时保护，避免网关关闭时挂起。
func (m *SFTPManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, c := range m.clients {
		closeSFTP(c)
		delete(m.clients, id)
	}
	m.sshClients = make(map[string]*ssh.Client)
}
