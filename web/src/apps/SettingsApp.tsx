import { useEffect, useRef, useState } from 'react'
import { api, ApiError } from '../lib/api'
import { emitHostsChanged } from '../lib/hostsBus'
import { useDesktopSettings, DEFAULT_MON_COLORS, type MonColorKey } from '../lib/desktopSettingsStore'
import { useSecuritySettings } from '../lib/securitySettingsStore'
import { PRESET_BGS, THEMES } from '../lib/desktopPresets'
import { useT, useI18n, tt, transErr, type Lang } from '../lib/i18n'
import type { AppProps } from '../desktop/appRegistry'

type Section = 'personalize' | 'desktop' | 'security' | 'language' | 'about'

/**
 * 设置 App：左侧导航（个性化 / 桌面 / 安全 / 语言 / 关于EZSSH）。
 * 个性化：主题风格（三套，绑定预制背景）+ 自定义背景上传 + 图标大小/监控文字大小/监控文字颜色。
 * 桌面：隐藏图标监控；安全：修改密码 + 自定义安全路由；语言：界面语言切换。
 */
export function SettingsApp({ onTitle }: AppProps) {
  const t = useT()
  const lang = useI18n((s) => s.lang)
  const setLang = useI18n((s) => s.setLang)
  const [section, setSection] = useState<Section>('personalize')

  // 修改密码
  const [oldPw, setOldPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [pwMsg, setPwMsg] = useState('')
  const [pwOk, setPwOk] = useState(false)
  const [savingPw, setSavingPw] = useState(false)

  // 安全路由
  const [route, setRoute] = useState('')
  const [routeMsg, setRouteMsg] = useState('')
  const [routeOk, setRouteOk] = useState(false)
  const [savingRoute, setSavingRoute] = useState(false)

  // 桌面隐藏的服务器
  const [hiddenCount, setHiddenCount] = useState(0)
  const [showAllMsg, setShowAllMsg] = useState('')
  const [showAllOk, setShowAllOk] = useState(false)

  // 安全设置：隐藏文件管理器用户名（后端持久化，任何设备一致）
  const hideFmUsername = useSecuritySettings((s) => s.hideFmUsername)
  const setHideFmUsername = useSecuritySettings((s) => s.setHideFmUsername)
  const hydrateSecurity = useSecuritySettings((s) => s.hydrate)

  const loadHiddenCount = () => {
    api
      .listHosts()
      .then((hs) => setHiddenCount(hs.filter((h) => h.hidden).length))
      .catch(() => {})
  }

  const showAllHidden = async () => {
    setShowAllMsg('')
    setShowAllOk(false)
    try {
      await api.showAllHosts()
      loadHiddenCount()
      emitHostsChanged()
      setShowAllOk(true)
      setShowAllMsg(tt('已显示所有桌面隐藏的服务器'))
    } catch (e) {
      setShowAllMsg(e instanceof ApiError ? e.message : tt('操作失败'))
    }
  }

  // 桌面设置
  const hideIconMonitor = useDesktopSettings((s) => s.hideIconMonitor)
  const setHideIconMonitor = useDesktopSettings((s) => s.setHideIconMonitor)
  // 个性化（主题 + 桌面背景）
  const theme = useDesktopSettings((s) => s.theme)
  const setTheme = useDesktopSettings((s) => s.setTheme)
  const bg = useDesktopSettings((s) => s.bg)
  const setBg = useDesktopSettings((s) => s.setBg)
  const resetBgStore = useDesktopSettings((s) => s.resetBg)
  // 桌面图标大小 / 监控文字大小
  const iconScale = useDesktopSettings((s) => s.iconScale)
  const setIconScale = useDesktopSettings((s) => s.setIconScale)
  const monitorFontSize = useDesktopSettings((s) => s.monitorFontSize)
  const setMonitorFontSize = useDesktopSettings((s) => s.setMonitorFontSize)
  // 图标监控文字颜色
  const monColors = useDesktopSettings((s) => s.monColors)
  const setMonColor = useDesktopSettings((s) => s.setMonColor)
  const resetMonColors = useDesktopSettings((s) => s.resetMonColors)
  const colorInputRef = useRef<HTMLInputElement>(null)
  const colorKeyRef = useRef<MonColorKey>('cpu')
  const [bgPreview, setBgPreview] = useState('')
  const [bgMsg, setBgMsg] = useState('')
  const [bgOk, setBgOk] = useState(false)

  // 关于
  const [version, setVersion] = useState('')

  const pickBg = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setBgMsg('')
    setBgOk(false)
    if (!file.type.startsWith('image/')) {
      setBgMsg(t('请选择图片文件（PNG/JPG/GIF/WebP）'))
      return
    }
    if (file.size > 3 * 1024 * 1024) {
      setBgMsg(t('图片过大，请选择 ≤ 3MB 的图片'))
      return
    }
    const reader = new FileReader()
    reader.onload = () => setBgPreview(String(reader.result ?? ''))
    reader.readAsDataURL(file)
  }

  const applyBg = () => {
    if (!bgPreview) return
    const ok = setBg(bgPreview)
    setBgOk(ok)
    setBgMsg(ok ? t('桌面背景已更新（动图 GIF 将自动播放）') : t('保存失败：图片可能超出浏览器存储限制，请换更小的图片'))
  }

  const resetBg = () => {
    resetBgStore()
    setBgPreview('')
    setBgOk(true)
    setBgMsg(t('已恢复默认背景'))
  }

  /** 点击示例区域：打开系统取色器，修改该段监控文字颜色 */
  const pickColor = (key: MonColorKey) => {
    colorKeyRef.current = key
    const input = colorInputRef.current
    if (!input) return
    input.value = monColors[key] || DEFAULT_MON_COLORS[key]
    input.click()
  }

  useEffect(() => {
    if (onTitle) onTitle(tt('设置'))
    loadHiddenCount()
    api
      .getSettings()
      .then((s) => {
        setRoute(s.login_route)
        hydrateSecurity(s)
      })
      .catch(() => {})
    api
      .initStatus()
      .then((s) => setVersion(s.version || ''))
      .catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [onTitle])

  const changePassword = async () => {
    setPwMsg('')
    setPwOk(false)
    if (newPw.length < 8) {
      setPwMsg(t('新密码至少 8 位'))
      return
    }
    if (newPw !== confirmPw) {
      setPwMsg(t('两次输入的新密码不一致'))
      return
    }
    setSavingPw(true)
    try {
      await api.changePassword(oldPw, newPw)
      setPwOk(true)
      setPwMsg(t('密码修改成功，下次登录请使用新密码'))
      setOldPw('')
      setNewPw('')
      setConfirmPw('')
    } catch (e) {
      setPwMsg(transErr(e, '修改失败'))
    } finally {
      setSavingPw(false)
    }
  }

  const saveRoute = async () => {
    setRouteMsg('')
    setRouteOk(false)
    setSavingRoute(true)
    try {
      const r = await api.updateSettings({ login_route: route.trim() })
      setRoute(r.login_route)
      setRouteOk(true)
      setRouteMsg(t('安全路由已更新为 {0}，请记牢它', r.login_route))
    } catch (e) {
      setRouteMsg(transErr(e, '保存失败'))
    } finally {
      setSavingRoute(false)
    }
  }

  const navItem = (key: Section, icon: string, label: string) => (
    <button className={`st-nav${section === key ? ' active' : ''}`} onClick={() => setSection(key)}>
      <span>{icon}</span>
      <span>{label}</span>
    </button>
  )

  /** 当前背景是否为某张预制背景（用于提示文字） */
  const activePreset = PRESET_BGS.find((p) => p.url === bg)

  return (
    <div className="st-root">
      {/* 左侧导航 */}
      <div className="st-side">
        <div className="st-side-title">{t('设置')}</div>
        {navItem('personalize', '🎨', t('个性化'))}
        {navItem('desktop', '🖥️', t('桌面'))}
        {navItem('security', '🛡️', t('安全'))}
        {navItem('language', '🌐', t('语言'))}
        {navItem('about', 'ℹ️', t('关于EZSSH'))}
      </div>

      {/* 右侧内容 */}
      <div className="st-main">
        {/* 隐藏的取色器：由示例区域点击触发 */}
        <input
          ref={colorInputRef}
          type="color"
          tabIndex={-1}
          onChange={(e) => setMonColor(colorKeyRef.current, e.target.value)}
          style={{ position: 'absolute', opacity: 0, width: 0, height: 0, pointerEvents: 'none' }}
        />
        {section === 'personalize' ? (
          <>
            <div className="st-page-title">{t('个性化')}</div>
            <div className="st-page-desc">{t('选择主题风格，整体配色与桌面背景将同步切换。')}</div>

            {/* 主题风格 */}
            <div className="st-card">
              <div className="st-card-title">{t('✨ 主题风格')}</div>
              <div className="st-theme-grid">
                {THEMES.map((th) => {
                  const active = theme === th.id
                  return (
                    <button
                      key={th.id}
                      className={`st-theme-card${active ? ' active' : ''}`}
                      onClick={() => {
                        setTheme(th.id)
                        setBgPreview('')
                        setBgOk(true)
                        setBgMsg(t('已应用主题「{0}」', th.name))
                      }}
                      title={t('应用主题：{0}', th.name)}
                    >
                      <img src={th.bg} alt={t(th.name)} />
                      <div className="st-theme-meta">
                        <div className="st-theme-name">
                          <span>{t(th.name)}</span>
                          {active && <span className="st-theme-check">✓</span>}
                        </div>
                        <div className="st-theme-desc">{t(th.desc)}</div>
                        <div className="st-theme-swatch">
                          <i style={{ background: th.swatch.primary }} />
                          <i style={{ background: th.swatch.window }} />
                          <i style={{ background: th.swatch.text }} />
                        </div>
                      </div>
                    </button>
                  )
                })}
              </div>
            </div>

            {/* 自定义背景 */}
            <div className="st-card">
              <div className="st-card-title">{t('🖼️ 自定义背景')}</div>
              <div className="st-tip" style={{ marginBottom: 10 }}>
                {activePreset ? t('当前背景：预制背景「{0}」', activePreset.name) : t('当前背景：自定义图片')}
              </div>              <div className="field">
                <label>{t('上传自定义图片')}</label>
                <input
                  type="file"
                  accept="image/png,image/jpeg,image/gif,image/webp"
                  onChange={pickBg}
                />
              </div>
              <div className="st-tip" style={{ marginBottom: 10 }}>
                {t('支持 PNG / JPG / GIF（动图自动播放）/ WebP，建议大小 ≤ 3MB，设置后立即生效。自定义背景不会改变当前主题配色。')}
              </div>
              {(bgPreview || bg) && (
                <img
                  src={bgPreview || bg}
                  alt={t('背景预览')}
                  style={{
                    maxWidth: '100%',
                    maxHeight: 150,
                    borderRadius: 10,
                    marginBottom: 12,
                    objectFit: 'contain',
                    border: '1px solid rgba(var(--rgb-line), 0.2)',
                  }}
                />
              )}
              <div style={{ display: 'flex', gap: 10 }}>
                <button className="btn" disabled={!bgPreview} onClick={applyBg}>
                  {t('设为桌面背景')}
                </button>
                <button className="btn btn-ghost" onClick={resetBg}>
                  {t('恢复默认背景')}
                </button>
              </div>
              {bgMsg && (
                <div className="error-text" style={bgOk ? { color: 'var(--green)' } : undefined}>
                  {bgMsg}
                </div>
              )}
            </div>

            {/* 桌面图标大小 */}
            <div className="st-card">
              <div className="st-card-title">{t('🖼️ 桌面图标大小')}</div>
              <div className="st-slider-row">
                <div className="st-slider-label">
                  <span>{t('桌面图标大小')}</span>
                  <span className="st-slider-value">{Math.round(iconScale * 100)}%</span>
                </div>
                <input
                  type="range"
                  min={60}
                  max={150}
                  step={5}
                  value={Math.round(iconScale * 100)}
                  onChange={(e) => setIconScale(parseInt(e.target.value, 10) / 100)}
                />
                <div className="st-tip">{t('调节桌面服务器图标的整体显示大小（Logo、名称与监控一起缩放）。')}</div>
              </div>
              <div className="st-slider-row" style={{ marginTop: 14 }}>
                <div className="st-slider-label">
                  <span>{t('图标监控文字大小')}</span>
                  <span className="st-slider-value">{monitorFontSize}px</span>
                </div>
                <input
                  type="range"
                  min={9}
                  max={16}
                  step={1}
                  value={monitorFontSize}
                  onChange={(e) => setMonitorFontSize(parseInt(e.target.value, 10))}
                />
                <div className="st-tip">{t('调节图标下方三行微型监控（CPU/内存/硬盘、上传/下载速率、总上传/总下载）的文字大小。')}</div>
              </div>
            </div>

            {/* 图标监控文字颜色 */}
            <div className="st-card">
              <div className="st-card-title">{t('🎨 图标监控文字颜色')}</div>
              <div className="st-tip" style={{ marginBottom: 10 }}>
                {t('点击下方示例中的数字 / 文字，弹出取色器选择颜色，桌面图标即时生效。')}
              </div>
              <div className="mon-preview">
                <div className="mon-preview-line">
                  <span
                    className="mon-preview-seg"
                    style={{ color: monColors.cpu }}
                    title={t('CPU（点击改色）')}
                    onClick={() => pickColor('cpu')}
                  >
                    0.0%
                  </span>
                  <span
                    className="mon-preview-sep"
                    style={{ color: monColors.sep }}
                    title={t('分隔符（点击改色）')}
                    onClick={() => pickColor('sep')}
                  >
                    |
                  </span>
                  <span
                    className="mon-preview-seg"
                    style={{ color: monColors.mem }}
                    title={t('内存（点击改色）')}
                    onClick={() => pickColor('mem')}
                  >
                    0.0%
                  </span>
                  <span
                    className="mon-preview-sep"
                    style={{ color: monColors.sep }}
                    title={t('分隔符（点击改色）')}
                    onClick={() => pickColor('sep')}
                  >
                    |
                  </span>
                  <span
                    className="mon-preview-seg"
                    style={{ color: monColors.disk }}
                    title={t('硬盘（点击改色）')}
                    onClick={() => pickColor('disk')}
                  >
                    0.0%
                  </span>
                </div>
                <div className="mon-preview-line mon-preview-line-center">
                  <span
                    className="mon-preview-seg"
                    style={{ color: monColors.tx }}
                    title={t('上传速率（点击改色）')}
                    onClick={() => pickColor('tx')}
                  >
                    ↑ 0B
                  </span>
                  <span
                    className="mon-preview-sep"
                    style={{ color: monColors.sep }}
                    title={t('分隔符（点击改色）')}
                    onClick={() => pickColor('sep')}
                  >
                    |
                  </span>
                  <span
                    className="mon-preview-seg"
                    style={{ color: monColors.rx }}
                    title={t('下载速率（点击改色）')}
                    onClick={() => pickColor('rx')}
                  >
                    ↓ 0B
                  </span>
                </div>
                <div className="mon-preview-line mon-preview-line-center">
                  <span
                    className="mon-preview-seg"
                    style={{ color: monColors.totalUp }}
                    title={t('总上传（点击改色）')}
                    onClick={() => pickColor('totalUp')}
                  >
                    ↑ 0B
                  </span>
                  <span
                    className="mon-preview-sep"
                    style={{ color: monColors.sep }}
                    title={t('分隔符（点击改色）')}
                    onClick={() => pickColor('sep')}
                  >
                    |
                  </span>
                  <span
                    className="mon-preview-seg"
                    style={{ color: monColors.totalDown }}
                    title={t('总下载（点击改色）')}
                    onClick={() => pickColor('totalDown')}
                  >
                    ↓ 0B
                  </span>
                </div>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 12 }}>
                <button className="btn btn-ghost" onClick={resetMonColors}>
                  {t('恢复默认颜色')}
                </button>
                <span className="st-tip">{t('分隔符「|」也可点击改色')}</span>
              </div>
            </div>
          </>
        ) : section === 'desktop' ? (
          <>
            <div className="st-page-title">{t('桌面')}</div>
            <div className="st-page-desc">{t('调整桌面图标与监控信息的显示方式。')}</div>
            <div className="st-card">
              <div className="st-card-title">{t('🖥️ 图标监控')}</div>
              <label
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  cursor: 'pointer',
                  fontSize: 14,
                }}
              >
                <input
                  type="checkbox"
                  checked={hideIconMonitor}
                  onChange={(e) => setHideIconMonitor(e.target.checked)}
                  style={{ width: 15, height: 15, accentColor: 'var(--primary)' }}
                />
                {t('隐藏图标监控')}
              </label>
              <div className="st-tip" style={{ marginTop: 8 }}>
                {t('勾选后隐藏桌面图标下方的实时监控（CPU/内存/硬盘、上传/下载速率、总上传/总下载）。默认不勾选，即显示监控。')}
              </div>
            </div>
          </>
        ) : section === 'security' ? (
          <>
            <div className="st-page-title">{t('安全')}</div>
            <div className="st-page-desc">{t('管理登录密码、登录入口与用户名显示等安全选项。')}</div>

            {/* 隐藏文件管理器用户名 */}
            <div className="st-card">
              <div className="st-card-title">{t('🙈 隐藏文件管理器用户名')}</div>
              <label
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  cursor: 'pointer',
                  fontSize: 14,
                }}
              >
                <input
                  type="checkbox"
                  checked={hideFmUsername}
                  onChange={(e) => {
                    const v = e.target.checked
                    const prev = hideFmUsername
                    setHideFmUsername(v)
                    api
                      .updateSettings({ hide_fm_username: v ? '1' : '0' })
                      .catch(() => setHideFmUsername(prev))
                  }}
                  style={{ width: 15, height: 15, accentColor: 'var(--primary)' }}
                />
                {t('隐藏文件管理器用户名')}
              </label>
              <div className="st-tip" style={{ marginTop: 8 }}>
                {t('勾选后，文件管理器窗口标题不显示服务器登录用户名（如「hk4 - 文件管理器 - root」将显示为「hk4 - 文件管理器」）。该偏好保存在服务器上，在任何设备打开都会生效。')}
              </div>
            </div>

            {/* 修改密码 */}
            <div className="st-card">
              <div className="st-card-title">{t('🔑 修改密码')}</div>
              <div className="field">
                <label>{t('当前密码')}</label>
                <input
                  type="password"
                  value={oldPw}
                  onChange={(e) => setOldPw(e.target.value)}
                  autoComplete="current-password"
                />
              </div>
              <div className="field">
                <label>{t('新密码（至少 8 位）')}</label>
                <input
                  type="password"
                  value={newPw}
                  onChange={(e) => setNewPw(e.target.value)}
                  autoComplete="new-password"
                />
              </div>
              <div className="field">
                <label>{t('确认新密码')}</label>
                <input
                  type="password"
                  value={confirmPw}
                  onChange={(e) => setConfirmPw(e.target.value)}
                  autoComplete="new-password"
                />
              </div>
              <button className="btn" disabled={savingPw} onClick={changePassword}>
                {savingPw ? t('修改中…') : t('修改密码')}
              </button>
              <div className="error-text" style={pwOk ? { color: 'var(--green)' } : undefined}>
                {pwMsg}
              </div>
            </div>

            {/* 安全路由 */}
            <div className="st-card">
              <div className="st-card-title">{t('🛡️ 安全路由')}</div>
              <div className="field">
                <label>{t('安全路由')}</label>
                <input
                  value={route}
                  onChange={(e) => setRoute(e.target.value)}
                  placeholder="/login"
                  style={{ fontFamily: 'Consolas, monospace' }}
                />
              </div>
              <div className="st-tip" style={{ marginBottom: 12 }}>
                {t('设置后，浏览器只能通过该地址访问登录页（当前为 #/login）。示例：/admin-entry 、/gate-9f3k 。修改后请立即记住，否则将找不到登录入口。')}
              </div>
              <button className="btn" disabled={savingRoute} onClick={saveRoute}>
                {savingRoute ? t('保存中…') : t('保存安全路由')}
              </button>
              <div className="error-text" style={routeOk ? { color: 'var(--green)' } : undefined}>
                {routeMsg}
              </div>
            </div>

            {/* 桌面隐藏的服务器 */}
            <div className="st-card">
              <div className="st-card-title">{t('🖥️ 桌面隐藏的服务器')}</div>
              <div className="st-tip" style={{ marginBottom: 10 }}>
                {t('当前有 {0} 台服务器图标在桌面被隐藏。点击下方按钮可一次性恢复显示。', hiddenCount)}
              </div>
              <button className="btn" onClick={() => void showAllHidden()}>
                {t('显示所有桌面隐藏的服务器')}
              </button>
              {showAllMsg && (
                <div className="error-text" style={showAllOk ? { color: 'var(--green)' } : undefined}>
                  {showAllMsg}
                </div>
              )}
            </div>
          </>
        ) : section === 'language' ? (
          <>
            <div className="st-page-title">{t('语言')}</div>
            <div className="st-page-desc">{t('选择界面显示语言，切换后立即生效。')}</div>
            <div className="st-card">
              <div className="st-card-title">🌐 {t('选择语言')}</div>
              <div className="field" style={{ maxWidth: 320 }}>
                <label>{t('界面语言')}</label>
                <select
                  value={lang}
                  onChange={(e) => {
                    const v = e.target.value as Lang
                    if (v === lang) return
                    const prev = lang
                    setLang(v)
                    api.updateSettings({ lang: v }).catch(() => setLang(prev))
                  }}
                  style={{ width: '100%' }}
                >
                  <option value="zh">{t('简体中文')}</option>
                  <option value="en">{t('English')}</option>
                </select>
              </div>
              <div className="st-tip" style={{ marginTop: 10 }}>
                {t('语言偏好保存在服务器上，在任何设备打开都会生效。')}
              </div>
            </div>
          </>
        ) : (
          <>
            <div className="st-page-title">{t('关于EZSSH')}</div>
            <div className="st-page-desc">{t('你干净、高效、可视化的自托管中心化SSH Web网关。')}</div>

            <div className="st-card">
              <div className="about-line">EZSSH v{version || '…'} power by pureages</div>
              <div className="about-line">
                GitHub{' '}
                <a
                  className="about-link"
                  href="https://github.com/pureages/EZSSH"
                  target="_blank"
                  rel="noreferrer"
                >
                  github.com/pureages/EZSSH ↗
                </a>
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
