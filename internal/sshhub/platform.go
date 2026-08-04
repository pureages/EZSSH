package sshhub

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const probeTimeout = 5 * time.Second

// Platform 返回主机平台：'linux' | 'windows'。
// 优先取活跃连接（等待首次平台探测完成）；未连接时查 store（显式选择/已持久化）；
// 仍未知则 GetClient 触发连接，其首次拨号路径会探测并持久化。
func (h *Hub) Platform(hostID string) (string, error) {
	if hc, ok := h.getConn(hostID); ok {
		<-hc.platformDone
		return hc.platform, nil
	}
	host, err := h.st.GetHost(hostID)
	if err != nil {
		return "", err
	}
	if host.Platform != "" {
		return host.Platform, nil
	}
	if _, err := h.GetClient(hostID); err != nil {
		return "", err
	}
	hc, ok := h.getConn(hostID)
	if !ok {
		return "", errors.New("connection lost during platform probe")
	}
	<-hc.platformDone
	return hc.platform, nil
}

// probePlatform 在新 session 上探测远端平台（5s 超时）。
// ① uname -s 成功且非空 → 含 mingw|msys|cygwin|windows 判 windows，否则 linux；
// ② 失败/空 → 裸 ver（注意：不能用 "cmd /c ver"，OpenSSH-for-Windows 会再包一层
//    cmd 造成嵌套引号，cmd 实际看到 ver" 而报错；裸 ver 由外层 cmd 直接执行，可靠），
//    含 Windows|Microsoft 判 windows；
// ③ 兜底：两者均失败返回错误（调用方回落 linux）。
func (h *Hub) probePlatform(ctx context.Context, client *ssh.Client) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	type res struct {
		p   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		p, err := h.probePlatformUnbounded(client)
		ch <- res{p, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.p, r.err
	}
}

func (h *Hub) probePlatformUnbounded(client *ssh.Client) (string, error) {
	exec := func(cmd string) (string, error) {
		sess, err := client.NewSession()
		if err != nil {
			return "", err
		}
		defer sess.Close()
		out, err := sess.CombinedOutput(cmd)
		return string(out), err
	}

	out, err := exec("uname -s")
	if err == nil && strings.TrimSpace(out) != "" {
		l := strings.ToLower(out)
		if strings.Contains(l, "mingw") || strings.Contains(l, "msys") ||
			strings.Contains(l, "cygwin") || strings.Contains(l, "windows") {
			return "windows", nil
		}
		return "linux", nil
	}

	out, err = exec("ver")
	if err == nil && strings.TrimSpace(out) != "" {
		l := strings.ToLower(out)
		if strings.Contains(l, "windows") || strings.Contains(l, "microsoft") {
			return "windows", nil
		}
		return "linux", nil
	}
	return "", errors.New("platform probe failed")
}
