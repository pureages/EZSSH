import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ws } from '../lib/ws'
import { api } from '../lib/api'
import { useT, transErr } from '../lib/i18n'
import type { AppProps } from '../desktop/appRegistry'
import {
  buildMarketSpec,
  emptySpec,
  marketApps,
  marketDefaults,
  type ContainerSpec,
  type MarketApp,
} from './dockerMarket'

interface Container {
  id: string
  names: string
  image: string
  state: string
  status: string
  ports: string
  created: number
}

interface Image {
  ID: string
  Repository: string
  Tag: string
  Size: string
  CreatedAt: string
  Containers: string
}

interface ContainerStats {
  Container: string
  ID: string
  Name: string
  CPUPerc: string
  MemUsage: string
  MemPerc: string
  NetIO: string
  BlockIO: string
  PIDs: string
}

interface ContainerDetails {
  id: string
  name: string
  image: string
  state: string
  created: string
  ports: string[]
  env: string[]
  volumes: string[]
  network: string
  restart: string
  command: string
}

/** 详情展示行 */
function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div style={{ fontSize: 12, lineHeight: 1.8 }}>
      <span style={{ color: 'var(--text-1)', display: 'inline-block', width: 80, flexShrink: 0 }}>{label}</span>
      <span
        style={{
          color: 'var(--text-0)',
          fontFamily: mono ? 'Consolas, Menlo, monospace' : undefined,
          wordBreak: 'break-all',
        }}
      >
        {value}
      </span>
    </div>
  )
}

/** 参数详情区块：标题 + 等宽文本块（明文展示安装时的参数配置） */
function ParamSection({ title, children, error }: { title: string; children: string; error?: boolean }) {
  return (
    <div style={{ marginTop: 6 }}>
      <div style={{ fontSize: 11, color: 'var(--text-1)', marginBottom: 4 }}>{title}</div>
      <pre
        style={{
          fontFamily: 'Consolas, Menlo, monospace',
          fontSize: 11,
          lineHeight: 1.6,
          maxHeight: 280,
          overflow: 'auto',
          background: 'rgba(var(--rgb-appbg),0.5)',
          borderRadius: 6,
          padding: 8,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-all',
          color: error ? 'var(--red)' : 'var(--text-0)',
          margin: 0,
        }}
      >
        {children}
      </pre>
    </div>
  )
}

/** 根据容器详情重建 docker run 参数（明文），方便查看安装时的全部参数 */
function dockerRunText(d: ContainerDetails): string {
  const lines: string[] = []
  lines.push(`docker run -d --name ${d.name}`)
  if (d.restart && d.restart !== 'no') lines.push(`  --restart ${d.restart}`)
  if (d.network && d.network !== 'default' && d.network !== 'bridge') lines.push(`  --network ${d.network}`)
  for (const p of d.ports) lines.push(`  -p ${p}`)
  for (const e of d.env) lines.push(`  -e ${e}`)
  for (const v of d.volumes) lines.push(`  -v ${v}`)
  lines.push(`  ${d.image}`)
  if (d.command) lines.push(`  command: ${d.command}`)
  return lines.join('\n')
}

const reqSeq = { n: 0 }

/** 简单字符串列表编辑器（端口映射 / 环境变量 / 数据卷等） */
function StringListEditor({
  items,
  onChange,
  placeholder,
}: {
  items: string[]
  onChange: (v: string[]) => void
  placeholder?: string
}) {
  const t = useT()
  return (
    <div>
      {items.map((it, i) => (
        <div key={i} style={{ display: 'flex', gap: 6, marginBottom: 6 }}>
          <input
            style={{ flex: 1, minWidth: 0 }}
            value={it}
            placeholder={placeholder}
            onChange={(e) => {
              const next = [...items]
              next[i] = e.target.value
              onChange(next)
            }}
          />
          <button
            className="btn btn-sm btn-ghost"
            onClick={() => onChange(items.filter((_, j) => j !== i))}
            title={t('删除')}
          >
            ✕
          </button>
        </div>
      ))}
      <button className="btn btn-sm btn-ghost" onClick={() => onChange([...items, ''])}>
        ＋ {t('添加')}
      </button>
    </div>
  )
}

/** 应用市场卡片（已安装显示绿色勾） */
function MarketCard({ app, installed, onClick }: { app: MarketApp; installed: boolean; onClick: () => void }) {
  const t = useT()
  return (
    <button
      className="market-card"
      style={{
        position: 'relative',
        textAlign: 'left',
        padding: '14px 16px',
        borderRadius: 10,
        border: installed ? '1px solid rgba(34,197,94,0.45)' : '1px solid rgba(var(--rgb-line),0.2)',
        background: installed ? 'rgba(34,197,94,0.06)' : 'rgba(var(--rgb-panel),0.6)',
        cursor: 'pointer',
        color: 'var(--text-0)',
        display: 'flex',
        flexDirection: 'column',
        gap: 6,
      }}
      onClick={onClick}
    >
      {installed && (
        <span
          style={{
            position: 'absolute',
            top: 8,
            right: 8,
            width: 20,
            height: 20,
            borderRadius: '50%',
            background: 'var(--green)',
            color: '#fff',
            fontSize: 13,
            fontWeight: 700,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            boxShadow: '0 2px 8px rgba(34,197,94,0.5)',
          }}
        >
          ✓
        </span>
      )}
      <div style={{ fontSize: 30, lineHeight: 1 }}>{app.icon}</div>
      <div style={{ fontWeight: 700, fontSize: 13 }}>{t(app.name)}</div>
      <div style={{ color: 'var(--text-1)', fontSize: 11 }}>{t(app.tagline)}</div>
      <div style={{ color: 'var(--text-1)', fontSize: 10, wordBreak: 'break-all' }}>{app.image}</div>
    </button>
  )
}

/**
 * Docker 管理器 App：容器/镜像列表、Docker 一键安装、应用市场、自定义安装。
 */
export function DockerApp({ hostId, platform }: AppProps) {
  const t = useT()
  const [tab, setTab] = useState<'containers' | 'images' | 'market'>('containers')
  const [containers, setContainers] = useState<Container[]>([])
  const [images, setImages] = useState<Image[]>([])
  const [stats, setStats] = useState<Record<string, ContainerStats>>({})
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [logsFor, setLogsFor] = useState<Container | null>(null)
  const [logs, setLogs] = useState('')
  const hostIdRef = useRef(hostId)

  // Docker 安装状态
  const [dockerStatus, setDockerStatus] = useState({ installed: false, version: '', checking: true })
  const [installing, setInstalling] = useState(false)
  const [installOutput, setInstallOutput] = useState('')
  const [installError, setInstallError] = useState('')

  // 应用市场
  const [marketApp, setMarketApp] = useState<MarketApp | null>(null)
  const [marketValues, setMarketValues] = useState<Record<string, string>>({})
  const [marketTab, setMarketTab] = useState<'all' | 'installed' | 'uninstalled'>('all')

  // 已安装应用的详情弹窗（容器配置 + 安装参数 + 卸载）
  const [installedDetail, setInstalledDetail] = useState<{
    app: MarketApp
    container: Container
    details: ContainerDetails | null
    loading: boolean
    error: string
    /** 安装时写入目标机的参数配置文件内容（明文，如 frps.toml） */
    configFile: string
    /** 配置文件读取失败原因（非致命，界面降级显示） */
    configError: string
  } | null>(null)
  // 已安装详情弹窗内的选项卡：配置信息 / 参数详情
  const [detailTab, setDetailTab] = useState<'info' | 'params'>('info')
  const [uninstalling, setUninstalling] = useState(false)

  // 自定义安装
  const [customOpen, setCustomOpen] = useState(false)
  const [custom, setCustom] = useState<ContainerSpec>(emptySpec())
  const [customMsg, setCustomMsg] = useState('')

  // 服务器 IP（用于端口点击跳转）
  const [serverHost, setServerHost] = useState('')

  // 安装进度终端窗口
  const [progress, setProgress] = useState<{
    title: string
    lines: string
    running: boolean
    ok: boolean | null
    message: string
    containerId: string
  } | null>(null)
  const progressPreRef = useRef<HTMLPreElement | null>(null)

  const sendReq = useCallback(
    (type: string, payload: Record<string, unknown>, timeout = 30000): Promise<any> => {
      return new Promise((resolve, reject) => {
        const id = `req_${++reqSeq.n}`
        ws.send(type, id, { ...payload, hostId: hostIdRef.current })
        const unsub = ws.onChannel(id, (msg) => {
          unsub()
          if (msg.type === 'error') {
            reject(new Error((msg.payload?.message as string) || t('操作失败')))
          } else {
            resolve(msg.payload)
          }
        })
        setTimeout(() => {
          unsub()
          reject(new Error(t('请求超时')))
        }, timeout)
      })
    },
    [],
  )

  const refresh = useCallback(async () => {
    if (!hostId) return
    setLoading(true)
    setError('')
    try {
      const c = await sendReq('docker.list', {})
      setContainers((c.containers as Container[]) || [])
      const s = await sendReq('docker.stats', {})
      const statMap: Record<string, ContainerStats> = {}
      for (const st of (s.stats as ContainerStats[]) || []) {
        statMap[st.ID] = st
      }
      setStats(statMap)
      if (tab === 'images') {
        const im = await sendReq('docker.images', {})
        setImages((im.images as Image[]) || [])
      }
    } catch (e) {
      setError(transErr(e, '加载失败'))
    } finally {
      setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hostId, tab, sendReq])

  // 检测 Docker 是否已安装；已安装则拉取列表
  const checkDocker = useCallback(async () => {
    setDockerStatus((s) => ({ ...s, checking: true }))
    try {
      const r = await sendReq('docker.check', {})
      const installed = Boolean((r as any).installed)
      setDockerStatus({ installed, version: String((r as any).version || ''), checking: false })
      if (installed) void refresh()
    } catch (e) {
      setDockerStatus({ installed: false, version: '', checking: false })
      setError(transErr(e, '检测 Docker 失败'))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sendReq, refresh])

  useEffect(() => {
    hostIdRef.current = hostId
    void ws.connect().catch(() => {})
    if (hostId) {
      void checkDocker()
      // 获取服务器地址用于端口跳转
      api
        .listHosts()
        .then((list) => {
          const h = list.find((x) => x.id === hostId)
          if (h) setServerHost(h.host)
        })
        .catch(() => {})
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hostId])

  // 安装进度终端自动滚动到底部
  useEffect(() => {
    const el = progressPreRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [progress?.lines])

  useEffect(() => {
    if (hostId && dockerStatus.installed && tab === 'images') {
      sendReq('docker.images', {})
        .then((im) => setImages((im.images as Image[]) || []))
        .catch(() => {})
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab, dockerStatus.installed])

  const action = async (act: string, id: string) => {
    if (!window.confirm(t('确认执行：docker {0} {1}？', act, id.slice(0, 12)))) return
    try {
      await sendReq('docker.action', { action: act, id })
      void refresh()
    } catch (e) {
      alert(transErr(e, '操作失败'))
    }
  }

  const showLogs = async (c: Container) => {
    setLogsFor(c)
    setLogs(t('加载中…'))
    try {
      const r = await sendReq('docker.logs', { id: c.id, tail: 200 })
      setLogs((r.logs as string) || t('(空)'))
    } catch (e) {
      setLogs(transErr(e, '获取日志失败'))
    }
  }

  // 一键安装 Docker（流式输出）
  const installDocker = async () => {
    if (!window.confirm(t('将下载并执行 Docker 官方安装脚本（get.docker.com），确认开始安装？'))) return
    setInstalling(true)
    setInstallOutput('')
    setInstallError('')
    try {
      await new Promise<void>((resolve, reject) => {
        const channelId = `di_${Date.now().toString(36)}_${++reqSeq.n}`
        let unsub: () => void = () => {}
        const timer = setTimeout(() => {
          unsub()
          reject(new Error(t('安装超时请稍后在终端中查看安装状态')))
        }, 600000)
        unsub = ws.onChannel(channelId, (msg) => {
          if (msg.type === 'docker.install.output') {
            setInstallOutput((o) => o + String((msg.payload as any)?.line || '') + '\n')
          } else if (msg.type === 'docker.install.done') {
            clearTimeout(timer)
            unsub()
            const p = msg.payload as any
            if (p?.ok) resolve()
            else reject(new Error(p?.message || t('安装失败')))
          }
        })
        ws.send('docker.install', channelId, { hostId: hostIdRef.current })
      })
      await checkDocker()
      setInstalling(false)
    } catch (e) {
      setInstallError(transErr(e, '安装失败'))
      setInstalling(false)
    }
  }

  // 已安装的应用（按容器名/镜像匹配）
  const installedAppIds = useMemo(() => {
    const s = new Set<string>()
    for (const c of containers) {
      const names = (c.names || '').split(',').map((n) => n.trim())
      for (const a of marketApps) {
        if (s.has(a.id)) continue
        if (names.includes(a.id)) s.add(a.id)
        else if (c.image === a.image || c.image.startsWith(a.image.split(':')[0] + ':')) s.add(a.id)
      }
    }
    return s
  }, [containers])

  /** 以终端式进度窗口执行安装，流式显示 docker 输出 */
  const runInstall = (title: string, spec: ContainerSpec): Promise<{ ok: boolean; containerId?: string; message?: string }> => {
    return new Promise((resolve) => {
      const channelId = `dc_${Date.now().toString(36)}_${++reqSeq.n}`
      let out = ''
      setProgress({ title, lines: '', running: true, ok: null, message: '', containerId: '' })
      let unsub: () => void = () => {}
      const timer = setTimeout(() => {
        unsub()
        setProgress((p) => (p ? { ...p, running: false, ok: false, message: t('安装超时请稍后查看容器状态') } : p))
        resolve({ ok: false, message: t('安装超时请稍后查看容器状态') })
      }, 600000)
      unsub = ws.onChannel(channelId, (msg) => {
        if (msg.type === 'docker.create.output') {
          const line = String((msg.payload as any)?.line || '')
          out += line + '\n'
          setProgress((p) => (p ? { ...p, lines: out } : p))
        } else if (msg.type === 'docker.create.done') {
          clearTimeout(timer)
          unsub()
          const pl = msg.payload as any
          if (pl?.ok) {
            setProgress((p) => (p ? { ...p, running: false, ok: true, containerId: String(pl.containerId || '') } : p))
            resolve({ ok: true, containerId: String(pl.containerId || '') })
          } else {
            setProgress((p) => (p ? { ...p, running: false, ok: false, message: pl?.message || t('安装失败') } : p))
            resolve({ ok: false, message: pl?.message || t('安装失败') })
          }
        }
      })
      ws.send('docker.create.stream', channelId, { ...spec, hostId: hostIdRef.current })
    })
  }

  // 从应用市场安装：立即关闭配置界面，弹出终端式安装进度窗口
  const installMarket = async () => {
    if (!marketApp) return
    const app = marketApp
    const spec = buildMarketSpec(app, marketValues)
    setMarketApp(null)
    const r = await runInstall(t('正在安装 {0}…', app.name), spec)
    if (r.ok) {
      setTab('containers')
      void refresh()
    }
  }

  // 自定义安装：同样弹出终端式安装进度窗口
  const installCustom = async () => {
    if (!custom.image.trim()) {
      setCustomMsg(t('请填写镜像名称'))
      return
    }
    setCustomMsg('')
    setCustomOpen(false)
    const r = await runInstall(t('正在安装容器 {0}…', custom.name || custom.image), custom)
    if (r.ok) {
      setTab('containers')
      void refresh()
    }
  }

  // 打开已安装应用的详情（容器配置信息 + 安装时的参数详情）
  const openInstalledApp = (app: MarketApp) => {
    const c = containers.find((x) =>
      (x.names || '')
        .split(',')
        .map((n) => n.trim())
        .includes(app.id),
    )
    if (!c) return
    setDetailTab('info')
    setInstalledDetail({ app, container: c, details: null, loading: true, error: '', configFile: '', configError: '' })
    sendReq('docker.inspect', { id: c.id })
      .then((r) => {
        setInstalledDetail((d) =>
          d ? { ...d, details: (r as any).container as ContainerDetails, loading: false } : d,
        )
      })
      .catch((e) => {
        setInstalledDetail((d) =>
          d ? { ...d, loading: false, error: transErr(e, '获取容器详情失败') } : d,
        )
      })
    // 读取安装时写入目标机的参数配置文件（如 frps/frpc 的 toml），在「参数详情」明文展示
    if (app.configPath && hostIdRef.current) {
      const hostPath = `/opt/${app.id}/${app.configPath.split('/').pop()}`
      api
        .sftpRead(hostIdRef.current, hostPath)
        .then((r) => setInstalledDetail((d) => (d ? { ...d, configFile: r.content } : d)))
        .catch((e) =>
          setInstalledDetail((d) => (d ? { ...d, configError: transErr(e, '读取配置文件失败') } : d)),
        )
    }
  }

  // 卸载已安装的应用（删除容器）
  const uninstallApp = async () => {
    if (!installedDetail) return
    const c = installedDetail.container
    if (!window.confirm(t('确认卸载应用「{0}」？将删除容器 {1}', installedDetail.app.name, c.names || c.id.slice(0, 12)))) return
    setUninstalling(true)
    try {
      await sendReq('docker.action', { action: 'rm', id: c.id })
      setInstalledDetail(null)
      void refresh()
    } catch (e) {
      setInstalledDetail((d) => (d ? { ...d, error: transErr(e, '卸载失败') } : d))
    } finally {
      setUninstalling(false)
    }
  }

  const shortID = (id: string) => id.slice(0, 12)
  const installed = dockerStatus.installed
  const installedApps = marketApps.filter((a) => installedAppIds.has(a.id))
  const uninstalledApps = marketApps.filter((a) => !installedAppIds.has(a.id))

  // Windows 主机不支持 Docker（入口已在右键菜单置灰，这里兜底拦截）
  if (platform === 'windows') {
    return (
      <div
        style={{
          height: '100%',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          gap: 10,
          background: 'rgba(var(--rgb-appbg),0.85)',
          fontSize: 13,
          color: 'var(--text-1)',
        }}
      >
        <div style={{ fontSize: 40 }}>🐳</div>
        <div>{t('该主机不支持 Docker')}</div>
      </div>
    )
  }

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', background: 'rgba(var(--rgb-appbg),0.85)', fontSize: 12 }}>
      {/* 页签 */}
      <div style={{ display: 'flex', gap: 8, padding: '8px 10px', borderBottom: '1px solid rgba(var(--rgb-line),0.15)', flexWrap: 'wrap' }}>
        <button
          className={`btn btn-sm ${tab === 'containers' ? '' : 'btn-ghost'}`}
          onClick={() => setTab('containers')}
        >
          {t('容器')}
        </button>
        <button
          className={`btn btn-sm ${tab === 'images' ? '' : 'btn-ghost'}`}
          onClick={() => setTab('images')}
        >
          {t('镜像')}
        </button>
        <button
          className={`btn btn-sm ${tab === 'market' ? '' : 'btn-ghost'}`}
          onClick={() => setTab('market')}
        >
          {t('应用市场')}
        </button>
        <div style={{ flex: 1 }} />
        <button className="btn btn-sm btn-ghost" onClick={() => setCustomOpen(true)}>
          {t('自定义安装')}
        </button>
        <button className="btn btn-sm btn-ghost" onClick={refresh} disabled={!installed}>
          {t('刷新')}
        </button>
      </div>

      {error && <div style={{ padding: '8px 10px', color: 'var(--red)' }}>{error}</div>}

      {/* Docker 未安装提示 / 安装进度 */}
      {!installed && !dockerStatus.checking && (
        <div
          style={{
            margin: 10,
            padding: '12px 14px',
            borderRadius: 8,
            border: '1px solid rgba(245,158,11,0.35)',
            background: 'rgba(245,158,11,0.08)',
          }}
        >
          <div style={{ color: 'var(--yellow)', fontWeight: 600, marginBottom: 6 }}>
            {t('目标服务器尚未安装 Docker')}
          </div>
          {installing ? (
            <>
              <div style={{ color: 'var(--text-1)', marginBottom: 6 }}>{t('正在执行安装脚本请耐心等待')}</div>
              <pre
                style={{
                  margin: 0,
                  maxHeight: 180,
                  overflow: 'auto',
                  padding: 8,
                  borderRadius: 6,
                  background: 'rgba(var(--rgb-appbg),0.8)',
                  color: 'var(--text-0)',
                  fontSize: 11,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-all',
                  fontFamily: 'Consolas, Menlo, monospace',
                }}
              >
                {installOutput || t('准备中…')}
              </pre>
            </>
          ) : (
            <button className="btn btn-sm" onClick={installDocker}>
              {t('一键安装 Docker')}
            </button>
          )}
          {installError && (
            <div style={{ color: 'var(--red)', marginTop: 6, wordBreak: 'break-all' }}>{installError}</div>
          )}
        </div>
      )}
      {dockerStatus.checking && (
        <div style={{ padding: '8px 12px', color: 'var(--text-1)' }}>{t('检测 Docker 状态…')}</div>
      )}

      <div style={{ flex: 1, overflow: 'auto' }}>
        {tab === 'containers' &&
          (installed ? (
            <>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead style={{ position: 'sticky', top: 0, background: 'rgba(var(--rgb-panel),0.95)' }}>
                  <tr style={{ textAlign: 'left', color: 'var(--text-1)', fontSize: 11 }}>
                    <th style={{ padding: '8px 10px' }}>{t('容器')}</th>
                    <th style={{ padding: '8px 10px' }}>{t('镜像')}</th>
                    <th style={{ padding: '8px 10px' }}>{t('状态')}</th>
                    <th style={{ padding: '8px 10px' }}>{t('端口')}</th>
                    <th style={{ padding: '8px 10px' }}>CPU</th>
                    <th style={{ padding: '8px 10px' }}>{t('内存')}</th>
                    <th style={{ padding: '8px 10px' }}>{t('操作')}</th>
                  </tr>
                </thead>
                <tbody>
                  {containers.map((c) => {
                    const st = stats[c.id]
                    return (
                      <tr key={c.id} style={{ borderBottom: '1px solid rgba(var(--rgb-line),0.06)' }}>
                        <td style={{ padding: '6px 10px' }}>
                          <span
                            style={{
                              display: 'inline-block',
                              width: 8,
                              height: 8,
                              borderRadius: '50%',
                              marginRight: 6,
                              background:
                                c.state === 'running'
                                  ? 'var(--green)'
                                  : c.state === 'exited'
                                    ? 'var(--text-1)'
                                    : 'var(--yellow)',
                            }}
                          />
                          {c.names || shortID(c.id)}
                          <div style={{ color: 'var(--text-1)', fontSize: 11 }}>{shortID(c.id)}</div>
                        </td>
                        <td style={{ padding: '6px 10px', color: 'var(--text-1)' }}>{c.image}</td>
                        <td style={{ padding: '6px 10px' }}>{c.status}</td>
                        <td style={{ padding: '6px 10px' }}>
                          {c.ports
                            ? c.ports.split(',').map((part, i) => {
                                const p = part.trim()
                                const hostPort = p.split('->')[0].trim().match(/:(\d+)$/)?.[1]
                                return (
                                  <span key={i}>
                                    {hostPort && serverHost ? (
                                      <a
                                        className="port-link"
                                        href={`http://${serverHost}:${hostPort}`}
                                        target="_blank"
                                        rel="noreferrer"
                                        title={t('在浏览器打开：http://{0}:{1}', serverHost, hostPort)}
                                      >
                                        {p}
                                      </a>
                                    ) : (
                                      <span style={{ color: 'var(--text-1)' }}>{p}</span>
                                    )}
                                    {i < c.ports.split(',').length - 1 ? ', ' : ''}
                                  </span>
                                )
                              })
                            : '-'}
                        </td>
                        <td style={{ padding: '6px 10px' }}>{st ? st.CPUPerc : '-'}</td>
                        <td style={{ padding: '6px 10px' }}>
                          {st ? `${st.MemUsage} (${st.MemPerc})` : '-'}
                        </td>
                        <td style={{ padding: '6px 10px', whiteSpace: 'nowrap' }}>
                          {c.state === 'running' ? (
                            <button className="btn btn-sm btn-ghost" onClick={() => action('stop', c.id)}>
                              {t('停止')}
                            </button>
                          ) : (
                            <button className="btn btn-sm btn-ghost" onClick={() => action('start', c.id)}>
                              {t('启动')}
                            </button>
                          )}
                          <button className="btn btn-sm btn-ghost" style={{ marginLeft: 4 }} onClick={() => action('restart', c.id)}>
                            {t('重启')}
                          </button>
                          <button className="btn btn-sm btn-ghost" style={{ marginLeft: 4 }} onClick={() => showLogs(c)}>
                            {t('日志')}
                          </button>
                          <button className="btn btn-sm btn-danger" style={{ marginLeft: 4 }} onClick={() => action('rm', c.id)}>
                            {t('删除')}
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
              {!loading && containers.length === 0 && (
                <div style={{ padding: 30, textAlign: 'center', color: 'var(--text-1)' }}>
                  {t('暂无容器，可通过「应用市场」或「自定义安装」创建')}
                </div>
              )}
            </>
          ) : (
            !dockerStatus.checking && (
              <div style={{ padding: 30, textAlign: 'center', color: 'var(--text-1)' }}>
                {t('请先在上方「一键安装 Docker」安装 Docker')}
              </div>
            )
          ))}

        {tab === 'images' &&
          (installed ? (
            <>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead style={{ position: 'sticky', top: 0, background: 'rgba(var(--rgb-panel),0.95)' }}>
                  <tr style={{ textAlign: 'left', color: 'var(--text-1)', fontSize: 11 }}>
                    <th style={{ padding: '8px 10px' }}>{t('镜像')}</th>
                    <th style={{ padding: '8px 10px' }}>ID</th>
                    <th style={{ padding: '8px 10px' }}>{t('大小')}</th>
                    <th style={{ padding: '8px 10px' }}>{t('操作')}</th>
                  </tr>
                </thead>
                <tbody>
                  {images.map((im) => (
                    <tr key={im.ID} style={{ borderBottom: '1px solid rgba(var(--rgb-line),0.06)' }}>
                      <td style={{ padding: '6px 10px' }}>
                        {im.Repository ? `${im.Repository}:${im.Tag}` : '<none>'}
                      </td>
                      <td style={{ padding: '6px 10px', color: 'var(--text-1)', fontFamily: 'Consolas, monospace' }}>
                        {shortID(im.ID)}
                      </td>
                      <td style={{ padding: '6px 10px', color: 'var(--text-1)' }}>{im.Size}</td>
                      <td style={{ padding: '6px 10px' }}>
                        <button className="btn btn-sm btn-danger" onClick={() => action('rmi', im.ID)}>
                          {t('删除')}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!loading && images.length === 0 && (
                <div style={{ padding: 30, textAlign: 'center', color: 'var(--text-1)' }}>{t('暂无镜像')}</div>
              )}
            </>
          ) : (
            !dockerStatus.checking && (
              <div style={{ padding: 30, textAlign: 'center', color: 'var(--text-1)' }}>
                {t('请先在上方「一键安装 Docker」安装 Docker')}
              </div>
            )
          ))}

        {tab === 'market' && (
          <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
            {/* 子标签：全部应用 / 已安装 / 未安装 */}
            <div
              style={{
                display: 'flex',
                gap: 8,
                padding: '8px 10px',
                borderBottom: '1px solid rgba(var(--rgb-line),0.15)',
                flexShrink: 0,
              }}
            >
              <button
                className={`btn btn-sm ${marketTab === 'all' ? '' : 'btn-ghost'}`}
                onClick={() => setMarketTab('all')}
              >
                {t('全部应用（{0}）', marketApps.length)}
              </button>
              <button
                className={`btn btn-sm ${marketTab === 'installed' ? '' : 'btn-ghost'}`}
                onClick={() => setMarketTab('installed')}
              >
                {t('已安装（{0}）', installedApps.length)}
              </button>
              <button
                className={`btn btn-sm ${marketTab === 'uninstalled' ? '' : 'btn-ghost'}`}
                onClick={() => setMarketTab('uninstalled')}
              >
                {t('未安装（{0}）', uninstalledApps.length)}
              </button>
            </div>

            <div style={{ padding: 10, overflow: 'auto', flex: 1 }}>
              {marketTab === 'installed' ? (
                installedApps.length > 0 ? (
                  <div
                    style={{
                      display: 'grid',
                      gridTemplateColumns: 'repeat(auto-fill, minmax(170px, 1fr))',
                      gap: 10,
                    }}
                  >
                    {installedApps.map((app) => (
                      <MarketCard key={app.id} app={app} installed onClick={() => openInstalledApp(app)} />
                    ))}
                  </div>
                ) : (
                  <div style={{ padding: 30, textAlign: 'center', color: 'var(--text-1)' }}>{t('暂无已安装的应用')}</div>
                )
              ) : marketTab === 'uninstalled' ? (
                uninstalledApps.length > 0 ? (
                  <div
                    style={{
                      display: 'grid',
                      gridTemplateColumns: 'repeat(auto-fill, minmax(170px, 1fr))',
                      gap: 10,
                    }}
                  >
                    {uninstalledApps.map((app) => (
                      <MarketCard
                        key={app.id}
                        app={app}
                        installed={false}
                        onClick={() => {
                          setMarketApp(app)
                          setMarketValues(marketDefaults(app))
                        }}
                      />
                    ))}
                  </div>
                ) : (
                  <div style={{ padding: 30, textAlign: 'center', color: 'var(--green)' }}>
                    {t('🎉 所有应用均已安装')}
                  </div>
                )
              ) : marketApps.length > 0 ? (
                <div
                  style={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(auto-fill, minmax(170px, 1fr))',
                    gap: 10,
                  }}
                >
                  {marketApps.map((app) => {
                    const isInstalled = installedAppIds.has(app.id)
                    return (
                      <MarketCard
                        key={app.id}
                        app={app}
                        installed={isInstalled}
                        onClick={() => {
                          // 已安装：打开详情（含参数详情）；未安装：进入安装界面
                          if (isInstalled) {
                            openInstalledApp(app)
                          } else {
                            setMarketApp(app)
                            setMarketValues(marketDefaults(app))
                          }
                        }}
                      />
                    )
                  })}
                </div>
              ) : (
                <div style={{ padding: 30, textAlign: 'center', color: 'var(--text-1)' }}>{t('暂无应用')}</div>
              )}
            </div>
          </div>
        )}
      </div>

      {/* 应用市场详情弹窗 */}
      {marketApp && (
        <div className="modal-mask" style={{ zIndex: 1000 }} onClick={() => setMarketApp(null)}>
          <div className="modal" style={{ maxHeight: '85%', overflow: 'auto' }} onClick={(e) => e.stopPropagation()}>
            <h3>
              {marketApp.icon} {t(marketApp.name)}
            </h3>
            <div style={{ color: 'var(--text-1)', fontSize: 12, marginBottom: 10, lineHeight: 1.7 }}>
              {t(marketApp.description)}
            </div>
            <div style={{ fontSize: 11, color: 'var(--text-1)', marginBottom: 12 }}>
              {t('镜像')}：<code>{marketApp.image}</code>
              {marketApp.network === 'host' && t('网络模式：{0}', marketApp.network)}
            </div>

            {marketApp.fields.map((f) => (
              <div className="field" key={f.key}>
                <label>{t(f.label)}</label>
                {f.type === 'select' ? (
                  <select
                    value={marketValues[f.key] ?? f.default}
                    onChange={(e) => setMarketValues({ ...marketValues, [f.key]: e.target.value })}
                  >
                    {(f.options || []).map((o) => (
                      <option key={o} value={o}>
                        {o}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    type={f.type === 'password' ? 'password' : f.type === 'number' ? 'number' : 'text'}
                    value={marketValues[f.key] ?? f.default}
                    placeholder={f.placeholder ? t(f.placeholder) : undefined}
                    onChange={(e) => setMarketValues({ ...marketValues, [f.key]: e.target.value })}
                  />
                )}
                {f.help && <div style={{ fontSize: 11, color: 'var(--text-1)', marginTop: 3 }}>{t(f.help)}</div>}
              </div>
            ))}

            {!installed && (
              <div style={{ marginBottom: 10, color: 'var(--yellow)', fontSize: 12 }}>
                {t('目标服务器尚未安装 Docker，请先返回容器页一键安装 Docker。')}
              </div>
            )}

            <div className="footer">
              <button className="btn btn-ghost" onClick={() => setMarketApp(null)}>
                {t('取消')}
              </button>
              <button className="btn" onClick={installMarket} disabled={!installed}>
                {t('一键安装')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 已安装应用详情弹窗（容器配置 + 卸载） */}
      {installedDetail && (
        <div className="modal-mask" style={{ zIndex: 1000 }} onClick={() => setInstalledDetail(null)}>
          <div className="modal" style={{ maxHeight: '85%', overflow: 'auto' }} onClick={(e) => e.stopPropagation()}>
            <h3>
              {installedDetail.app.icon} {t(installedDetail.app.name)}
              <span style={{ fontSize: 12, color: 'var(--green)', fontWeight: 600, marginLeft: 8 }}>{t('✅ 已安装')}</span>
            </h3>

            {installedDetail.loading ? (
              <div style={{ color: 'var(--text-1)', padding: '24px 0', textAlign: 'center' }}>
                {t('正在获取容器配置…')}
              </div>
            ) : installedDetail.details ? (
              <>
                {/* 配置信息 / 参数详情 选项卡 */}
                <div style={{ display: 'flex', gap: 8, marginBottom: 10 }}>
                  <button
                    className={`btn btn-sm ${detailTab === 'info' ? '' : 'btn-ghost'}`}
                    onClick={() => setDetailTab('info')}
                  >
                    {t('配置信息')}
                  </button>
                  <button
                    className={`btn btn-sm ${detailTab === 'params' ? '' : 'btn-ghost'}`}
                    onClick={() => setDetailTab('params')}
                  >
                    {t('参数详情')}
                  </button>
                </div>
                {detailTab === 'params' ? (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 4, marginBottom: 8 }}>
                    <ParamSection title={t('Docker 运行参数')}>
                      {dockerRunText(installedDetail.details)}
                    </ParamSection>

                    {installedDetail.configError ? (
                      <ParamSection title={t('参数配置文件')} error>
                        {installedDetail.configError}
                      </ParamSection>
                    ) : installedDetail.configFile ? (
                      <ParamSection title={t('参数配置文件（安装时写入目标机 {0}）', '/opt/' + installedDetail.app.id + '/' + (installedDetail.app.configPath || '').split('/').pop())}>
                        {installedDetail.configFile}
                      </ParamSection>
                    ) : installedDetail.app.configPath ? (
                      <div style={{ fontSize: 11, color: 'var(--text-1)' }}>{t('正在读取配置文件…')}</div>
                    ) : null}
                  </div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 4, marginBottom: 8 }}>
                <InfoRow label={t('容器名')} value={installedDetail.details.name} />
                <InfoRow label={t('镜像')} value={installedDetail.details.image} />
                <InfoRow label={t('状态')} value={installedDetail.details.state} />
                <InfoRow label={t('创建时间')} value={installedDetail.details.created} />
                <InfoRow label={t('网络模式')} value={installedDetail.details.network} />
                <InfoRow label={t('重启策略')} value={installedDetail.details.restart || 'no'} />
                {installedDetail.details.command && (
                  <InfoRow label={t('启动命令')} value={installedDetail.details.command} mono />
                )}

                {installedDetail.details.ports && installedDetail.details.ports.length > 0 && (
                  <div style={{ marginTop: 6 }}>
                    <div style={{ fontSize: 11, color: 'var(--text-1)', marginBottom: 4 }}>{t('端口映射')}</div>
                    <div style={{ fontFamily: 'Consolas, Menlo, monospace', fontSize: 12, lineHeight: 1.8 }}>
                      {installedDetail.details.ports.map((p, i) => {
                        const hostPort = p.split('->')[0].split(':').pop()
                        return (
                          <div key={i}>
                            {hostPort && serverHost ? (
                              <a
                                className="port-link"
                                href={`http://${serverHost}:${hostPort}`}
                                target="_blank"
                                rel="noreferrer"
                                title={t('在浏览器打开：http://{0}:{1}', serverHost, hostPort)}
                              >
                                {p}
                              </a>
                            ) : (
                              p
                            )}
                          </div>
                        )
                      })}
                    </div>
                  </div>
                )}

                {installedDetail.details.env && installedDetail.details.env.length > 0 && (
                  <div style={{ marginTop: 6 }}>
                    <div style={{ fontSize: 11, color: 'var(--text-1)', marginBottom: 4 }}>
                      {t('环境变量（{0}）', installedDetail.details.env.length)}
                    </div>
                    <div
                      style={{
                        fontFamily: 'Consolas, Menlo, monospace',
                        fontSize: 11,
                        lineHeight: 1.6,
                        maxHeight: 160,
                        overflow: 'auto',
                        background: 'rgba(var(--rgb-appbg),0.5)',
                        borderRadius: 6,
                        padding: 8,
                      }}
                    >
                      {installedDetail.details.env.map((e, i) => (
                        <div key={i} style={{ wordBreak: 'break-all' }}>
                          {e}
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {installedDetail.details.volumes && installedDetail.details.volumes.length > 0 && (
                  <div style={{ marginTop: 6 }}>
                    <div style={{ fontSize: 11, color: 'var(--text-1)', marginBottom: 4 }}>{t('数据卷')}</div>
                    <div style={{ fontFamily: 'Consolas, Menlo, monospace', fontSize: 11, lineHeight: 1.6 }}>
                      {installedDetail.details.volumes.map((v, i) => (
                        <div key={i} style={{ wordBreak: 'break-all' }}>
                          {v}
                        </div>
                      ))}
                    </div>
                  </div>
                )}
                  </div>
                )}
                </>
            ) : (
              <div style={{ color: 'var(--red)', padding: '12px 0' }}>
                {installedDetail.error || t('获取容器配置失败')}
              </div>
            )}

            {installedDetail.error && installedDetail.details && (
              <div style={{ color: 'var(--red)', marginTop: 4 }}>{installedDetail.error}</div>
            )}

            <div className="footer">
              <button className="btn btn-ghost" onClick={() => setInstalledDetail(null)} disabled={uninstalling}>
                {t('关闭')}
              </button>
              <button className="btn btn-danger" onClick={uninstallApp} disabled={uninstalling}>
                {uninstalling ? t('卸载中…') : `🗑 ${t('卸载')}`}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 自定义安装弹窗 */}
      {customOpen && (
        <div className="modal-mask" style={{ zIndex: 1000 }} onClick={() => setCustomOpen(false)}>
          <div className="modal" style={{ maxHeight: '85%', overflow: 'auto' }} onClick={(e) => e.stopPropagation()}>
            <h3>{t('自定义安装容器')}</h3>

            <div className="field">
              <label>{t('容器名称（可选）')}</label>
              <input
                value={custom.name}
                placeholder={t('如：myapp')}
                onChange={(e) => setCustom({ ...custom, name: e.target.value })}
              />
            </div>

            <div className="field">
              <label>{t('镜像（必填）')}</label>
              <input
                value={custom.image}
                placeholder={t('如：nginx:latest')}
                onChange={(e) => setCustom({ ...custom, image: e.target.value })}
                autoFocus
              />
            </div>

            <div className="field">
              <label>{t('端口映射（宿主机端口:容器端口）')}</label>
              <StringListEditor
                items={custom.ports}
                onChange={(ports) => setCustom({ ...custom, ports })}
                placeholder="8080:80"
              />
            </div>

            <div className="field">
              <label>{t('环境变量（KEY=VALUE）')}</label>
              <StringListEditor
                items={custom.env}
                onChange={(env) => setCustom({ ...custom, env })}
                placeholder="MYSQL_ROOT_PASSWORD=123456"
              />
            </div>

            <div className="field">
              <label>{t('数据卷（宿主机路径:容器路径）')}</label>
              <StringListEditor
                items={custom.volumes}
                onChange={(volumes) => setCustom({ ...custom, volumes })}
                placeholder="/data:/data"
              />
            </div>

            <div className="field">
              <label>{t('网络模式')}</label>
              <select value={custom.network} onChange={(e) => setCustom({ ...custom, network: e.target.value })}>
                <option value="bridge">{t('bridge（桥接，默认）')}</option>
                <option value="host">{t('host（宿主机网络）')}</option>
                <option value="none">none</option>
              </select>
            </div>

            <div className="field">
              <label>{t('重启策略')}</label>
              <select value={custom.restart} onChange={(e) => setCustom({ ...custom, restart: e.target.value })}>
                <option value="always">{t('always（总是重启）')}</option>
                <option value="unless-stopped">unless-stopped</option>
                <option value="on-failure">on-failure</option>
                <option value="no">{t('no（不自动重启）')}</option>
              </select>
            </div>

            <div className="field">
              <label style={{ display: 'flex', alignItems: 'center', gap: 6, cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={custom.privileged}
                  onChange={(e) => setCustom({ ...custom, privileged: e.target.checked })}
                  style={{ width: 'auto' }}
                />
                {t('特权模式')}
              </label>
            </div>

            <div className="field">
              <label>{t('额外参数（docker run 参数，如 --cap-add=NET_ADMIN）')}</label>
              <StringListEditor
                items={custom.extraArgs}
                onChange={(extraArgs) => setCustom({ ...custom, extraArgs })}
                placeholder="--cap-add=NET_ADMIN"
              />
            </div>

            {customMsg && (
              <div style={{ margin: '8px 0', color: 'var(--red)', wordBreak: 'break-all' }}>{customMsg}</div>
            )}

            {!installed && (
              <div style={{ marginBottom: 10, color: 'var(--yellow)', fontSize: 12 }}>
                {t('目标服务器尚未安装 Docker，请先返回容器页一键安装 Docker。')}
              </div>
            )}

            <div className="footer">
              <button className="btn btn-ghost" onClick={() => setCustomOpen(false)}>
                {t('取消')}
              </button>
              <button className="btn" onClick={installCustom} disabled={!installed}>
                {t('安装')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 安装进度终端窗口 */}
      {progress && (
        <div
          className="modal-mask"
          style={{ zIndex: 1000 }}
          onClick={() => {
            if (!progress.running) setProgress(null)
          }}
        >
          <div
            className="modal"
            style={{ width: 640, maxHeight: '80%', display: 'flex', flexDirection: 'column' }}
            onClick={(e) => e.stopPropagation()}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 10 }}>
              <span style={{ fontWeight: 600 }}>{progress.title}</span>
              <span
                style={{
                  color: progress.ok === null ? 'var(--yellow)' : progress.ok ? 'var(--green)' : 'var(--red)',
                  fontSize: 12,
                  fontWeight: 600,
                }}
              >
                {progress.running ? `⏳ ${t('安装中…')}` : progress.ok ? `✅ ${t('安装成功')}` : `❌ ${t('安装失败')}`}
              </span>
            </div>
            <pre
              ref={progressPreRef}
              style={{
                flex: 1,
                minHeight: 200,
                maxHeight: '60vh',
                overflow: 'auto',
                margin: 0,
                padding: 12,
                borderRadius: 8,
                background: 'rgba(var(--rgb-appbg),0.9)',
                color: 'var(--text-0)',
                fontFamily: 'Consolas, Menlo, monospace',
                fontSize: 12,
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
              }}
            >
              {progress.lines || t('准备中…')}
              {progress.running && <span className="term-cursor">▊</span>}
            </pre>
            {!progress.running && (
              <div style={{ marginTop: 12, fontSize: 12, color: progress.ok ? 'var(--green)' : 'var(--red)', wordBreak: 'break-all' }}>
                {progress.ok ? t('安装成功，容器 ID：{0}', progress.containerId.slice(0, 12)) : progress.message}
              </div>
            )}
            <div className="footer">
              <button className="btn" onClick={() => setProgress(null)} disabled={progress.running}>
                {progress.running ? t('安装中…') : t('完成')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 日志弹窗 */}
      {logsFor && (
        <div
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 1000,
            background: 'rgba(0,0,0,0.6)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
          onClick={() => setLogsFor(null)}
        >
          <div
            style={{
              width: '70%',
              height: '70%',
              background: 'var(--bg-1)',
              border: '1px solid rgba(var(--rgb-line),0.25)',
              borderRadius: 10,
              display: 'flex',
              flexDirection: 'column',
              overflow: 'hidden',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div
              style={{
                padding: '10px 14px',
                borderBottom: '1px solid rgba(var(--rgb-line),0.15)',
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
              }}
            >
              <span>{t('容器日志：{0}', logsFor.names || logsFor.id.slice(0, 12))}</span>
              <button className="btn btn-sm btn-ghost" onClick={() => setLogsFor(null)}>
                {t('关闭')}
              </button>
            </div>
            <pre
              style={{
                flex: 1,
                margin: 0,
                padding: 12,
                overflow: 'auto',
                background: 'rgba(var(--rgb-appbg),0.9)',
                color: 'var(--text-0)',
                fontFamily: 'Consolas, Menlo, monospace',
                fontSize: 12,
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
              }}
            >
              {logs}
            </pre>
          </div>
        </div>
      )}
    </div>
  )
}
