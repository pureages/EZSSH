import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, ApiError } from '../lib/api'
import { useSession } from '../lib/session'
import { useT, transErr } from '../lib/i18n'

/**
 * 登录 / 首次初始化 合一页：
 * - 未初始化 → 显示初始化向导（创建管理员口令）
 * - 已初始化 → 登录
 * - 登录失败一次后 → 显示验证码（防暴力破解）
 *
 * 注意：不再依据 vault 解锁状态自动跳桌面（vault 解锁 ≠ 会话有效，
 * 那样会在会话失效时与桌面页互相跳转造成死循环）。是否进入桌面
 * 完全由 App 层的 authed 状态与路由守卫决定。
 */
export function AuthPage() {
  const navigate = useNavigate()
  const t = useT()
  const setAuthed = useSession((s) => s.setAuthed)
  const [initialized, setInitialized] = useState<boolean | null>(null)
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  // 验证码状态
  const [needCaptcha, setNeedCaptcha] = useState(false)
  const [captchaId, setCaptchaId] = useState('')
  const [captchaSvg, setCaptchaSvg] = useState('')
  const [captchaCode, setCaptchaCode] = useState('')

  const loadCaptcha = useCallback(async () => {
    try {
      const c = await api.captcha()
      setCaptchaId(c.id)
      setCaptchaSvg(c.svg)
      setCaptchaCode('')
    } catch {
      /* 验证码加载失败不阻塞，下次失败会再次尝试 */
    }
  }, [])

  useEffect(() => {
    api
      .initStatus()
      .then((s) => setInitialized(s.initialized))
      .catch(() => setInitialized(true))
  }, [])

  const submit = async () => {
    setError('')
    setLoading(true)
    try {
      if (!initialized) {
        if (password.length < 8) {
          setError(t('密码至少 8 位'))
          setLoading(false)
          return
        }
        if (password !== confirm) {
          setError(t('两次输入的密码不一致'))
          setLoading(false)
          return
        }
        await api.init(username.trim(), password)
      } else {
        await api.login(
          username.trim(),
          password,
          needCaptcha ? { id: captchaId, code: captchaCode } : undefined,
        )
      }
      // 登录成功：标记认证通过，路由守卫放行后跳转
      setAuthed(true)
      navigate('/desktop', { replace: true })
    } catch (e) {
      if (e instanceof ApiError && e.needCaptcha && initialized) {
        // 失败一次后要求验证码
        setNeedCaptcha(true)
        setError(e.message || t('登录失败，请输入验证码'))
        void loadCaptcha()
      } else {
        setError(transErr(e, '请求失败，请重试'))
      }
    } finally {
      setLoading(false)
    }
  }

  if (initialized === null) {
    return (
      <div className="auth-page">
        <div className="auth-card" style={{ textAlign: 'center', color: 'var(--text-1)' }}>
          {t('加载中…')}
        </div>
      </div>
    )
  }

  return (
    <div className="auth-page">
      <form className="auth-card" onSubmit={(e) => { e.preventDefault(); void submit() }}>
        <div style={{ display: 'flex', justifyContent: 'center', marginBottom: 10 }}>
          <img
            src="/logo.png"
            alt="EZSSH"
            style={{
              width: 80,
              height: 80,
              borderRadius: 18,
              boxShadow: '0 8px 24px rgba(34,211,238,0.25)',
            }}
          />
        </div>
        <h1>EZSSH</h1>
        <div className="subtitle">
          {initialized ? t('登录你的 SSH 桌面网关') : t('首次使用，创建管理员密码')}
        </div>

        <div className="field">
          <label>{t('用户名')}</label>
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoFocus
            autoComplete="username"
          />
        </div>

        <div className="field">
          <label>{t('密码')}</label>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
        </div>

        {!initialized && (
          <div className="field">
            <label>{t('确认密码')}</label>
            <input
              type="password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              autoComplete="new-password"
            />
          </div>
        )}

        {needCaptcha && (
          <div className="field">
            <label>{t('验证码')}</label>
            {captchaSvg ? (
              <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                {/* SVG 由后端生成，通过 innerHTML 渲染为图片 */}
                <div
                  style={{
                    width: 120,
                    height: 44,
                    borderRadius: 8,
                    overflow: 'hidden',
                    border: '1px solid var(--glass-border)',
                    cursor: 'pointer',
                    flexShrink: 0,
                  }}
                  title={t('点击刷新')}
                  onClick={() => void loadCaptcha()}
                  // eslint-disable-next-line react/no-danger
                  dangerouslySetInnerHTML={{ __html: captchaSvg }}
                />
                <input
                  value={captchaCode}
                  onChange={(e) => setCaptchaCode(e.target.value)}
                  placeholder={t('输入验证码')}
                  style={{ flex: 1, minWidth: 0 }}
                  autoComplete="off"
                />
              </div>
            ) : (
              <button type="button" className="btn btn-sm btn-ghost" onClick={() => void loadCaptcha()}>
                {t('点击加载验证码')}
              </button>
            )}
          </div>
        )}

        <button type="submit" className="btn btn-block" disabled={loading}>
          {loading ? t('请稍候…') : initialized ? t('登录') : t('创建并进入')}
        </button>
        <div className="error-text">{error}</div>
      </form>
    </div>
  )
}
