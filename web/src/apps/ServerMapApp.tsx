import { useEffect, useRef, useState } from 'react'
import * as echarts from 'echarts'
import { api } from '../lib/api'
import { useGeoStore } from '../lib/geoStore'
import { useMonitorStore, type MonitorPoint } from '../lib/monitorStore'
import { useDesktopSettings } from '../lib/desktopSettingsStore'
import type { AppProps } from '../desktop/appRegistry'
import type { Host, GeoInfo } from '../lib/types'
import { useT, tt } from '../lib/i18n'

const WORLD_URL = '/map/world.json'
/** 与后端采集间隔一致（monitor.go 每 2s 采样一次）：地图一次间隔只合并刷新一次 */
const REFRESH_MS = 2000

/** 世界地图全局只注册一次（并发加载去重） */
let worldReady: Promise<void> | null = null
function ensureWorld(): Promise<void> {
  if (!worldReady) {
    worldReady = fetch(WORLD_URL)
      .then((r) => {
        if (!r.ok) throw new Error('load world map failed')
        return r.json()
      })
      .then((geo) => echarts.registerMap('world', geo))
      .catch((e) => {
        worldReady = null // 允许失败后重试
        throw e
      })
  }
  return worldReady
}

/** B/s 速率 -> "12.3K/s" / "3.4M/s" */
function fmtRate(bps: number): string {
  if (bps >= 1024 * 1024) return `${(bps / 1024 / 1024).toFixed(1)}M/s`
  if (bps >= 1024) return `${(bps / 1024).toFixed(1)}K/s`
  return `${Math.round(bps)}B/s`
}

/** 字节 -> "123.2K" / "3.4M" / "1.2G" */
function fmtBytes(b: number): string {
  if (b >= 1024 ** 3) return `${(b / 1024 ** 3).toFixed(1)}G`
  if (b >= 1024 ** 2) return `${(b / 1024 ** 2).toFixed(1)}M`
  if (b >= 1024) return `${(b / 1024).toFixed(1)}K`
  return `${Math.round(b)}B`
}

function esc(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]!))
}

interface MapPoint {
  name: string
  value: [number, number]
  host: Host
  geo: GeoInfo
  monitor?: MonitorPoint
  /** 该城市在线服务器台数（同城聚合点 > 1） */
  count?: number
  /** 聚合点：该城市全部服务器 */
  hosts?: Host[]
  /** 聚合点：与 hosts 对齐的监控数据（可能缺省） */
  monitors?: (MonitorPoint | undefined)[]
}

/** 散点 tooltip 内容（地图上方高亮显示服务器详情） */
function ptTooltip(p: MapPoint): string {
  // 同城聚合点：列出该城市每台服务器
  if (p.count && p.hosts) {
    const g = p.geo
    const rows = [
      `<b style="font-size:13px">📍 ${esc(g.country)}${g.region ? ' · ' + esc(g.region) : ''}</b>`,
      `<span style="color:var(--primary-light)">${tt('{0} 台服务器', p.count)}</span>`,
    ]
    p.hosts.forEach((h, i) => {
      const m = p.monitors?.[i]
      let line = tt('{0}（{1}@{2}）', esc(h.name), esc(h.username), esc(h.host))
      line += h.connected
        ? ` <span style="color:#22c55e">● ${tt('在线')}</span>`
        : ` <span style="color:#64748b">● ${tt('离线')}</span>`
      if (m) line += tt('　CPU {0}%　内存 {1}%', m.cpu.toFixed(1), m.memPct.toFixed(1))
      rows.push(line)
    })
    return rows.join('<br/>')
  }

  // 单台服务器
  const { host: h, geo: g, monitor: m } = p
  const rows = [
    `<b style="font-size:13px">${esc(h.name)}</b>`,
    `${esc(h.username)}@${esc(h.host)}:${h.port}`,
  ]
  rows.push(
    h.connected
      ? `<span style="color:#22c55e">● ${tt('在线')}</span>`
      : `<span style="color:#64748b">● ${tt('离线')}</span>`,
  )
  if (g) {
    rows.push(`📍 ${esc(g.country)}${g.region ? ' · ' + esc(g.region) : ''}`)
  }
  if (m) {
    rows.push(tt('CPU {0}%　内存 {1}%　硬盘 {2}%', m.cpu.toFixed(1), m.memPct.toFixed(1), m.diskPct.toFixed(1)))
    rows.push(`↑ ${fmtRate(m.tx)}　↓ ${fmtRate(m.rx)}`)
    rows.push(tt('总上传 {0}　总下载 {1}', fmtBytes(m.txBytes), fmtBytes(m.rxBytes)))
  }
  return rows.join('<br/>')
}

/** 按城市聚合在线服务器：同城多台在线合并为一个绿色点（count = 台数） */
function buildPoints(
  hosts: Host[],
  geoByAddr: Record<string, GeoInfo>,
  latest: Record<string, MonitorPoint>,
): { online: MapPoint[]; offline: MapPoint[] } {
  const offline: MapPoint[] = []
  const cityMap = new Map<string, MapPoint[]>()

  for (const h of hosts) {
    const g = geoByAddr[h.host]
    if (!g || !g.lat || !g.lon) continue
    const pt: MapPoint = {
      name: h.name,
      value: [g.lon, g.lat],
      host: h,
      geo: g,
      monitor: latest[h.id],
      count: 1,
      hosts: [h],
      monitors: [latest[h.id]],
    }
    if (!h.connected) {
      offline.push(pt)
      continue
    }
    const key = `${g.lat.toFixed(2)},${g.lon.toFixed(2)}`
    const arr = cityMap.get(key)
    if (arr) arr.push(pt)
    else cityMap.set(key, [pt])
  }

  const online: MapPoint[] = []
  for (const arr of cityMap.values()) {
    if (arr.length === 1) {
      online.push(arr[0])
    } else {
      const first = arr[0]
      online.push({
        name: `${first.geo.country}${first.geo.region ? ' · ' + first.geo.region : ''}`,
        value: first.value,
        host: first.host,
        geo: first.geo,
        count: arr.length,
        hosts: arr.map((a) => a.host),
        monitors: arr.map((a) => a.monitor),
      })
    }
  }
  return { online, offline }
}

/**
 * 世界地图：世界地图 + 服务器分布散点 + 概览统计。
 * 绿色脉冲 = 在线（大小随 CPU 负载），灰色圆点 = 离线。
 */
export function ServerMapApp({ onTitle }: AppProps) {
  const t = useT()
  const boxRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<echarts.ECharts | null>(null)
  // 监控数据快照：按后端采样间隔（REFRESH_MS）合并刷新，一次只更新一次。
  // 若直接订阅 latest，每台主机的 monitor.data 都会各自触发一次重绘
  //（N 台主机 → N 次/间隔），导致地图散点涟漪反复重启、统计数字连续跳变。
  const [latest, setLatest] = useState<Record<string, MonitorPoint>>(
    () => useMonitorStore.getState().latest,
  )
  useEffect(() => {
    const apply = () => {
      const cur = useMonitorStore.getState().latest
      setLatest((prev) => {
        const keys = Object.keys(prev)
        if (keys.length !== Object.keys(cur).length) return cur
        for (const k of keys) if (prev[k]?.ts !== cur[k]?.ts) return cur
        return prev
      })
    }
    apply()
    const timer = setInterval(apply, REFRESH_MS)
    return () => clearInterval(timer)
  }, [])
  const geoByAddr = useGeoStore((s) => s.byAddr)
  const [hosts, setHosts] = useState<Host[]>([])
  const [mapOk, setMapOk] = useState(false)
  const [err, setErr] = useState('')
  // 当前主题：tooltip / 计数标签颜色需随 --text-0 / --primary-light 适配
  const theme = useDesktopSettings((s) => s.theme)

  // 聚合后的散点数据最新快照（供两个渲染 effect 读取，避免重复计算与过期闭包）
  const pointsRef = useRef<{ online: MapPoint[]; offline: MapPoint[] }>({ online: [], offline: [] })
  pointsRef.current = buildPoints(hosts, geoByAddr, latest)

  // 初始化：标题 + 世界地图 + 主机列表
  useEffect(() => {
    if (onTitle) onTitle(t('世界地图'))
    api.listHosts().then(setHosts).catch(() => {})
    ensureWorld()
      .then(() => setMapOk(true))
      .catch(() => setErr(t('世界地图加载失败，请检查网络后重试')))
  }, [onTitle])

  // 主机列表变化时批量补齐地理位置（geoStore 内部有缓存）
  useEffect(() => {
    if (hosts.length > 0) {
      void useGeoStore.getState().load(hosts.map((h) => h.host))
    }
  }, [hosts])

  // 初始化图表 + 跟随窗口尺寸
  useEffect(() => {
    const el = boxRef.current
    if (!el) return
    let c: echarts.ECharts | null = null
    try {
      c = echarts.init(el)
    } catch {
      return
    }
    chartRef.current = c

    // 单击地图块时由 geo 内置 selectedMode 自动高亮/取消，悬停不再变亮。
    const ro = new ResizeObserver(() => c?.resize())
    ro.observe(el)
    return () => {
      ro.disconnect()
      c.dispose()
      if (chartRef.current === c) chartRef.current = null
    }
  }, [])

  // 完整图表配置：仅在主机列表/地理信息/地图就绪变化时构建。
  // 与下面的数据刷新 effect 分离，避免监控刷新重建地图时重置缩放视图。
  useEffect(() => {
    const c = chartRef.current
    if (!c || !mapOk) return
    const { online, offline } = pointsRef.current
    // 主题适配：tooltip 文字取 --text-0、在线计数标签取 --primary-light
    //（与 TaskManagerApp 图表同法，避免浅色主题下白色文字融进背景）
    const tooltipColor =
      getComputedStyle(document.documentElement).getPropertyValue('--text-0').trim() || '#f1f5f9'
    const countColor =
      getComputedStyle(document.documentElement).getPropertyValue('--primary-light').trim() ||
      '#7dd3fc'
    const countShadow =
      countColor.length === 7 ? `${countColor}cc` : 'rgba(125,211,252,0.9)'
    c.setOption({
      tooltip: {
        trigger: 'item',
        backgroundColor: 'rgba(var(--rgb-appbg),0.92)',
        borderColor: 'rgba(var(--rgb-primary-light),0.4)',
        textStyle: { color: tooltipColor, fontSize: 12 },
        formatter: (params: any) =>
          params?.data
            ? ptTooltip(params.data as MapPoint)
            : `<b style="font-size:13px">${esc(String(params?.name ?? ''))}</b>`,
      },
      geo: {
        map: 'world',
        roam: true,
        layoutCenter: ['50%', '52%'],
        layoutSize: '94%',
        // 单击地图块高亮（geo 内置 selectedMode toggle）；悬停不变亮
        selectedMode: 'multiple',
        itemStyle: {
          areaColor: 'rgba(var(--rgb-panel),0.9)',
          borderColor: 'rgba(var(--rgb-primary-light),0.4)',
          borderWidth: 0.6,
          shadowColor: 'rgba(0,0,0,0.5)',
          shadowBlur: 10,
        },
        emphasis: {
          label: { show: false },
          itemStyle: { areaColor: 'rgba(var(--rgb-panel),0.9)' },
        },
        select: {
          label: { show: false },
          itemStyle: { areaColor: 'rgba(29,78,216,0.55)' },
        },
      },
      series: [
        {
          id: 'online',
          name: t('在线'),
          type: 'effectScatter',
          coordinateSystem: 'geo',
          zlevel: 2,
          data: online,
          symbolSize: (_val: any, params: any) => {
            const d = params.data as MapPoint
            if (d.count && d.count > 1) return 12 + Math.min(20, d.count * 3)
            const cpu = d.monitor?.cpu ?? 0
            return 9 + Math.min(15, cpu / 4)
          },
          rippleEffect: { brushType: 'stroke', scale: 3.2, period: 3.5 },
          itemStyle: {
            color: '#22c55e',
            shadowBlur: 10,
            shadowColor: 'rgba(34,197,94,0.7)',
          },
          // 绿色点下方：发光数字表示该城市在线服务器台数
          label: {
            show: true,
            position: 'bottom',
            distance: 2,
            formatter: (params: any) => String((params.data as MapPoint).count ?? 1),
            color: countColor,
            fontSize: 12,
            fontWeight: 700,
            fontFamily: 'Consolas, Menlo, monospace',
            textShadowBlur: 8,
            textShadowColor: countShadow,
          },
        },
        {
          id: 'offline',
          name: t('离线'),
          type: 'scatter',
          coordinateSystem: 'geo',
          zlevel: 1,
          data: offline,
          symbolSize: 7,
          itemStyle: { color: '#64748b', opacity: 0.8 },
          label: { show: false },
        },
      ],
    })
  }, [hosts, geoByAddr, mapOk, theme])

  // 监控数据刷新：latest 已是按间隔合并的快照，此处每次间隔只触发一次；
  // 仅更新散点数据，不触碰 geo，保证地图悬停高亮/缩放不被重置
  useEffect(() => {
    const c = chartRef.current
    if (!c || !mapOk) return
    const { online, offline } = pointsRef.current
    c.setOption({
      series: [
        { id: 'online', data: online },
        { id: 'offline', data: offline },
      ],
    })
  }, [latest, mapOk])

  // ---- 概览统计 ----
  const total = hosts.length
  const onlineCount = hosts.filter((h) => h.connected).length
  const offlineCount = total - onlineCount
  const geoCount = new Set(
    hosts.map((h) => geoByAddr[h.host]?.country_code).filter(Boolean),
  ).size
  const withMon = hosts.filter((h) => latest[h.id])
  const avgCpu = withMon.length
    ? withMon.reduce((a, h) => a + (latest[h.id]?.cpu ?? 0), 0) / withMon.length
    : null
  const avgMem = withMon.length
    ? withMon.reduce((a, h) => a + (latest[h.id]?.memPct ?? 0), 0) / withMon.length
    : null
  const sumTx = hosts.reduce((a, h) => a + (latest[h.id]?.tx ?? 0), 0)
  const sumRx = hosts.reduce((a, h) => a + (latest[h.id]?.rx ?? 0), 0)

  const stats: { label: string; value: string; color: string }[] = [
    { label: t('服务器总数'), value: String(total), color: 'var(--text-0)' },
    { label: t('在线'), value: String(onlineCount), color: 'var(--green)' },
    { label: t('离线'), value: String(offlineCount), color: '#64748b' },
    { label: t('覆盖国家/地区'), value: String(geoCount), color: 'var(--cyan)' },
    { label: t('平均 CPU'), value: avgCpu === null ? '—' : `${avgCpu.toFixed(1)}%`, color: '#60a5fa' },
    { label: t('平均内存'), value: avgMem === null ? '—' : `${avgMem.toFixed(1)}%`, color: '#c084fc' },
    { label: t('总上传'), value: fmtRate(sumTx), color: '#34d399' },
    { label: t('总下载'), value: fmtRate(sumRx), color: '#fb923c' },
  ]

  return (
    <div
      style={{
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
        background: 'rgba(var(--rgb-appbg),0.85)',
        padding: 12,
        gap: 10,
        overflow: 'hidden',
      }}
    >
      {/* 概览统计 */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(118px, 1fr))',
          gap: 8,
          flexShrink: 0,
        }}
      >
        {stats.map((s) => (
          <div
            key={s.label}
            style={{
              background: 'rgba(var(--rgb-card),0.5)',
              border: '1px solid rgba(var(--rgb-line),0.14)',
              borderRadius: 10,
              padding: '8px 12px',
              textAlign: 'center',
            }}
          >
            <div style={{ fontSize: 20, fontWeight: 700, color: s.color, lineHeight: 1.2 }}>
              {s.value}
            </div>
            <div style={{ fontSize: 11, color: 'var(--text-1)', marginTop: 2 }}>{s.label}</div>
          </div>
        ))}
      </div>

      {/* 世界地图 */}
      <div
        ref={boxRef}
        style={{
          flex: 1,
          minHeight: 0,
          borderRadius: 12,
          border: '1px solid rgba(var(--rgb-line),0.14)',
          background: 'radial-gradient(900px 500px at 50% 30%, rgba(29,78,216,0.16), transparent 65%)',
        }}
      />
      {err && <div style={{ color: 'var(--red)', fontSize: 12 }}>{err}</div>}
      {mapOk && !err && total === 0 && (
        <div
          style={{
            position: 'absolute',
            inset: 0,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'var(--text-1)',
            fontSize: 14,
            pointerEvents: 'none',
          }}
        >
          {t('暂无服务器，请先在应用中心「添加服务器」')}
        </div>
      )}
    </div>
  )
}
