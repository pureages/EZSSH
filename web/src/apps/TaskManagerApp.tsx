import { useEffect, useRef, useState, type ReactNode } from 'react'
import * as echarts from 'echarts'
import { ws } from '../lib/ws'
import { useT, tt } from '../lib/i18n'
import type { AppProps } from '../desktop/appRegistry'

interface Snapshot {
  ts: number
  error?: string
  cpu: number
  cpu_per: number[]
  load1: number
  load5: number
  load15: number
  mem_total: number
  mem_used: number
  mem_pct: number
  swap_total: number
  swap_used: number
  swap_pct: number
  disks: { mount: string; total: number; used: number; pct: number }[]
  net: { iface: string; rx_bps: number; tx_bps: number; rx_bytes: number; tx_bytes: number }[]
}

interface Process {
  pid: number
  ppid: number
  user: string
  cpu: number
  mem: number
  rss: number
  vsz: number
  start: string
  command: string
}

interface HardwareInfo {
  os: string
  distro: string
  distroName: string
  hostname: string
  uptime: number
  cpuModel: string
  cpuCores: number
  vm: boolean
  hypervisor: string
  productName: string
  vendor: string
}

const MAX_POINTS = 60

function fmtBytes(n: number): string {
  if (n >= 1024 * 1024 * 1024) return `${(n / 1024 / 1024 / 1024).toFixed(1)} GB`
  if (n >= 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  if (n >= 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${n} B`
}

function fmtRate(bps: number): string {
  if (bps >= 1024 * 1024) return `${(bps / 1024 / 1024).toFixed(1)} MB/s`
  if (bps >= 1024) return `${(bps / 1024).toFixed(1)} KB/s`
  return `${Math.round(bps)} B/s`
}

function fmtMem(kb: number): string {
  if (kb >= 1024 * 1024) return `${(kb / 1024 / 1024).toFixed(1)} GB`
  if (kb >= 1024) return `${(kb / 1024).toFixed(1)} MB`
  return `${kb} KB`
}

// fmtUptime 将秒数格式化为 "X 天 Y 小时" 之类的中文时长。
function fmtUptime(sec: number): string {
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  const parts: string[] = []
  if (d) parts.push(tt('{0} 天', d))
  if (h) parts.push(tt('{0} 小时', h))
  if (m) parts.push(tt('{0} 分钟', m))
  if (parts.length === 0) parts.push(tt('{0} 秒', s))
  return parts.slice(0, 2).join(' ')
}

const COLUMNS = ['pid', 'user', 'cpu', 'mem', 'rss', 'vsz', 'start', 'command'] as const
type Col = (typeof COLUMNS)[number]

/**
 * 任务管理器：融合「硬件」与「进程」两个页签，参考 Windows 任务管理器。
 * 硬件页采用左侧硬件列表 + 右侧详情布局（系统 / CPU / 内存 / Swap / 硬盘 / 网络）。
 * 后端数据仍走 monitor.subscribe/monitor.data 与 ps.list/ps.kill。
 */
export function TaskManagerApp({ windowId, hostId }: AppProps) {
  const t = useT()
  const [tab, setTab] = useState<'perf' | 'proc'>('perf')

  if (!hostId) {
    return (
      <div style={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-1)', background: 'rgba(var(--rgb-appbg),0.9)' }}>
        {t('未选择主机')}
      </div>
    )
  }

  return (
    <div className="tm-root">
      <div className="tm-tabs">
        <button className={`tm-tab${tab === 'perf' ? ' active' : ''}`} onClick={() => setTab('perf')}>
          {t('硬件')}
        </button>
        <button className={`tm-tab${tab === 'proc' ? ' active' : ''}`} onClick={() => setTab('proc')}>
          {t('进程')}
        </button>
      </div>
      <div className="tm-body">
        {tab === 'perf' ? <HardwareView hostId={hostId} subId={`tm-${windowId}`} /> : <ProcView hostId={hostId} />}
      </div>
    </div>
  )
}

// ---- 趋势图：可复用的小型 ECharts 折线图 ----

interface TrendSeries {
  name: string
  data: number[]
  color: string
  area?: boolean
}

function TrendChart({ time, series, height = 190 }: { time: string[]; series: TrendSeries[]; height?: number }) {
  const ref = useRef<HTMLDivElement>(null)
  const inst = useRef<echarts.ECharts | null>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    let c: echarts.ECharts | null = null
    try {
      c = echarts.init(el)
    } catch {
      return
    }
    inst.current = c
    return () => {
      c.dispose()
      if (inst.current === c) inst.current = null
    }
  }, [])

  useEffect(() => {
    const c = inst.current
    if (!c) return
    const axisColor = getComputedStyle(document.documentElement).getPropertyValue('--text-1').trim() || '#94a3b8'
    const lineColor = getComputedStyle(document.documentElement).getPropertyValue('--rgb-line').trim() || '148,163,184'
    c.setOption({
      backgroundColor: 'transparent',
      legend: {
        bottom: 0,
        left: 'center',
        itemWidth: 16,
        itemHeight: 9,
        itemGap: 18,
        textStyle: { color: axisColor, fontSize: 13 },
      },
      grid: { left: 46, right: 16, top: 8, bottom: 44 },
      tooltip: { trigger: 'axis' },
      xAxis: { type: 'category', data: time, axisLine: { lineStyle: { color: `rgba(${lineColor},0.35)` } }, axisLabel: { color: axisColor } },
      yAxis: { type: 'value', axisLabel: { color: axisColor }, splitLine: { lineStyle: { color: `rgba(${lineColor},0.12)` } } },
      series: series.map((s) => ({
        name: s.name,
        type: 'line',
        smooth: true,
        showSymbol: false,
        data: s.data,
        lineStyle: { color: s.color },
        itemStyle: { color: s.color },
        areaStyle: s.area ? { color: `${s.color}2e` } : undefined,
      })),
    })
  }, [time, series])

  return <div ref={ref} style={{ width: '100%', height }} />
}

// ---- 硬件页：左侧硬件列表 + 右侧详情（Windows 任务管理器风格） ----

type SideKey = 'system' | 'cpu' | 'mem' | 'swap' | 'disk' | 'net'

function HardwareView({ hostId, subId }: { hostId: string; subId: string }) {
  const t = useT()
  const dataRef = useRef<{
    cpu: number[]
    mem: number[]
    netRx: number[]
    netTx: number[]
    load: number[]
    time: string[]
  }>({
    cpu: [],
    mem: [],
    netRx: [],
    netTx: [],
    load: [],
    time: [],
  })
  const [last, setLast] = useState<Snapshot | null>(null)
  const [hwInfo, setHwInfo] = useState<HardwareInfo | null>(null)
  const hwReqSeq = useRef(0)
  const [sel, setSel] = useState<SideKey>('system')

  useEffect(() => {
    if (!hostId) return
    let disposed = false
    let unsub: (() => void) | undefined
    const hwUnsubs: (() => void)[] = []
    const boot = async () => {
      try {
        await ws.connect()
      } catch {
        return
      }
      if (disposed) return
      unsub = ws.onType('monitor.data', (msg) => {
        if (disposed) return
        const payload = msg.payload as { hostId?: string; snapshot: Snapshot }
        if (payload.hostId && payload.hostId !== hostId) return
        const snap = payload.snapshot
        const d = dataRef.current
        d.cpu.push(snap.cpu)
        d.mem.push(snap.mem_pct)
        d.load.push(snap.load1)
        const rx = (snap.net ?? []).reduce((s, n) => s + (n.rx_bps ?? 0), 0)
        const tx = (snap.net ?? []).reduce((s, n) => s + (n.tx_bps ?? 0), 0)
        d.netRx.push(rx)
        d.netTx.push(tx)
        d.time.push(new Date(snap.ts * 1000).toLocaleTimeString())
        if (d.cpu.length > MAX_POINTS) {
          d.cpu.shift()
          d.mem.shift()
          d.load.shift()
          d.netRx.shift()
          d.netTx.shift()
          d.time.shift()
        }
        setLast({ ...snap })
      })
      ws.send('monitor.subscribe', '', { hostId, subId })

      // 一次性采集硬件/系统静态信息（非实时流）：发行版 / CPU 型号 / 是否虚拟机
      const hwSeq = ++hwReqSeq.current
      ws.send('monitor.hwinfo', `hw_${hwSeq}`, { hostId })
      hwUnsubs.push(
        ws.onChannel(`hw_${hwSeq}`, (msg) => {
          if (disposed) return
          if (msg.type === 'monitor.hwinfo') {
            setHwInfo((msg.payload?.info as HardwareInfo) || null)
          }
        })
      )
    }
    void boot()
    return () => {
      disposed = true
      hwUnsubs.forEach((u) => u())
      if (unsub) {
        unsub()
        ws.send('monitor.unsubscribe', '', { hostId, subId })
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hostId, subId])

  // 汇总派生数据
  const disks = last?.disks ?? []
  const rootDisk = disks.find((d) => d.mount === '/') ?? disks[0]
  const net = last?.net ?? []
  const netRx = net.reduce((s, n) => s + (n.rx_bps ?? 0), 0)
  const netTx = net.reduce((s, n) => s + (n.tx_bps ?? 0), 0)
  const netRxBytes = net.reduce((s, n) => s + (n.rx_bytes ?? 0), 0)
  const netTxBytes = net.reduce((s, n) => s + (n.tx_bytes ?? 0), 0)

  const d = dataRef.current

  const SIDES: { key: SideKey; icon: string; label: string; pct: string }[] = [
    { key: 'system', icon: '🖥️', label: t('系统'), pct: hwInfo?.distro || hwInfo?.os?.split(' ')[0] || '—' },
    { key: 'cpu', icon: '⚙️', label: 'CPU', pct: `${(last?.cpu ?? 0).toFixed(0)}%` },
    { key: 'mem', icon: '💾', label: t('内存'), pct: `${(last?.mem_pct ?? 0).toFixed(0)}%` },
    { key: 'swap', icon: '🔄', label: 'Swap', pct: `${(last?.swap_pct ?? 0).toFixed(0)}%` },
    { key: 'disk', icon: '💽', label: t('硬盘'), pct: rootDisk ? `${rootDisk.pct.toFixed(0)}%` : '—' },
    { key: 'net', icon: '🌐', label: t('网络'), pct: fmtRate(netRx + netTx) },
  ]

  const detailHeader = (icon: string, title: string, sub?: string) => (
    <div className="tm-head">
      <span className="tm-head-icon">{icon}</span>
      <span className="tm-head-title">{title}</span>
      {sub && <span className="tm-head-sub">{sub}</span>}
    </div>
  )

  const kv = (k: string, v: ReactNode) => (
    <div className="tm-kv">
      <span className="tm-kv-k">{k}</span>
      <span className="tm-kv-v">{v}</span>
    </div>
  )

  /** 顶部统计小卡片 */
  const statCard = (label: string, value: ReactNode, color: string) => (
    <div className="tm-stat">
      <div className="tm-stat-label">{label}</div>
      <div className="tm-stat-value" style={{ color }}>{value}</div>
    </div>
  )

  /** 区块小标题 */
  const blockTitle = (t: string) => <div className="tm-block-title">{t}</div>

  /** 空数据占位 */
  const emptyBlock = (t: string) => <div className="tm-empty">{t}</div>

  const pbar = (pct: number, color: string, height = 10) => (
    <div className="tm-pbar" style={{ height }}>
      <div
        className="tm-pbar-fill"
        style={{
          width: `${Math.min(100, Math.max(0, pct))}%`,
          background: color,
        }}
      />
    </div>
  )

  const renderDetail = () => {
    switch (sel) {
      case 'cpu':
        return (
          <div>
            {detailHeader('⚙️', 'CPU', last ? t('当前使用率 {0}%', last.cpu.toFixed(1)) : '')}
            <div className="tm-stats">
              {statCard(t('型号'), hwInfo?.cpuModel || '—', '#60a5fa')}
              {statCard(t('逻辑核心'), hwInfo ? t('{0} 核', hwInfo.cpuCores) : '—', '#60a5fa')}
              {statCard(
                t('负载1 / 5 / 15'),
                last ? `${last.load1.toFixed(2)} / ${last.load5.toFixed(2)} / ${last.load15.toFixed(2)}` : '—',
                '#60a5fa',
              )}
            </div>
            <div style={{ maxWidth: 640 }}>
              {blockTitle(t('每核心使用率'))}
              {(last?.cpu_per ?? []).length === 0 ? (
                emptyBlock(t('暂无每核心数据…'))
              ) : (
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))', gap: '8px 18px' }}>
                  {(last?.cpu_per ?? []).map((c, i) => (
                    <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <span style={{ fontSize: 11, color: 'var(--text-1)', width: 24, textAlign: 'right' }}>#{i}</span>
                      <div style={{ flex: 1 }}>{pbar(c, c > 80 ? 'var(--red)' : '#60a5fa', 8)}</div>
                      <span style={{ fontSize: 11, color: 'var(--text-1)', width: 38, textAlign: 'right' }}>{c.toFixed(0)}%</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
            <div style={{ marginTop: 22 }}>
              {blockTitle(t('使用率趋势'))}
              <TrendChart time={d.time} series={[{ name: 'CPU %', data: d.cpu, color: '#60a5fa', area: true }]} />
            </div>
          </div>
        )
      case 'mem':
        return (
          <div>
            {detailHeader('💾', t('内存'), last ? t('已使用 {0}%', last.mem_pct.toFixed(1)) : '')}
            <div className="tm-stats">
              {statCard(t('总大小'), last ? fmtBytes(last.mem_total) : '—', '#a78bfa')}
              {statCard(t('已使用'), last ? fmtBytes(last.mem_used) : '—', '#a78bfa')}
              {statCard(t('可用'), last ? fmtBytes(last.mem_total - last.mem_used) : '—', '#a78bfa')}
            </div>
            <div style={{ maxWidth: 560 }}>
              {blockTitle(t('使用率'))}
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, color: 'var(--text-1)', marginBottom: 6 }}>
                <span>{last ? `${fmtBytes(last.mem_used)} / ${fmtBytes(last.mem_total)}` : ''}</span>
                <span>{(last?.mem_pct ?? 0).toFixed(1)}%</span>
              </div>
              {pbar(last?.mem_pct ?? 0, '#a78bfa', 14)}
            </div>
            <div style={{ marginTop: 22 }}>
              {blockTitle(t('使用率趋势'))}
              <TrendChart time={d.time} series={[{ name: t('内存 %'), data: d.mem, color: '#a78bfa', area: true }]} />
            </div>
          </div>
        )
      case 'swap':
        return (
          <div>
            {detailHeader('🔄', 'Swap', last ? t('已使用 {0}%', last.swap_pct.toFixed(1)) : '')}
            {last && last.swap_total === 0 ? (
              emptyBlock(t('未配置Swap交换分区'))
            ) : (
              <>
                <div className="tm-stats">
                  {statCard(t('总大小'), last ? fmtBytes(last.swap_total) : '—', '#fbbf24')}
                  {statCard(t('已使用'), last ? fmtBytes(last.swap_used) : '—', '#fbbf24')}
                  {statCard(t('可用'), last ? fmtBytes(last.swap_total - last.swap_used) : '—', '#fbbf24')}
                </div>
                <div style={{ maxWidth: 560 }}>
                  {blockTitle(t('使用率'))}
                  <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, color: 'var(--text-1)', marginBottom: 6 }}>
                    <span>{last ? `${fmtBytes(last.swap_used)} / ${fmtBytes(last.swap_total)}` : ''}</span>
                    <span>{(last?.swap_pct ?? 0).toFixed(1)}%</span>
                  </div>
                  {pbar(last?.swap_pct ?? 0, '#fbbf24', 14)}
                </div>
              </>
            )}
          </div>
        )
      case 'disk':
        return (
          <div>
            {detailHeader('💽', t('硬盘'), rootDisk ? t('根分区 {0}%', rootDisk.pct.toFixed(1)) : '')}
            {disks.length === 0 ? (
              emptyBlock(t('暂无磁盘数据'))
            ) : (
              <>
                <div className="tm-stats">
                  {statCard(t('分区数量'), t('{0} 个', disks.length), '#f59e0b')}
                  {statCard(t('根分区使用'), rootDisk ? `${fmtBytes(rootDisk.used)} / ${fmtBytes(rootDisk.total)}` : '—', '#f59e0b')}
                  {statCard(t('根分区占用'), rootDisk ? `${rootDisk.pct.toFixed(1)}%` : '—', '#f59e0b')}
                </div>
                {blockTitle(t('各分区'))}
                {disks.map((dk) => (
                  <div key={dk.mount} className="tm-subcard">
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, color: 'var(--text-1)', marginBottom: 6 }}>
                      <span style={{ fontFamily: 'Consolas, monospace', color: 'var(--text-0)' }}>{dk.mount}</span>
                      <span>
                        {fmtBytes(dk.used)} / {fmtBytes(dk.total)} ·{' '}
                        <b style={{ color: dk.pct > 85 ? 'var(--red)' : 'var(--text-0)' }}>{dk.pct.toFixed(1)}%</b>
                      </span>
                    </div>
                    {pbar(dk.pct, dk.pct > 85 ? 'var(--red)' : '#f59e0b', 10)}
                  </div>
                ))}
              </>
            )}
          </div>
        )
      case 'net':
        return (
          <div>
            {detailHeader('🌐', t('网络'), `${fmtRate(netRx)} ↓ / ${fmtRate(netTx)} ↑`)}
            <div className="tm-stats">
              <div className="tm-stat" style={{ minWidth: 200 }}>
                <div style={{ color: '#fb923c', fontSize: 12, marginBottom: 6 }}>{t('下载速率')}</div>
                <div style={{ fontSize: 22, fontWeight: 700, color: 'var(--text-0)', fontFamily: 'Consolas, Menlo, monospace' }}>{fmtRate(netRx)}</div>
                <div style={{ fontSize: 11, color: 'var(--text-1)', marginTop: 4 }}>{t('累计 {0}', fmtBytes(netRxBytes))}</div>
              </div>
              <div className="tm-stat" style={{ minWidth: 200 }}>
                <div style={{ color: '#34d399', fontSize: 12, marginBottom: 6 }}>{t('上传速率')}</div>
                <div style={{ fontSize: 22, fontWeight: 700, color: 'var(--text-0)', fontFamily: 'Consolas, Menlo, monospace' }}>{fmtRate(netTx)}</div>
                <div style={{ fontSize: 11, color: 'var(--text-1)', marginTop: 4 }}>{t('累计 {0}', fmtBytes(netTxBytes))}</div>
              </div>
            </div>
            {blockTitle(t('网卡'))}
            {net.length === 0 ? (
              emptyBlock(t('暂无网卡数据'))
            ) : (
              net.map((n) => (
                <div
                  key={n.iface}
                  className="tm-subcard"
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    marginBottom: 6,
                    fontSize: 12,
                  }}
                >
                  <span style={{ fontFamily: 'Consolas, monospace', color: 'var(--text-0)' }}>{n.iface}</span>
                  <span style={{ color: 'var(--text-1)' }}>
                    <span style={{ color: '#fb923c' }}>↓ {fmtRate(n.rx_bps)}</span>
                    <span style={{ margin: '0 8px', color: 'rgba(var(--rgb-line),0.45)' }}>·</span>
                    <span style={{ color: '#34d399' }}>↑ {fmtRate(n.tx_bps)}</span>
                  </span>
                </div>
              ))
            )}
            <div style={{ marginTop: 22 }}>
              {blockTitle(t('流量趋势'))}
              <TrendChart
                time={d.time}
                series={[
                  { name: t('下载KBs'), data: d.netRx.map((v) => v / 1024), color: '#fb923c' },
                  { name: t('上传KBs'), data: d.netTx.map((v) => v / 1024), color: '#34d399' },
                ]}
              />
            </div>
          </div>
        )
      default: {
        // 系统概览：第一行最前面即为发行版名称
        const distro = hwInfo?.distroName || hwInfo?.distro || hwInfo?.os?.split(' ')[0] || t('未知系统')
        return (
          <div>
            {detailHeader('🖥️', distro, hwInfo?.os)}
            {kv(t('发行版'), hwInfo?.distroName || hwInfo?.distro || '—')}
            {kv(t('系统内核'), hwInfo?.os || '—')}
            {kv(t('主机名'), hwInfo?.hostname || '—')}
            {kv(t('已运行', ''), hwInfo ? fmtUptime(hwInfo.uptime) : '—')}
            {kv(t('虚拟化'), hwInfo ? (hwInfo.vm ? t('虚拟机（{0}）', hwInfo.hypervisor || t('未知平台')) : t('物理机')) : '—')}
            {kv(t('厂商产品'), [hwInfo?.vendor, hwInfo?.productName].filter(Boolean).join(' ') || '—')}
            {kv(t('CPU型号'), hwInfo?.cpuModel || '—')}
            {kv(t('逻辑核心'), hwInfo ? t('{0} 核', hwInfo.cpuCores) : '—')}
            {kv(t('内存'), last ? `${fmtBytes(last.mem_used)} / ${fmtBytes(last.mem_total)}` : '—')}
            {kv(t('硬盘根分区'), rootDisk ? `${fmtBytes(rootDisk.used)} / ${fmtBytes(rootDisk.total)}` : '—')}
          </div>
        )
      }
    }
  }

  return (
    <div className="tm-hw">
      {last?.error && (
        <div className="tm-alert">
          {t('⚠ 监控采集失败：{0}', last.error)}
        </div>
      )}

      {/* 左侧：硬件列表（Windows 任务管理器样式） */}
      <aside className="tm-side">
        <div className="tm-side-label">{t('硬件')}</div>
        {SIDES.map((s) => (
          <button
            key={s.key}
            onClick={() => setSel(s.key)}
            className={`tm-side-item${sel === s.key ? ' active' : ''}`}
          >
            <span className="tm-side-name">
              {s.icon} {s.label}
            </span>
            <span className="tm-side-val">{s.pct}</span>
          </button>
        ))}
        {hwInfo && (
          <div className="tm-side-foot">
            <div>⏱ {t('已运行 {0}', fmtUptime(hwInfo.uptime))}</div>
          </div>
        )}
      </aside>

      {/* 右侧：选中硬件的详细信息 */}
      <div className="tm-main">{renderDetail()}</div>
    </div>
  )
}

// ---- 进程页：列表 + 搜索 + 结束进程 ----

function ProcView({ hostId }: { hostId: string }) {
  const t = useT()
  const [procs, setProcs] = useState<Process[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [kw, setKw] = useState('')
  const [sortBy, setSortBy] = useState<Col>('cpu')
  const hostIdRef = useRef(hostId)
  const reqSeq = useRef(0)

  useEffect(() => {
    hostIdRef.current = hostId
    if (!hostId) return
    let cancelled = false
    const boot = async () => {
      try {
        await ws.connect()
      } catch {
        return
      }
      const fetchList = () => {
        const seq = ++reqSeq.current
        ws.send('ps.list', `ps_${seq}`, { hostId: hostIdRef.current })
        const unsub = ws.onChannel(`ps_${seq}`, (msg) => {
          if (cancelled) return
          if (msg.type === 'ps.list') {
            const list = (msg.payload?.processes as Process[]) || []
            setProcs(list)
            setLoading(false)
            setError('')
          } else if (msg.type === 'error') {
            setLoading(false)
            setError((msg.payload?.message as string) || t('加载失败'))
          }
          unsub()
        })
      }
      fetchList()
      const timer = setInterval(fetchList, 3000)
      return () => clearInterval(timer)
    }
    let stop = () => {}
    // 全局错误兜底：握手/请求失败等错误消息可能不带 channelId
    const unsubErr = ws.onType('error', (msg) => {
      if (cancelled) return
      setLoading(false)
      setError((msg.payload?.message as string) || t('加载失败'))
    })
    void boot().then((f) => {
      if (f) stop = f
    })
    return () => {
      cancelled = true
      stop()
      unsubErr()
    }
  }, [hostId])

  const kill = (p: Process, sig: 15 | 9) => {
    if (!window.confirm(t('确认结束进程 {0}（{1}）？\n{2}', p.pid, sig === 9 ? t('KILL 强制') : 'TERM', p.command))) return
    ws.send('ps.kill', `ps_kill_${Date.now().toString(36)}`, {
      hostId: hostIdRef.current,
      pid: p.pid,
      signal: sig,
    })
  }

  const filtered = procs
    .filter((p) => !kw || p.command.toLowerCase().includes(kw.toLowerCase()) || String(p.pid).includes(kw))
    .sort((a, b) => {
      if (sortBy === 'pid') return a.pid - b.pid
      if (sortBy === 'rss' || sortBy === 'vsz') return Number(b[sortBy]) - Number(a[sortBy])
      return String(b[sortBy]).localeCompare(String(a[sortBy]))
    })

  const cell = (label: string, col: Col) => (
    <th key={col} onClick={() => setSortBy(col)}>
      {label} {sortBy === col ? '▾' : ''}
    </th>
  )

  return (
    <div className="tm-proc">
      <div className="tm-proc-bar">
        <span className="tm-proc-count">{t('进程数：{0}', filtered.length)}</span>
        <input
          type="text"
          className="tm-proc-search"
          placeholder={t('搜索进程 / PID…')}
          value={kw}
          onChange={(e) => setKw(e.target.value)}
        />
        <button className="btn btn-sm btn-ghost" onClick={() => { setLoading(true); ws.send('ps.list', `ps_${++reqSeq.current}`, {}) }}>
          {t('刷新')}
        </button>
      </div>

      {error && <div style={{ padding: '8px 10px', color: 'var(--red)' }}>{error}</div>}

      <div style={{ flex: 1, overflow: 'auto' }}>
        {loading && procs.length === 0 ? (
          <div style={{ padding: 20, color: 'var(--text-1)' }}>{t('加载中…')}</div>
        ) : (
          <table className="tm-table">
            <thead>
              <tr>
                {cell('PID', 'pid')}
                {cell(t('用户'), 'user')}
                {cell('CPU %', 'cpu')}
                {cell('MEM %', 'mem')}
                {cell('RSS', 'rss')}
                {cell(t('启动时间'), 'start')}
                {cell(t('命令'), 'command')}
                <th>{t('操作')}</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((p) => (
                <tr key={p.pid}>
                  <td style={{ fontFamily: 'Consolas, monospace' }}>{p.pid}</td>
                  <td style={{ color: 'var(--text-1)' }}>{p.user}</td>
                  <td style={{ color: p.cpu > 50 ? 'var(--red)' : 'var(--text-0)' }}>{p.cpu.toFixed(1)}</td>
                  <td style={{ color: 'var(--text-0)' }}>{p.mem.toFixed(1)}</td>
                  <td style={{ color: 'var(--text-1)' }}>{fmtMem(p.rss)}</td>
                  <td style={{ color: 'var(--text-1)', whiteSpace: 'nowrap' }}>{p.start}</td>
                  <td
                    style={{ fontFamily: 'Consolas, monospace', maxWidth: 380, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                    title={p.command}
                  >
                    {p.command}
                  </td>
                  <td style={{ whiteSpace: 'nowrap' }}>
                    <button className="btn btn-sm btn-ghost" onClick={() => kill(p, 15)}>
                      TERM
                    </button>
                    <button className="btn btn-sm btn-danger" style={{ marginLeft: 4 }} onClick={() => kill(p, 9)}>
                      KILL
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
