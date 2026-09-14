import { useT } from '../lib/i18n'

/**
 * 路由错误页。
 *
 * 启用「安全路由」后，访问首页或默认 /login 等非安全路由地址时不再自动跳转到安全路由，
 * 而是落到本页提示「路由错误」——安全路由必须由用户手动输入完整地址访问。
 * 出于安全考虑，本页不会透露真实的安全路由。
 */
export function RouteErrorPage() {
  const t = useT()
  return (
    <div className="auth-page">
      <div className="auth-card" style={{ textAlign: 'center' }}>
        <div style={{ fontSize: 40, lineHeight: 1, marginBottom: 12 }}>🚫</div>
        <h1 style={{ marginBottom: 8 }}>{t('路由错误')}</h1>
        <div className="subtitle" style={{ marginBottom: 10 }}>
          {t('当前访问路径无效，无法打开登录页。')}
        </div>
        <div style={{ fontSize: 12, color: 'var(--text-1)', lineHeight: 1.9 }}>
          {t('请手动输入完整的访问地址（含 # 与安全路由）后重试。')}
        </div>
      </div>
    </div>
  )
}
