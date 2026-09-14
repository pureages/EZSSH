import { create } from 'zustand'

interface SessionState {
  /** null=未知（正在校验），true=已登录，false=未登录 */
  authed: boolean | null
  setAuthed: (v: boolean) => void
}

/**
 * 全局会话状态：所有 401 统一触发 setAuthed(false)，
 * 由路由层根据 authed 决定重定向，避免页面间互相跳转造成死循环。
 *
 * 注意：这里不保存登录路由（安全路由）——服务端不下发该值，
 * 登录入口是否放行由 LoginGate 通过 /api/route-check 判定。
 */
export const useSession = create<SessionState>((set) => ({
  authed: null,
  setAuthed: (authed) => set({ authed }),
}))
