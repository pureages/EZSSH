package main

import (
	"os"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run 解析参数并分发：
//
//	ezssh setup --lang en|zh       → 交互式安装向导
//	ezssh                          → 交互式管理菜单
//	ezssh status|account|passwd|route|start|stop → 一次性子命令
//	全局 --config <path>            → 指定配置文件（默认 ~/.ezssh/agent.json）
//	全局 --lang <en|zh>             → 界面语言（默认 en）
func run(args []string) int {
	configPath := ""
	langArg := ""
	sub := ""
	help := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config", "-c":
			if i+1 < len(args) {
				configPath = args[i+1]
				i++
			}
		case "--lang", "-l":
			if i+1 < len(args) {
				langArg = args[i+1]
				i++
			}
		case "help", "-h", "--help":
			help = true
		default:
			if sub == "" && !strings.HasPrefix(args[i], "-") {
				sub = args[i]
			}
		}
	}

	if langArg != "" {
		setLang(langArg)
	}
	if help {
		printUsage()
		return 0
	}

	if configPath == "" {
		p, err := DefaultConfigPath()
		if err != nil {
			epl("无法确定配置路径: %v", err)
			return 1
		}
		configPath = p
	}

	if sub == "setup" {
		if err := cmdSetup(configPath, langArg); err != nil {
			epl("安装向导失败: %v", err)
			return 1
		}
		return 0
	}

	cfg, err := Load(configPath)
	if err == ErrNoConfig {
		pl("尚未配置，请先运行: ezssh setup")
		return 1
	}
	if err != nil {
		epl("读取配置失败: %v", err)
		return 1
	}
	// --lang 仅对本次会话生效（非 setup 不写回配置）；否则使用配置持久化的语言。
	if langArg != "" {
		setLang(langArg)
	} else {
		setLang(cfg.Lang)
	}

	switch sub {
	case "", "menu":
		if err := cmdMenu(cfg); err != nil {
			epl("错误: %v", err)
			return 1
		}
	case "status", "account", "passwd", "route", "start", "stop":
		if err := cmdOneShot(cfg, sub); err != nil {
			epl("错误: %v", err)
			return 1
		}
	default:
		epl("未知命令: %s\n\n", sub)
		printUsage()
		return 1
	}
	return 0
}

// printUsage 打印帮助信息。
func printUsage() {
	pl("EZSSH 终端管理 Agent v%s", appVersion)
	pl("")
	pl("用法:")
	pl("  ezssh setup                交互式安装向导（首次运行）")
	pl("  ezssh                      打开交互式管理菜单")
	pl("  ezssh status               查看运行状态")
	pl("  ezssh account              查看账号信息")
	pl("  ezssh passwd               修改账号密码（交互式输入）")
	pl("  ezssh route                修改登录路由（交互式输入）")
	pl("  ezssh start                启动服务")
	pl("  ezssh stop                 停止服务")
	pl("")
	pl("全局选项:")
	pl("  --config <path>            指定配置文件路径（默认 ~/.ezssh/agent.json）")
	pl("  --lang <en|zh>             界面语言（默认 en）")
}
