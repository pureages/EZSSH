import { create } from 'zustand'
import { ws } from './ws'

export interface HwInfo {
  os: string
  distro: string
  distroName: string
  hostname: string
  uptime: number
}

/**
 * 系统识别 store：通过 monitor.hwinfo 一次性探测每个主机的系统/发行版，
 * 供桌面图标显示对应的 OS logo。同一主机只探测一次；右键刷新时清空重探。
 */
interface OsState {
  /** hostId -> 发行版 ID（如 ubuntu / alpine / debian），未识别则缺失 */
  distro: Record<string, string>
  /** hostId -> 发行版全名（如 "Ubuntu 22.04.3 LTS"），用于 tooltip 等 */
  distroName: Record<string, string>
  /** 已探测的主机（含失败），避免重复请求 */
  probed: Record<string, boolean>
  /** 加载指定主机集合的系统信息（跳过已探测的） */
  load: (hosts: { id: string; connected: boolean }[]) => void
  /** 清空全部探测结果（右键刷新时调用，图标回到图标A再重新识别） */
  resetAll: () => void
}

export const useOsStore = create<OsState>((set, get) => ({
  distro: {},
  distroName: {},
  probed: {},

  load: async (hosts) => {
    const need = hosts.filter((h) => h.connected && !get().probed[h.id])
    if (need.length === 0) return
    for (const h of need) {
      set((s) => ({ probed: { ...s.probed, [h.id]: true } }))
    }
    try {
      await ws.connect()
    } catch {
      return
    }
    let seq = 0
    for (const h of need) {
      const channel = `os_${Date.now().toString(36)}_${++seq}`
      const unsub = ws.onChannel(channel, (msg) => {
        unsub()
        if (msg.type !== 'monitor.hwinfo') return
        const info = (msg.payload?.info as HwInfo) || null
        if (!info) return
        set((s) => ({
          distro: { ...s.distro, [h.id]: info.distro },
          distroName: { ...s.distroName, [h.id]: info.distroName },
        }))
      })
      ws.send('monitor.hwinfo', channel, { hostId: h.id })
    }
  },

  resetAll: () => set({ distro: {}, distroName: {}, probed: {} }),
}))
