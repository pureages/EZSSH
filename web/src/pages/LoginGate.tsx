import { useEffect, useState } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { api } from '../lib/api'
import { useT } from '../lib/i18n'
import { DEFAULT_LOGIN_ROUTE } from '../lib/routes'
import { AuthPage } from './AuthPage'
import { RouteErrorPage } from './RouteErrorPage'

/**
 * 登录入口闸门。
 *
 * 服务端不向未认证请求下发安全路由的值，前端只能把「当前访问路径」交给
 * POST /api/route-check 询问是否允许展示登录页：
 *   - 允许 → 渲染登录页；
 *   - 不允许 → 非登录入口，统一落到默认 /login 并提示「路由错误」（不透露真实入口）。
 *
 * 这样即便有人抓包/扫描公开接口，也拿不到安全路由。
 */
export function LoginGate() {
  const t = useT()
  const location = useLocation()
  const [state, setState] = useState<'checking' | 'allowed' | 'denied'>('checking')

  useEffect(() => {
    let cancelled = false
    setState('checking')
    api
      .routeCheck(location.pathname)
      .then((r) => {
        if (!cancelled) setState(r.ok ? 'allowed' : 'denied')
      })
      .catch(() => {
        // 网络错误 / 探测被限流（429）等：按「非登录入口」处理，避免误放行
        if (!cancelled) setState('denied')
      })
    return () => {
      cancelled = true
    }
  }, [location.pathname])

  if (state === 'checking') {
    return (
      <div className="auth-page">
        <div className="auth-card" style={{ textAlign: 'center', color: 'var(--text-1)' }}>
          {t('加载中…')}
        </div>
      </div>
    )
  }

  if (state === 'allowed') return <AuthPage />

  // 非登录入口：统一跳到默认 /login 再展示「路由错误」，避免把访问者停在他猜的路径上
  return location.pathname === DEFAULT_LOGIN_ROUTE ? (
    <RouteErrorPage />
  ) : (
    <Navigate to={DEFAULT_LOGIN_ROUTE} replace />
  )
}
