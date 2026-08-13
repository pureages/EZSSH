import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import { newChannelId, ws } from '../lib/ws'
import { consumePendingTerminalCwd } from '../lib/terminalLaunch'
import type { AppProps } from '../desktop/appRegistry'
import { useT } from '../lib/i18n'

function b64encode(data: Uint8Array): string {
  let bin = ''
  const chunk = 0x8000
  for (let i = 0; i < data.length; i += chunk) {
    bin += String.fromCharCode(...data.subarray(i, i + chunk))
  }
  return btoa(bin)
}

function b64decode(data: string): Uint8Array {
  const bin = atob(data)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

/** 复制降级：clipboard API 不可用时用临时 textarea + execCommand */
function copyFallback(text: string) {
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    ta.remove()
  } catch {
    /* ignore */
  }
}

/**
 * 粘贴降级：非安全上下文（http 访问）下无 Clipboard API，
 * 聚焦隐藏 textarea 后尝试 execCommand('paste')，通过 capture 阶段
 * 的 paste 事件读取剪贴板文本；execCommand('paste') 被浏览器禁用时
 * 再尝试 navigator.clipboard.readText()（HTTPS 场景），最后回调空串
 * 由调用方引导用户按 Ctrl+V。
 */
function pasteFallback(onText: (text: string) => void) {
  const ta = document.createElement('textarea')
  ta.style.position = 'fixed'
  ta.style.left = '-9999px'
  ta.style.top = '0'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  let settled = false
  const settle = (ok: boolean, text = '') => {
    if (settled) return
    settled = true
    window.removeEventListener('paste', onPaste, true)
    ta.remove()
    if (ok) onText(text)
    else tryReadClipboard()
  }
  const onPaste = (e: ClipboardEvent) => {
    const text = e.clipboardData?.getData('text/plain') ?? ''
    settle(true, text)
  }
  const tryReadClipboard = () => {
    if (navigator.clipboard?.readText) {
      navigator.clipboard
        .readText()
        .then((txt) => {
          if (txt) onText(txt)
          else onText('')
        })
        .catch(() => onText(''))
    } else {
      onText('')
    }
  }
  window.addEventListener('paste', onPaste, true)
  ta.focus()
  try {
    if (!document.execCommand('paste')) settle(false)
  } catch {
    settle(false)
  }
}

/**
 * 终端 App：xterm.js + WebSocket 与后端 SSH shell 双向打通。
 * - 右键复制：仅复制到剪贴板，保留选区与画面（不弹提示、不黑屏）
 * - 右键粘贴：优先 Clipboard API，失败走降级
 * - 断线自动重连：WS 断开重连后自动重建终端会话（应对浏览器节能冻结/空闲断连）
 */
export function TerminalApp({ windowId, hostId, channelId, platform }: AppProps) {
  const t = useT()
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const channelIdRef = useRef(channelId || newChannelId())
  const hostIdRef = useRef(hostId)
  // 顶部居中的临时提示（粘贴结果反馈等）
  const [toastMsg, setToastMsg] = useState('')
  const toastTimerRef = useRef<number | null>(null)
  const toast = (msg: string) => {
    setToastMsg(msg)
    if (toastTimerRef.current) window.clearTimeout(toastTimerRef.current)
    toastTimerRef.current = window.setTimeout(() => setToastMsg(''), 1800)
  }

  /** 终端右键（Linux 习惯）：有选区 → 复制（保留选区，不提示）；无选区 → 粘贴 */
  const handleTermContextMenu = (e: React.MouseEvent) => {
    e.preventDefault()
    const term = termRef.current
    if (!term) return
    if (term.hasSelection()) {
      // 只复制到剪贴板：保留选区与画面内容，避免清除选区导致"内容消失/黑屏"
      const text = term.getSelection()
      if (navigator.clipboard?.writeText) {
        navigator.clipboard.writeText(text).catch(() => copyFallback(text))
      } else {
        copyFallback(text)
      }
      return
    }
    const target = (text: string) => {
      const tm = termRef.current
      if (!tm) return
      if (text) {
        tm.paste(text)
        tm.focus()
        toast(t('已粘贴'))
      } else {
        toast(t('浏览器禁止自动读取剪贴板，请按 Ctrl+V 粘贴'))
      }
    }
    if (navigator.clipboard?.readText) {
      navigator.clipboard
        .readText()
        .then(target)
        .catch(() => pasteFallback(target))
    } else {
      pasteFallback(target)
    }
  }

  useEffect(() => {
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Menlo, Consolas, "Courier New", monospace',
      theme: {
        background: 'rgba(2, 6, 23, 0.92)',
        foreground: '#f1f5f9',
        cursor: '#60a5fa',
        selectionBackground: 'rgba(59, 130, 246, 0.4)',
      },
      scrollback: 10000,
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.loadAddon(new WebLinksAddon())
    termRef.current = term
    fitRef.current = fit

    const cid = channelIdRef.current
    let disposed = false
    let cleanup: (() => void) | null = null
    let mounted = false

    /** 挂载/重建终端会话：订阅、open、输入、心跳。断线重连后再次调用。 */
    const mount = () => {
      if (disposed) return
      if (cleanup) {
        try {
          cleanup()
        } catch {
          /* ignore */
        }
        cleanup = null
      }
      mounted = true

      // 消费启动上下文（文件管理器打开终端 cd / 一键命令注入）
      const init = consumePendingTerminalCwd(hostIdRef.current ?? '')
      const isWin = platform === 'windows'

      // 初始命令注入：等 shell 就绪（收到首次输出）后再写，规避 Windows
      // ConPTY 在启动期收到突发输入时回显乱序/倒序的问题；超时兜底。
      let injected = false
      let injectTimer: number | undefined
      const injectInit = () => {
        if (injected || disposed) return
        injected = true
        if (injectTimer) window.clearTimeout(injectTimer)
        if (!init.cwd && !init.command) return
        // 首屏输出渲染稳定后再写，避免与控制台初始化竞争
        injectTimer = window.setTimeout(() => {
          if (disposed) return
          const parts: string[] = []
          if (init.cwd && init.cwd !== '/') {
            // Windows PowerShell 5.1 不支持 && / clear，仅 cd + \r
            parts.push(
              isWin
                ? `cd ${JSON.stringify(init.cwd)}\r`
                : `cd ${JSON.stringify(init.cwd)} && clear\n`,
            )
          }
          if (init.command) {
            // Windows 控制台只把 \r 识别为回车提交命令，\n 不会提交
            parts.push(isWin ? `${init.command}\r` : `${init.command}\n`)
          }
          if (parts.length) {
            ws.send('terminal.input', cid, {
              data: b64encode(new TextEncoder().encode(parts.join(''))),
            })
          }
        }, isWin ? 350 : 80)
      }
      const onFirstOutput = () => injectInit()
      // 超时兜底：shell 无输出（静默 shell / 启动极慢）时也强制注入
      injectTimer = window.setTimeout(injectInit, isWin ? 5000 : 2000)

      // 订阅该 channel 的消息
      const unsub = ws.onChannel(cid, (msg) => {
        if (disposed) return
        switch (msg.type) {
          case 'terminal.output': {
            const raw = msg.payload?.data as string
            if (raw) {
              term.write(b64decode(raw))
              onFirstOutput()
            }
            break
          }
          case 'terminal.exit': {
            term.write(`\r\n\x1b[33m${t('[进程已退出，连接已关闭]')}\x1b[0m\r\n`)
            break
          }
          case 'error': {
            const e = (msg.payload?.message as string) || t('未知错误')
            term.write(`\r\n\x1b[31m${t('[EZSSH] ${0}', e)}\x1b[0m\r\n`)
            break
          }
        }
      })
      // 订阅全局错误（terminal.open 失败等错误消息不带 channelId）
      const unsubErr = ws.onType('error', (msg) => {
        if (disposed) return
        const e = (msg.payload?.message as string) || t('未知错误')
        term.write(`\r\n\x1b[31m${t('[EZSSH] ${0}', e)}\x1b[0m\r\n`)
      })

      // 打开远端 shell（hostId 必须随消息发送，否则后端无法定位目标主机）
      ws.send('terminal.open', cid, {
        hostId: hostIdRef.current,
        cols: term.cols,
        rows: term.rows,
      })

      // 输入透传（base64）
      const dataDisposable = term.onData((d) => {
        if (disposed) return
        ws.send('terminal.input', cid, { data: b64encode(new TextEncoder().encode(d)) })
      })

      // 尺寸变化时同步 pty
      const resizeDisposable = term.onResize(({ cols, rows }) => {
        if (disposed) return
        ws.send('terminal.resize', cid, { cols, rows })
      })

      // 首次渲染后 fit 并同步尺寸
      requestAnimationFrame(() => {
        try {
          fit.fit()
        } catch {
          /* container 尚未布局完成 */
        }
        if (!disposed) {
          ws.send('terminal.resize', cid, { cols: term.cols, rows: term.rows })
        }
      })

      // 心跳：连接空闲时的保活（每 30s）
      const ping = setInterval(() => {
        if (!disposed) ws.send('ping', cid, undefined)
      }, 30000)

      cleanup = () => {
        if (injectTimer) window.clearTimeout(injectTimer)
        clearInterval(ping)
        dataDisposable.dispose()
        resizeDisposable.dispose()
        unsub()
        unsubErr()
        ws.send('terminal.close', cid, undefined)
      }
    }

    // WebSocket 断线自动重连后重建终端会话（首次连接用已挂载标志去重）
    const unsubReconn = ws.onReconnect(() => {
      if (mounted) {
        term.write(`\r\n\x1b[2m--- ${t('[连接已恢复]')} ---\x1b[0m\r\n`)
        mount()
      }
    })

    const el = containerRef.current
    if (el) {
      term.open(el)
    }

    void (async () => {
      try {
        await ws.connect()
      } catch {
        term.write(`\r\n\x1b[31m${t('[EZSSH] WebSocket 连接失败，请刷新重试')}\x1b[0m\r\n`)
        return
      }
      if (disposed) return
      mount()
    })()

    return () => {
      disposed = true
      if (cleanup) {
        try {
          cleanup()
        } catch {
          /* ignore */
        }
      }
      unsubReconn()
      term.dispose()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [windowId])

  // 窗口尺寸变化时触发 fit（通过 ResizeObserver）
  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const ro = new ResizeObserver(() => {
      const fit = fitRef.current
      const term = termRef.current
      if (fit && term) {
        try {
          fit.fit()
          ws.send('terminal.resize', channelIdRef.current, {
            cols: term.cols,
            rows: term.rows,
          })
        } catch {
          /* ignore */
        }
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  return (
    <div
      ref={containerRef}
      className="terminal-container"
      style={{ position: 'relative' }}
      onContextMenu={handleTermContextMenu}
      onMouseDownCapture={(e) => {
        // 阻止右键 mousedown 进入 xterm：避免其清除选区（选中复制后内容不应消失）
        if (e.button === 2) e.stopPropagation()
      }}
    >
      {toastMsg && (
        <div
          style={{
            position: 'absolute',
            top: 12,
            left: '50%',
            transform: 'translateX(-50%)',
            padding: '6px 14px',
            borderRadius: 6,
            background: 'rgba(30,41,59,0.94)',
            border: '1px solid rgba(148,163,184,0.35)',
            color: '#e2e8f0',
            fontSize: 12.5,
            zIndex: 10,
            pointerEvents: 'none',
            boxShadow: '0 8px 24px rgba(0,0,0,0.4)',
            whiteSpace: 'nowrap',
          }}
        >
          {toastMsg}
        </div>
      )}
    </div>
  )
}
