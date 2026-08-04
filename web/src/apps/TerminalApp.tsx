import { useEffect, useRef } from 'react'
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
 * 终端 App：xterm.js + WebSocket 与后端 SSH shell 双向打通。
 */
export function TerminalApp({ windowId, hostId, channelId }: AppProps) {
  const t = useT()
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const channelIdRef = useRef(channelId || newChannelId())
  const hostIdRef = useRef(hostId)

  /** 终端右键（Linux 习惯）：有选区 → 复制选中文本并清空选区；无选区 → 粘贴剪贴板内容 */
  const handleTermContextMenu = (e: React.MouseEvent) => {
    e.preventDefault()
    const term = termRef.current
    if (!term) return
    if (term.hasSelection()) {
      const text = term.getSelection()
      navigator.clipboard.writeText(text).catch(() => copyFallback(text))
      term.clearSelection()
    } else {
      navigator.clipboard
        .readText()
        .then((text) => {
          const tm = termRef.current
          if (text && tm) tm.paste(text)
        })
        .catch(() => {
          /* 剪贴板不可用（非安全上下文等），静默 */
        })
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

    const open = async () => {
      try {
        await ws.connect()
      } catch {
        term.write(`\r\n\x1b[31m${t('[EZSSH] WebSocket 连接失败，请刷新重试')}\x1b[0m\r\n`)
        return
      }
      if (disposed) return

      // 订阅该 channel 的消息
      const unsub = ws.onChannel(cid, (msg) => {
        if (disposed) return
        switch (msg.type) {
          case 'terminal.output': {
            const raw = msg.payload?.data as string
            if (raw) term.write(b64decode(raw))
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

      // 若由文件管理器打开，自动 cd 到其所在目录，并可自动执行初始命令
      const init = consumePendingTerminalCwd(hostIdRef.current ?? '')
      if (init.cwd && init.cwd !== '/') {
        setTimeout(() => {
          if (disposed) return
          const cmd = `cd ${JSON.stringify(init.cwd)} && clear\n`
          ws.send('terminal.input', cid, {
            data: b64encode(new TextEncoder().encode(cmd)),
          })
        }, 500)
      }
      if (init.command) {
        setTimeout(() => {
          if (disposed) return
          ws.send('terminal.input', cid, {
            data: b64encode(new TextEncoder().encode(init.command + '\n')),
          })
        }, 900)
      }

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

      // 清理函数
      const cleanup = () => {
        clearInterval(ping)
        dataDisposable.dispose()
        resizeDisposable.dispose()
        unsub()
        unsubErr()
        ws.send('terminal.close', cid, undefined)
      }
      ;(term as unknown as { _ezsshCleanup?: () => void })._ezsshCleanup = cleanup
    }

    const el = containerRef.current
    if (el) {
      term.open(el)
    }

    void open()

    return () => {
      disposed = true
      const cleanup = (term as unknown as { _ezsshCleanup?: () => void })
        ._ezsshCleanup
      cleanup?.()
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

  return <div ref={containerRef} className="terminal-container" onContextMenu={handleTermContextMenu} />
}
