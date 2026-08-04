/**
 * 文件管理器 / 一键命令 → 终端 的启动上下文传递。
 * 按 hostId 键控的待办表：登记初始目录与可选初始命令，终端组件挂载时消费：
 * 自动 cd 到初始目录，并在必要时自动执行初始命令。
 * 用 Map 替代单槽，保证「一键命令」对多台服务器同时开终端时各自携带自己的命令，
 * 不会互相覆盖；带 30s 过期防止陈旧命令误入后续打开的同主机终端。
 */
interface PendingContext {
  hostId: string
  cwd: string
  /** 终端打开后自动执行的命令（可选） */
  command?: string
  /** 登记时间戳（毫秒），消费时超过 TTL 则丢弃 */
  at: number
}

const pendingByHost = new Map<string, PendingContext>()
const TTL = 30_000

export function setPendingTerminalCwd(hostId: string, cwd: string, command?: string) {
  pendingByHost.set(hostId, { hostId, cwd, command, at: Date.now() })
}

export function consumePendingTerminalCwd(hostId: string): {
  cwd: string | null
  command: string | null
} {
  const p = pendingByHost.get(hostId)
  if (p) {
    pendingByHost.delete(hostId)
    if (Date.now() - p.at > TTL) return { cwd: null, command: null }
    return { cwd: p.cwd, command: p.command ?? null }
  }
  return { cwd: null, command: null }
}
