import { useCallback } from 'react'
import { create } from 'zustand'
import { enDict } from './i18nDict'

export type Lang = 'zh' | 'en'

function applyDocLang(l: Lang) {
  try {
    document.documentElement.lang = l === 'en' ? 'en' : 'zh'
  } catch {
    /* ignore */
  }
}

interface I18nState {
  lang: Lang
  setLang: (l: Lang) => void
}

/**
 * 界面语言 store：zh / en，切换即时生效。
 * 语言偏好由后端持久化（settings 表）：App 启动时通过 init-status 应用服务器语言，
 * SettingsApp 切换时显式调用 updateSettings。所有 UI 文案统一走 useT()/tt() 翻译函数
 * （key 即中文原文，en 字典查不到时回退原文）。
 */
export const useI18n = create<I18nState>((set) => ({
  lang: 'en',
  setLang: (l) => {
    if (l !== 'zh' && l !== 'en') return
    applyDocLang(l)
    set({ lang: l })
  },
}))

applyDocLang(useI18n.getState().lang)

/** 后端动态错误：占位符夹在中文中间，前缀/后缀匹配都失效时用正则转换（$1… 保留动态值） */
const DYNAMIC_ERRORS: Array<[RegExp, string]> = [
  [/^创建目录 (.+?) 失败: (.+)$/, 'Failed to create directory $1: $2'],
  [/^创建文件 (.+?) 失败: (.+)$/, 'Failed to create file $1: $2'],
  [/^写入文件 (.+?) 失败: (.+)$/, 'Failed to write file $1: $2'],
  [/^自动放行 ssh 端口 (\d+) 失败: (.+)$/, 'Failed to auto-allow ssh port $1: $2'],
  [/^安装失败：未识别到容器 ID（(.+?)）$/, 'Install failed: could not identify container ID ($1)'],
  [/^docker 安装失败: (.+)$/, 'Docker install failed: $1'],
  [/^sftp 连接失败: (.+)$/, 'SFTP connect failed: $1'],
  [/^sftp connect failed: (.+)$/, 'SFTP connect failed: $1'],
  [/^创建保存目录失败: (.+)$/, 'Failed to create save directory: $1'],
  [/^目录不存在或无权访问: (.+)$/, 'Directory does not exist or is not accessible: $1'],
  [/^aria2 启动失败: (.+)$/, 'Failed to start aria2: $1'],
  [/^启动 aria2 失败: (.+)$/, 'Failed to start aria2: $1'],
  [/^aria2 安装失败: (.+)$/, 'aria2 install failed: $1'],
  [/^aria2 隧道拨号超时: (.+)$/, 'aria2 tunnel dial timed out: $1'],
  [/^aria2 rpc 响应解析失败: (.+)$/, 'Failed to parse aria2 RPC response: $1'],
  [/^remote exec 超时: (.+)$/, 'Remote exec timed out: $1'],
  [/^aria2 启动失败，请检查目标机系统日志$/, 'Failed to start aria2. Check the target machine system logs.'],
  [/^aria2 启动异常（未获取到 RPC 密钥）$/, 'aria2 started abnormally (no RPC secret obtained).'],
  [/^aria2 未返回任务编号$/, 'aria2 did not return a task ID.'],
  [/^目标机无 curl\/wget\/python3，无法访问 aria2 RPC$/, 'The target machine has no curl/wget/python3, so the aria2 RPC is unreachable.'],
  [/^配置了 configFile 时必须同时提供容器名称与 configPath$/, 'When configFile is set, both container name and configPath are required.'],
  [/^解析 docker inspect 输出失败$/, 'Failed to parse docker inspect output.'],
  [/^容器名称不合法（仅允许字母数字、下划线、点、横线）$/, 'Container name is invalid (only letters, digits, underscores, dots and dashes).'],
  [/^目标机未安装 aria2，请先安装$/, 'aria2 is not installed on the target machine. Install it first.'],
  [/^不支持的规则动作：(.+)$/, 'Unsupported rule action: $1.'],
  [/^端口范围操作必须指定协议（tcp 或 udp）$/, 'A port range operation must specify a protocol (tcp or udp).'],
  [/^必须指定来源 IP 或端口$/, 'A source IP or port is required.'],
  [/^目标机未安装 ufw 防火墙$/, 'The target machine does not have ufw installed.'],
  [/^不能解压目录$/, 'Cannot extract a directory.'],
  [/^不支持的归档格式（支持 zip \/ tar \/ tar\.gz \/ tar\.bz2 \/ tar\.xz）$/, 'Unsupported archive format (supported: zip / tar / tar.gz / tar.bz2 / tar.xz).'],
]

/** 中文后缀附加在英文错误末尾的特殊模式（如 "host key mismatch! ... 主机密钥可能被更换，请谨慎处理"） */
const SUFFIX_PATTERNS: Array<[string, string]> = [
  ['主机密钥可能被更换，请谨慎处理', ' Host key may have changed. Please handle with caution.'],
  ['（可能需要管理员权限）', ' (may require administrator privileges)'],
  ['中转传输', 'relay transfer'],
]

function lookup(lang: Lang, key: string, params: ReadonlyArray<string | number>): string {
  let tpl: string | undefined
  if (lang === 'en') {
    tpl = enDict[key]
    // 占位符夹在中文中间的动态错误（正则 $1 保留动态值）——先于前缀匹配，避免被通用短键（如「端口」「监控采集失败」）劫持
    if (tpl === undefined && params.length === 0) {
      for (const [re, to] of DYNAMIC_ERRORS) {
        if (re.test(key)) {
          tpl = key.replace(re, to)
          break
        }
      }
    }
    // 动态错误文案：精确键找不到时按「最长」前缀匹配（如 "连接失败：timeout" → "Connection failed: timeout"）
    if (tpl === undefined && params.length === 0) {
      let best = ''
      for (const k in enDict) {
        if (k.length >= 2 && k !== key && key.startsWith(k) && k.length > best.length) {
          best = k
        }
      }
      if (best) tpl = enDict[best] + key.slice(best.length)
    }
    // 中文后缀附加在英文错误末尾
    if (tpl === undefined && params.length === 0) {
      for (const [suf, eng] of SUFFIX_PATTERNS) {
        if (key.endsWith(suf)) {
          tpl = key.slice(0, key.length - suf.length) + eng
          break
        }
      }
    }
    if (tpl === undefined) tpl = key
  } else {
    tpl = key
  }
  if (params.length > 0) {
    tpl = tpl.replace(/\{(\d+)\}/g, (m, i) => {
      const idx = Number(i)
      if (idx >= params.length) return m
      const p = String(params[idx])
      // 参数本身若是字典键（如主题名、应用名），一并翻译
      return lang === 'en' ? (enDict[p] ?? p) : p
    })
  }
  return tpl
}

/** 渲染期响应式翻译函数：语言切换时自动触发重渲染。占位符用 {0}、{1}… */
export function useT(): (key: string, ...params: Array<string | number>) => string {
  const lang = useI18n((s) => s.lang)
  return useCallback(
    (key: string, ...params: Array<string | number>): string => lookup(lang, key, params),
    [lang],
  )
}

/** 事件回调 / 非组件模块中的静态翻译（读取当前语言，不触发渲染） */
export function tt(key: string, ...params: Array<string | number>): string {
  return lookup(useI18n.getState().lang, key, params)
}

/** 统一错误文案：ApiError / Error 的 message 透传字典（后端中文→英文），未知信息保留原文，非 Error 用 fallback */
export function transErr(e: unknown, fallback: string): string {
  if (e && typeof e === 'object' && 'message' in e) {
    const m = (e as { message: string }).message
    if (typeof m === 'string' && m) return tt(m)
  }
  return tt(fallback)
}

/** 当前语言（供非组件逻辑快速判断） */
export function currentLang(): Lang {
  return useI18n.getState().lang
}
