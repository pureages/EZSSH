import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { appRegistry } from '../desktop/appRegistry'
import { useWindowStore } from '../desktop/windowStore'
import { openAppWindow } from '../desktop/openApp'
import { Window } from '../desktop/Window'
import { Taskbar } from '../desktop/Taskbar'
import { ws } from '../lib/ws'
import { api, ApiError } from '../lib/api'
import { useSession } from '../lib/session'
import { useMonitorStore } from '../lib/monitorStore'
import {
  startMonitorBridge,
  stopMonitorBridge,
  syncDesktopSubscriptions,
} from '../lib/monitorBridge'
import { useDesktopSettings } from '../lib/desktopSettingsStore'
import { useGeoStore } from '../lib/geoStore'
import { useOsStore } from '../lib/osStore'
import { useSecuritySettings } from '../lib/securitySettingsStore'
import { onHostsChanged } from '../lib/hostsBus'
import { useT } from '../lib/i18n'
import { topEscClose } from '../lib/escClose'
import { OsLogo } from '../components/OsLogo'
import { FlagBadge } from '../components/FlagBadge'
import { HostFormModal } from '../components/HostFormModal'
import type { Host } from '../lib/types'

/** B/s 速率格式化为 "123.2K" / "3.4M"，保留一位小数 */
function fmtRate(bps: number): string {
  if (bps >= 1024 * 1024) return `${(bps / 1024 / 1024).toFixed(1)}M`
  if (bps >= 1024) return `${(bps / 1024).toFixed(1)}K`
  return `${Math.round(bps)}B`
}

/** 累计字节格式化为 "123.2K" / "3.4M" / "1.2G"，保留一位小数 */
function fmtBytes(b: number): string {
  if (b >= 1024 ** 3) return `${(b / 1024 ** 3).toFixed(1)}G`
  if (b >= 1024 ** 2) return `${(b / 1024 ** 2).toFixed(1)}M`
  if (b >= 1024) return `${(b / 1024).toFixed(1)}K`
  return `${Math.round(b)}B`
}

interface IconMenu {
  x: number
  y: number
  host: Host
}

/**
 * Web 桌面：壁纸 + 桌面图标区（含微型监控）+ 窗口层 + 任务栏 + 应用中心。
 */
// 图标默认位置：网格排列
const ICON_COL_GAP = 130
const ICON_ROW_GAP = 118
const ICON_ORIGIN = { x: 20, y: 20 }

function defaultIconPos(index: number): { x: number; y: number } {
  const col = index % 5
  const row = Math.floor(index / 5)
  return { x: ICON_ORIGIN.x + col * ICON_COL_GAP, y: ICON_ORIGIN.y + row * ICON_ROW_GAP }
}

function loadIconPositions(): Record<string, { x: number; y: number }> {
  try {
    return JSON.parse(localStorage.getItem('ezssh_icon_pos') || '{}')
  } catch {
    return {}
  }
}

function loadAutoSnap(): boolean {
  return localStorage.getItem('ezssh_icon_snap') === '1'
}

/** 将坐标吸附到网格点 */
function snapToGrid(v: number, origin: number, gap: number): number {
  return origin + Math.round((v - origin) / gap) * gap
}

/** 应用中心卡片默认顺序 */
const DEFAULT_APP_ORDER = ['servermap', 'addhost', 'settings', 'download', 'oneclick', 'website']

/** 从 localStorage 读取应用中心排序（非法/缺失时返回空数组） */
function loadAppOrder(): string[] {
  try {
    const raw = JSON.parse(localStorage.getItem('ezssh_app_order') || 'null')
    if (Array.isArray(raw)) return raw.filter((x) => typeof x === 'string')
  } catch {
    /* ignore */
  }
  return []
}

export function DesktopPage() {
  const navigate = useNavigate()
  const t = useT()
  const windows = useWindowStore((s) => s.windows)
  const setTitleTemplate = useWindowStore((s) => s.setTitleTemplate)
  const latest = useMonitorStore((s) => s.latest)
  const hideFmUsername = useSecuritySettings((s) => s.hideFmUsername)
  const hydrateSecurity = useSecuritySettings((s) => s.hydrate)
  const hideIconMonitor = useDesktopSettings((s) => s.hideIconMonitor)
  const desktopBg = useDesktopSettings((s) => s.bg)
  const iconScale = useDesktopSettings((s) => s.iconScale)
  const monitorFontSize = useDesktopSettings((s) => s.monitorFontSize)
  const monColors = useDesktopSettings((s) => s.monColors)
  const [appCenterOpen, setAppCenterOpen] = useState(false)
  const [hosts, setHosts] = useState<Host[]>([])
  const [iconMenu, setIconMenu] = useState<IconMenu | null>(null)
  const [desktopMenu, setDesktopMenu] = useState<{ x: number; y: number } | null>(null)
  const [hostModalOpen, setHostModalOpen] = useState(false)
  const [editingHost, setEditingHost] = useState<Host | null>(null)
  const [iconPos, setIconPos] = useState<Record<string, { x: number; y: number }>>(loadIconPositions)
  const [autoSnap, setAutoSnap] = useState<boolean>(loadAutoSnap)
  // 框选 / 多选相关
  const selectedRef = useRef<Set<string>>(new Set())
  const [, forceSelected] = useState(0)
  const setSelected = useCallback((s: Set<string>) => {
    selectedRef.current = s
    forceSelected((v) => v + 1)
  }, [])
  const [bandRect, setBandRect] = useState<{ x: number; y: number; w: number; h: number } | null>(null)
  const bandRef = useRef<{ startX: number; startY: number } | null>(null)
  const iconRefs = useRef<Map<string, HTMLDivElement>>(new Map())
  // 刷新闪烁动画
  const [flashKey, setFlashKey] = useState(0)
  // 闪烁期间强制所有图标显示为图标A（模仿 Windows 刷新）
  const [flashActive, setFlashActive] = useState(false)
  const flashTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  // 系统/地理位置识别（桌面图标用）
  const distroMap = useOsStore((s) => s.distro)
  const distroNameMap = useOsStore((s) => s.distroName)
  const geoByAddr = useGeoStore((s) => s.byAddr)
  const dragRef = useRef<{
    startX: number
    startY: number
    origs: Record<string, { x: number; y: number }>
    moved: boolean
  } | null>(null)
  const suppressDblClick = useRef(false)

  // ---- 应用中心拖拽排序 ----
  const [appOrder, setAppOrder] = useState<string[]>(() => {
    const saved = loadAppOrder()
    const seen = new Set<string>()
    const merged: string[] = []
    for (const k of saved) {
      if (DEFAULT_APP_ORDER.includes(k) && !seen.has(k)) {
        seen.add(k)
        merged.push(k)
      }
    }
    for (const k of DEFAULT_APP_ORDER) {
      if (!seen.has(k)) {
        seen.add(k)
        merged.push(k)
      }
    }
    return merged
  })
  const dragIdRef = useRef<string | null>(null)
  const dragStartRef = useRef(0)
  const [dragOverId, setDragOverId] = useState<string | null>(null)

  // 持久化应用中心排序
  useEffect(() => {
    try {
      localStorage.setItem('ezssh_app_order', JSON.stringify(appOrder))
    } catch {
      /* ignore */
    }
  }, [appOrder])

  // 持久化图标位置
  useEffect(() => {
    try {
      localStorage.setItem('ezssh_icon_pos', JSON.stringify(iconPos))
    } catch {
      /* ignore */
    }
  }, [iconPos])

  // 持久化自动对齐开关
  useEffect(() => {
    try {
      localStorage.setItem('ezssh_icon_snap', autoSnap ? '1' : '0')
    } catch {
      /* ignore */
    }
  }, [autoSnap])

  // 拖拽处理器（支持多选拖动）
  const onIconPointerDown = (e: React.PointerEvent, h: Host, _index: number) => {
    if (e.button !== 0) return // 仅左键
    // 判断本次拖拽涉及的图标集合：
    // - 当前图标已在多选集合中 → 拖动整个多选组
    // - 否则只拖动当前图标，并把多选集合替换为仅包含本图标
    let dragIds: string[]
    if (selectedRef.current.has(h.id) && selectedRef.current.size > 1) {
      dragIds = Array.from(selectedRef.current)
    } else {
      dragIds = [h.id]
      setSelected(new Set([h.id]))
    }
    const origs: Record<string, { x: number; y: number }> = {}
    for (const id of dragIds) {
      const ix = hosts.findIndex((x) => x.id === id)
      origs[id] = iconPos[id] ?? defaultIconPos(Math.max(0, ix))
    }
    dragRef.current = {
      startX: e.clientX,
      startY: e.clientY,
      origs,
      moved: false,
    }
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    // 阻止冒泡到桌面，避免同时触发框选
    e.stopPropagation()
  }

  const onIconPointerMove = (e: React.PointerEvent) => {
    const d = dragRef.current
    if (!d) return
    const dx = e.clientX - d.startX
    const dy = e.clientY - d.startY
    if (Math.abs(dx) > 3 || Math.abs(dy) > 3) d.moved = true
    if (d.moved) {
      setIconPos((prev) => {
        const next = { ...prev }
        for (const [id, o] of Object.entries(d.origs)) {
          let x = Math.max(0, o.x + dx)
          let y = Math.max(0, o.y + dy)
          if (autoSnap) {
            x = snapToGrid(x, ICON_ORIGIN.x, ICON_COL_GAP)
            y = snapToGrid(y, ICON_ORIGIN.y, ICON_ROW_GAP)
          }
          next[id] = { x, y }
        }
        return next
      })
    }
  }

  const onIconPointerUp = () => {
    const d = dragRef.current
    dragRef.current = null
    // 发生拖拽则本次忽略后续双击
    if (d?.moved) {
      suppressDblClick.current = true
      setTimeout(() => (suppressDblClick.current = false), 400)
    }
  }

  /** 打开新增服务器表单 */
  const openAddHost = () => {
    setEditingHost(null)
    setHostModalOpen(true)
  }

  /** 应用中心卡片（可拖拽排序） */
  const APP_CARDS: Record<string, { icon: string; label: string; run: () => void }> = {
    servermap: { icon: '🌍', label: t('世界地图'), run: () => openApp('servermap', null) },
    addhost: { icon: '➕', label: t('添加服务器'), run: () => openAddHost() },
    settings: { icon: '⚙️', label: t('设置'), run: () => openApp('settings', null) },
    download: { icon: '⬇️', label: t('直链下载'), run: () => openApp('download', null) },
    oneclick: { icon: '⚡', label: t('一键命令'), run: () => openApp('oneclick', null) },
    website: { icon: '🌐', label: t('网站管理'), run: () => openApp('website', null) },
  }

  /** 打开编辑服务器表单 */
  const openEditHost = (h: Host) => {
    setEditingHost(h)
    setHostModalOpen(true)
    setIconMenu(null)
  }

  /** 移除服务器（不可逆） */
  const removeHost = async (h: Host) => {
    setIconMenu(null)
    if (!window.confirm(t('您确定要移除此服务器吗？操作不可逆！\n\n{0}（{1}@{2}）', h.name, h.username, h.host))) {
      return
    }
    try {
      await api.deleteHost(h.id)
      refreshHosts()
    } catch (e) {
      alert(e instanceof ApiError ? e.message : t('删除失败'))
    }
  }

  // 主机类应用（右键图标菜单使用）
  const hostApps = appRegistry.filter((a) => a.needsHost)

  const refreshHosts = () => {
    api
      .listHosts()
      .then(setHosts)
      .catch(() => {
        // 401 由全局会话状态统一处理；其它错误静默
      })
  }

  // 监控数据已到达但主机列表仍标记未连接时，刷新一次主机列表：
  // 平台探测 + 连接状态在首次 GetClient 后异步完成/持久化，桌面图标的
  // 在线圆点与 Docker 置灰需要最新的 hosts 数据才能即时生效。
  const refreshedLiveRef = useRef<Set<string>>(new Set())
  useEffect(() => {
    for (const h of hosts) {
      if (h.connected) refreshedLiveRef.current.delete(h.id)
    }
    for (const h of hosts) {
      const m = latest[h.id]
      if (m && !m.error && !h.connected && !refreshedLiveRef.current.has(h.id)) {
        refreshedLiveRef.current.add(h.id)
        refreshHosts()
        break
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [latest, hosts])

  useEffect(() => {
    refreshHosts()
    void ws.connect().then(() => startMonitorBridge())
    // 拉取后端安全设置（隐藏文件管理器用户名等），用于窗口标题
    api
      .getSettings()
      .then((s) => hydrateSecurity(s))
      .catch(() => {})
    return () => {
      stopMonitorBridge()
      if (flashTimer.current) clearTimeout(flashTimer.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 隐藏用户名开关变化时：同步已打开的文件管理器窗口标题（新窗口在 openApp 时已按当前值生成）
  useEffect(() => {
    const hostsById = new Map(hosts.map((h) => [h.id, h]))
    for (const w of useWindowStore.getState().windows) {
      if (w.appId !== 'files' || !w.hostId) continue
      const host = hostsById.get(w.hostId)
      if (!host) continue
      const showUser = !hideFmUsername && !!host.username
      setTitleTemplate(
        w.id,
        showUser ? '{0} - {1} - {2}' : '{0} - {1}',
        showUser ? [host.name, '文件管理器', host.username] : [host.name, '文件管理器'],
      )
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hideFmUsername, hosts])

  // ESC：优先关闭最上层的子窗口（弹窗），否则关闭当前活动窗口。
  // 终端内部不拦截（xterm 占用 ESC 键）；后台进度角标存在时不关闭窗口。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      const t = e.target as HTMLElement | null
      if (t && t.closest && t.closest('.xterm')) return // 终端内 ESC 交给终端处理
      const top = topEscClose()
      if (top) {
        top()
        return
      }
      const masks = document.querySelectorAll<HTMLElement>('.modal-mask')
      if (masks.length > 0) {
        masks[masks.length - 1].click() // 触发最上层弹窗的"点击遮罩关闭"
        return
      }
      // 后台运行进度角标存在时不关闭窗口（避免丢失正在进行的操作追踪）
      if (document.querySelector('.progress-badge')) return
      const s = useWindowStore.getState()
      const active = s.windows.find((w) => w.z === s.zTop && !w.minimized)
      if (active) s.close(active.id)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  // 设置页「显示所有桌面隐藏的服务器」等操作后刷新图标
  useEffect(() => onHostsChanged(refreshHosts), [])

  // 主机列表变化时：同步桌面监控订阅 + 探测系统 logo 与地理位置（国旗）
  useEffect(() => {
    const visible = hosts.filter((h) => !h.hidden)
    void ws.connect().then(() => syncDesktopSubscriptions(visible.map((h) => h.id)))
    useOsStore.getState().load(visible)
    useGeoStore.getState().load(visible.map((h) => h.host))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hosts])

  const openApp = (appId: string, hostId: string | null) => {
    const host = hostId ? hosts.find((h) => h.id === hostId) : undefined
    openAppWindow(appId, host)
  }

  const onIconContextMenu = (e: React.MouseEvent, host: Host) => {
    e.preventDefault()
    e.stopPropagation()
    setAppCenterOpen(false)
    setDesktopMenu(null)
    setIconMenu({ x: e.clientX, y: e.clientY, host })
  }

  /** 桌面空白处右键：刷新 / 添加服务器 / 退出登录 */
  const onDesktopContextMenu = (e: React.MouseEvent) => {
    // 仅在桌面空白处（壁纸本身或其空白容器）触发，窗口/图标/任务栏/菜单不冒泡到此处
    const t = e.target as HTMLElement
    if (t.closest('.window') || t.closest('.d-icon') || t.closest('.taskbar') ||
        t.closest('.app-center') || t.closest('.ctx-menu') || t.closest('.host-picker')) {
      return
    }
    e.preventDefault()
    setIconMenu(null)
    setAppCenterOpen(false)
    setDesktopMenu({ x: e.clientX, y: e.clientY })
  }

  const onLogout = async () => {
    try {
      await api.logout()
    } finally {
      ws.close()
      useSession.getState().setAuthed(false)
      navigate(useSession.getState().loginRoute, { replace: true })
    }
  }

  /** 触发桌面刷新闪烁动画：闪烁瞬间所有图标回到图标A，随后重新探测系统/地理位置 */
  const triggerFlash = () => {
    setFlashKey((k) => k + 1)
    setFlashActive(true)
    if (flashTimer.current) clearTimeout(flashTimer.current)
    flashTimer.current = setTimeout(() => setFlashActive(false), 93)
    // 重置识别结果：图标先全部回到图标A，再由 hosts 变更 effect 重新探测
    useOsStore.getState().resetAll()
    useGeoStore.getState().clear()
  }

  /** 桌面空白处按下：启动框选 */
  const onDesktopPointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return
    const t = e.target as HTMLElement
    if (
      t.closest('.window') ||
      t.closest('.taskbar') ||
      t.closest('.ctx-menu') ||
      t.closest('.app-center') ||
      t.closest('.d-icon') ||
      t.closest('.host-picker') ||
      t.closest('.modal-mask')
    ) {
      return
    }
    bandRef.current = { startX: e.clientX, startY: e.clientY }
    setBandRect({ x: e.clientX, y: e.clientY, w: 0, h: 0 })
    setSelected(new Set())
    try {
      ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    } catch {
      /* ignore */
    }
  }

  const onDesktopPointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    const b = bandRef.current
    if (!b) return
    const x = Math.min(b.startX, e.clientX)
    const y = Math.min(b.startY, e.clientY)
    const w = Math.abs(e.clientX - b.startX)
    const h = Math.abs(e.clientY - b.startY)
    setBandRect({ x, y, w, h })
    const sel = new Set<string>()
    for (const host of hosts) {
      const el = iconRefs.current.get(host.id)
      if (!el) continue
      const r = el.getBoundingClientRect()
      if (r.right >= x && r.left <= x + w && r.bottom >= y && r.top <= y + h) {
        sel.add(host.id)
      }
    }
    setSelected(sel)
  }

  const onDesktopPointerUp = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!bandRef.current) return
    const rect = bandRect
    bandRef.current = null
    setBandRect(null)
    if (!rect || rect.w < 4 || rect.h < 4) {
      setSelected(new Set())
    }
    try {
      ;(e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId)
    } catch {
      /* ignore */
    }
  }

  return (
    <div
      className="desktop"
      style={{
        // 图标大小与监控文字大小（设置-个性化调节）
        ['--icon-scale' as string]: String(iconScale),
        ['--mon-font-size' as string]: `${monitorFontSize}px`,
        // 监控各段文字颜色（设置-个性化调节）
        ['--mon-color-cpu' as string]: monColors.cpu,
        ['--mon-color-mem' as string]: monColors.mem,
        ['--mon-color-disk' as string]: monColors.disk,
        ['--mon-color-tx' as string]: monColors.tx,
        ['--mon-color-rx' as string]: monColors.rx,
        ['--mon-color-total-up' as string]: monColors.totalUp,
        ['--mon-color-total-down' as string]: monColors.totalDown,
        ['--mon-color-sep' as string]: monColors.sep,
        ...(desktopBg
          ? {
              backgroundImage: `url(${desktopBg})`,
              backgroundSize: 'cover',
              backgroundPosition: 'center',
              backgroundRepeat: 'no-repeat',
            }
          : {}),
      }}
      onClick={() => {
        if (appCenterOpen) setAppCenterOpen(false)
        if (iconMenu) setIconMenu(null)
        if (desktopMenu) setDesktopMenu(null)
      }}
      onContextMenu={onDesktopContextMenu}
      onPointerDown={onDesktopPointerDown}
      onPointerMove={onDesktopPointerMove}
      onPointerUp={onDesktopPointerUp}
      onPointerCancel={onDesktopPointerUp}
    >
      {/* 桌面图标区：主机快捷方式 + 微型监控（可左键拖拽定位） */}
      <div className="desktop-icons">
        {hosts
          .filter((h) => !h.hidden)
          .map((h, index) => {
            const m = latest[h.id]
            const pos = iconPos[h.id] ?? defaultIconPos(index)
            const distroName = distroNameMap[h.id]
            const geo = geoByAddr[h.host]
            const titleLines = [
              t('{0}（{1}@{2}）', h.name, h.username, h.host),
              distroName ? t('系统：{0}', distroName) : '',
              geo ? t('位置：{0}', `${geo.country}${geo.region ? '·' + geo.region : ''}`) : '',
              t('左键拖动可调整位置，双击打开文件管理器，右键选择应用'),
            ].filter(Boolean)
            return (
              <div
                key={h.id}
                className={`d-icon${selectedRef.current.has(h.id) ? ' selected' : ''}`}
                style={{ left: pos.x, top: pos.y }}
                ref={(el) => {
                  if (el) iconRefs.current.set(h.id, el)
                  else iconRefs.current.delete(h.id)
                }}
                title={titleLines.join('\n')}
                onPointerDown={(e) => onIconPointerDown(e, h, index)}
                onPointerMove={onIconPointerMove}
                onPointerUp={onIconPointerUp}
                onDoubleClick={() => {
                  if (suppressDblClick.current) return
                  openApp('files', h.id)
                }}
                onContextMenu={(e) => onIconContextMenu(e, h)}
              >
                <span className="os-box">
                  <span className={`dot ${h.connected ? 'online' : 'offline'}`} />
                  {/* 系统 logo：未识别或刷新闪烁瞬间显示图标A */}
                  <OsLogo distro={distroMap[h.id]} forceBase={flashActive} size={30} />
                  <span className="flag-badge">
                    <FlagBadge code={geo?.country_code} size={22.5} />
                  </span>
                </span>
                <span className="lbl">{t(h.name)}</span>
                {!hideIconMonitor && (
                  <div
                    className="mon"
                    title={t('CPU | 内存 | 硬盘\n上传速率 | 下载速率\n总上传 | 总下载')}
                  >
                    {m && !m.error ? (
                      <>
                        <div className="mon-line">
                          <span className="mon-cpu">{m.cpu.toFixed(1)}%</span>
                          <span className="mon-sep">|</span>
                          <span className="mon-mem">{m.memPct.toFixed(1)}%</span>
                          <span className="mon-sep">|</span>
                          <span className="mon-disk">{m.diskPct.toFixed(1)}%</span>
                        </div>
                        <div className="mon-line mon-line-center">
                          <span className="mon-tx">↑ {fmtRate(m.tx)}</span>
                          <span className="mon-sep">|</span>
                          <span className="mon-rx">↓ {fmtRate(m.rx)}</span>
                        </div>
                        <div className="mon-line mon-line-center">
                          <span className="mon-total-up">↑ {fmtBytes(m.txBytes)}</span>
                          <span className="mon-sep">|</span>
                          <span className="mon-total-down">↓ {fmtBytes(m.rxBytes)}</span>
                        </div>
                      </>
                    ) : !h.connected || m?.error ? (
                      <span className="mon-offline">{t('已离线')}</span>
                    ) : (
                      '…'
                    )}
                  </div>
                )}
              </div>
            )
          })}
      </div>

      {/* 窗口层 */}
      {windows.map((w) => (
        <Window key={w.id} win={w} />
      ))}

      {/* 框选虚线框 */}
      {bandRect && (bandRect.w > 2 || bandRect.h > 2) && (
        <div
          className="rubber-band"
          style={{ left: bandRect.x, top: bandRect.y, width: bandRect.w, height: bandRect.h }}
        />
      )}

      {/* 桌面刷新闪烁动画（key 变化重启动画） */}
      {flashKey > 0 && <div key={flashKey} className="desktop-flash" />}

      {/* 应用中心：添加服务器 / 设置（可拖拽排序，localStorage 持久化） */}
      {appCenterOpen && (
        <div className="app-center" onClick={(e) => e.stopPropagation()}>
          <div className="app-grid">
            {appOrder.map((id) => {
              const card = APP_CARDS[id]
              if (!card) return null
              return (
                <div
                  key={id}
                  className={`app-card${dragOverId === id ? ' drag-over' : ''}`}
                  draggable
                  onDragStart={(e) => {
                    dragStartRef.current = Date.now()
                    dragIdRef.current = id
                    try {
                      e.dataTransfer.setData('text/plain', id)
                    } catch {
                      /* ignore */
                    }
                    e.dataTransfer.effectAllowed = 'move'
                  }}
                  onDragOver={(e) => {
                    e.preventDefault()
                    e.dataTransfer.dropEffect = 'move'
                    if (dragOverId !== id) setDragOverId(id)
                  }}
                  onDragLeave={() => {
                    if (dragOverId === id) setDragOverId(null)
                  }}
                  onDrop={(e) => {
                    e.preventDefault()
                    const from = dragIdRef.current
                    setDragOverId(null)
                    if (from && from !== id) {
                      setAppOrder((prev) => {
                        const i = prev.indexOf(from)
                        const j = prev.indexOf(id)
                        if (i < 0 || j < 0) return prev
                        const next = [...prev]
                        next.splice(i, 1)
                        next.splice(j, 0, from)
                        return next
                      })
                    }
                  }}
                  onDragEnd={() => {
                    dragIdRef.current = null
                    setDragOverId(null)
                  }}
                  onClick={() => {
                    // 拖拽后的残留 click 不触发（时间守卫）
                    if (dragStartRef.current && Date.now() - dragStartRef.current < 200) return
                    setAppCenterOpen(false)
                    card.run()
                  }}
                >
                  <span className="ico">{card.icon}</span>
                  <span className="lbl">{card.label}</span>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* 桌面空白处右键菜单：刷新 / 添加服务器 / 退出登录 */}
      {desktopMenu && (
        <div
          className="ctx-menu"
          style={{ left: desktopMenu.x, top: desktopMenu.y }}
          onClick={(e) => e.stopPropagation()}
        >
          <div
            className="ctx-menu-item"
            onClick={() => {
              setDesktopMenu(null)
              refreshHosts()
              triggerFlash()
            }}
          >
            {t('🔄 刷新')}
          </div>
          <div
            className="ctx-menu-item"
            onClick={() => {
              setDesktopMenu(null)
              openAddHost()
            }}
          >
            {t('➕ 添加服务器')}
          </div>
          <div
            className="ctx-menu-item"
            onClick={() => {
              setDesktopMenu(null)
              openApp('settings', null)
            }}
          >
            {t('⚙️ 设置')}
          </div>
          <div
            className="ctx-menu-item"
            onClick={() => setAutoSnap((v) => !v)}
          >
            {autoSnap ? '✅' : '⬜'} {t('自动对齐')}
          </div>
          <div className="ctx-menu-sep" />
          <div className="ctx-menu-item" onClick={() => { setDesktopMenu(null); void onLogout() }}>
            {t('⏻ 退出登录')}
          </div>
        </div>
      )}

      {/* 图标右键菜单：选择应用 */}
      {iconMenu && (
        <div
          className="ctx-menu"
          style={{ left: iconMenu.x, top: iconMenu.y }}
          onClick={(e) => e.stopPropagation()}
        >
          <div className="ctx-menu-title">
            {t(iconMenu.host.name)}（{t(iconMenu.host.host)}）
          </div>
          {hostApps.map((app) => {
            const hp = iconMenu.host.platform
            const disabled =
              !!hp && !!app.platforms?.length && !app.platforms.includes(hp as 'linux' | 'windows')
            return (
              <div
                key={app.id}
                className={`ctx-menu-item${disabled ? ' disabled' : ''}`}
                title={
                  disabled
                    ? t(app.disabledTip || '当前系统不支持该应用')
                    : undefined
                }
                onClick={
                  disabled
                    ? undefined
                    : () => {
                        openApp(app.id, iconMenu.host.id)
                        setIconMenu(null)
                      }
                }
              >
                {app.icon} {t(app.name)}
              </div>
            )
          })}
          <div className="ctx-menu-sep" />
          <div
            className="ctx-menu-item"
            onClick={() => openEditHost(iconMenu.host)}
          >
            {t('✏️ 编辑')}
          </div>
          <div
            className="ctx-menu-item"
            onClick={() => {
              void (async () => {
                try {
                  await api.hideHost(iconMenu.host.id, true)
                  setIconMenu(null)
                  refreshHosts()
                } catch (err) {
                  alert(err instanceof ApiError ? err.message : t('隐藏失败'))
                }
              })()
            }}
          >
            {t('🙈 隐藏')}
          </div>
          <div className="ctx-menu-sep" />
          <div
            className="ctx-menu-item danger"
            onClick={() => {
              setIconMenu(null)
              void removeHost(iconMenu.host)
            }}
          >
            {t('🗑️ 删除')}
          </div>
        </div>
      )}

      <Taskbar
        onStartClick={() => {
          setIconMenu(null)
          setDesktopMenu(null)
          setAppCenterOpen((v) => !v)
        }}
      />

      <HostFormModal
        open={hostModalOpen}
        editing={editingHost}
        onClose={() => setHostModalOpen(false)}
        onSaved={refreshHosts}
      />
    </div>
  )
}
