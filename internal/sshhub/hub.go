package sshhub

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"ezssh/internal/store"
	"ezssh/internal/vault"
)

// Hub 是连接中枢：每台主机维持一条 SSH 连接（单连接多 Channel 复用的基础）。
// 断线采用惰性重连策略：下一次 GetClient 时自动重建。
type Hub struct {
	mu      sync.Mutex
	conns   map[string]*hostConn
	st      *store.Store
	vault   *vault.Vault
	dialMu  sync.Mutex
}

type hostConn struct {
	client       *ssh.Client
	hostKey      ssh.PublicKey
	cancel       context.CancelFunc
	platform     string       // 'linux' | 'windows'；platformDone 关闭后可读
	platformDone chan struct{} // 首次平台探测/回填完成后关闭（探测可能需数秒）
}

func New(st *store.Store, v *vault.Vault) *Hub {
	return &Hub{conns: make(map[string]*hostConn), st: st, vault: v}
}

// GetClient 返回该主机可用的 SSH Client，必要时建连。
func (h *Hub) GetClient(hostID string) (*ssh.Client, error) {
	if c, ok := h.peek(hostID); ok {
		return c, nil
	}

	h.dialMu.Lock()
	defer h.dialMu.Unlock()
	if c, ok := h.peek(hostID); ok {
		return c, nil
	}

	host, err := h.st.GetHost(hostID)
	if err != nil {
		return nil, err
	}
	if !h.vault.IsUnlocked() {
		return nil, errors.New("vault locked")
	}
	// 内置网关主机默认账号密码留空，尚未配置前直接给友好错误（空凭据解密会报 bad ciphertext）。
	if strings.TrimSpace(host.Username) == "" {
		return nil, errors.New("该内置网关主机尚未配置用户名，请先在“编辑”中填写用户名与密码")
	}
	cred, err := h.vault.Decrypt(host.Credential)
	if err != nil {
		return nil, fmt.Errorf("decrypt credential: %w", err)
	}

	client, hostKey, err := dial(host, cred)
	if err != nil {
		return nil, err
	}

	// TOFU 主机密钥校验：已记录指纹则必须一致，否则拒绝（防中间人替换）。
	fp := ssh.FingerprintSHA256(hostKey)
	if host.Fingerprint != "" {
		if host.Fingerprint != fp {
			client.Close()
			return nil, fmt.Errorf("host key mismatch! expected %s, got %s. 主机密钥可能被更换，请谨慎处理", host.Fingerprint, fp)
		}
	} else {
		// 首次连接，记录指纹
		if err := h.st.UpdateHostFingerprint(hostID, fp); err != nil {
			client.Close()
			return nil, fmt.Errorf("record fingerprint: %w", err)
		}
		host.Fingerprint = fp
	}

	ctx, cancel := context.WithCancel(context.Background())
	hc := &hostConn{client: client, hostKey: hostKey, cancel: cancel, platformDone: make(chan struct{})}
	go hc.keepAlive(ctx)

	h.mu.Lock()
	h.conns[hostID] = hc
	h.mu.Unlock()

	// 平台探测：仅首次拨号且 store 未持久化平台时进行（含隐式 ''）。
	// 探明后回填 conn 并尽力持久化，后续连接直接复用；探测失败回落 linux 但不持久化，
	// 下次重连会再试，避免把 Windows 主机误判并写死。
	if host.Platform != "" {
		hc.platform = host.Platform
		close(hc.platformDone)
	} else {
		p, perr := h.probePlatform(context.Background(), client)
		if perr != nil {
			log.Printf("sshhub: probe platform for %s: %v (default linux)", hostID, perr)
			p = "linux"
		} else if err := h.st.UpdateHostPlatform(hostID, p); err != nil {
			log.Printf("sshhub: persist platform for %s: %v", hostID, err)
		}
		hc.platform = p
		close(hc.platformDone)
	}
	return client, nil
}

// Status 返回连接状态与远端主机密钥指纹（连接不存在或已断线时 ok=false）。
func (h *Hub) Status(hostID string) (ok bool, fingerprint string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	hc, ok := h.conns[hostID]
	if !ok {
		return false, ""
	}
	if hc.hostKey != nil {
		return true, ssh.FingerprintSHA256(hc.hostKey)
	}
	return true, ""
}

// DialInfo 返回主机的连接参数与解密后的明文凭据。
// 供"服务器间直连传输"使用：由源主机主动向目标主机发起 scp 推送。
func (h *Hub) DialInfo(hostID string) (*store.Host, []byte, error) {
	host, err := h.st.GetHost(hostID)
	if err != nil {
		return nil, nil, err
	}
	if !h.vault.IsUnlocked() {
		return nil, nil, errors.New("vault locked")
	}
	cred, err := h.vault.Decrypt(host.Credential)
	if err != nil {
		return nil, nil, fmt.Errorf("decrypt credential: %w", err)
	}
	return host, cred, nil
}

// TestConnect 用传入的明文凭据测试连通性，不缓存连接、不记录 TOFU。
// 返回远端主机密钥指纹与探测到的平台（'' 表示探测失败）。
func (h *Hub) TestConnect(host *store.Host, cred []byte) (fingerprint, platform string, err error) {
	client, hostKey, err := dial(host, cred)
	if err != nil {
		return "", "", err
	}
	defer client.Close()
	fp := ""
	if hostKey != nil {
		fp = ssh.FingerprintSHA256(hostKey)
	}
	// 复用平台探测：供表单"测试连接后自动回填系统类型"
	if p, perr := h.probePlatform(context.Background(), client); perr == nil {
		platform = p
	}
	return fp, platform, nil
}

// Fingerprint 返回某主机当前连接的主机密钥指纹（未连接返回空串）。
func (h *Hub) Fingerprint(hostID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if hc, ok := h.conns[hostID]; ok && hc.hostKey != nil {
		return ssh.FingerprintSHA256(hc.hostKey)
	}
	return ""
}

// CloseHost 主动断开某台主机的连接。
func (h *Hub) CloseHost(hostID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if hc, ok := h.conns[hostID]; ok {
		hc.close()
		delete(h.conns, hostID)
	}
}

func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, hc := range h.conns {
		hc.close()
		delete(h.conns, id)
	}
}

// peek 检查并返回活跃连接；发现已断线则清理。
func (h *Hub) peek(hostID string) (*ssh.Client, bool) {
	hc, ok := h.getConn(hostID)
	if !ok {
		return nil, false
	}
	return hc.client, true
}

// getConn 返回活跃连接的 hostConn；发现断线则清理。
func (h *Hub) getConn(hostID string) (*hostConn, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	hc, ok := h.conns[hostID]
	if !ok {
		return nil, false
	}
	if hc.alive() {
		return hc, true
	}
	hc.close()
	delete(h.conns, hostID)
	return nil, false
}

func dial(host *store.Host, cred []byte) (*ssh.Client, ssh.PublicKey, error) {
	var auths []ssh.AuthMethod
	switch host.AuthType {
	case "password":
		auths = append(auths, ssh.Password(string(cred)))
	case "privatekey":
		signer, err := ssh.ParsePrivateKey(cred)
		if err != nil {
			return nil, nil, fmt.Errorf("parse private key: %w", err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	default:
		return nil, nil, fmt.Errorf("unsupported auth type %q", host.AuthType)
	}

	// 捕获远端主机密钥用于指纹展示；TODO(M6): 改为 TOFU 指纹确认
	var hostKey ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User: host.Username,
		Auth: auths,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			hostKey = key
			return nil
		},
		Timeout: 10 * time.Second,
	}

	addr := net.JoinHostPort(host.Host, fmt.Sprintf("%d", host.Port))
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, nil, err
	}
	return client, hostKey, nil
}

func (hc *hostConn) alive() bool {
	_, _, err := hc.client.Conn.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}

func (hc *hostConn) close() {
	if hc.cancel != nil {
		hc.cancel()
	}
	hc.client.Close()
}

// keepAlive 每 30s 发送 SSH keepalive 防止 NAT 超时。
func (hc *hostConn) keepAlive(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !hc.alive() {
				hc.client.Close()
				return
			}
		}
	}
}
