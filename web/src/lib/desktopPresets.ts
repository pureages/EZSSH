/** 桌面背景选项：CSS 渐变（默认）+ 三张预制背景图，与主题互相独立 */

export interface BgOption {
  id: string
  name: string
  /** 背景图片 URL；空串 = CSS 渐变（默认，柔和偏暗） */
  url: string
  /** 非图片背景的预览样式（CSS background），用于设置页缩略图 */
  cssPreview?: string
}

/** 桌面背景选项（4 个）：CSS 渐变 + 流光 + 幸运 + 远方 */
export const BG_OPTIONS: BgOption[] = [
  {
    id: 'gradient',
    name: 'CSS 渐变（默认）',
    url: '',
    cssPreview:
      'radial-gradient(1200px 720px at 18% 8%, rgba(59, 130, 246, 0.1) 0%, transparent 55%), radial-gradient(1000px 620px at 88% 82%, rgba(109, 40, 217, 0.09) 0%, transparent 50%), radial-gradient(760px 520px at 72% 16%, rgba(14, 116, 144, 0.07) 0%, transparent 45%), linear-gradient(165deg, #0a0f1e 0%, #0f172a 55%, #0a0f1e 100%)',
  },
  { id: 'flow', name: '流光', url: '/backgrounds/bg-flow.png' },
  { id: 'lucky', name: '幸运', url: '/backgrounds/bg-lucky.png' },
  { id: 'faraway', name: '远方', url: '/backgrounds/bg-faraway.png' },
]

/** 预制背景图（仅图片项） */
export const PRESET_BGS: BgOption[] = BG_OPTIONS.filter((o) => o.url)

export interface ThemePreset {
  id: string
  /** 主题名（风格名） */
  name: string
  /** 风格描述 */
  desc: string
  /** 主题色板预览（用于设置页主题卡片渲染） */
  swatch: {
    primary: string
    window: string
    text: string
  }
}

/** 三套主题风格：流光·深空（默认，深色蓝紫）/ 幸运·青野（浅色清新绿）/ 远方·暮晖（暖色落日） */
export const THEMES: ThemePreset[] = [
  {
    id: 'flow',
    name: '流光 · 深空',
    desc: '深色蓝紫玻璃拟态，默认主题',
    swatch: { primary: '#3b82f6', window: 'rgba(17,26,46,0.9)', text: '#f1f5f9' },
  },
  {
    id: 'lucky',
    name: '幸运 · 青野',
    desc: '浅色清新翡翠绿，轻盈通透',
    swatch: { primary: '#10b981', window: 'rgba(255,255,255,0.85)', text: '#17352a' },
  },
  {
    id: 'faraway',
    name: '远方 · 暮晖',
    desc: '暖色落日琥珀调，温润现代',
    swatch: { primary: '#f59e0b', window: 'rgba(34,24,19,0.9)', text: '#f5ede6' },
  },
]

export const DEFAULT_THEME = 'flow'
