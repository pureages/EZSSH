#!/usr/bin/env bash
#
# EZssh installer script
# EZssh 一键安装脚本
#
# Usage / 用法:
#   bash <(curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install.sh)
#
# 说明: 本脚本直接下载 GitHub Releases 上的预编译产物（服务端 + Agent + 前端），
#       无需安装 Go / Node.js，也不会拉取源码。
# This script downloads prebuilt artifacts from GitHub Releases (server + agent + frontend).
# No Go / Node.js required, and it never clones the source tree.
#
# 可配置环境变量:
#   EZSSH_LANG=en|zh        安装界面语言
#   EZSSH_VERSION=v0.0.2    指定版本（默认 latest release）
#   EZSSH_BIN=/path         安装目录（默认 root 时 /usr/local/bin，否则 ~/.local/bin）
#   EZSSH_WEB=/path/web/dist  前端安装位置（默认 ~/.ezssh/web/dist）
#
# Features / 功能:
#   1. 选择安装语言
#   2. 检测平台/架构
#   3. 从 GitHub Releases 下载预编译包
#   4. 安装 ezsshd / ezssh 到 $BIN，前端到 ~/.ezssh/web/dist
#   5. 进入 `ezssh setup` 交互向导
set -euo pipefail

# ---- Step 1: choose installation language / 第一步: 选择安装语言 --------------
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
for tool in curl tar; do
  command -v "$tool" >/dev/null 2>&1 || { say "Error: $tool is required" "错误: 需要 $tool 命令"; exit 1; }
done

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

# ---- Resolve version / 解析版本 ----------------------------------------------
REPO="pureages/EZSSH"
VERSION="${EZSSH_VERSION:-}"
if [ -z "$VERSION" ]; then
  say "Resolving latest release ..." "正在获取最新版本 ..."
  RELEASE_JSON="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest")"
  TAG="$(printf '%s' "$RELEASE_JSON" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
  if [ -z "$TAG" ]; then
    say "Error: failed to resolve latest release" "错误: 无法获取最新版本"
    exit 1
  fi
  VERSION="$TAG"
fi
VERSION="${VERSION#v}"

ASSET="ezssh-${VERSION}-${PLATFORM}-${GOARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/v${VERSION}/${ASSET}"

# ---- Install directory / 安装目录 --------------------------------------------
BIN="${EZSSH_BIN:-}"
if [ -z "$BIN" ]; then
  if [ "$(id -u 2>/dev/null || echo 0)" -eq 0 ] && [ -w /usr/local/bin ]; then
    BIN=/usr/local/bin
  else
    BIN="$HOME/.local/bin"
  fi
fi
mkdir -p "$BIN"

# 前端安装位置（默认 ~/.ezssh/web/dist）
WEB_DIR="${EZSSH_WEB:-$HOME/.ezssh/web/dist}"
mkdir -p "$(dirname "$WEB_DIR")"

echo "=============================================="
say " EZssh installer (platform: $PLATFORM/$GOARCH)" " EZssh 安装向导 (平台: $PLATFORM/$GOARCH)"
say " Version:  v$VERSION" " 版本:     v$VERSION"
say " Install to: $BIN" " 安装目录: $BIN"
say " Web dist:  $WEB_DIR" " 前端目录: $WEB_DIR"
echo "=============================================="

# ---- Download & install / 下载并安装 ----------------------------------------
echo
say "[1/2] Downloading $ASSET ..." "[1/2] 正在下载 $ASSET ..."
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if ! curl -fsSL -o "$TMP/$ASSET" "$URL"; then
  say "Error: download failed. Check network/proxy, or set EZSSH_VERSION."
  say "Available assets are built on tag push (see GitHub Actions 'release')."
  say "错误: 下载失败。请检查网络/代理，或通过 EZSSH_VERSION 指定版本。"
  say "发布包在推送 v* 标签后由 GitHub Actions 自动构建。"
  exit 1
fi

say "[2/2] Installing ..." "[2/2] 正在安装 ..."
tar -xzf "$TMP/$ASSET" -C "$TMP"

# 二进制
cp "$TMP/ezsshd$EXT" "$BIN/ezsshd$EXT"
cp "$TMP/ezssh$EXT" "$BIN/ezssh$EXT"
chmod +x "$BIN/ezsshd$EXT" "$BIN/ezssh$EXT"

# 前端
mkdir -p "$WEB_DIR"
cp -r "$TMP"/web/. "$WEB_DIR"/

# ---- PATH check / PATH 提示 ----------------------------------------------------
if ! echo ":$PATH:" | grep -q ":$BIN:"; then
  say "Note: $BIN is not in PATH." "注意: $BIN 不在 PATH 中。"
  say "  Add the following line to ~/.bashrc or ~/.zshrc:" "  请将以下行加入 ~/.bashrc 或 ~/.zshrc:"
  echo "    export PATH=\"$BIN:\$PATH\""
  say "  Then run: source ~/.bashrc" "  然后执行: source ~/.bashrc"
fi

echo
say "Installation complete." "安装完成。"
say "  ezsshd   server:  $BIN/ezsshd$EXT" "  ezsshd  服务端:  $BIN/ezsshd$EXT"
say "  ezssh    terminal: $BIN/ezssh$EXT" "  ezssh   管理终端: $BIN/ezssh$EXT"
say "  frontend:         $WEB_DIR" "  前端:            $WEB_DIR"
echo
say "Next, the initialization wizard will start (account/password/login route/port)..." "接下来进入初始化向导（账号/密码/登录路由/监听端口）..."
say "Press Enter to use defaults: admin / admin123456 / /login / 49466" "直接回车使用默认值: admin / admin123456 / /login / 49466"
echo

exec "$BIN/ezssh$EXT" setup --lang "$LANG_CODE"
