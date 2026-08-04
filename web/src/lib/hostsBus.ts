/** 极简事件总线：通知「主机列表已变化」（设置页隐藏/显示全部后刷新桌面）。 */
type Listener = () => void

const listeners = new Set<Listener>()

export function onHostsChanged(fn: Listener): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

export function emitHostsChanged() {
  for (const fn of [...listeners]) fn()
}
