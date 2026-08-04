//go:build windows

package main

import (
	"os/exec"
	"strconv"
)

// setProcAttrs Windows 下无需特殊进程属性。
func setProcAttrs(cmd *exec.Cmd) {}

// stopProcess 停止进程：taskkill /F。
func stopProcess(pid int) {
	// 进程已退出时 taskkill 报错，忽略即可
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F").Run()
}

// alive Windows 下无法用 Signal(0) 探测，返回 false（判活以 HTTP 健康探测为准）。
func alive(pid int) bool {
	return false
}
