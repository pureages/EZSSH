import { create } from 'zustand'
import { api } from './api'
import type { GeoInfo } from './types'

/**
 * 地理位置 store：以主机地址（IP/域名）为键缓存 geo 结果。
 * 后端已有 7 天内存缓存，此处仅做会话内去重，避免桌面图标反复请求。
 */
interface GeoState {
  /** 地址 -> 地理位置信息（含国家/区划/经纬度） */
  byAddr: Record<string, GeoInfo>
  loading: boolean
  /** 批量加载一批地址，已缓存的跳过 */
  load: (addrs: string[]) => Promise<void>
  /** 清空缓存（刷新时重新探测） */
  clear: () => void
}

export const useGeoStore = create<GeoState>((set, get) => ({
  byAddr: {},
  loading: false,

  load: async (addrs) => {
    const uniq = Array.from(new Set(addrs.filter(Boolean).map((a) => a.trim())))
    const need = uniq.filter((a) => !get().byAddr[a])
    if (need.length === 0) return
    if (get().loading) return
    set({ loading: true })
    try {
      const map = await api.geo(need)
      set((s) => ({ byAddr: { ...s.byAddr, ...map } }))
    } catch {
      /* 网络/后端错误静默：图标保持无徽标 */
    } finally {
      set({ loading: false })
    }
  },

  clear: () => set({ byAddr: {}, loading: false }),
}))
