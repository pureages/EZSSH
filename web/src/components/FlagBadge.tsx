/**
 * 国旗/区旗徽标：显示主机 IP 所在国家/地区的小旗（web/public/flags/*.svg）。
 * country_code 为 ISO 3166-1 alpha-2（大写），如 CN / HK / MO / TW / US。
 */

export function FlagBadge({ code, size = 16 }: { code?: string; size?: number }) {
  const cc = (code || '').toLowerCase()
  if (!/^[a-z]{2}$/.test(cc)) return null
  return (
    <img
      src={`/flags/${cc}.svg`}
      alt={cc.toUpperCase()}
      draggable={false}
      title={cc.toUpperCase()}
      style={{
        width: size,
        height: Math.round(size * 0.75), // 4x3 比例
        borderRadius: 3,
        border: '1px solid rgba(255,255,255,0.4)',
        boxShadow: '0 1px 3px rgba(0,0,0,0.45)',
        display: 'block',
      }}
    />
  )
}
