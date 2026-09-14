/**
 * 服务器价格展示工具：币种符号 + 金额 + 计费周期（每年 / 每月 / 每三年 / 一次性）。
 */

/** 币种符号 */
export const CURRENCY_SYMBOLS: Record<string, string> = { CNY: '¥', USD: '$', EUR: '€' }

/** 计费周期 → i18n 键（文案为中文原文，由 t() 翻译） */
export const BILLING_CYCLE_KEYS: Record<string, string> = {
  month: '每月',
  year: '每年',
  '3year': '每三年',
  once: '一次性',
}

/** 币种下拉选项（值为后端存储的代码） */
export const CURRENCY_OPTIONS: Array<{ value: string; symbol: string; key: string }> = [
  { value: 'CNY', symbol: '¥', key: '人民币' },
  { value: 'USD', symbol: '$', key: '美元' },
  { value: 'EUR', symbol: '€', key: '欧元' },
]

/** 计费周期下拉选项 */
export const BILLING_CYCLE_OPTIONS: Array<{ value: string; key: string }> = [
  { value: 'month', key: '每月' },
  { value: 'year', key: '每年' },
  { value: '3year', key: '每三年' },
  { value: 'once', key: '一次性' },
]

/**
 * 价格展示文本：如「¥99.00 / 每月」；未设置（金额为空）时返回 ''。
 * t 由调用方传入（useT）以保证语言切换时响应式更新。
 */
export function fmtHostPrice(
  price: string | undefined,
  currency: string | undefined,
  cycle: string | undefined,
  t: (key: string, ...params: Array<string | number>) => string,
): string {
  const amount = (price || '').trim()
  if (!amount) return ''
  const sym = CURRENCY_SYMBOLS[currency || 'CNY'] || '¥'
  const cycKey = cycle ? BILLING_CYCLE_KEYS[cycle] : ''
  return cycKey ? `${sym}${amount} / ${t(cycKey)}` : `${sym}${amount}`
}

/** 计费周期 → 紧凑后缀（卡片角标用）：/月 /年 /三年 /一次性 */
const CYCLE_SUFFIX_KEYS: Record<string, string> = {
  month: '/月',
  year: '/年',
  '3year': '/三年',
  once: '/一次性',
}

/**
 * 紧凑价格文本（桌面卡片角标）：如「¥99/年」「¥99/三年」「¥99/一次性」；
 * 未设置金额时返回 ''，仅有金额无周期时为「¥99」。
 */
export function fmtHostPriceShort(
  price: string | undefined,
  currency: string | undefined,
  cycle: string | undefined,
  t: (key: string, ...params: Array<string | number>) => string,
): string {
  const amount = (price || '').trim()
  if (!amount) return ''
  const sym = CURRENCY_SYMBOLS[currency || 'CNY'] || '¥'
  const suffix = cycle ? CYCLE_SUFFIX_KEYS[cycle] : ''
  return `${sym}${amount}${suffix ? t(suffix) : ''}`
}
