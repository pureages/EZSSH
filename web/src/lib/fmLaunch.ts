/**
 * 网站管理 → 文件管理器 的启动上下文传递。
 * 按 hostId 键控的单槽：登记初始目录，文件管理器组件挂载时消费并自动导航到该目录。
 * 带 30s 过期防止陈旧路径误入后续打开的同主机文件管理器。
 */
const pendingByHost = new Map<string, { cwd: string; at: number }>()
const TTL = 30_000

export function setPendingFmCwd(hostId: string, cwd: string) {
  pendingByHost.set(hostId, { cwd, at: Date.now() })
}

export function consumePendingFmCwd(hostId: string): string | null {
  const p = pendingByHost.get(hostId)
  if (p) {
    pendingByHost.delete(hostId)
    if (Date.now() - p.at > TTL) return null
    return p.cwd
  }
  return null
}
