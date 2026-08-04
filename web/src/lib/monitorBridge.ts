import { ws } from './ws'
import { useMonitorStore, toPoint } from './monitorStore'
import type { MonitorSnapshot } from './types'

const DESKTOP_SUB_ID = 'desktop'

let listening = false
const subscribed = new Set<string>()

/**
 * 桌面监控桥：全局监听 monitor.data，写入 monitorStore。
 * 图标微型监控与监控窗口共用此数据源。
 */
export function startMonitorBridge() {
  if (listening) return
  listening = true
  ws.onType('monitor.data', (msg) => {
    const payload = msg.payload as { hostId?: string; snapshot?: MonitorSnapshot } | undefined
    if (!payload?.hostId || !payload.snapshot) return
    useMonitorStore.getState().push(payload.hostId, toPoint(payload.snapshot))
  })
}

/** 让桌面桥订阅给定主机集合（增删自动同步）。 */
export function syncDesktopSubscriptions(hostIds: string[]) {
  const want = new Set(hostIds)
  for (const id of want) {
    if (!subscribed.has(id)) {
      ws.send('monitor.subscribe', '', { hostId: id, subId: DESKTOP_SUB_ID })
      subscribed.add(id)
    }
  }
  for (const id of [...subscribed]) {
    if (!want.has(id)) {
      ws.send('monitor.unsubscribe', '', { hostId: id, subId: DESKTOP_SUB_ID })
      subscribed.delete(id)
      useMonitorStore.getState().clear(id)
    }
  }
}

/** 退订全部桌面监控。 */
export function stopMonitorBridge() {
  for (const id of [...subscribed]) {
    ws.send('monitor.unsubscribe', '', { hostId: id, subId: DESKTOP_SUB_ID })
  }
  subscribed.clear()
}
