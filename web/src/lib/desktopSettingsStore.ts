import { create } from 'zustand'
import { THEMES, DEFAULT_THEME } from './desktopPresets'

const HIDE_MONITOR_KEY = 'ezssh.hide_icon_monitor'
const DESKTOP_BG_KEY = 'ezssh.desktop_bg'
const THEME_KEY = 'ezssh.theme'
const ICON_SCALE_KEY = 'ezssh.icon_scale'
const MON_FONT_KEY = 'ezssh.mon_font'
const MON_COLORS_KEY = 'ezssh.mon_colors'
/** 默认桌面背景：空串 = 不使用图片，由 CSS 渐变渲染（柔和偏暗） */
const DEFAULT_BG = ''
/** 桌面图标缩放范围（1 = 100%，越小图标越小） */
const MIN_ICON_SCALE = 0.6
const MAX_ICON_SCALE = 1.5
/** 图标下方监控文字大小范围（px） */
const MIN_MON_FONT = 9
const MAX_MON_FONT = 16

/** 桌面图标监控各段颜色键（与 index.css 的 --mon-color-* 对应） */
export type MonColorKey = 'cpu' | 'mem' | 'disk' | 'tx' | 'rx' | 'totalUp' | 'totalDown' | 'sep'

/** 各段默认颜色（与 index.css 原始配色一致） */
export const DEFAULT_MON_COLORS: Record<MonColorKey, string> = {
  cpu: '#60a5fa', // 蓝：CPU
  mem: '#c084fc', // 紫：内存
  disk: '#fbbf24', // 黄：硬盘
  tx: '#34d399', // 绿：上传速率
  rx: '#fb923c', // 橙：下载速率
  totalUp: '#ffffff', // 白：总上传
  totalDown: '#cbd5e1', // 灰白：总下载
  sep: '#94a3b8', // 灰：分隔符「|」
}

/** 从 localStorage 读取监控颜色（仅接受合法 #rrggbb，缺省回退默认色） */
function loadMonColors(): Record<MonColorKey, string> {
  const out: Record<MonColorKey, string> = { ...DEFAULT_MON_COLORS }
  try {
    const raw = JSON.parse(localStorage.getItem(MON_COLORS_KEY) || '{}')
    for (const k of Object.keys(DEFAULT_MON_COLORS) as MonColorKey[]) {
      const v = raw[k]
      if (typeof v === 'string' && /^#[0-9a-fA-F]{6}$/.test(v)) out[k] = v
    }
  } catch {
    /* ignore */
  }
  return out
}

function readLS(key: string): string {
  try {
    return localStorage.getItem(key) ?? ''
  } catch {
    return ''
  }
}

function writeLS(key: string, v: string): boolean {
  try {
    localStorage.setItem(key, v)
    return true
  } catch {
    return false
  }
}

/** 将主题应用到文档根元素（data-theme 驱动 index.css 中的主题变量块） */
function applyTheme(id: string) {
  try {
    document.documentElement.setAttribute('data-theme', id)
  } catch {
    /* ignore */
  }
}

interface DesktopSettingsState {
  /** 是否隐藏桌面图标下方的实时监控（默认显示，即 false） */
  hideIconMonitor: boolean
  setHideIconMonitor: (v: boolean) => void
  toggleIconMonitor: () => void
  /** 自定义桌面背景（图片 dataURL，空串=默认渐变） */
  bg: string
  /** 设置背景，写入失败（图片过大超出存储限制）返回 false */
  setBg: (dataUrl: string) => boolean
  resetBg: () => void
  /** 当前主题风格 id（flow / lucky / faraway） */
  theme: string
  /** 切换主题：仅切换整体配色，不影响桌面背景（背景与主题互相独立） */
  setTheme: (id: string) => void
  /** 桌面图标缩放比例（1 = 100%） */
  iconScale: number
  setIconScale: (v: number) => void
  /** 桌面图标下方监控文字大小（px） */
  monitorFontSize: number
  setMonitorFontSize: (v: number) => void
  /** 图标下方监控各段文字颜色（key -> #rrggbb） */
  monColors: Record<MonColorKey, string>
  setMonColor: (key: MonColorKey, color: string) => void
  resetMonColors: () => void
}

const initialTheme = THEMES.some((t) => t.id === readLS(THEME_KEY)) ? readLS(THEME_KEY) : DEFAULT_THEME
// 模块加载即应用主题，保证登录页等桌面之外的页面也使用正确配色
applyTheme(initialTheme)

/** 桌面设置：跨设置窗口与桌面共享，修改即时生效 */
export const useDesktopSettings = create<DesktopSettingsState>((set) => ({
  hideIconMonitor: readLS(HIDE_MONITOR_KEY) === '1',
  setHideIconMonitor: (v) => {
    writeLS(HIDE_MONITOR_KEY, v ? '1' : '0')
    set({ hideIconMonitor: v })
  },
  toggleIconMonitor: () => {
    set((s) => {
      const v = !s.hideIconMonitor
      writeLS(HIDE_MONITOR_KEY, v ? '1' : '0')
      return { hideIconMonitor: v }
    })
  },
  bg: readLS(DESKTOP_BG_KEY) || DEFAULT_BG,
  setBg: (dataUrl) => {
    if (!writeLS(DESKTOP_BG_KEY, dataUrl)) return false
    set({ bg: dataUrl })
    return true
  },
  resetBg: () => {
    writeLS(DESKTOP_BG_KEY, DEFAULT_BG)
    set({ bg: DEFAULT_BG })
  },
  theme: initialTheme,
  setTheme: (id) => {
    const t = THEMES.find((x) => x.id === id)
    if (!t) return
    writeLS(THEME_KEY, id)
    applyTheme(id)
    set({ theme: id })
  },
  iconScale: (() => {
    const v = parseFloat(readLS(ICON_SCALE_KEY))
    return Number.isFinite(v) ? Math.min(MAX_ICON_SCALE, Math.max(MIN_ICON_SCALE, v)) : 1
  })(),
  setIconScale: (v) => {
    const c = Math.min(MAX_ICON_SCALE, Math.max(MIN_ICON_SCALE, v))
    writeLS(ICON_SCALE_KEY, String(c))
    set({ iconScale: c })
  },
  monitorFontSize: (() => {
    const v = parseInt(readLS(MON_FONT_KEY), 10)
    return Number.isFinite(v) ? Math.min(MAX_MON_FONT, Math.max(MIN_MON_FONT, v)) : 11
  })(),
  setMonitorFontSize: (v) => {
    const c = Math.min(MAX_MON_FONT, Math.max(MIN_MON_FONT, Math.round(v)))
    writeLS(MON_FONT_KEY, String(c))
    set({ monitorFontSize: c })
  },
  monColors: loadMonColors(),
  setMonColor: (key, color) => {
    if (!(key in DEFAULT_MON_COLORS)) return
    if (!/^#[0-9a-fA-F]{6}$/.test(color)) return
    set((s) => {
      const next = { ...s.monColors, [key]: color }
      try {
        localStorage.setItem(MON_COLORS_KEY, JSON.stringify(next))
      } catch {
        /* ignore */
      }
      return { monColors: next }
    })
  },
  resetMonColors: () => {
    const d = { ...DEFAULT_MON_COLORS }
    try {
      localStorage.setItem(MON_COLORS_KEY, JSON.stringify(d))
    } catch {
      /* ignore */
    }
    set({ monColors: d })
  },
}))
