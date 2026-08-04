import { create } from 'zustand'

/** 单个复制/剪切条目：源路径 + 展示名 */
export interface ClipEntry {
  /** 源完整路径 */
  path: string
  /** 条目名称（用于展示） */
  name: string
  isDir: boolean
}

/** 文件管理器剪贴板：记录从哪台服务器复制/剪切了什么（支持多选） */
export interface ClipItem {
  /** 源主机 ID */
  hostId: string
  /** 复制的条目列表（多选时为多个） */
  items: ClipEntry[]
  /** copy=复制；cut=剪切（粘贴成功后删除源） */
  action: 'copy' | 'cut'
}

interface ClipboardState {
  item: ClipItem | null
  setItem: (item: ClipItem | null) => void
  clear: () => void
}

/**
 * 全局文件剪贴板：跨窗口、跨服务器共享。
 * 在服务器 A 的文件管理器窗口复制，切到服务器 B 的文件管理器窗口粘贴。
 */
export const useClipboardStore = create<ClipboardState>((set) => ({
  item: null,
  setItem: (item) => set({ item }),
  clear: () => set({ item: null }),
}))
