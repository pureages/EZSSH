package apps

import (
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"

	"ezssh/internal/sshhub"
)

// OnOutput 输出回调（可能并发调用，调用方需自行串行化）。
type OnOutput func(channelID string, data []byte)

// OnExit 会话退出回调。
type OnExit func(channelID string)

// TerminalManager 管理单个 WebSocket 连接下的终端会话集合。
type TerminalManager struct {
	hub      *sshhub.Hub
	mu       sync.Mutex
	sessions map[string]*termSession
}

type termSession struct {
	session *ssh.Session
	stdin   io.WriteCloser
}

func NewTerminalManager(hub *sshhub.Hub) *TerminalManager {
	return &TerminalManager{hub: hub, sessions: make(map[string]*termSession)}
}

// isWindows 判断目标机是否为 Windows（探测失败按 Linux 处理）。
func (tm *TerminalManager) isWindows(hostID string) bool {
	p, err := tm.hub.Platform(hostID)
	if err != nil {
		return false
	}
	return p == "windows"
}

// Open 打开目标主机的交互式 shell（pty + shell）。
func (tm *TerminalManager) Open(hostID, channelID string, cols, rows int, onOutput OnOutput, onExit OnExit) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if _, ok := tm.sessions[channelID]; ok {
		return fmt.Errorf("channel %s already open", channelID)
	}

	client, err := tm.hub.GetClient(hostID)
	if err != nil {
		return err
	}
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("open session: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		sess.Close()
		return err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		sess.Close()
		return fmt.Errorf("request pty: %w", err)
	}
	if tm.isWindows(hostID) {
		// Windows 主机强制进入 PowerShell（Win7+ 自带）；不走 Shell() 以免落到默认 cmd。
		if err := sess.Start("powershell -NoLogo"); err != nil {
			sess.Close()
			return fmt.Errorf("start shell: %w", err)
		}
	} else {
		if err := sess.Shell(); err != nil {
			sess.Close()
			return fmt.Errorf("start shell: %w", err)
		}
	}

	tm.sessions[channelID] = &termSession{session: sess, stdin: stdin}

	// stdout 与 stderr 合并转发
	go func() {
		defer tm.Close(channelID)
		buf := make([]byte, 8192)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				onOutput(channelID, buf[:n])
			}
			if err != nil {
				onExit(channelID)
				return
			}
		}
	}()
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				onOutput(channelID, buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	return nil
}

// Write 写入 stdin。
func (tm *TerminalManager) Write(channelID string, data []byte) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	ts, ok := tm.sessions[channelID]
	if !ok {
		return fmt.Errorf("channel %s not open", channelID)
	}
	_, err := ts.stdin.Write(data)
	return err
}

// Resize 同步 pty 尺寸。
func (tm *TerminalManager) Resize(channelID string, cols, rows int) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	ts, ok := tm.sessions[channelID]
	if !ok {
		return fmt.Errorf("channel %s not open", channelID)
	}
	return ts.session.WindowChange(rows, cols)
}

func (tm *TerminalManager) Close(channelID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if ts, ok := tm.sessions[channelID]; ok {
		delete(tm.sessions, channelID)
		ts.session.Close()
	}
}

func (tm *TerminalManager) CloseAll() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	for id, ts := range tm.sessions {
		delete(tm.sessions, id)
		ts.session.Close()
	}
}
