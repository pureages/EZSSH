package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// readLine 读取一行输入，仅去除行尾换行（保留密码等字段内的其他空白）。
func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

// prompt 读取输入，默认值直接回车复用；去掉首尾空白。
func prompt(r *bufio.Reader, label, def string) string {
	if def != "" {
		pf("%s [%s]: ", label, def)
	} else {
		pf("%s: ", label)
	}
	v := strings.TrimSpace(readLine(r))
	if v == "" {
		return def
	}
	return v
}

// promptPwd 读取密码输入（不回显默认值，仅提示回车使用当前值）。
func promptPwd(r *bufio.Reader, label, def string) string {
	suffix := ""
	if def != "" {
		suffix = T("（回车使用当前值）")
	}
	pf("%s%s: ", label, suffix)
	v := readLine(r)
	if v == "" {
		return def
	}
	return v
}

// validatePassword 校验密码长度（与服务端一致：至少 8 位）。
func validatePassword(p string) error {
	if len(p) < 8 {
		return errors.New(T("密码至少 8 位"))
	}
	return nil
}

// validateRoute 校验登录路由（与服务端一致：/ 开头、无空白/#/?、≤64）。
func validateRoute(route string) error {
	if !strings.HasPrefix(route, "/") {
		return errors.New(T("登录路由必须以 / 开头"))
	}
	if strings.ContainsAny(route, " #?") {
		return errors.New(T("登录路由不能包含空格/#/?"))
	}
	if len(route) > 64 {
		return errors.New(T("登录路由过长（≤64）"))
	}
	return nil
}

// cmdSetup 交互式安装向导。
func cmdSetup(configPath string, langArg string) error {
	if langArg != "" {
		setLang(langArg)
	}

	cfg := DefaultConfig()
	cfg.path = configPath

	if old, err := Load(configPath); err == nil {
		cfg = old
		pl("检测到已有配置，回车可直接复用当前值。")
	}

	pl("===== EZSSH 安装向导 =====")
	pl("配置文件: %s", configPath)
	r := bufio.NewReader(os.Stdin)

	// 账号
	username := prompt(r, T("管理员账号"), cfg.Username)
	for username == "" || len(username) > 64 {
		pl("账号长度需为 1-64 个字符")
		username = prompt(r, T("管理员账号"), cfg.Username)
	}

	// 密码（两次确认，回车默认 admin123456 / 已保存值）
	var password string
	for {
		password = promptPwd(r, T("管理员密码"), cfg.Password)
		if err := validatePassword(password); err != nil {
			pl("%s", err)
			continue
		}
		confirm := promptPwd(r, T("确认密码"), cfg.Password)
		if confirm != password {
			pl("两次输入不一致，请重试")
			continue
		}
		break
	}

	// 登录路由
	route := prompt(r, T("登录路由"), cfg.LoginRoute)
	for err := validateRoute(route); err != nil; err = validateRoute(route) {
		pl("%s", err)
		route = prompt(r, T("登录路由"), cfg.LoginRoute)
	}

	// 监听端口
	portStr := prompt(r, T("监听端口"), strconv.Itoa(cfg.Port))
	port, err := strconv.Atoi(portStr)
	for err != nil || port < 1 || port > 65535 {
		if err != nil {
			pl("端口必须是数字")
		} else {
			pl("端口需在 1-65535 之间")
		}
		portStr = prompt(r, T("监听端口"), strconv.Itoa(cfg.Port))
		port, err = strconv.Atoi(portStr)
	}

	cfg.Username = username
	cfg.Password = password
	cfg.LoginRoute = route
	cfg.Port = port
	cfg.Lang = lang

	// 服务端二进制
	bin, err := detectServerBinary(cfg.ServerBinary)
	if err != nil {
		return err
	}
	cfg.ServerBinary = prompt(r, T("服务端程序 (ezsshd)"), bin)

	// 数据目录
	if cfg.DataDir == "" {
		d, err := defaultDir()
		if err != nil {
			return err
		}
		cfg.DataDir = filepath.Join(d, "data")
	}
	cfg.DataDir = prompt(r, T("数据目录"), cfg.DataDir)

	// PID 与日志文件默认落在 ~/.ezssh 下
	if cfg.PidFile == "" || cfg.LogFile == "" {
		d, err := defaultDir()
		if err != nil {
			return err
		}
		if cfg.PidFile == "" {
			cfg.PidFile = filepath.Join(d, "server.pid")
		}
		if cfg.LogFile == "" {
			cfg.LogFile = filepath.Join(d, "server.log")
		}
	}

	if err := cfg.Save(); err != nil {
		return errf("保存配置失败: %v", err)
	}
	pl("配置已保存。")

	// 后台启动服务
	if cfg.IsRunning() {
		pl("服务已在运行 (PID %d)。", cfg.Pid())
	} else {
		pid, err := cfg.StartServer()
		if err != nil {
			return errf("启动服务失败: %v", err)
		}
		pl("服务启动中 (PID %d)…", pid)
	}
	if !cfg.waitHealthy(15 * time.Second) {
		return errf("服务启动超时，请查看日志: %s", cfg.LogFile)
	}
	pl("服务运行正常。")

	// 初始化：未初始化则创建管理员
	client := NewClient(cfg)
	st, err := client.InitStatus()
	if err != nil {
		return errf("读取服务状态失败: %v", err)
	}
	if initialized, _ := st["initialized"].(bool); !initialized {
		pl("首次使用，正在创建管理员账号…")
		if err := client.Init(cfg.Username, cfg.Password); err != nil {
			return errf("初始化失败: %v", err)
		}
		pl("管理员账号已创建。")
	} else {
		pl("检测到已初始化，跳过账号创建（不会改动既有数据）。")
	}

	// 设置登录路由（幂等）
	if err := client.SetLoginRoute(cfg.LoginRoute); err != nil {
		pl("警告: 设置登录路由失败: %v%s", err, loginErrHint(err))
	} else {
		pl("登录路由已设为 %s。", cfg.LoginRoute)
	}

	pl("")
	pl("===== 安装完成 =====")
	pl("  地址:     %s", cfg.BaseURL())
	pl("  账号:     %s", cfg.Username)
	pl("  密码:     %s", cfg.Password)
	pl("  登录路由: %s", cfg.LoginRoute)
	pl("  数据目录: %s", cfg.DataDir)
	pl("")
	pl("在终端输入 `ezssh` 打开管理菜单。")
	return nil
}

// detectServerBinary 定位 ezsshd 服务端程序：
// 1. 已有配置值；2. EZSSHD 环境变量；3. Agent 同目录下的 ezsshd(.exe)；4. PATH 中的 ezsshd。
func detectServerBinary(hint string) (string, error) {
	if hint != "" {
		return hint, nil
	}
	if v := os.Getenv("EZSSHD"); v != "" {
		return v, nil
	}
	if exe, err := os.Executable(); err == nil {
		name := "ezsshd"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		cand := filepath.Join(filepath.Dir(exe), name)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
	}
	if p, err := exec.LookPath("ezsshd"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%s", T("未找到服务端程序 ezsshd，请设置 EZSSHD 环境变量或将其放入 PATH"))
}
