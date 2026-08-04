#!/usr/bin/env bash
#
# EZssh 一键安装脚本
#
# 用法:
#   bash install.sh
#   curl -fsSL https://example.com/install.sh | bash    # 需先拉取到本地
#
# 功能:
#   1. 检测平台/架构（linux / darwin / windows(msys,git-bash)）
#   2. 编译两个二进制:
#        ezsshd  → 网关服务端（守护进程, cmd/ezssh）
#        ezssh   → 终端管理 Agent（安装向导 + 交互菜单, cmd/ezcli）
#   3. 可选: 若本机有 node/npm 且存在 web/ 目录，则构建前端界面
#   4. 安装到 $BIN 并进入 `ezssh setup` 交互向导
#
# 产物安装位置:
#   root 且 /usr/local/bin 可写 → /usr/local/bin
#   否则                          → ~/.local/bin
set -euo pipefail

# ---- 基础检测 -----------------------------------------------------------
command -v go >/dev/null 2>&1 || { echo "错误: 需要 Go 1.25+ (go 命令未找到)"; exit 1; }
GO_MAJOR=$(go version | sed -E 's/.*go([0-9]+)\.([0-9]+).*/\1/')
GO_MINOR=$(go version | sed -E 's/.*go([0-9]+)\.([0-9]+).*/\2/')
if [ "$GO_MAJOR" -lt 1 ] || { [ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 25 ]; }; then
  echo "错误: 需要 Go 1.25+，当前 $(go version)"
  exit 1
fi

# 平台/架构检测
OS="$(uname -s)"
case "$OS" in
  Linux)  PLATFORM=linux ;;
  Darwin) PLATFORM=darwin ;;
  MINGW*|MSYS*|CYGWIN*) PLATFORM=windows ;;
  *) echo "错误: 不支持的平台 $OS"; exit 1 ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  i386|i686) GOARCH=386 ;;
  *) echo "错误: 不支持的架构 $ARCH"; exit 1 ;;
esac

EXT=""
[ "$PLATFORM" = "windows" ] && EXT=".exe"

# 脚本所在目录（安装脚本与仓库根目录的关系）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# ---- 安装目录 ------------------------------------------------------------
BIN=""
if [ "$(id -u 2>/dev/null || echo 0)" -eq 0 ] && [ -w /usr/local/bin ]; then
  BIN=/usr/local/bin
else
  BIN="$HOME/.local/bin"
fi
mkdir -p "$BIN"

echo "=============================================="
echo " EZssh 安装向导 (平台: $PLATFORM/$GOARCH)"
echo " 安装目录: $BIN"
echo "=============================================="

# ---- 编译 ---------------------------------------------------------------
echo
echo "[1/4] 编译服务端 ezsshd ..."
(cd "$REPO_DIR" && CGO_ENABLED=0 GOOS="$PLATFORM" GOARCH="$GOARCH" \
  go build -buildvcs=false -trimpath -ldflags "-s -w" \
  -o "$BIN/ezsshd$EXT" ./cmd/ezssh)

echo "[2/4] 编译终端 Agent ezssh ..."
(cd "$REPO_DIR" && CGO_ENABLED=0 GOOS="$PLATFORM" GOARCH="$GOARCH" \
  go build -buildvcs=false -trimpath -ldflags "-s -w" \
  -o "$BIN/ezssh$EXT" ./cmd/ezcli)

# ---- 前端构建（可选）-----------------------------------------------------
echo "[3/4] 前端界面 ..."
WEB_OK=0
if [ -d "$REPO_DIR/web" ] && command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
  echo "      检测到 web/ 与 node/npm，构建前端..."
  (cd "$REPO_DIR/web" && npm install --silent && npm run build)
  WEB_OK=1
  echo "      前端构建完成。"
else
  echo "      跳过（未检测到 web/ 或 node/npm）。"
  echo "      将以 API-only 模式运行：管理菜单与全部 REST API 不受影响，但浏览器界面不可用。"
fi

# ---- PATH 提示 ------------------------------------------------------------
echo "[4/4] 环境检查 ..."
if ! echo ":$PATH:" | grep -q ":$BIN:"; then
  echo "注意: $BIN 不在 PATH 中。"
  echo "  请将以下行加入 ~/.bashrc 或 ~/.zshrc:"
  echo "    export PATH=\"$BIN:\$PATH\""
  echo "  然后执行: source ~/.bashrc"
fi

echo
echo "安装完成。"
if [ "$WEB_OK" = "1" ]; then
  echo "  ezsshd  服务端: $BIN/ezsshd$EXT"
  echo "  ezssh   管理终端: $BIN/ezssh$EXT"
  echo "  前端    界面已构建到 web/dist（由服务端自动托管）"
else
  echo "  ezsshd  服务端: $BIN/ezsshd$EXT (API-only 模式，无 Web 界面)"
  echo "  ezssh   管理终端: $BIN/ezssh$EXT"
fi
echo
echo "接下来进入初始化向导（账号/密码/登录路由/监听端口）..."
echo "直接回车使用默认值: admin / admin123456 / /login / 49466"
echo

exec "$BIN/ezssh$EXT" setup
