#!/usr/bin/env bash
#
# EZssh 国内一键安装脚本（基于 Gitee 镜像）
# EZssh CN installer (via Gitee mirror)
#
# 用法 / Usage:
#   bash <(curl -fsSL https://gitee.com/pureages/EZSSH/raw/master/scripts/install-cn.sh)
#
# 说明: 国内网络访问 GitHub 受限时使用本脚本。它从 Gitee 镜像仓库拉取安装脚本，
#       并以 gitee 为下载源获取预编译产物（服务端 + Agent + 前端）。
# 本脚本本身逻辑与 install.sh 完全一致，仅固定 download source = gitee。
set -euo pipefail

# 从 Gitee 拉取标准安装脚本，固定使用 gitee 源，再以当前终端执行。
# 分支名统一为 master（Gitee 默认分支）。
TMP_SCRIPT="$(mktemp)"
trap 'rm -f "$TMP_SCRIPT"' EXIT

if ! curl -fsSL -o "$TMP_SCRIPT" \
  "https://gitee.com/pureages/EZSSH/raw/master/scripts/install.sh"; then
  echo "错误: 无法从 Gitee 拉取 install.sh，请检查网络或仓库地址。"
  echo "Error: failed to fetch install.sh from Gitee."
  exit 1
fi

# 固定 gitee 源执行
export EZSSH_SRC=gitee
exec bash "$TMP_SCRIPT"
