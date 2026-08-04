package main

import (
	"fmt"
	"os"
)

// 全局界面语言：en | zh。由 --lang 参数或配置中的 lang 字段决定（默认 en，与产品默认语言一致）。
var lang = "en"

// setLang 设置全局语言（非 zh 一律回退 en）。
func setLang(l string) {
	if l == "zh" {
		lang = "zh"
	} else {
		lang = "en"
	}
}

// messages 消息表：键 = 中文原文（稳定 key），值为 [中文, English]。
var messages = map[string][2]string{
	// ---- setup.go ----
	"检测到已有配置，回车可直接复用当前值。": {"检测到已有配置，回车可直接复用当前值。", "Existing config detected. Press Enter to reuse current values."},
	"===== EZSSH 安装向导 =====":       {"===== EZSSH 安装向导 =====", "===== EZSSH Setup Wizard ====="},
	"配置文件: %s":                      {"配置文件: %s", "Config file: %s"},
	"管理员账号":                         {"管理员账号", "Admin account"},
	"账号长度需为 1-64 个字符":              {"账号长度需为 1-64 个字符", "Account name must be 1-64 characters"},
	"管理员密码":                         {"管理员密码", "Admin password"},
	"（回车使用当前值）":                    {"（回车使用当前值）", " (Enter to use current value)"},
	"确认密码":                          {"确认密码", "Confirm password"},
	"两次输入不一致，请重试":                {"两次输入不一致，请重试", "The two inputs do not match. Please try again."},
	"密码至少 8 位":                      {"密码至少 8 位", "Password must be at least 8 characters"},
	"登录路由":                          {"登录路由", "Login route"},
	"登录路由必须以 / 开头":                 {"登录路由必须以 / 开头", "Login route must start with /"},
	"登录路由不能包含空格/#/?":              {"登录路由不能包含空格/#/?", "Login route cannot contain spaces, # or ?"},
	"登录路由过长（≤64）":                  {"登录路由过长（≤64）", "Login route is too long (max 64)"},
	"监听端口":                          {"监听端口", "Listen port"},
	"端口必须是数字":                      {"端口必须是数字", "Port must be a number"},
	"端口需在 1-65535 之间":               {"端口需在 1-65535 之间", "Port must be between 1 and 65535"},
	"服务端程序 (ezsshd)":                {"服务端程序 (ezsshd)", "Server binary (ezsshd)"},
	"数据目录":                          {"数据目录", "Data directory"},
	"保存配置失败: %v":                   {"保存配置失败: %v", "Failed to save config: %v"},
	"配置已保存。":                        {"配置已保存。", "Config saved."},
	"服务已在运行 (PID %d)。":             {"服务已在运行 (PID %d)。", "Service already running (PID %d)."},
	"服务启动中 (PID %d)…":               {"服务启动中 (PID %d)…", "Starting service (PID %d)…"},
	"启动服务失败: %v":                    {"启动服务失败: %v", "Failed to start service: %v"},
	"初始化失败: %v":                      {"初始化失败: %v", "Initialization failed: %v"},
	"服务启动超时，请查看日志: %s":           {"服务启动超时，请查看日志: %s", "Service start timed out. Check the log: %s"},
	"服务运行正常。":                      {"服务运行正常。", "Service is healthy."},
	"读取服务状态失败: %v":                 {"读取服务状态失败: %v", "Failed to read service status: %v"},
	"首次使用，正在创建管理员账号…":           {"首次使用，正在创建管理员账号…", "First run: creating admin account…"},
	"管理员账号已创建。":                   {"管理员账号已创建。", "Admin account created."},
	"检测到已初始化，跳过账号创建（不会改动既有数据）。": {"检测到已初始化，跳过账号创建（不会改动既有数据）。", "Already initialized. Skipping account creation (existing data untouched)."},
	"警告: 设置登录路由失败: %v%s":          {"警告: 设置登录路由失败: %v%s", "Warning: failed to set login route: %v%s"},
	"登录路由已设为 %s。":                  {"登录路由已设为 %s。", "Login route set to %s."},
	"===== 安装完成 =====":               {"===== 安装完成 =====", "===== Installation Complete ====="},
	"  地址:     %s":                     {"  地址:     %s", "  URL:       %s"},
	"  账号:     %s":                     {"  账号:     %s", "  Account:   %s"},
	"  密码:     %s":                     {"  密码:     %s", "  Password:  %s"},
	"  登录路由: %s":                     {"  登录路由: %s", "  Login route: %s"},
	"  数据目录: %s":                     {"  数据目录: %s", "  Data dir:  %s"},
	"在终端输入 `ezssh` 打开管理菜单。":       {"在终端输入 `ezssh` 打开管理菜单。", "Type `ezssh` in the terminal to open the management menu."},
	"未找到服务端程序 ezsshd，请设置 EZSSHD 环境变量或将其放入 PATH": {"未找到服务端程序 ezsshd，请设置 EZSSHD 环境变量或将其放入 PATH", "Server binary ezsshd not found. Set the EZSSHD env var or put it in PATH"},

	// ---- process.go ----
	"服务端二进制未配置，请先运行 `ezssh setup`": {"服务端二进制未配置，请先运行 `ezssh setup`", "Server binary not configured. Run `ezssh setup` first."},

	// ---- menu.go ----
	"EZSSH 管理终端 v%s":                    {"EZSSH 管理终端 v%s", "EZSSH Management Console v%s"},
	"服务: %s    状态: 运行中 (PID %d)":      {"服务: %s    状态: 运行中 (PID %d)", "Service: %s    Status: running (PID %d)"},
	"服务: %s    状态: 已停止":               {"服务: %s    状态: 已停止", "Service: %s    Status: stopped"},
	"  1) 运行状态      2) 查看账号信息":       {"  1) 运行状态      2) 查看账号信息", "  1) Status          2) Account info"},
	"  3) 修改账号密码  4) 修改登录路由":       {"  3) 修改账号密码  4) 修改登录路由", "  3) Change password  4) Change login route"},
	"  5) 停止服务      6) 启动服务":         {"  5) 停止服务      6) 启动服务", "  5) Stop service     6) Start service"},
	"  0) 退出":                           {"  0) 退出", "  0) Exit"},
	"请选择: ":                            {"请选择: ", "Select: "},
	"再见。":                             {"再见。", "Goodbye."},
	"无效选择，请重新输入。":                   {"无效选择，请重新输入。", "Invalid selection. Please try again."},
	"--- 运行状态 ---":                     {"--- 运行状态 ---", "--- Status ---"},
	"PID: %d":                            {"PID: %d", "PID: %d"},
	"PID: (无记录)":                       {"PID: (无记录)", "PID: (none)"},
	"服务: 未运行":                         {"服务: 未运行", "Service: not running"},
	"服务: 运行中":                         {"服务: 运行中", "Service: running"},
	"状态查询失败: %v":                      {"状态查询失败: %v", "Failed to query status: %v"},
	"版本: %s":                            {"版本: %s", "Version: %s"},
	"已初始化: %s":                         {"已初始化: %s", "Initialized: %s"},
	"是":                                 {"是", "Yes"},
	"否":                                 {"否", "No"},
	"保险库: %s":                          {"保险库: %s", "Vault: %s"},
	"已解锁":                              {"已解锁", "unlocked"},
	"未解锁":                              {"未解锁", "locked"},
	"登录路由: %s":                         {"登录路由: %s", "Login route: %s"},
	"界面语言: %s":                         {"界面语言: %s", "UI language: %s"},
	"--- 账号信息 ---":                     {"--- 账号信息 ---", "--- Account Info ---"},
	"账号: %s":                            {"账号: %s", "Account: %s"},
	"密码: %s":                            {"密码: %s", "Password: %s"},
	"地址: %s":                            {"地址: %s", "URL: %s"},
	"（服务端实际路由: %s）":                  {"（服务端实际路由: %s）", " (server actual route: %s)"},
	"--- 修改账号密码 ---":                   {"--- 修改账号密码 ---", "--- Change Password ---"},
	"旧密码":                              {"旧密码", "Old password"},
	"新密码":                              {"新密码", "New password"},
	"确认新密码":                           {"确认新密码", "Confirm new password"},
	"修改失败: %v%s":                       {"修改失败: %v%s", "Change failed: %v%s"},
	"密码已修改并保存到本地配置。":              {"密码已修改并保存到本地配置。", "Password changed and saved to local config."},
	"密码已修改，但保存配置失败: %v":          {"密码已修改，但保存配置失败: %v", "Password changed, but saving config failed: %v"},
	"--- 修改登录路由 ---":                   {"--- 修改登录路由 ---", "--- Change Login Route ---"},
	"新登录路由":                           {"新登录路由", "New login route"},
	"设置失败: %v%s":                       {"设置失败: %v%s", "Set failed: %v%s"},
	"登录路由已改为 %s。":                    {"登录路由已改为 %s。", "Login route changed to %s."},
	"已设置，但保存配置失败: %v":               {"已设置，但保存配置失败: %v", "Set succeeded, but saving config failed: %v"},
	"服务未在运行。":                        {"服务未在运行。", "Service is not running."},
	"停止失败: %v":                        {"停止失败: %v", "Stop failed: %v"},
	"服务已停止。":                         {"服务已停止。", "Service stopped."},
	"服务已在运行。":                        {"服务已在运行。", "Service is already running."},
	"启动失败: %v":                        {"启动失败: %v", "Start failed: %v"},
	"启动超时，请查看日志: %s":               {"启动超时，请查看日志: %s", "Start timed out. Check the log: %s"},
	"服务已启动 (PID %d)。":                 {"服务已启动 (PID %d)。", "Service started (PID %d)."},
	"\n提示: 同一 IP 近期有登录失败触发了验证码校验。等待约 5 分钟自动解除，或通过 `ezssh stop` 后 `ezssh start` 重启服务清除记录。": {
		"\n提示: 同一 IP 近期有登录失败触发了验证码校验。等待约 5 分钟自动解除，或通过 `ezssh stop` 后 `ezssh start` 重启服务清除记录。",
		"\nNote: recent login failures from this IP triggered captcha verification. It resets automatically after about 5 minutes, or run `ezssh stop` then `ezssh start` to restart the service and clear the records.",
	},
	"未知子命令: %s": {"未知子命令: %s", "Unknown subcommand: %s"},

	// ---- main.go ----
	"无法确定配置路径: %v":                     {"无法确定配置路径: %v", "Unable to determine config path: %v"},
	"尚未配置，请先运行: ezssh setup":           {"尚未配置，请先运行: ezssh setup", "Not configured yet. Run `ezssh setup` first."},
	"读取配置失败: %v":                       {"读取配置失败: %v", "Failed to read config: %v"},
	"错误: %v":                             {"错误: %v", "Error: %v"},
	"安装向导失败: %v":                       {"安装向导失败: %v", "Setup wizard failed: %v"},
	"未知命令: %s\n\n":                      {"未知命令: %s\n\n", "Unknown command: %s\n\n"},
	"EZSSH 终端管理 Agent v%s":               {"EZSSH 终端管理 Agent v%s", "EZSSH Terminal Management Agent v%s"},
	"用法:":                                {"用法:", "Usage:"},
	"  ezssh setup                交互式安装向导（首次运行）": {"  ezssh setup                交互式安装向导（首次运行）", "  ezssh setup                Interactive setup wizard (first run)"},
	"  ezssh                      打开交互式管理菜单":      {"  ezssh                      打开交互式管理菜单", "  ezssh                      Open interactive management menu"},
	"  ezssh status               查看运行状态":         {"  ezssh status               查看运行状态", "  ezssh status               Show running status"},
	"  ezssh account              查看账号信息":         {"  ezssh account              查看账号信息", "  ezssh account              Show account info"},
	"  ezssh passwd               修改账号密码（交互式输入）": {"  ezssh passwd               修改账号密码（交互式输入）", "  ezssh passwd               Change account password (interactive input)"},
	"  ezssh route                修改登录路由（交互式输入）": {"  ezssh route                修改登录路由（交互式输入）", "  ezssh route                Change login route (interactive input)"},
	"  ezssh start                启动服务":            {"  ezssh start                启动服务", "  ezssh start                Start the service"},
	"  ezssh stop                 停止服务":            {"  ezssh stop                 停止服务", "  ezssh stop                 Stop the service"},
	"全局选项:":                               {"全局选项:", "Global options:"},
	"  --config <path>            指定配置文件路径（默认 ~/.ezssh/agent.json）": {"  --config <path>            指定配置文件路径（默认 ~/.ezssh/agent.json）", "  --config <path>            Config file path (default ~/.ezssh/agent.json)"},
	"  --lang <en|zh>             界面语言（默认 en）":       {"  --lang <en|zh>             界面语言（默认 en）", "  --lang <en|zh>             UI language (default en)"},
}

// T 返回当前语言下的消息文案（缺失 key 回退原文）。
func T(key string) string {
	if m, ok := messages[key]; ok {
		if lang == "zh" {
			return m[0]
		}
		return m[1]
	}
	return key
}

// Tf 格式化翻译（key 内含 %s/%d/%v 等占位符）。
func Tf(key string, args ...any) string {
	return fmt.Sprintf(T(key), args...)
}

// pf 打印到 stdout（无换行，用于提示语）。
func pf(key string, args ...any) { fmt.Printf(T(key), args...) }

// pl 打印到 stdout（带换行）。
func pl(key string, args ...any) { fmt.Printf(T(key)+"\n", args...) }

// epl 打印到 stderr（带换行）。
func epl(key string, args ...any) { fmt.Fprintf(os.Stderr, T(key)+"\n", args...) }

// errf 构造翻译后的错误。
func errf(key string, args ...any) error { return fmt.Errorf("%s", Tf(key, args...)) }
