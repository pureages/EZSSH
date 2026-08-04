import { create } from 'zustand'

export interface WinState {
  id: string
  appId: string
  title: string
  icon: string
  hostId: string | null
  /** 目标主机系统类型（"" 表示未知/未探测） */
  platform: string
  channelId: string
  x: number
  y: number
  width: number
  height: number
  minimized: boolean
  maximized: boolean
  z: number
  /** 标题模板键（用于语言切换时重新翻译；onTitle 设置自定义标题后清除） */
  titleKey?: string
  titleArgs?: Array<string | number>
}

export type OpenWindow = Omit<WinState, 'minimized' | 'maximized' | 'z'>

interface WinStore {
  windows: WinState[]
  zTop: number
  open: (w: OpenWindow) => string
  close: (id: string) => void
  focus: (id: string) => void
  minimize: (id: string) => void
  restore: (id: string) => void
  toggleMaximize: (id: string) => void
  move: (id: string, x: number, y: number) => void
  resize: (id: string, x: number, y: number, width: number, height: number) => void
  setTitle: (id: string, title: string) => void
  /** 更新窗口标题模板（模板标题随语言重译；应用 onTitle 设置自定义标题后应改用 setTitle） */
  setTitleTemplate: (id: string, titleKey?: string, titleArgs?: Array<string | number>) => void
}

export const useWindowStore = create<WinStore>((set, get) => ({
  windows: [],
  zTop: 10,

  open: (w) => {
    const z = get().zTop + 1
    set((s) => ({
      zTop: z,
      windows: [...s.windows, { ...w, minimized: false, maximized: false, z }],
    }))
    return w.id
  },

  close: (id) => set((s) => ({ windows: s.windows.filter((x) => x.id !== id) })),

  focus: (id) =>
    set((s) => {
      const z = s.zTop + 1
      return {
        zTop: z,
        windows: s.windows.map((x) =>
          x.id === id ? { ...x, z, minimized: false } : x,
        ),
      }
    }),

  minimize: (id) =>
    set((s) => ({
      windows: s.windows.map((x) => (x.id === id ? { ...x, minimized: true } : x)),
    })),

  restore: (id) => get().focus(id),

  toggleMaximize: (id) =>
    set((s) => ({
      windows: s.windows.map((x) =>
        x.id === id ? { ...x, maximized: !x.maximized } : x,
      ),
    })),

  move: (id, x, y) =>
    set((s) => ({
      windows: s.windows.map((w) => (w.id === id ? { ...w, x, y } : w)),
    })),

  resize: (id, x, y, width, height) =>
    set((s) => ({
      windows: s.windows.map((w) => (w.id === id ? { ...w, x, y, width, height } : w)),
    })),

  setTitle: (id, title) =>
    set((s) => ({
      windows: s.windows.map((w) => {
        if (w.id !== id) return w
        const { titleKey, titleArgs, ...rest } = w
        return { ...rest, title }
      }),
    })),

  setTitleTemplate: (id, titleKey, titleArgs) =>
    set((s) => ({
      windows: s.windows.map((w) =>
        w.id === id ? { ...w, titleKey, titleArgs } : w,
      ),
    })),
}))
