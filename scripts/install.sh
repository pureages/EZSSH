#!/usr/bin/env bash
#
# EZssh installer script
# EZssh 一键安装脚本
#
# Usage / 用法:
#   本地源码目录内:   bash scripts/install.sh
#   远程一键安装:     curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install.sh | bash
#                    (脚本会自动克隆源码到临时目录再编译)
#
# Features / 功能:
#   1. Detect platform/arch (linux / darwin / windows(msys,git-bash)) / 检测平台/架构
#   2. Build two binaries / 编译两个二进制:
#        ezsshd  → gateway server (daemon, cmd/ezssh) / 网关服务端（守护进程, cmd/ezssh）
#        ezssh   → terminal agent (wizard + menu, cmd/ezcli) / 终端管理 Agent（cmd/ezcli）
#   3. Optional: build web UI if node/npm and web/ exist / 可选: 若有 node/npm 且存在 web/ 则构建前端界面
#   4. Install to $BIN and launch `ezssh setup` wizard / 安装到 $BIN 并进入 `ezssh setup` 交互向导
#
# Install location / 产物安装位置:
#   root and /usr/local/bin writable → /usr/local/bin
#   otherwise                          → ~/.local/bin
set -euo pipefail

# ---- Step 1: choose installation language / 第一步: 选择安装语言 --------------
# 可用环境变量 EZSSH_LANG=en|zh 跳过交互式选择（便于脚本/管道调用）
LANG_CODE="en"
if [ -n "${EZSSH_LANG:-}" ]; then
  case "$EZSSH_LANG" in
    en|EN|english|English) LANG_CODE=en ;;
    zh|zh-CN|zh_CN|chinese|Chinese|中文) LANG_CODE=zh ;;
    *) LANG_CODE=en ;;
  esac
else
  echo ""
  echo "=============================================="
  echo " EZssh installer | 安装向导"
  echo " Select installation language | 请选择安装语言:"
  echo ""
  echo "   1) English"
  echo "   2) 简体中文"
  echo "=============================================="
  printf " > "
  read -r choice || true
  case "$choice" in
    1|en|EN|english|English) LANG_CODE=en ;;
    2|zh|zh-CN|zh_CN|chinese|Chinese|中文) LANG_CODE=zh ;;
    *) LANG_CODE=en ;;   # default English / 默认英文
  esac
fi

# Bilingual output / 双语输出: say "English text" "中文文本"
say() {
  if [ "$LANG_CODE" = "zh" ]; then
    printf '%s\n' "$2"
  else
    printf '%s\n' "$1"
  fi
}

# ---- Basic checks / 基础检测 --------------------------------------------------
command -v go >/dev/null 2>&1 || { say "Error: Go 1.25+ is required (go command not found)" "错误: 需要 Go 1.25+ (go 命令未找到)"; exit 1; }
GO_MAJOR=$(go version | sed -E 's/.*go([0-9]+)\.([0-9]+).*/\1/')
GO_MINOR=$(go version | sed -E 's/.*go([0-9]+)\.([0-9]+).*/\2/')
if [ "$GO_MAJOR" -lt 1 ] || { [ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 25 ]; }; then
  say "Error: Go 1.25+ is required, current: $(go version)" "错误: 需要 Go 1.25+，当前 $(go version)"
  exit 1
fi

# Platform / arch detection / 平台/架构检测
OS="$(uname -s)"
case "$OS" in
  Linux)  PLATFORM=linux ;;
  Darwin) PLATFORM=darwin ;;
  MINGW*|MSYS*|CYGWIN*) PLATFORM=windows ;;
  *) say "Error: unsupported platform $OS" "错误: 不支持的平台 $OS"; exit 1 ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  i386|i686) GOARCH=386 ;;
  *) say "Error: unsupported architecture $ARCH" "错误: 不支持的架构 $ARCH"; exit 1 ;;
esac

EXT=""
[ "$PLATFORM" = "windows" ] && EXT=".exe"

# Script directory (relationship between script and repo root) / 脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd || pwd)"

# Locate source tree / 定位源码目录:
#   本地仓库内执行 → 直接使用; 远程( curl | bash )执行 → 自动克隆到临时目录
REPO_DIR=""
if [ -f "$SCRIPT_DIR/../go.mod" ] && [ -d "$SCRIPT_DIR/../cmd" ]; then
  REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
else
  say "Source tree not found locally, cloning from GitHub ..." "本地未找到源码，正在从 GitHub 克隆 ..."
  command -v git >/dev/null 2>&1 || { say "Error: git is required for remote installation" "错误: 远程安装需要 git 命令"; exit 1; }
  TMP_DIR="$(mktemp -d 2>/dev/null || echo /tmp/ezssh_install)"
  say "  git clone https://github.com/pureages/EZSSH.git -> $TMP_DIR" "  git clone https://github.com/pureages/EZSSH.git -> $TMP_DIR"
  if ! git clone --depth 1 https://github.com/pureages/EZSSH.git "$TMP_DIR"; then
    say "Error: failed to clone repository, please check network/proxy." "错误: 克隆仓库失败，请检查网络/代理。"
    exit 1
  fi
  REPO_DIR="$TMP_DIR"
fi

# ---- Install directory / 安装目录 --------------------------------------------
BIN=""
if [ "$(id -u 2>/dev/null || echo 0)" -eq 0 ] && [ -w /usr/local/bin ]; then
  BIN=/usr/local/bin
else
  BIN="$HOME/.local/bin"
fi
mkdir -p "$BIN"

echo "=============================================="
say " EZssh installer (platform: $PLATFORM/$GOARCH)" " EZssh 安装向导 (平台: $PLATFORM/$GOARCH)"
say " Install to: $BIN" " 安装目录: $BIN"
echo "=============================================="

# ---- Build / 编译 -------------------------------------------------------------
echo
say "[1/4] Building server ezsshd ..." "[1/4] 编译服务端 ezsshd ..."
(cd "$REPO_DIR" && CGO_ENABLED=0 GOOS="$PLATFORM" GOARCH="$GOARCH" \
  go build -buildvcs=false -trimpath -ldflags "-s -w" \
  -o "$BIN/ezsshd$EXT" ./cmd/ezssh)

say "[2/4] Building terminal agent ezssh ..." "[2/4] 编译终端 Agent ezssh ..."
(cd "$REPO_DIR" && CGO_ENABLED=0 GOOS="$PLATFORM" GOARCH="$GOARCH" \
  go build -buildvcs=false -trimpath -ldflags "-s -w" \
  -o "$BIN/ezssh$EXT" ./cmd/ezcli)

# ---- Frontend build (optional) / 前端构建（可选）------------------------------
say "[3/4] Web UI ..." "[3/4] 前端界面 ..."
WEB_OK=0
if [ -d "$REPO_DIR/web" ] && command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
  say "      Detected web/ and node/npm, building frontend..." "      检测到 web/ 与 node/npm，构建前端..."
  (cd "$REPO_DIR/web" && npm install --silent && npm run build)
  WEB_OK=1
  say "      Frontend build completed." "      前端构建完成。"
else
  say "      Skipped (no web/ or node/npm detected)." "      跳过（未检测到 web/ 或 node/npm）。"
  say "      Will run in API-only mode: management menu and all REST APIs work, but the web UI is unavailable." "      将以 API-only 模式运行：管理菜单与全部 REST API 不受影响，但浏览器界面不可用。"
fi

# ---- PATH check / PATH 提示 ----------------------------------------------------
say "[4/4] Environment check ..." "[4/4] 环境检查 ..."
if ! echo ":$PATH:" | grep -q ":$BIN:"; then
  say "Note: $BIN is not in PATH." "注意: $BIN 不在 PATH 中。"
  say "  Add the following line to ~/.bashrc or ~/.zshrc:" "  请将以下行加入 ~/.bashrc 或 ~/.zshrc:"
  echo "    export PATH=\"$BIN:\$PATH\""
  say "  Then run: source ~/.bashrc" "  然后执行: source ~/.bashrc"
fi

echo
say "Installation complete." "安装完成。"
if [ "$WEB_OK" = "1" ]; then
  say "  ezsshd  server: $BIN/ezsshd$EXT" "  ezsshd  服务端: $BIN/ezsshd$EXT"
  say "  ezssh   terminal agent: $BIN/ezssh$EXT" "  ezssh   管理终端: $BIN/ezssh$EXT"
  say "  Frontend built to web/dist (served automatically by the server)" "  前端    界面已构建到 web/dist（由服务端自动托管）"
else
  say "  ezsshd  server: $BIN/ezsshd$EXT (API-only mode, no web UI)" "  ezsshd  服务端: $BIN/ezsshd$EXT (API-only 模式，无 Web 界面)"
  say "  ezssh   terminal agent: $BIN/ezssh$EXT" "  ezssh   管理终端: $BIN/ezssh$EXT"
fi
echo
say "Next, the initialization wizard will start (account/password/login route/port)..." "接下来进入初始化向导（账号/密码/登录路由/监听端口）..."
say "Press Enter to use defaults: admin / admin123456 / /login / 49466" "直接回车使用默认值: admin / admin123456 / /login / 49466"
echo

exec "$BIN/ezssh$EXT" setup
