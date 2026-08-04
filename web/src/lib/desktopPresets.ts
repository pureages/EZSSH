/** 预制桌面背景（AI 生成，存放于 web/public/backgrounds）与主题风格 */

export interface PresetBg {
  id: string
  name: string
  url: string
  /** 该背景对应的主题风格 id */
  theme: string
}

export const PRESET_BGS: PresetBg[] = [
  { id: 'lucky', name: '幸运', url: '/backgrounds/bg-lucky.png', theme: 'lucky' },
  { id: 'faraway', name: '远方', url: '/backgrounds/bg-faraway.png', theme: 'faraway' },
  { id: 'flow', name: '流光', url: '/backgrounds/bg-flow.png', theme: 'flow' },
]

export interface ThemePreset {
  id: string
  /** 主题名（背景名 + 风格名） */
  name: string
  /** 风格描述 */
  desc: string
  /** 配套桌面背景 */
  bg: string
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
    bg: '/backgrounds/bg-flow.png',
    swatch: { primary: '#3b82f6', window: 'rgba(17,26,46,0.9)', text: '#f1f5f9' },
  },
  {
    id: 'lucky',
    name: '幸运 · 青野',
    desc: '浅色清新翡翠绿，轻盈通透',
    bg: '/backgrounds/bg-lucky.png',
    swatch: { primary: '#10b981', window: 'rgba(255,255,255,0.85)', text: '#17352a' },
  },
  {
    id: 'faraway',
    name: '远方 · 暮晖',
    desc: '暖色落日琥珀调，温润现代',
    bg: '/backgrounds/bg-faraway.png',
    swatch: { primary: '#f59e0b', window: 'rgba(34,24,19,0.9)', text: '#f5ede6' },
  },
]

export const DEFAULT_THEME = 'flow'
