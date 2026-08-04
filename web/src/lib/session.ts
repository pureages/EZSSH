import { create } from 'zustand'

interface SessionState {
  /** null=未知（正在校验），true=已登录，false=未登录 */
  authed: boolean | null
  /** 自定义登录路由（设置中可修改，默认 /login） */
  loginRoute: string
  setAuthed: (v: boolean) => void
  setLoginRoute: (route: string) => void
}

/**
 * 全局会话状态：所有 401 统一触发 setAuthed(false)，
 * 由路由层根据 authed 决定重定向，避免页面间互相跳转造成死循环。
 */
export const useSession = create<SessionState>((set) => ({
  authed: null,
  loginRoute: '/login',
  setAuthed: (authed) => set({ authed }),
  setLoginRoute: (loginRoute) => set({ loginRoute }),
}))
