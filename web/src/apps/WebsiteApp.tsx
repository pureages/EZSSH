import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, ApiError } from '../lib/api'
import type {
  Certificate,
  DnsAccount,
  Host,
  NginxStatus,
  Website,
} from '../lib/types'
import type { AppProps } from '../desktop/appRegistry'
import { useT, transErr, currentLang } from '../lib/i18n'
import { openAppWindow } from '../desktop/openApp'
import { ws } from '../lib/ws'
import { setPendingFmCwd } from '../lib/fmLaunch'
import { useEscClose } from '../lib/escClose'

type SiteType = Website['site_type']

interface ProgressState {
  title: string
  lines: string
  running: boolean
  ok: boolean | null
  error: string
  /** 最小化到后台：操作继续执行，界面收起为悬浮角标，不再遮挡页面 */
  minimized?: boolean
}

interface SiteForm {
  hostId: string
  name: string
  /** 站点名称是否跟随域名自动填充（手动改过后停止） */
  nameAuto: boolean
  group_name: string
  domains: string
  site_type: SiteType
  root_dir: string
  /** 网站根目录是否跟随域名自动填充（手动改过后停止） */
  rootAuto: boolean
  proxy_pass: string
  redirect_url: string
  ssl: boolean
}

const emptyForm = (hostId: string): SiteForm => ({
  hostId,
  name: '',
  nameAuto: true,
  group_name: '',
  domains: '',
  site_type: 'static',
  root_dir: '',
  rootAuto: true,
  proxy_pass: '',
  redirect_url: '',
  ssl: false,
})

/** 网站管理：Nginx 建站 + Let's Encrypt 证书。跨服务器浏览（App Center 全局卡片）。 */
export function WebsiteApp({ onTitle }: AppProps) {
  const t = useT()

  const [hosts, setHosts] = useState<Host[]>([])
  const [selHostId, setSelHostId] = useState('')
  const [sites, setSites] = useState<Website[]>([])
  const [groups, setGroups] = useState<string[]>([])
  const [selGroup, setSelGroup] = useState('')
  const [nginx, setNginx] = useState<NginxStatus | null>(null)
  const [nginxChecking, setNginxChecking] = useState(false)
  const [certs, setCerts] = useState<Certificate[]>([])
  const [dnsAccounts, setDnsAccounts] = useState<DnsAccount[]>([])

  const [tab, setTab] = useState<'sites' | 'certs' | 'dns'>('sites')
  const [err, setErr] = useState('')

  // 站点表单
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Website | null>(null)
  const [form, setForm] = useState<SiteForm>(emptyForm(''))
  const [formErr, setFormErr] = useState('')

  // SSL 勾选时的证书可用性检测（已安装到 /etc/nginx/ssl/<域名>/）
  const [sslCheck, setSslCheck] = useState<{ checking: boolean; installed: boolean | null; expiresAt: string }>({
    checking: false,
    installed: null,
    expiresAt: '',
  })

  // 删除确认（输入域名）
  const [deleteTarget, setDeleteTarget] = useState<Website | null>(null)
  const [deleteInput, setDeleteInput] = useState('')

  // 通用进度弹窗（Nginx 安装 / 证书签发/续签 / 部署输出）
  const [progress, setProgress] = useState<ProgressState | null>(null)

  // 签发证书表单
  const [issueOpen, setIssueOpen] = useState(false)
  const [issue, setIssue] = useState<{
    website_id: string
    domain: string
    method: 'http' | 'dns'
    dns_account_id: string
    email: string
    webroot: string
  }>({ website_id: '', domain: '', method: 'dns', dns_account_id: '', email: '', webroot: '' })

  // DNS 账户表单
  const [dnsFormOpen, setDnsFormOpen] = useState(false)
  const [dnsEditing, setDnsEditing] = useState<DnsAccount | null>(null)
  const [dnsForm, setDnsForm] = useState<{ name: string; provider: string; token: string }>({
    name: '',
    provider: 'cloudflare',
    token: '',
  })
  const [dnsFormErr, setDnsFormErr] = useState('')

  // ESC 关闭子窗口（点击遮罩关闭的弹窗也一并支持 ESC；表单类弹窗点击遮罩不关闭）
  useEscClose(formOpen, () => setFormOpen(false))
  useEscClose(!!deleteTarget, () => setDeleteTarget(null))
  useEscClose(issueOpen, () => setIssueOpen(false))
  useEscClose(dnsFormOpen, () => setDnsFormOpen(false))

  const appendLine = useCallback((l: string) => {
    setProgress((p) => (p ? { ...p, lines: p.lines + l + '\n' } : p))
  }, [])

  // 进度弹窗最小化：操作在后台继续，界面收起为右下角悬浮角标（可点击还原）
  const minimizeProgress = useCallback(() => setProgress((p) => (p ? { ...p, minimized: true } : p)), [])
  const restoreProgress = useCallback(() => setProgress((p) => (p ? { ...p, minimized: false } : p)), [])

  // ---- 数据加载 ----
  const loadSites = useCallback(async (hostId: string, group: string) => {
    if (!hostId) return
    const s = await api.websiteList(hostId, group || undefined).catch(() => [] as Website[])
    setSites(s ?? [])
  }, [])

  const loadHostData = useCallback(async (hostId: string) => {
    if (!hostId) return
    setNginxChecking(true)
    const [g, ng, c] = await Promise.all([
      api.websiteGroups(hostId).catch(() => [] as string[]),
      api.nginxStatus(hostId).catch(() => null as NginxStatus | null),
      api.certList(hostId).catch(() => [] as Certificate[]),
    ])
    setGroups(g ?? [])
    setNginx(ng)
    setCerts(c ?? [])
    setNginxChecking(false)
  }, [])

  const loadDns = useCallback(async () => {
    setDnsAccounts((await api.dnsList().catch(() => [] as DnsAccount[])) ?? [])
  }, [])

  useEffect(() => {
    onTitle?.(t('网站管理'))
    void api
      .listHosts()
      .then((h) => {
        setHosts(h)
        // 默认选中第一台非 Windows 服务器（Windows 暂不支持网站管理）
        const first = h.find((x) => x.platform !== 'windows') ?? h[0]
        if (first) setSelHostId(first.id)
      })
      .catch(() => {})
    void loadDns()
  }, [onTitle, loadDns, t])

  useEffect(() => {
    if (selHostId) void loadHostData(selHostId)
  }, [selHostId, loadHostData])

  useEffect(() => {
    if (selHostId) void loadSites(selHostId, selGroup)
  }, [selHostId, selGroup, loadSites])

  // 表单勾选 SSL 时，检测域名证书是否已安装到 /etc/nginx/ssl/<域名>/
  useEffect(() => {
    if (!formOpen || !form.ssl || !form.hostId) {
      setSslCheck((c) => (c.checking || c.installed !== null ? { checking: false, installed: null, expiresAt: '' } : c))
      return
    }
    const primary = form.domains.split(',')[0].trim()
    if (!primary) return
    let cancelled = false
    setSslCheck((c) => ({ ...c, checking: true }))
    api
      .certCheck(form.hostId, primary)
      .then((r) => {
        if (!cancelled) setSslCheck({ checking: false, installed: r.installed, expiresAt: r.expires_at })
      })
      .catch(() => {
        if (!cancelled) setSslCheck({ checking: false, installed: false, expiresAt: '' })
      })
    return () => {
      cancelled = true
    }
  }, [formOpen, form.ssl, form.hostId, form.domains])

  const changeHost = (id: string) => {
    const h = hosts.find((x) => x.id === id)
    if (h?.platform === 'windows') return // 下拉已禁用，此处兜底
    setSelHostId(id)
    setSelGroup('')
    setErr('')
  }

  const selHost = useMemo(() => hosts.find((h) => h.id === selHostId), [hosts, selHostId])
  const primaryDomain = (ws: Website) => ws.domains.split(',')[0].trim()
  /** 当前选中的是 Windows 服务器（全部主机都是 Windows 时的兜底提示） */
  const winSelected = selHost?.platform === 'windows'

  /** Windows 服务器是否可选（下拉禁用 + 悬停提示） */
  const hostOption = (h: Host) => {
    const win = h.platform === 'windows'
    return (
      <option key={h.id} value={h.id} disabled={win} title={win ? t('Windows 暂不支持') : undefined}>
        {h.name}（{h.host}）
        {win ? ` · ${t('Windows 暂不支持')}` : ''}
      </option>
    )
  }

  // ---- 站点 CRUD ----
  const openAdd = () => {
    setEditing(null)
    setForm(emptyForm(selHostId))
    setFormErr('')
    setFormOpen(true)
  }

  const openEdit = (ws: Website) => {
    setEditing(ws)
    setForm({
      hostId: ws.hostId,
      name: ws.name,
      nameAuto: false,
      group_name: ws.group_name,
      domains: ws.domains,
      site_type: ws.site_type,
      root_dir: ws.root_dir,
      rootAuto: false,
      proxy_pass: ws.proxy_pass,
      redirect_url: ws.redirect_url,
      ssl: ws.ssl,
    })
    setFormErr('')
    setFormOpen(true)
  }

  const runProgress = useCallback(
    (title: string, start: (onLine: (l: string) => void) => Promise<unknown>): Promise<boolean> => {
      setProgress({ title, lines: '', running: true, ok: null, error: '' })
      return start(appendLine)
        .then(() => {
          setProgress((p) => {
            if (!p) return p
            // 已最小化且成功 → 自动关闭角标；未最小化 → 保留窗口显示 ✔
            if (p.minimized) return null
            return { ...p, running: false, ok: true }
          })
          return true
        })
        .catch((e: unknown) => {
          const msg = transErr(e, t('操作失败'))
          setProgress((p) => {
            if (!p) return p
            // 失败 → 无论是否最小化都还原完整窗口，让用户看到错误
            return { ...p, running: false, ok: false, minimized: false, error: msg }
          })
          return false
        })
    },
    [appendLine, t],
  )

  const installNginx = async (hostId: string): Promise<boolean> => {
    const ok = await runProgress(t('正在安装 Nginx…'), (onLine) => api.nginxInstall(hostId, onLine))
    if (ok) {
      setNginx({ installed: true, version: '', running: true })
      if (hostId === selHostId) void loadHostData(hostId)
    }
    return ok
  }

  const saveSite = async () => {
    setFormErr('')
    const input = {
      hostId: form.hostId,
      name: form.name.trim(),
      group_name: form.group_name.trim(),
      domains: form.domains.trim(),
      site_type: form.site_type,
      root_dir: form.root_dir.trim(),
      proxy_pass: form.proxy_pass.trim(),
      redirect_url: form.redirect_url.trim(),
      ssl: form.ssl,
      enabled: true,
    }
    if (!input.name || !input.domains) {
      setFormErr(t('请填写站点名称与域名'))
      return
    }
    const targetHost = hosts.find((x) => x.id === input.hostId)
    if (targetHost?.platform === 'windows') {
      setFormErr(t('Windows 服务器暂不支持网站管理'))
      return
    }
    if (input.site_type === 'proxy' && !/^https?:\/\//.test(input.proxy_pass)) {
      setFormErr(t('反向代理地址需以 http:// 或 https:// 开头'))
      return
    }
    if (input.site_type === 'redirect' && !/^https?:\/\//.test(input.redirect_url)) {
      setFormErr(t('重定向目标需以 http:// 或 https:// 开头'))
      return
    }
    try {
      const saved = editing
        ? await api.updateWebsite(editing.id, input)
        : await api.createWebsite(input)
      setFormOpen(false)
      setEditing(null)
      if (saved.hostId !== selHostId) {
        await loadHostData(saved.hostId)
        setSelHostId(saved.hostId)
        setSelGroup('')
      } else {
        await loadSites(saved.hostId, '')
      }
      // 若该服务器无 Nginx：询问是否一键安装，然后部署
      const ng = await api.nginxStatus(saved.hostId).catch(() => null)
      if (ng && !ng.installed) {
        if (!window.confirm(t('该服务器未安装 Nginx，是否一键安装？'))) return
        const okInstall = await installNginx(saved.hostId)
        if (!okInstall) return
      }
      await deploySite(saved)
    } catch (e) {
      setFormErr(e instanceof ApiError ? e.message : t('保存失败'))
    }
  }

  const deploySite = async (ws: Website) => {
    setProgress({ title: t('部署网站 {0}', ws.name), lines: '', running: true, ok: null, error: '' })
    try {
      const res = await api.deployWebsite(ws.id, currentLang())
      let lines = res.output || ''
      if (res.warning) lines = lines + '\n[警告] ' + res.warning
      if (!res.ok && res.error) lines = lines + '\n' + res.error
      setProgress((p) => {
        if (!p) return p
        // 已最小化且成功 → 自动关闭角标
        if (p.minimized && res.ok) return null
        return { ...p, running: false, ok: res.ok, lines }
      })
      await loadSites(ws.hostId, '')
    } catch (e) {
      setProgress((p) => {
        if (!p) return p
        // 失败 → 还原完整窗口展示错误
        return { ...p, running: false, ok: false, minimized: false, error: transErr(e, t('部署失败')) }
      })
    }
  }

  /** 打开文件管理器并进入站点目录（静态站根目录 / 自定义 root_dir） */
  const openFmForSite = async (site: Website) => {
    const dir = site.site_type === 'static'
      ? (site.root_dir?.trim() || `/opt/ezssh/apps/WebManager/www/${primaryDomain(site)}`)
      : `/opt/ezssh/apps/WebManager/www/${primaryDomain(site)}`
    try {
      await ws.connect()
    } catch {
      /* 忽略 */
    }
    setPendingFmCwd(site.hostId, dir)
    const host = hosts.find((h) => h.id === site.hostId)
    openAppWindow('files', host)
  }

  const toggleSite = async (ws: Website) => {
    try {
      await api.toggleWebsite(ws.id, !ws.enabled, currentLang())
      await loadSites(ws.hostId, '')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : t('操作失败'))
    }
  }

  const doDelete = async () => {
    if (!deleteTarget) return
    const target = deleteTarget
    setDeleteTarget(null)
    setDeleteInput('')
    try {
      const res = await api.deleteWebsite(target.id)
      if (res.warnings && res.warnings.length > 0) {
        setProgress({
          title: t('删除网站 {0}', target.name),
          lines: res.warnings.join('\n'),
          running: false,
          ok: true,
          error: '',
        })
      }
      await loadSites(target.hostId, '')
      void loadHostData(target.hostId)
      void loadDns()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : t('删除失败'))
    }
  }

  // ---- 证书 ----
  const openIssue = () => {
    setIssue({
      website_id: '',
      domain: '',
      method: 'dns',
      dns_account_id: dnsAccounts.length > 0 ? dnsAccounts[0].id : '',
      email: '',
      webroot: '',
    })
    setIssueOpen(true)
  }

  const pickIssueSite = (websiteId: string) => {
    const ws = sites.find((s) => s.id === websiteId)
    setIssue((prev) => ({
      ...prev,
      website_id: websiteId,
      domain: ws ? primaryDomain(ws) : prev.domain,
      webroot: ws && ws.site_type === 'static' ? ws.root_dir : prev.webroot,
    }))
  }

  const issueCert = async () => {
    if (!issue.domain.trim()) return
    const ws = sites.find((s) => s.id === issue.website_id)
    const ok = await runProgress(t('正在签发证书 {0}…', issue.domain.trim()), (onLine) =>
      api.certIssue(
        {
          host_id: selHostId,
          website_id: ws ? ws.id : undefined,
          domain: issue.domain.trim(),
          method: issue.method,
          dns_account_id: issue.method === 'dns' ? issue.dns_account_id : '',
          email: issue.email.trim(),
          webroot: issue.webroot.trim(),
        },
        onLine,
      ),
    )
    if (ok) {
      setIssueOpen(false)
      void loadHostData(selHostId)
      void loadSites(selHostId, selGroup)
    }
  }

  const renewCert = async (c: Certificate) => {
    const ok = await runProgress(t('正在续签证书 {0}…', c.domain), (onLine) => api.certRenew(c.id, onLine))
    if (ok) void loadHostData(selHostId)
  }

  const syncCert = async (c: Certificate) => {
    try {
      await api.certSync(c.id)
      await loadHostData(selHostId)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : t('同步失败'))
    }
  }

  const deleteCert = async (c: Certificate) => {
    if (!window.confirm(t('删除证书记录「{0}」？远端 acme.sh 证书保留。', c.domain))) return
    try {
      await api.certDelete(c.id)
      await loadHostData(selHostId)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : t('删除失败'))
    }
  }

  const certStatusLabel = (s: string) =>
    s === 'active' ? t('有效') : s === 'issuing' ? t('签发中') : s === 'renewing' ? t('续签中') : t('错误')
  const certStatusColor = (s: string) =>
    s === 'active' ? '#34d399' : s === 'issuing' || s === 'renewing' ? '#fbbf24' : 'var(--red)'

  // ---- DNS 账户 ----
  const openDnsForm = (a: DnsAccount | null) => {
    setDnsEditing(a)
    setDnsForm({ name: a?.name ?? '', provider: a?.provider ?? 'cloudflare', token: '' })
    setDnsFormErr('')
    setDnsFormOpen(true)
  }

  const saveDns = async () => {
    setDnsFormErr('')
    if (!dnsForm.name.trim()) {
      setDnsFormErr(t('请填写账户名称'))
      return
    }
    try {
      const input = { name: dnsForm.name.trim(), provider: dnsForm.provider, token: dnsForm.token || undefined }
      if (dnsEditing) await api.dnsUpdate(dnsEditing.id, input)
      else await api.dnsCreate(input)
      setDnsFormOpen(false)
      setDnsEditing(null)
      await loadDns()
    } catch (e) {
      setDnsFormErr(e instanceof ApiError ? e.message : t('保存失败'))
    }
  }

  const deleteDns = async (a: DnsAccount) => {
    if (!window.confirm(t('删除 DNS 账户「{0}」？', a.name))) return
    try {
      await api.dnsDelete(a.id)
      await loadDns()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : t('删除失败'))
    }
  }

  // ---- 渲染辅助 ----
  const typeLabel = (ty: SiteType) =>
    ty === 'static' ? t('静态网站') : ty === 'proxy' ? t('反向代理') : t('重定向')
  const typeColor = (ty: SiteType) =>
    ty === 'static' ? 'var(--cyan)' : ty === 'proxy' ? '#60a5fa' : 'var(--yellow)'

  const thStyle = {
    textAlign: 'left' as const,
    padding: '8px 10px',
    fontSize: 12,
    color: 'var(--text-1)',
    borderBottom: '1px solid rgba(var(--rgb-line),0.15)',
    whiteSpace: 'nowrap' as const,
  }
  const tdStyle = {
    padding: '8px 10px',
    fontSize: 13,
    borderBottom: '1px solid rgba(var(--rgb-line),0.08)',
  }

  const tabBtn = (key: typeof tab, label: string, count?: number) => (
    <button
      onClick={() => {
        setTab(key)
        setErr('')
        if (key === 'dns') void loadDns()
      }}
      style={{
        padding: '8px 16px',
        borderRadius: 8,
        border: 'none',
        cursor: 'pointer',
        fontSize: 13,
        background: tab === key ? 'rgba(var(--rgb-primary),0.22)' : 'transparent',
        color: tab === key ? 'var(--text-0)' : 'var(--text-1)',
        fontWeight: tab === key ? 600 : 400,
      }}
    >
      {label}
      {count != null && count > 0 && <span style={{ marginLeft: 6, color: '#fbbf24' }}>{count}</span>}
    </button>
  )

  const serverSelect = (
    <select
      value={selHostId}
      onChange={(e) => changeHost(e.target.value)}
      style={{ padding: '7px 10px', borderRadius: 8, border: '1px solid rgba(var(--rgb-line),0.25)', background: 'var(--bg-1)', color: 'var(--text-0)', fontSize: 13 }}
    >
      {hosts.map(hostOption)}
    </select>
  )

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', background: 'rgba(var(--rgb-appbg),0.9)', color: 'var(--text-0)' }}>
      {/* 页签 */}
      <div style={{ display: 'flex', gap: 4, padding: '10px 14px 0', borderBottom: '1px solid rgba(var(--rgb-line),0.15)' }}>
        {tabBtn('sites', t('网站'))}
        {tabBtn('certs', t('证书'))}
        {tabBtn('dns', t('DNS 账户'), dnsAccounts.length)}
      </div>

      <div style={{ flex: 1, overflow: 'auto', padding: 14 }}>
        {err && (
          <div style={{ marginBottom: 10, padding: '8px 12px', borderRadius: 8, background: 'rgba(239,68,68,0.12)', color: 'var(--red)', fontSize: 13 }}>
            {err}
          </div>
        )}
        {winSelected && (
          <div
            style={{
              marginBottom: 12,
              padding: '10px 14px',
              borderRadius: 10,
              background: 'rgba(148,163,184,0.1)',
              border: '1px solid rgba(var(--rgb-line),0.2)',
              fontSize: 13,
              color: 'var(--text-1)',
            }}
          >
            {t('所选服务器为 Windows，暂不支持网站管理。请在左侧选择一台 Linux 服务器。')}
          </div>
        )}

        {/* 顶栏：服务器 + Nginx 状态 + 分组 */}
        <div style={{ display: 'flex', gap: 10, alignItems: 'center', marginBottom: 12, flexWrap: 'wrap' }}>
          <span style={{ fontSize: 13, color: 'var(--text-1)' }}>{t('服务器')}</span>
          {serverSelect}
          {tab !== 'dns' && selHostId && (
            nginxChecking ? (
              <span style={{ fontSize: 12, color: 'var(--text-1)' }}>{t('检测 Nginx 状态…')}</span>
            ) : nginx && nginx.installed ? (
              <span style={{ fontSize: 12, color: '#34d399' }}>{t('✅ Nginx {0}', nginx.version || t('已安装'))}</span>
            ) : nginx && !nginx.installed ? (
              <span style={{ fontSize: 12, color: 'var(--yellow)' }}>{t('❌ Nginx 未安装')}</span>
            ) : null
          )}
          {tab === 'sites' && (
            <>
              <span style={{ fontSize: 13, color: 'var(--text-1)' }}>{t('分组')}</span>
              <select
                value={selGroup}
                onChange={(e) => setSelGroup(e.target.value)}
                style={{ padding: '7px 10px', borderRadius: 8, border: '1px solid rgba(var(--rgb-line),0.25)', background: 'var(--bg-1)', color: 'var(--text-0)', fontSize: 13 }}
              >
                <option value="">{t('全部分组')}</option>
                {groups.map((g) => (
                  <option key={g} value={g}>
                    {g}
                  </option>
                ))}
              </select>
              <div style={{ flex: 1 }} />
              <button className="btn" onClick={openAdd} style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                <span>➕</span> {t('添加网站')}
              </button>
            </>
          )}
          {tab === 'certs' && (
            <>
              <div style={{ flex: 1 }} />
              <button className="btn btn-ghost" onClick={openIssue}>
                {t('签发证书')}
              </button>
            </>
          )}
        </div>

        {/* Nginx 未安装横幅 */}
        {tab !== 'dns' && selHostId && nginx && !nginx.installed && (
          <div
            style={{
              marginBottom: 12,
              padding: '10px 14px',
              borderRadius: 10,
              background: 'rgba(251,191,36,0.1)',
              border: '1px solid rgba(251,191,36,0.3)',
              fontSize: 13,
              display: 'flex',
              alignItems: 'center',
              gap: 12,
            }}
          >
            <span style={{ color: 'var(--yellow)' }}>⚠️ {t('服务器「{0}」尚未安装 Nginx。', selHost?.name ?? '')}</span>
            <button className="btn" onClick={() => void installNginx(selHostId)}>
              {t('一键安装 Nginx')}
            </button>
          </div>
        )}

        {tab === 'sites' && (
          <>
            {sites.length === 0 ? (
              <div className="empty-tip" style={{ padding: 40, textAlign: 'center', color: 'var(--text-1)' }}>
                {t('暂无网站，点击右上角「添加网站」开始建站。')}
              </div>
            ) : (
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    <th style={thStyle}>{t('域名')}</th>
                    <th style={thStyle}>{t('类型')}</th>
                    <th style={thStyle}>{t('分组')}</th>
                    <th style={thStyle}>{t('SSL')}</th>
                    <th style={thStyle}>{t('状态')}</th>
                    <th style={{ ...thStyle, textAlign: 'right' }}>{t('操作')}</th>
                  </tr>
                </thead>
                <tbody>
                  {sites.map((ws) => (
                    <tr key={ws.id}>
                      <td style={tdStyle}>
                        <a
                          href={`${ws.ssl ? 'https' : 'http'}://${primaryDomain(ws)}`}
                          target="_blank"
                          rel="noreferrer"
                          style={{ color: 'var(--primary)', textDecoration: 'none' }}
                          title={t('在浏览器打开：{0}', primaryDomain(ws))}
                        >
                          {primaryDomain(ws)}
                        </a>
                        {ws.domains.includes(',') && (
                          <span style={{ marginLeft: 6, fontSize: 11, color: 'var(--text-1)' }}>
                            +{ws.domains.split(',').length - 1}
                          </span>
                        )}
                      </td>
                      <td style={tdStyle}>
                        <span className="tag" style={{ color: typeColor(ws.site_type) }}>
                          {typeLabel(ws.site_type)}
                        </span>
                      </td>
                      <td style={tdStyle}>
                        {ws.group_name ? (
                          <span className="tag">{ws.group_name}</span>
                        ) : (
                          <span style={{ color: 'var(--text-1)', fontSize: 12 }}>—</span>
                        )}
                      </td>
                      <td style={tdStyle}>
                        {ws.ssl ? (
                          <span className="tag" style={{ color: '#34d399' }}>
                            🔒 SSL
                          </span>
                        ) : (
                          <span style={{ color: 'var(--text-1)', fontSize: 12 }}>—</span>
                        )}
                      </td>
                      <td style={tdStyle}>
                        <span style={{ color: ws.enabled ? '#34d399' : 'var(--text-1)' }}>
                          {ws.enabled ? t('启用') : t('停用')}
                        </span>
                      </td>
                      <td style={{ ...tdStyle, textAlign: 'right' }} className="row-actions">
                        <button className="btn btn-sm btn-ghost" onClick={() => void openFmForSite(ws)}>
                          {t('查看文件')}
                        </button>
                        <button className="btn btn-sm btn-ghost" onClick={() => openEdit(ws)}>
                          {t('编辑')}
                        </button>
                        <button className="btn btn-sm btn-ghost" onClick={() => toggleSite(ws)}>
                          {ws.enabled ? t('停用') : t('启用')}
                        </button>
                        <button className="btn btn-sm btn-ghost" onClick={() => deploySite(ws)}>
                          {t('部署')}
                        </button>
                        <button className="btn btn-sm btn-danger" onClick={() => { setDeleteTarget(ws); setDeleteInput('') }}>
                          {t('删除')}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </>
        )}

        {tab === 'certs' && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            {certs.length === 0 ? (
              <div className="empty-tip" style={{ padding: 40, textAlign: 'center', color: 'var(--text-1)' }}>
                {t("暂无证书。点击「签发证书」为网站申请 Let's Encrypt 证书。")}
              </div>
            ) : (
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    <th style={thStyle}>{t('域名')}</th>
                    <th style={thStyle}>{t('方式')}</th>
                    <th style={thStyle}>{t('状态')}</th>
                    <th style={thStyle}>{t('到期时间')}</th>
                    <th style={{ ...thStyle, textAlign: 'right' }}>{t('操作')}</th>
                  </tr>
                </thead>
                <tbody>
                  {certs.map((c) => (
                    <tr key={c.id}>
                      <td style={tdStyle}>{c.domain}</td>
                      <td style={tdStyle}>
                        <span className="tag">{c.method === 'dns' ? 'DNS-01' : 'HTTP-01'}</span>
                      </td>
                      <td style={tdStyle}>
                        <span style={{ color: certStatusColor(c.status) }}>{certStatusLabel(c.status)}</span>
                      </td>
                      <td style={tdStyle}>
                        {c.expires_at ? (
                          <span style={{ fontSize: 13 }}>{c.expires_at.slice(0, 10)}</span>
                        ) : (
                          <span style={{ color: 'var(--text-1)', fontSize: 12 }}>—</span>
                        )}
                      </td>
                      <td style={{ ...tdStyle, textAlign: 'right' }} className="row-actions">
                        <button className="btn btn-sm btn-ghost" onClick={() => renewCert(c)}>
                          {t('续签')}
                        </button>
                        <button className="btn btn-sm btn-ghost" onClick={() => syncCert(c)}>
                          {t('同步')}
                        </button>
                        <button className="btn btn-sm btn-danger" onClick={() => deleteCert(c)}>
                          {t('删除')}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            {certs.some((c) => c.error) && (
              <div style={{ fontSize: 12, color: 'var(--text-1)' }}>
                {certs
                  .filter((c) => c.error)
                  .map((c) => (
                    <div key={c.id} style={{ marginTop: 4, color: 'var(--red)' }}>
                      {c.domain}: {c.error}
                    </div>
                  ))}
              </div>
            )}
          </div>
        )}

        {tab === 'dns' && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            <div style={{ display: 'flex', alignItems: 'center' }}>
              <span style={{ fontSize: 13, color: 'var(--text-1)' }}>
                {t('DNS 验证账户用于签发证书（如 Cloudflare API Token）。')}
              </span>
              <div style={{ flex: 1 }} />
              <button className="btn" onClick={() => openDnsForm(null)}>
                {t('新增账户')}
              </button>
            </div>
            {dnsAccounts.length === 0 ? (
              <div className="empty-tip" style={{ padding: 40, textAlign: 'center', color: 'var(--text-1)' }}>
                {t('暂无 DNS 账户。')}
              </div>
            ) : (
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    <th style={thStyle}>{t('名称')}</th>
                    <th style={thStyle}>{t('服务商')}</th>
                    <th style={thStyle}>{t('Token')}</th>
                    <th style={{ ...thStyle, textAlign: 'right' }}>{t('操作')}</th>
                  </tr>
                </thead>
                <tbody>
                  {dnsAccounts.map((a) => (
                    <tr key={a.id}>
                      <td style={tdStyle}>{a.name}</td>
                      <td style={tdStyle}>
                        <span className="tag">Cloudflare</span>
                      </td>
                      <td style={tdStyle}>
                        <span style={{ color: a.has_token ? '#34d399' : 'var(--red)' }}>
                          {a.has_token ? t('已配置') : t('未配置')}
                        </span>
                      </td>
                      <td style={{ ...tdStyle, textAlign: 'right' }} className="row-actions">
                        <button className="btn btn-sm btn-ghost" onClick={() => openDnsForm(a)}>
                          {t('编辑')}
                        </button>
                        <button className="btn btn-sm btn-danger" onClick={() => deleteDns(a)}>
                          {t('删除')}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </div>

      {/* ---- 添加/编辑网站弹窗（点击遮罩不关闭，防止误触丢失输入） ---- */}
      {formOpen && (
        <div className="modal-mask">
          <div className="modal" style={{ width: 560 }} onClick={(e) => e.stopPropagation()}>
            <h3>{editing ? t('编辑网站') : t('添加网站')}</h3>
            <div className="field" style={{ marginBottom: 12 }}>
              <label>{t('服务器')}</label>
              <select
                value={form.hostId}
                disabled={!!editing}
                onChange={(e) => setForm((f) => ({ ...f, hostId: e.target.value }))}
              >
                {hosts.map(hostOption)}
              </select>
              {!editing && <div style={{ fontSize: 12, color: 'var(--text-1)', marginTop: 4 }}>{t('选择要部署到的服务器。若未安装 Nginx，保存后会提示一键安装。Windows 服务器暂不支持。')}</div>}
            </div>
            <div className="field" style={{ marginBottom: 12 }}>
              <label>{t('域名')}</label>
              <input
                value={form.domains}
                onChange={(e) => {
                  const domains = e.target.value
                  setForm((f) => {
                    const primary = domains.split(',')[0].trim()
                    return {
                      ...f,
                      domains,
                      name: f.nameAuto ? primary : f.name,
                      root_dir: f.rootAuto ? (primary ? `/opt/ezssh/apps/WebManager/www/${primary}` : '') : f.root_dir,
                    }
                  })
                }}
                placeholder={t('example.com（多个用逗号分隔）')}
              />
            </div>
            <div className="field" style={{ marginBottom: 12 }}>
              <label>{t('站点名称')}</label>
              <input
                value={form.name}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value, nameAuto: false }))}
                placeholder={t('如：我的博客（默认自动取域名）')}
              />
            </div>
            <div className="field" style={{ marginBottom: 12 }}>
              <label>{t('分组')}</label>
              <input value={form.group_name} onChange={(e) => setForm((f) => ({ ...f, group_name: e.target.value }))} placeholder={t('如：生产环境')} />
            </div>
            <div className="field" style={{ marginBottom: 12 }}>
              <label>{t('类型')}</label>
              <select
                value={form.site_type}
                onChange={(e) => setForm((f) => ({ ...f, site_type: e.target.value as SiteType }))}
              >
                <option value="static">{t('静态网站')}</option>
                <option value="proxy">{t('反向代理')}</option>
                <option value="redirect">{t('重定向')}</option>
              </select>
            </div>

            {form.site_type === 'static' && (
              <div className="field" style={{ marginBottom: 12 }}>
                <label>{t('网站根目录')}</label>
                <input
                  value={form.root_dir}
                  onChange={(e) => setForm((f) => ({ ...f, root_dir: e.target.value, rootAuto: false }))}
                  placeholder={t('默认 /opt/ezssh/apps/WebManager/www/域名')}
                />
              </div>
            )}
            {form.site_type === 'proxy' && (
              <div className="field" style={{ marginBottom: 12 }}>
                <label>{t('反向代理地址')}</label>
                <input
                  value={form.proxy_pass}
                  onChange={(e) => setForm((f) => ({ ...f, proxy_pass: e.target.value }))}
                  placeholder={t('如 http://127.0.0.1:8080')}
                />
              </div>
            )}
            {form.site_type === 'redirect' && (
              <div className="field" style={{ marginBottom: 12 }}>
                <label>{t('重定向目标')}</label>
                <input
                  value={form.redirect_url}
                  onChange={(e) => setForm((f) => ({ ...f, redirect_url: e.target.value }))}
                  placeholder={t('如 https://new.example.com')}
                />
              </div>
            )}

            <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, marginBottom: 8, cursor: 'pointer' }}>
              <input type="checkbox" checked={form.ssl} onChange={(e) => setForm((f) => ({ ...f, ssl: e.target.checked }))} />
              {t("启用 SSL（需先在「证书」页签为域名签发 Let's Encrypt 证书）")}
            </label>
            {form.ssl && (
              <div style={{ fontSize: 12, marginBottom: 12, lineHeight: 1.5 }}>
                {sslCheck.checking ? (
                  <span style={{ color: 'var(--text-dim)' }}>{t('检测证书状态…')}</span>
                ) : sslCheck.installed ? (
                  <span style={{ color: '#34d399' }}>
                    ✅ {t('证书已安装，到期时间 {0}', sslCheck.expiresAt || t('未知'))}
                  </span>
                ) : (
                  <span style={{ color: 'var(--red)' }}>
                    ❌ {t('该域名证书未安装到 /etc/nginx/ssl/域名/，请先在「证书」页签签发或续签，否则部署时将降级为 HTTP。')}
                  </span>
                )}
              </div>
            )}

            {formErr && <div className="error-text" style={{ color: 'var(--red)', fontSize: 13, marginBottom: 10 }}>{formErr}</div>}
            <div className="footer">
              <button className="btn btn-ghost" onClick={() => setFormOpen(false)}>
                {t('取消')}
              </button>
              <button className="btn" onClick={() => void saveSite()}>
                {editing ? t('保存') : t('创建并部署')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ---- 删除确认（需输入域名） ---- */}
      {deleteTarget && (
        <div className="modal-mask" onClick={() => setDeleteTarget(null)}>
          <div className="modal" style={{ width: 480 }} onClick={(e) => e.stopPropagation()}>
            <h3>{t('删除网站')}</h3>
            <div style={{ fontSize: 13, color: 'var(--text-1)', marginBottom: 8 }}>
              {t('确定删除网站「{0}」？此操作会移除服务器上的 Nginx 配置并清理证书，且不可恢复。', deleteTarget.name)}
            </div>
            <div style={{ fontSize: 13, color: 'var(--text-1)', marginBottom: 12 }}>
              {t('请输入域名 {0} 以确认删除：', primaryDomain(deleteTarget))}
            </div>
            <input
              value={deleteInput}
              onChange={(e) => setDeleteInput(e.target.value)}
              placeholder={primaryDomain(deleteTarget)}
              style={{
                width: '100%',
                padding: '9px 12px',
                borderRadius: 8,
                border: '1px solid rgba(var(--rgb-line),0.25)',
                background: 'var(--bg-1)',
                color: 'var(--text-0)',
                fontSize: 14,
              }}
            />
            <div className="footer">
              <button className="btn btn-ghost" onClick={() => setDeleteTarget(null)}>
                {t('取消')}
              </button>
              <button
                className="btn btn-danger"
                disabled={deleteInput.trim() !== primaryDomain(deleteTarget)}
                onClick={() => void doDelete()}
                style={{ opacity: deleteInput.trim() === primaryDomain(deleteTarget) ? 1 : 0.5, cursor: deleteInput.trim() === primaryDomain(deleteTarget) ? 'pointer' : 'not-allowed' }}
              >
                {t('确定删除')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ---- 签发证书弹窗 ---- */}
      {issueOpen && (
        <div className="modal-mask" onClick={() => setIssueOpen(false)}>
          <div className="modal" style={{ width: 520 }} onClick={(e) => e.stopPropagation()}>
            <h3>{t("签发 Let's Encrypt 证书")}</h3>
            <div className="field" style={{ marginBottom: 12 }}>
              <label>{t('关联网站（可选）')}</label>
              <select value={issue.website_id} onChange={(e) => pickIssueSite(e.target.value)}>
                <option value="">{t('自定义域名')}</option>
                {sites.map((s) => (
                  <option key={s.id} value={s.id}>
                    {primaryDomain(s)}（{s.name}）
                  </option>
                ))}
              </select>
            </div>
            <div className="field" style={{ marginBottom: 12 }}>
              <label>{t('域名')}</label>
              <input
                value={issue.domain}
                onChange={(e) => setIssue((i) => ({ ...i, domain: e.target.value }))}
                placeholder={t('example.com')}
              />
            </div>
            <div className="field" style={{ marginBottom: 12 }}>
              <label>{t('验证方式')}</label>
              <select
                value={issue.method}
                onChange={(e) => setIssue((i) => ({ ...i, method: e.target.value as 'http' | 'dns' }))}
              >
                <option value="dns">{t('DNS 验证（Cloudflare，推荐）')}</option>
                <option value="http">{t('HTTP 验证（需服务器 80 端口公网可达）')}</option>
              </select>
            </div>
            {issue.method === 'dns' ? (
              <div className="field" style={{ marginBottom: 12 }}>
                <label>{t('DNS 账户')}</label>
                <select value={issue.dns_account_id} onChange={(e) => setIssue((i) => ({ ...i, dns_account_id: e.target.value }))}>
                  {dnsAccounts.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.name}（Cloudflare）
                    </option>
                  ))}
                </select>
              </div>
            ) : (
              <div className="field" style={{ marginBottom: 12 }}>
                <label>{t('网站根目录（webroot）')}</label>
                <input
                  value={issue.webroot}
                  onChange={(e) => setIssue((i) => ({ ...i, webroot: e.target.value }))}
                  placeholder={t('如 /opt/ezssh/apps/WebManager/www/example.com')}
                />
              </div>
            )}
            <div className="field" style={{ marginBottom: 12 }}>
              <label>{t('注册邮箱（可选）')}</label>
              <input
                value={issue.email}
                onChange={(e) => setIssue((i) => ({ ...i, email: e.target.value }))}
                placeholder={t("用于 Let's Encrypt 到期提醒")}
              />
            </div>
            <div className="footer">
              <button className="btn btn-ghost" onClick={() => setIssueOpen(false)}>
                {t('取消')}
              </button>
              <button className="btn" disabled={!issue.domain.trim()} onClick={() => void issueCert()}>
                {t('签发')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ---- DNS 账户表单 ---- */}
      {dnsFormOpen && (
        <div className="modal-mask" onClick={() => setDnsFormOpen(false)}>
          <div className="modal" style={{ width: 460 }} onClick={(e) => e.stopPropagation()}>
            <h3>{dnsEditing ? t('编辑 DNS 账户') : t('新增 DNS 账户')}</h3>
            <div className="field" style={{ marginBottom: 12 }}>
              <label>{t('账户名称')}</label>
              <input value={dnsForm.name} onChange={(e) => setDnsForm((f) => ({ ...f, name: e.target.value }))} placeholder={t('如：我的 Cloudflare')} />
            </div>
            <div className="field" style={{ marginBottom: 12 }}>
              <label>{t('服务商')}</label>
              <select value={dnsForm.provider} onChange={(e) => setDnsForm((f) => ({ ...f, provider: e.target.value }))}>
                <option value="cloudflare">Cloudflare</option>
              </select>
            </div>
            <div className="field" style={{ marginBottom: 12 }}>
              <label>{dnsEditing ? t('API Token（留空则保持不变）') : t('API Token')}</label>
              <input
                type="password"
                value={dnsForm.token}
                onChange={(e) => setDnsForm((f) => ({ ...f, token: e.target.value }))}
                placeholder={t('Cloudflare API Token（需 DNS 编辑权限）')}
              />
            </div>
            {dnsFormErr && <div style={{ color: 'var(--red)', fontSize: 13, marginBottom: 10 }}>{dnsFormErr}</div>}
            <div className="footer">
              <button className="btn btn-ghost" onClick={() => setDnsFormOpen(false)}>
                {t('取消')}
              </button>
              <button className="btn" onClick={() => void saveDns()}>
                {t('保存')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ---- 进度/输出弹窗（可最小化到后台） ---- */}
      {progress && progress.minimized && (
        <div
          className="progress-badge"
          onClick={restoreProgress}
          style={{
            position: 'fixed',
            right: 20,
            bottom: 20,
            zIndex: 300,
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            maxWidth: 320,
            padding: '8px 14px',
            background: 'rgba(24,26,31,0.95)',
            border: '1px solid rgba(var(--rgb-line),0.25)',
            borderRadius: 10,
            boxShadow: '0 8px 28px rgba(0,0,0,0.5)',
            cursor: 'pointer',
            fontSize: 13,
            color: '#d4d4d4',
          }}
          title={t('点击还原进度窗口')}
        >
          {progress.running ? (
            <span style={{ color: '#fbbf24' }}>⏳</span>
          ) : progress.ok ? (
            <span style={{ color: '#34d399' }}>✔</span>
          ) : (
            <span style={{ color: 'var(--red)' }}>✘</span>
          )}
          <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{progress.title}</span>
          <span style={{ marginLeft: 4, color: 'var(--text-dim)' }}>
            {progress.running ? t('运行中…') : t('已完成')}
          </span>
        </div>
      )}
      {progress && !progress.minimized && (
        <div className="modal-mask">
          <div className="modal" style={{ width: 640 }} onClick={(e) => e.stopPropagation()}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <h3 style={{ margin: 0 }}>
                {progress.title}
                {progress.running && <span style={{ marginLeft: 8, fontSize: 13, color: '#fbbf24' }}>{t('运行中…')}</span>}
                {!progress.running && progress.ok && <span style={{ marginLeft: 8, fontSize: 13, color: '#34d399' }}>✔</span>}
                {!progress.running && progress.ok === false && <span style={{ marginLeft: 8, fontSize: 13, color: 'var(--red)' }}>✘</span>}
              </h3>
              <button
                onClick={minimizeProgress}
                title={t('最小化到后台，操作继续执行')}
                style={{
                  minWidth: 26,
                  height: 26,
                  borderRadius: 6,
                  border: '1px solid rgba(var(--rgb-line),0.2)',
                  background: 'rgba(255,255,255,0.06)',
                  color: 'var(--text-dim)',
                  fontSize: 14,
                  lineHeight: 1,
                  cursor: 'pointer',
                }}
              >
                ─
              </button>
            </div>
            <pre
              style={{
                height: 320,
                overflow: 'auto',
                background: 'rgba(0,0,0,0.35)',
                border: '1px solid rgba(var(--rgb-line),0.15)',
                borderRadius: 10,
                padding: 12,
                fontSize: 12,
                lineHeight: 1.6,
                color: '#d4d4d4',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
                margin: '12px 0 0 0',
              }}
            >
              {progress.lines}
              {progress.error && <span style={{ color: 'var(--red)' }}>{progress.error}</span>}
            </pre>
            <div className="footer">
              {!progress.running && (
                <button className="btn" onClick={() => setProgress(null)}>
                  {t('关闭')}
                </button>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
