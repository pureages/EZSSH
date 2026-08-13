#!/usr/bin/env bash
# pi-SSH-Agent (pissh) 安装脚本
# 在 EZSSH 网关服务器上执行，安装 pi (coding agent) 并注册 EZSSH SSH 扩展，
# 使 AI 可通过 EZSSH 已纳管的服务器凭证执行命令。
#
# 用法:
#   curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install-pissh.sh | bash -s -- --token <AGENT_TOKEN>
#   或: bash install-pissh.sh --token <AGENT_TOKEN> [--base-url http://127.0.0.1:49466]
set -euo pipefail

TOKEN=""
BASE_URL="http://127.0.0.1:49466"
PI_PKG="@earendil-works/pi-coding-agent"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --token) TOKEN="${2:-}"; shift 2 ;;
    --base-url) BASE_URL="${2:-}"; shift 2 ;;
    -h|--help) grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 1 ;;
  esac
done

if [[ -z "$TOKEN" ]]; then
  echo "错误: 缺少 --token（在 EZSSH 应用中心打开 pi-SSH-Agent 获取安装命令）" >&2
  exit 1
fi

if [[ "$(id -u)" -ne 0 ]]; then
  SUDO="sudo"
else
  SUDO=""
fi

echo "[1/4] 检查 Node.js ..."
if ! command -v node >/dev/null 2>&1; then
  echo "  未检测到 node，尝试安装 ..."
  if command -v apt-get >/dev/null 2>&1; then
    $SUDO apt-get update -y >/dev/null
    $SUDO apt-get install -y nodejs npm >/dev/null
  elif command -v dnf >/dev/null 2>&1; then
    $SUDO dnf install -y nodejs npm >/dev/null
  elif command -v yum >/dev/null 2>&1; then
    $SUDO yum install -y nodejs npm >/dev/null
  else
    echo "  无法自动安装 Node.js，请先手动安装后重试。" >&2
    exit 1
  fi
fi
echo "  node $(node -v)"

echo "[2/4] 安装 pi ($PI_PKG) ..."
if ! npm ls -g --depth=0 "$PI_PKG" >/dev/null 2>&1; then
  $SUDO npm install -g "$PI_PKG"
fi
echo "  pi 已安装: $(command -v pi || echo '见 ~/.npm-global/bin')"

PI_HOME="${HOME}/.pi/agent"
EXT_DIR="${PI_HOME}/extensions"
mkdir -p "$EXT_DIR"

echo "[3/4] 写入 EZSSH SSH 扩展与配置 ..."
# agent 配置（token / 网关地址）
cat > "${PI_HOME}/ezssh-agent.json" <<EOF
{
  "base_url": "${BASE_URL}",
  "token": "${TOKEN}"
}
EOF
chmod 600 "${PI_HOME}/ezssh-agent.json"

# pi 扩展：注册 list_hosts / ssh_exec 两个工具
cat > "${EXT_DIR}/ezssh-agent.ts" <<'TSEOF'
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

function loadConfig(): { base_url: string; token: string } {
  try {
    const raw = readFileSync(join(homedir(), ".pi/agent/ezssh-agent.json"), "utf8");
    const cfg = JSON.parse(raw);
    return {
      base_url: process.env.EZSSH_BASE_URL || cfg.base_url || "http://127.0.0.1:49466",
      token: process.env.EZSSH_AGENT_TOKEN || cfg.token || "",
    };
  } catch {
    return {
      base_url: process.env.EZSSH_BASE_URL || "http://127.0.0.1:49466",
      token: process.env.EZSSH_AGENT_TOKEN || "",
    };
  }
}

const CFG = loadConfig();

async function callApi(path: string, body?: unknown): Promise<any> {
  const res = await fetch(`${CFG.base_url}${path}`, {
    method: body ? "POST" : "GET",
    headers: {
      "Content-Type": "application/json",
      "X-Agent-Token": CFG.token,
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error((data && data.error) || `HTTP ${res.status}`);
  return data;
}

export default function (pi: ExtensionAPI) {
  pi.registerTool({
    name: "list_hosts",
    label: "List Hosts",
    description:
      "列出 EZSSH 网关中保存的所有服务器（名称、地址、端口、登录用户、系统平台）。当需要知道有哪些可用服务器时使用。",
    promptSnippet: "列出 EZSSH 网关纳管的所有服务器",
    promptGuidelines: [
      "当用户提到某台服务器但你不确定有哪些主机时，先调用 list_hosts 获取清单。",
    ],
    parameters: Type.Object({}),
    async execute() {
      const data = await callApi("/api/agent/hosts");
      const hosts = (data && data.hosts) || [];
      const text =
        hosts
          .map(
            (h: any) =>
              `${h.name} (${h.username}@${h.host}:${h.port}${h.platform ? ", " + h.platform : ""})`,
          )
          .join("\n") || "(no hosts)";
      return { content: [{ type: "text", text }] };
    },
  });

  pi.registerTool({
    name: "ssh_exec",
    label: "SSH Exec",
    description:
      "在 EZSSH 网关保存的某台服务器上执行 shell 命令并返回输出。host 参数接受主机名称（如 home99）或 ID。先用 list_hosts 确认主机名。",
    promptSnippet: "在已纳管的服务器上执行 SSH 命令",
    promptGuidelines: [
      "需要在某台服务器上执行命令时使用 ssh_exec，host 传主机名称或 ID。",
      "执行前先确认目标主机存在（可用 list_hosts），命令尽量只读；写操作前先向用户确认。",
    ],
    parameters: Type.Object({
      host: Type.String({ description: "主机名称或 ID" }),
      command: Type.String({ description: "要执行的 shell 命令" }),
    }),
    async execute(_id: string, params: { host: string; command: string }) {
      const data = await callApi("/api/agent/exec", {
        host: params.host,
        command: params.command,
      });
      let text = data.output || "";
      if (data.error) text += (text ? "\n" : "") + `[exit error] ${data.error}`;
      if (!text) text = "(no output)";
      // 截断大输出，避免撑爆上下文
      if (text.length > 48000) text = text.slice(0, 48000) + "\n... (truncated)";
      return { content: [{ type: "text", text }] };
    },
  });
}
TSEOF
echo "  扩展: ${EXT_DIR}/ezssh-agent.ts"

echo "[4/4] 创建 pissh 启动命令 ..."
WRAPPER="/usr/local/bin/pissh"
$SUDO tee "$WRAPPER" >/dev/null <<'WEOF'
#!/usr/bin/env bash
# pi-SSH-Agent 启动器：以 pi 为载体，自动加载 EZSSH SSH 扩展。
exec pi "$@"
WEOF
$SUDO chmod +x "$WRAPPER"
echo "  命令: $WRAPPER"

echo ""
echo "✅ pi-SSH-Agent 安装完成！"
echo "   运行 'pissh' 进入 AI 聊天界面。"
echo "   首次使用请在 pissh 中配置模型（/login 或 provider API key）。"
echo "   示例: \"把 home99 的硬件信息给我看一下。\""
