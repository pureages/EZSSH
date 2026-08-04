package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// appVersion 为 ezssh Agent 版本号。
const appVersion = "0.0.5-2"

// cmdMenu 交互式管理菜单主循环。
func cmdMenu(cfg *Config) error {
	r := bufio.NewReader(os.Stdin)
	for {
		printHeader(cfg)
		pl("")
		pl("  1) 运行状态")
		pl("  2) 查看账号信息")
		pl("  3) 修改账号密码")
		pl("  4) 修改登录路由")
		pl("  5) 停止服务")
		pl("  6) 启动服务")
		pl("  7) 检查更新")
		pl("  8) 卸载 EZSSH")
		pl("  0) 退出")
		pf("请选择: ")
		line := strings.TrimSpace(readLine(r))
		switch line {
		case "1":
			cmdStatus(cfg)
		case "2":
			cmdAccount(cfg)
		case "3":
			cmdChangePwd(cfg, r)
		case "4":
			cmdRoute(cfg, r)
		case "5":
			cmdStop(cfg)
		case "6":
			cmdStart(cfg)
		case "7":
			cmdCheckUpdate(cfg)
		case "8":
			cmdUninstall(cfg, r)
		case "0", "q", "exit":
			pl("再见。")
			return nil
		default:
			pl("无效选择，请重新输入。")
		}
		pl("")
	}
}

// printHeader 打印菜单头（服务地址 + 运行状态）。
func printHeader(cfg *Config) {
	pl("")
	pl("EZSSH 管理终端 v%s", appVersion)
	if cfg.IsRunning() {
		pl("服务: %s    状态: 运行中 (PID %d)", cfg.BaseURL(), cfg.Pid())
	} else {
		pl("服务: %s    状态: 已停止", cfg.BaseURL())
	}
}

// cmdStatus 运行状态。
func cmdStatus(cfg *Config) {
	pl("")
	pl("--- 运行状态 ---")
	if pid := cfg.Pid(); pid > 0 {
		pl("PID: %d", pid)
	} else {
		pl("PID: (无记录)")
	}
	client := NewClient(cfg)
	if !cfg.IsRunning() {
		pl("服务: 未运行")
		return
	}
	pl("服务: 运行中")
	st, err := client.InitStatus()
	if err != nil {
		pl("状态查询失败: %v", err)
		return
	}
	pl("版本: %s", strVal(st, "version"))
	pl("已初始化: %s", boolText(st, "initialized", T("是"), T("否")))
	pl("保险库: %s", boolText(st, "unlocked", T("已解锁"), T("未解锁")))
	pl("登录路由: %s", strVal(st, "login_route"))
	pl("界面语言: %s", strVal(st, "lang"))
}

// cmdAccount 查看账号信息。
func cmdAccount(cfg *Config) {
	pl("")
	pl("--- 账号信息 ---")
	pl("账号: %s", cfg.Username)
	pl("密码: %s", cfg.Password)
	pl("登录路由: %s", cfg.LoginRoute)
	pl("地址: %s", cfg.BaseURL())
	if st, err := NewClient(cfg).InitStatus(); err == nil {
		if v := strVal(st, "login_route"); v != "" && v != cfg.LoginRoute {
			pl("（服务端实际路由: %s）", v)
		}
	}
}

// cmdChangePwd 修改账号密码。
func cmdChangePwd(cfg *Config, r *bufio.Reader) error {
	pl("")
	pl("--- 修改账号密码 ---")
	oldPwd := promptPwd(r, T("旧密码"), cfg.Password)
	var newPwd string
	for {
		newPwd = promptPwd(r, T("新密码"), "")
		if err := validatePassword(newPwd); err != nil {
			pl("%s", err)
			continue
		}
		confirm := promptPwd(r, T("确认新密码"), "")
		if confirm != newPwd {
			pl("两次输入不一致，请重试")
			continue
		}
		break
	}
	if err := NewClient(cfg).ChangePassword(oldPwd, newPwd); err != nil {
		pl("修改失败: %v%s", err, loginErrHint(err))
		return err
	}
	cfg.Password = newPwd
	if err := cfg.Save(); err != nil {
		pl("密码已修改，但保存配置失败: %v", err)
		return err
	}
	pl("密码已修改并保存到本地配置。")
	return nil
}

// cmdRoute 修改登录路由。
func cmdRoute(cfg *Config, r *bufio.Reader) error {
	pl("")
	pl("--- 修改登录路由 ---")
	route := prompt(r, T("新登录路由"), cfg.LoginRoute)
	if err := validateRoute(route); err != nil {
		pl("%s", err)
		return err
	}
	if err := NewClient(cfg).SetLoginRoute(route); err != nil {
		pl("设置失败: %v%s", err, loginErrHint(err))
		return err
	}
	cfg.LoginRoute = route
	if err := cfg.Save(); err != nil {
		pl("已设置，但保存配置失败: %v", err)
		return err
	}
	pl("登录路由已改为 %s。", route)
	return nil
}

// cmdStop 停止服务。
func cmdStop(cfg *Config) {
	pl("")
	if !cfg.IsRunning() {
		pl("服务未在运行。")
		return
	}
	if err := cfg.StopServer(); err != nil {
		pl("停止失败: %v", err)
		return
	}
	pl("服务已停止。")
}

// cmdStart 启动服务。
func cmdStart(cfg *Config) {
	pl("")
	if cfg.IsRunning() {
		pl("服务已在运行。")
		return
	}
	pid, err := cfg.StartServer()
	if err != nil {
		pl("启动失败: %v", err)
		return
	}
	if !cfg.waitHealthy(15 * time.Second) {
		pl("启动超时，请查看日志: %s", cfg.LogFile)
		return
	}
	pl("服务已启动 (PID %d)。", pid)
}

// cmdCheckUpdate 检查最新版本（调用服务端 /api/update-check）。
func cmdCheckUpdate(cfg *Config) {
	pl("")
	pl("--- 检查更新 ---")
	if !cfg.IsRunning() {
		pl("服务未运行，无法检查更新。请先启动服务。")
		return
	}
	st, err := NewClient(cfg).UpdateCheck()
	if err != nil {
		pl("检查更新失败: %v%s", err, loginErrHint(err))
		return
	}
	current := strVal(st, "current")
	latest := strVal(st, "latest")
	available, _ := st["update_available"].(bool)
	pl("当前版本: v%s", current)
	pl("最新版本: v%s", latest)
	if available {
		pl("发现新版本！请打开 Web 界面，在「设置 → 关于EZSSH」中点击一键更新。")
		pl("或重新运行安装脚本以升级: bash <(curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install.sh)")
	} else {
		pl("已是最新版本。")
	}
}

// cmdUninstall 卸载 EZSSH：停止服务、删除二进制、删除配置文件与数据目录。
func cmdUninstall(cfg *Config, r *bufio.Reader) {
	pl("")
	pl("--- 卸载 EZSSH ---")
	pl("此操作将:")
	pl("  • 停止正在运行的服务")
	pl("  • 删除可执行文件: %s", cfg.ServerBinary)
	if self, err := os.Executable(); err == nil {
		pl("  • 删除可执行文件: %s", self)
	}
	dir := filepath.Dir(cfg.path)
	pl("  • 删除配置与数据目录: %s（含数据库、加密凭据、日志）", dir)
	pl("")
	pf("确定要卸载吗？输入 yes 继续: ")
	ans := strings.ToLower(strings.TrimSpace(readLine(r)))
	if ans != "yes" && ans != "y" {
		pl("已取消卸载。")
		return
	}
	// 停止服务
	if cfg.IsRunning() {
		if err := cfg.StopServer(); err != nil {
			pl("停止服务失败: %v", err)
		} else {
			pl("服务已停止。")
		}
	}
	// 删除二进制
	removed := map[string]bool{}
	if cfg.ServerBinary != "" {
		if err := os.Remove(cfg.ServerBinary); err == nil {
			removed[cfg.ServerBinary] = true
		}
	}
	if self, err := os.Executable(); err == nil {
		if err := os.Remove(self); err == nil {
			removed[self] = true
		}
	}
	// 删除配置与数据目录（仅删除 agent 的 ~/.ezssh 下与 EZSSH 相关文件）
	if cfg.path != "" {
		dir := filepath.Dir(cfg.path)
		_ = os.RemoveAll(dir)
		pl("已删除配置与数据目录: %s", dir)
	}
	for f := range removed {
		pl("已删除可执行文件: %s", f)
	}
	pl("EZSSH 已卸载。再见。")
}

// cmdOneShot 一次性子命令（便于脚本调用；passwd/route 仍以交互方式接收输入）。
func cmdOneShot(cfg *Config, sub string) error {
	r := bufio.NewReader(os.Stdin)
	switch sub {
	case "status":
		cmdStatus(cfg)
	case "account":
		cmdAccount(cfg)
	case "passwd":
		return cmdChangePwd(cfg, r)
	case "route":
		return cmdRoute(cfg, r)
	case "start":
		cmdStart(cfg)
	case "stop":
		cmdStop(cfg)
	case "update":
		cmdCheckUpdate(cfg)
	case "uninstall":
		cmdUninstall(cfg, r)
	default:
		return errf("未知子命令: %s", sub)
	}
	return nil
}

// strVal 取 map 中的字符串值。
func strVal(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// boolText 取 map 中的布尔值并映射为文案。
func boolText(m map[string]any, key string, yes, no string) string {
	if v, ok := m[key].(bool); ok {
		if v {
			return yes
		}
		return no
	}
	return "-"
}

// loginErrHint 对验证码相关的登录限制给出可操作提示（同一 IP 近期登录失败触发）。
func loginErrHint(err error) string {
	if err != nil && (strings.Contains(err.Error(), "验证码") || strings.Contains(err.Error(), "captcha")) {
		return T("\n提示: 同一 IP 近期有登录失败触发了验证码校验。等待约 5 分钟自动解除，或通过 `ezssh stop` 后 `ezssh start` 重启服务清除记录。")
	}
	return ""
}
