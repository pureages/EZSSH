import { useCallback, useRef } from 'react'
import { useWindowStore, type WinState } from './windowStore'
import { getApp } from './appRegistry'
import { useI18n, tt } from '../lib/i18n'

export function Window({ win }: { win: WinState }) {
  const { focus, close, minimize, toggleMaximize, move, resize } = useWindowStore()
  const zTop = useWindowStore((s) => s.zTop)
  // 订阅语言：切换时窗口标题自动重译（模板标题 + 自定义标题回退）
  useI18n((s) => s.lang)
  const dragRef = useRef<{ sx: number; sy: number; ox: number; oy: number } | null>(null)
  const resizeRef = useRef<{ sx: number; sy: number; ox: number; oy: number; ow: number; oh: number; dir: string } | null>(null)
  const app = getApp(win.appId)
  const active = win.z === zTop

  // 窗口标题：模板标题（{0} - {1}）按当前语言重译；自定义标题整体尝试查字典，查不到保留原文
  let windowTitle: string
  if (win.titleKey) {
    const args = (win.titleArgs || []).map((a) => tt(String(a)))
    windowTitle = tt(win.titleKey, ...args)
  } else {
    windowTitle = tt(win.title)
  }

  // onTitle 必须稳定引用，否则 App 组件的 useEffect 依赖它会陷入无限循环
  const onTitle = useCallback(
    (title: string) => {
      useWindowStore.setState((s) => ({
        windows: s.windows.map((w) => (w.id === win.id ? { ...w, title } : w)),
      }))
    },
    [win.id],
  )

  const onDragStart = (e: React.PointerEvent) => {
    if (win.maximized) return
    e.preventDefault()
    focus(win.id)
    dragRef.current = { sx: e.clientX, sy: e.clientY, ox: win.x, oy: win.y }
    ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
  }

  const onDragMove = (e: React.PointerEvent) => {
    const d = dragRef.current
    if (!d) return
    const dx = e.clientX - d.sx
    const dy = e.clientY - d.sy
    move(win.id, d.ox + dx, d.oy + dy)
  }

  const onDragEnd = () => {
    dragRef.current = null
  }

  const onResizeStart = (dir: string) => (e: React.PointerEvent) => {
    e.preventDefault()
    e.stopPropagation()
    focus(win.id)
    resizeRef.current = {
      sx: e.clientX,
      sy: e.clientY,
      ox: win.x,
      oy: win.y,
      ow: win.width,
      oh: win.height,
      dir,
    }
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  }

  const MIN_W = 320
  const MIN_H = 200

  const onResizeMove = (e: React.PointerEvent) => {
    const d = resizeRef.current
    if (!d) return
    const dx = e.clientX - d.sx
    const dy = e.clientY - d.sy
    let { x, y, width, height } = { x: d.ox, y: d.oy, width: d.ow, height: d.oh }
    if (d.dir.includes('e')) width = d.ow + dx
    if (d.dir.includes('s')) height = d.oh + dy
    if (d.dir.includes('w')) {
      width = d.ow - dx
      x = d.ox + dx
    }
    if (d.dir.includes('n')) {
      height = d.oh - dy
      y = d.oy + dy
    }
    // 从左侧/上边拉伸时，宽度/高度不能小于最小限制，否则反向越过对边
    if (width < MIN_W) {
      if (d.dir.includes('w')) x = d.ox + d.ow - MIN_W
      width = MIN_W
    }
    if (height < MIN_H) {
      if (d.dir.includes('n')) y = d.oy + d.oh - MIN_H
      height = MIN_H
    }
    resize(win.id, x, y, width, height)
  }

  const onResizeEnd = () => {
    resizeRef.current = null
  }

  // 八个方向的缩放把手：e/w/n/s + 四角
  const RESIZE_DIRS = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw'] as const

  const style: React.CSSProperties = win.maximized
    ? { inset: 0, zIndex: win.z, width: 'auto', height: 'auto' }
    : {
        left: win.x,
        top: win.y,
        width: win.width,
        height: win.height,
        zIndex: win.z,
      }

  return (
    <div
      className={`window${active ? ' active' : ''}${win.minimized ? ' minimized' : ''}${win.maximized ? ' maximized' : ''}`}
      style={style}
      onPointerDown={() => focus(win.id)}
    >
      <div
        className="win-titlebar"
        onPointerDown={onDragStart}
        onPointerMove={onDragMove}
        onPointerUp={onDragEnd}
        onDoubleClick={() => toggleMaximize(win.id)}
      >
        <div className="win-title">
          {win.icon} {windowTitle}
        </div>
        {/* Windows 风格：右上角最小化 / 全屏 / 关闭 */}
        <div className="win-actions">
          <button
            className="win-act-btn"
            title={tt('最小化')}
            onPointerDown={(e) => e.stopPropagation()}
            onClick={(e) => {
              e.stopPropagation()
              minimize(win.id)
            }}
          >
            ─
          </button>
          <button
            className="win-act-btn"
            title={win.maximized ? tt('还原') : tt('全屏')}
            onPointerDown={(e) => e.stopPropagation()}
            onClick={(e) => {
              e.stopPropagation()
              toggleMaximize(win.id)
            }}
          >
            {win.maximized ? '❐' : '□'}
          </button>
          <button
            className="win-act-btn win-act-close"
            title={tt('关闭')}
            onPointerDown={(e) => e.stopPropagation()}
            onClick={(e) => {
              e.stopPropagation()
              close(win.id)
            }}
          >
            ✕
          </button>
        </div>
      </div>
      <div className="win-body">
        {app ? (
          <app.component
            windowId={win.id}
            hostId={win.hostId}
            platform={win.platform}
            channelId={win.channelId}
            onTitle={onTitle}
          />
        ) : (
          <div style={{ padding: 24, color: 'var(--text-1)' }}>{tt('未知应用')}</div>
        )}
      </div>
      {!win.maximized &&
        RESIZE_DIRS.map((dir) => (
          <div
            key={dir}
            className={`win-resize win-resize-${dir}`}
            onPointerDown={onResizeStart(dir)}
            onPointerMove={onResizeMove}
            onPointerUp={onResizeEnd}
          />
        ))}
    </div>
  )
}
