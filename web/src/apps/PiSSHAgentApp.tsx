import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { TerminalApp } from './TerminalApp'
import { setPendingTerminalCwd } from '../lib/terminalLaunch'
import type { AppProps } from '../desktop/appRegistry'
import { useT } from '../lib/i18n'

type Phase = 'loading' | 'needHost' | 'needInstall' | 'ready'

/**
 * pi-SSH-Agent：打开网关本机终端并自动执行 pissh 进入 AI 聊天。
 *
 * 打开流程：
 *  1. 找到内置网关主机（Local/Gateway），要求已配置 SSH 凭证；
 *  2. 在网关执行 `command -v pissh` 探测是否已安装；
 *  3. 已安装 → 注入命令 `pissh` 并渲染终端；未安装 → 展示一键安装命令。
 */
export function PiSSHAgentApp({ windowId, channelId, platform }: AppProps) {
  const t = useT()
  const [phase, setPhase] = useState<Phase>('loading')
  const [hostId, setHostId] = useState<string | null>(null)
  const [installCmd, setInstallCmd] = useState('')
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    let cancelled = false
    void (async () => {
      let hosts
      try {
        hosts = await api.listHosts()
      } catch {
        if (!cancelled) setPhase('needHost')
        return
      }
      const gw = hosts.find((h) => h.builtin || h.id === 'h_builtin_gateway')
      if (!gw || !gw.username) {
        if (!cancelled) setPhase('needHost')
        return
      }
      // 探测网关是否已安装 pissh
      let missing = true
      try {
        const r = await api.exec(gw.id, 'command -v pissh >/dev/null 2>&1 && echo OK || echo MISSING')
        missing = !r.output.includes('OK')
      } catch {
        if (!cancelled) setPhase('needHost')
        return
      }
      if (cancelled) return
      if (missing) {
        try {
          const { token } = await api.agentToken()
          setInstallCmd(
            `curl -fsSL https://raw.githubusercontent.com/pureages/EZSSH/main/scripts/install-pissh.sh | bash -s -- --token ${token}`,
          )
        } catch {
          /* ignore */
        }
        setPhase('needInstall')
        return
      }
      setHostId(gw.id)
      setPhase('ready')
    })()
    return () => {
      cancelled = true
    }
  }, [])

  // ready 后注入自动执行 pissh（终端 mount 时消费）
  useEffect(() => {
    if (phase === 'ready' && hostId) {
      setPendingTerminalCwd(hostId, '/', 'pissh')
    }
  }, [phase, hostId])

  if (phase === 'ready' && hostId) {
    return <TerminalApp windowId={windowId} hostId={hostId} channelId={channelId} platform={platform} />
  }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(installCmd)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      /* ignore */
    }
  }

  return (
    <div
      style={{
        padding: 28,
        height: '100%',
        overflow: 'auto',
        color: 'var(--text-1)',
        fontFamily: 'inherit',
      }}
    >
      <div style={{ fontSize: 18, fontWeight: 600, marginBottom: 14 }}>
        🤖 pi-SSH-Agent
      </div>

      {phase === 'loading' && <div style={{ color: 'var(--text-2)' }}>{t('正在检测…')}</div>}

      {phase === 'needHost' && (
        <div style={{ lineHeight: 1.9, color: 'var(--text-2)' }}>
          <div style={{ color: 'var(--text-1)', fontWeight: 600, marginBottom: 6 }}>
            {t('未配置网关本机的 SSH 凭证')}
          </div>
          {t('pi-SSH-Agent 需要在网关本机运行。请先在主机列表中配置网关本机（Local/Gateway）的 SSH 账号密码，然后重新打开本应用。')}
        </div>
      )}

      {phase === 'needInstall' && (
        <div style={{ lineHeight: 1.9 }}>
          <div style={{ color: 'var(--text-1)', fontWeight: 600, marginBottom: 6 }}>
            {t('网关未安装 pissh')}
          </div>
          <div style={{ color: 'var(--text-2)', marginBottom: 12 }}>
            {t('在网关服务器的终端中执行以下命令完成安装，完成后重新打开本应用：')}
          </div>
          <pre
            style={{
              background: 'rgba(2,6,23,0.6)',
              border: '1px solid rgba(var(--rgb-line), 0.18)',
              borderRadius: 8,
              padding: '12px 14px',
              fontSize: 12.5,
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-all',
              color: '#a5f3fc',
              userSelect: 'all',
            }}
          >
            {installCmd}
          </pre>
          <button
            onClick={copy}
            style={{
              marginTop: 10,
              padding: '7px 16px',
              borderRadius: 8,
              border: 'none',
              cursor: 'pointer',
              background: 'var(--primary)',
              color: '#fff',
              fontSize: 13,
            }}
          >
            {copied ? t('已复制') : t('复制安装命令')}
          </button>
        </div>
      )}
    </div>
  )
}
