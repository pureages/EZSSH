import { create } from 'zustand'
import type { MonitorSnapshot } from './types'

export interface MonitorPoint {
  ts: number
  cpu: number
  memPct: number
  swapPct: number
  rx: number // 合计下载速率 B/s
  tx: number // 合计上传速率 B/s
  /** 硬盘使用率（根分区 / 优先，否则取第一个分区） */
  diskPct: number
  /** 累计上传字节 */
  txBytes: number
  /** 累计下载字节 */
  rxBytes: number
  memUsed: number
  memTotal: number
  load1: number
  /** 后端采集失败的错误信息（此时其它指标均为 0/空，应显示为已离线） */
  error?: string
}

const MAX_POINTS = 60

interface MonitorState {
  /** 每主机最新一次采样 */
  latest: Record<string, MonitorPoint>
  /** 每主机最近 60 点时间序列（监控窗口画图用） */
  series: Record<string, MonitorPoint[]>
  push: (hostId: string, p: MonitorPoint) => void
  clear: (hostId: string) => void
}

export const useMonitorStore = create<MonitorState>((set) => ({
  latest: {},
  series: {},
  push: (hostId, p) =>
    set((s) => {
      const arr = [...(s.series[hostId] ?? []), p]
      if (arr.length > MAX_POINTS) arr.shift()
      return {
        latest: { ...s.latest, [hostId]: p },
        series: { ...s.series, [hostId]: arr },
      }
    }),
  clear: (hostId) =>
    set((s) => {
      const latest = { ...s.latest }
      delete latest[hostId]
      const series = { ...s.series }
      delete series[hostId]
      return { latest, series }
    }),
}))

export function toPoint(snap: MonitorSnapshot): MonitorPoint {
  const rx = (snap.net ?? []).reduce((a, n) => a + (n.rx_bps ?? 0), 0)
  const tx = (snap.net ?? []).reduce((a, n) => a + (n.tx_bps ?? 0), 0)
  const rxBytes = (snap.net ?? []).reduce((a, n) => a + (n.rx_bytes ?? 0), 0)
  const txBytes = (snap.net ?? []).reduce((a, n) => a + (n.tx_bytes ?? 0), 0)
  // 硬盘使用率：优先根分区 /
  const disks = snap.disks ?? []
  const disk = disks.find((d) => d.mount === '/') ?? disks[0]
  return {
    ts: snap.ts,
    cpu: snap.cpu ?? 0,
    memPct: snap.mem_pct ?? 0,
    swapPct: snap.swap_pct ?? 0,
    rx,
    tx,
    diskPct: disk?.pct ?? 0,
    txBytes,
    rxBytes,
    memUsed: snap.mem_used ?? 0,
    memTotal: snap.mem_total ?? 0,
    load1: snap.load1 ?? 0,
    error: snap.error,
  }
}
