//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// setProcAttrs 让子进程进入独立进程组，终端关闭不影响服务。
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// stopProcess 停止进程：SIGTERM，1.5s 后未退出则 SIGKILL。
func stopProcess(pid int) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = p.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_, _ = p.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1500 * time.Millisecond):
		_ = p.Kill()
	}
}

// alive 检查进程是否存活（Signal(0) 探测）。
func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
