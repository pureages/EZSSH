package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// StartServer 启动服务进程（后台运行，日志写入 LogFile），返回 PID。
// 已运行则直接返回当前 PID。通过环境变量注入端口与数据目录。
func (c *Config) StartServer() (int, error) {
	if c.ServerBinary == "" {
		return 0, errors.New(T("服务端二进制未配置，请先运行 `ezssh setup`"))
	}
	if c.IsRunning() {
		return c.Pid(), nil
	}
	bin := c.ServerBinary
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(bin), ".exe") {
		bin += ".exe"
	}

	// 确保日志/数据/PID 目录存在
	for _, p := range []string{c.LogFile, c.PidFile, c.DataDir} {
		if dir := filepath.Dir(p); dir != "" {
			_ = os.MkdirAll(dir, 0o700)
		}
	}
	logFile, err := os.OpenFile(c.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()

	cmd := exec.Command(bin)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		"EZSSH_LISTEN=127.0.0.1",
		"EZSSH_PORT="+strconv.Itoa(c.Port),
		"EZSSH_DATA="+c.DataDir,
		"EZSSH_WEB="+webDir(c),
	)
	setProcAttrs(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	if err := os.WriteFile(c.PidFile, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		// PID 文件写失败不阻断启动，仅提示
		_ = cmd.Process.Kill()
		return 0, err
	}
	return pid, nil
}

// webDir 返回前端 web/dist 目录：
// 优先服务端二进制同目录的 web/dist（预编译包布局：ezsshd 与 web/dist 同目录），
// 其次 ~/.ezssh/web/dist（一键安装脚本布局），找不到返回空（后端会回退到其他候选）。
func webDir(c *Config) string {
	if c.ServerBinary != "" {
		cand := filepath.Join(filepath.Dir(c.ServerBinary), "web", "dist")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		cand := filepath.Join(home, ".ezssh", "web", "dist")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
	}
	return ""
}

// StopServer 停止服务进程并删除 PID 文件；未记录 PID 时直接清理。
func (c *Config) StopServer() error {
	if pid := c.Pid(); pid > 0 {
		stopProcess(pid)
	}
	_ = os.Remove(c.PidFile)
	return nil
}

// Pid 读取 PID 文件中的进程号（0 表示无记录）。
func (c *Config) Pid() int {
	data, err := os.ReadFile(c.PidFile)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// IsRunning 判断服务是否在运行：以 HTTP 健康探测为主判活标准（跨平台可靠），
// 辅以进程存活检查（Windows 上无法 Signal(0)，仅依赖健康探测）。
func (c *Config) IsRunning() bool {
	if c.PidFile == "" {
		return false
	}
	if NewClient(c).Health() {
		return true
	}
	if pid := c.Pid(); pid > 0 {
		return alive(pid)
	}
	return false
}

// waitHealthy 轮询等待服务可达，最长 timeout。
func (c *Config) waitHealthy(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for !c.IsRunning() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(300 * time.Millisecond)
	}
	return true
}
