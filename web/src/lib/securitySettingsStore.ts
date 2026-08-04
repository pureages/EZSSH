import { create } from 'zustand'

interface SecuritySettingsState {
  /** 文件管理器窗口标题是否隐藏服务器登录用户名（后端 settings 表持久化） */
  hideFmUsername: boolean
  /** 是否已从后端拉取过设置 */
  ready: boolean
  /** 用 GET /api/settings 的返回值填充（hide_fm_username: "0"|"1"） */
  hydrate: (s: { hide_fm_username?: string }) => void
  setHideFmUsername: (v: boolean) => void
}

/**
 * 安全相关设置（后端持久化，任何设备一致）：
 * 当前仅「隐藏文件管理器用户名」。桌面窗口标题与设置页共享此 store，
 * 修改即时反映到已打开的文件管理器窗口标题。
 */
export const useSecuritySettings = create<SecuritySettingsState>((set) => ({
  hideFmUsername: false,
  ready: false,
  hydrate: (s) =>
    set({ hideFmUsername: s.hide_fm_username === '1', ready: true }),
  setHideFmUsername: (v) => set({ hideFmUsername: v }),
}))
