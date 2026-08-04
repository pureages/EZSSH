import { useCallback, useEffect, useRef, useState } from 'react'
import { ws } from '../lib/ws'
import { useT, tt, transErr } from '../lib/i18n'
import type { AppProps } from '../desktop/appRegistry'

interface FirewallStatus {
  supported: boolean
  backend: string
  active: boolean
  version: string
  sshPort: string
}

interface FirewallRule {
  id: string
  action: string // allow | deny | reject
  proto: string // tcp | udp | any
  port: string // "8080" | "49000:50000" | "any"
  from: string // 来源 IP | "any"
  description: string
}

/** 规则动作显示名 */
function actionLabel(action: string): string {
  switch (action) {
    case 'allow':
      return tt('允许')
    case 'deny':
      return tt('禁止')
    case 'reject':
      return tt('拒绝')
    default:
      return action
  }
}

/** 协议显示名 */
function protoLabel(proto: string): string {
  switch (proto) {
    case 'tcp':
      return tt('TCP')
    case 'udp':
      return tt('UDP')
    case 'any':
      return tt('不限')
    default:
      return proto
  }
}

/** 将解析出的规则转为可读描述 */
function describeRule(r: FirewallRule): string {
  const action = actionLabel(r.action)
  const proto = r.proto && r.proto !== 'any' ? tt('（{0}）', r.proto.toUpperCase()) : ''
  const parts: string[] = [action]
  if (r.port && r.port !== 'any') {
    parts.push(tt('端口 {0}', r.port))
  }
  if (r.from && r.from !== 'any') {
    parts.push(tt('来源 {0}', r.from))
  } else if (!r.port || r.port === 'any') {
    parts.push(tt('全部流量'))
  }
  return parts.join(' ') + proto
}

const reqSeq = { n: 0 }

/**
 * 防火墙管理器 App：查看/打开/关闭目标机防火墙，维护 IP / 端口（支持范围、批量）规则。
 * 打开前自动放行 ssh 端口，避免把自己锁在外面。
 */
export function FirewallApp({ hostId }: AppProps) {
  const t = useT()
  const [status, setStatus] = useState<FirewallStatus | null>(null)
  const [checking, setChecking] = useState(true)
  const [rules, setRules] = useState<FirewallRule[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  // 新增规则表单
  const [form, setForm] = useState({ action: 'allow', proto: 'tcp', port: '', from: '' })

  const hostIdRef = useRef(hostId)

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

  const loadRules = useCallback(async () => {
    try {
      const r = await sendReq('firewall.list', {})
      setRules((r.rules as FirewallRule[]) || [])
    } catch (e) {
      setError(transErr(e, '加载规则失败'))
    }
  }, [sendReq])

  const loadStatus = useCallback(async () => {
    setChecking(true)
    setError('')
    try {
      const r = await sendReq('firewall.status', {})
      setStatus((r.status as FirewallStatus) || null)
    } catch (e) {
      setError(transErr(e, '检测防火墙失败'))
    } finally {
      setChecking(false)
    }
  }, [sendReq])

  useEffect(() => {
    hostIdRef.current = hostId
    void ws.connect().catch(() => {})
    if (hostId) {
      void loadStatus()
      void loadRules()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hostId])

  /** 打开 / 关闭防火墙 */
  const toggle = async (enabled: boolean) => {
    if (!status) return
    if (enabled) {
      const ssh = status.sshPort || '22'
      const ok = window.confirm(t('打开防火墙前将自动放行 ssh 端口 {0}，防止把自己锁在外面。确认打开？', ssh))
      if (!ok) return
    } else {
      if (!window.confirm(t('确认关闭防火墙？关闭后所有自定义规则将不再生效。'))) return
    }
    setBusy(true)
    setError('')
    try {
      await sendReq('firewall.set', { enabled })
      await loadStatus()
      await loadRules()
    } catch (e) {
      setError(transErr(e, '操作失败'))
    } finally {
      setBusy(false)
    }
  }

  /**
   * 解析端口输入：支持单个 "8080"、范围 "49000-50000"（转 ufw 的冒号写法）、
   * 批量（逗号/空格分隔多个端口或范围），如 "8080, 49000-50000"。
   */
  const parsePorts = (input: string): string[] => {
    return input
      .split(/[,，\s]+/)
      .map((s) => s.trim())
      .filter(Boolean)
      .map((s) => s.replace(/^(\d+)-(\d+)$/, '$1:$2'))
  }

  /** 添加规则（支持批量端口） */
  const addRule = async () => {
    if (!form.from.trim() && !form.port.trim()) {
      setError(t('请填写来源 IP 或端口'))
      return
    }
    const ports = parsePorts(form.port)
    for (const p of ports) {
      if (p.includes(':') && form.proto === 'any') {
        setError(t('端口范围 {0} 必须指定协议（TCP 或 UDP）', p))
        return
      }
    }
    setBusy(true)
    setError('')
    try {
      const targets = ports.length > 0 ? ports : ['']
      for (const port of targets) {
        await sendReq('firewall.rule.add', {
          action: form.action,
          proto: form.proto === 'any' ? '' : form.proto,
          port,
          from: form.from.trim(),
        })
      }
      setForm({ action: form.action, proto: form.proto, port: '', from: '' })
      await loadRules()
    } catch (e) {
      setError(transErr(e, '添加规则失败'))
    } finally {
      setBusy(false)
    }
  }

  /** 删除规则：由解析结果还原成添加时一致的 spec */
  const removeRule = async (r: FirewallRule) => {
    const desc = describeRule(r)
    if (!window.confirm(t('确认删除规则：{0}？', desc))) return
    setBusy(true)
    setError('')
    try {
      await sendReq('firewall.rule.remove', {
        action: r.action,
        proto: r.proto && r.proto !== 'any' ? r.proto : '',
        port: r.port && r.port !== 'any' ? r.port : '',
        from: r.from && r.from !== 'any' ? r.from : '',
      })
      await loadRules()
    } catch (e) {
      setError(transErr(e, '删除规则失败'))
    } finally {
      setBusy(false)
    }
  }

  const unsupported = status ? !status.supported : false

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', background: 'rgba(var(--rgb-appbg),0.85)', fontSize: 12 }}>
      {/* 顶部状态栏 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          padding: '10px 14px',
          borderBottom: '1px solid rgba(var(--rgb-line),0.15)',
          flexWrap: 'wrap',
        }}
      >
        {checking ? (
          <div style={{ color: 'var(--text-1)' }}>{t('检测防火墙状态…')}</div>
        ) : unsupported ? (
          <div style={{ color: 'var(--yellow)', fontWeight: 600 }}>{t('⚠️ 目标机未安装 ufw 防火墙')}</div>
        ) : (
          <>
            <span
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                fontWeight: 700,
                color: status?.active ? 'var(--green)' : 'var(--red)',
              }}
            >
              <span
                style={{
                  width: 10,
                  height: 10,
                  borderRadius: '50%',
                  background: status?.active ? 'var(--green)' : 'var(--red)',
                  boxShadow: `0 0 8px ${status?.active ? 'rgba(34,197,94,0.8)' : 'rgba(239,68,68,0.8)'}`,
                }}
              />
              {status?.active ? t('防火墙已开启') : t('防火墙已关闭')}
            </span>
            <span style={{ color: 'var(--text-1)' }}>
              {status?.active ? t('当前规则生效') : t('当前规则不生效（仅保存）')}
            </span>
            {status?.version && <span style={{ color: 'var(--text-1)' }}>{status.version}</span>}
            {status?.sshPort && (
              <span style={{ color: 'var(--text-1)' }}>{t('ssh 端口：{0}', status.sshPort)}</span>
            )}
            <div style={{ flex: 1 }} />
            <button className="btn btn-sm" onClick={loadStatus} disabled={busy}>
              {t('刷新状态')}
            </button>
            {status?.active ? (
              <button
                className="btn btn-sm btn-danger"
                onClick={() => toggle(false)}
                disabled={busy}
              >
                {busy ? t('处理中…') : t('关闭防火墙')}
              </button>
            ) : (
              <button className="btn btn-sm" onClick={() => toggle(true)} disabled={busy}>
                {busy ? t('处理中…') : t('打开防火墙')}
              </button>
            )}
          </>
        )}
      </div>

      {error && (
        <div style={{ padding: '8px 12px', color: 'var(--red)', wordBreak: 'break-all' }}>{error}</div>
      )}

      {!unsupported && (
        <>
          {/* 新增规则表单 */}
          <div
            style={{
              display: 'flex',
              gap: 8,
              padding: '10px 14px',
              borderBottom: '1px solid rgba(var(--rgb-line),0.15)',
              flexWrap: 'wrap',
              alignItems: 'center',
            }}
          >
            <span style={{ color: 'var(--text-1)', fontWeight: 600 }}>{t('添加规则：')}</span>
            <select
              value={form.action}
              onChange={(e) => setForm({ ...form, action: e.target.value })}
              style={{ width: 88 }}
            >
              <option value="allow">{t('允许')}</option>
              <option value="deny">{t('禁止')}</option>
              <option value="reject">{t('拒绝')}</option>
            </select>
            <select
              value={form.proto}
              onChange={(e) => setForm({ ...form, proto: e.target.value })}
              style={{ width: 84 }}
            >
              <option value="tcp">{t('TCP')}</option>
              <option value="udp">{t('UDP')}</option>
              <option value="any">{t('不限协议')}</option>
            </select>
            <input
              style={{ width: 200, flexShrink: 1 }}
              value={form.port}
              placeholder={t('端口/范围，可批量：8080, 49000-50000')}
              onChange={(e) => setForm({ ...form, port: e.target.value })}
            />
            <input
              style={{ width: 150 }}
              value={form.from}
              placeholder={t('来源 IP（留空=所有来源）')}
              onChange={(e) => setForm({ ...form, from: e.target.value })}
            />
            <button className="btn btn-sm" onClick={addRule} disabled={busy}>
              {t('＋ 添加')}
            </button>
          </div>

          {/* 规则列表 */}
          <div style={{ flex: 1, overflow: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead style={{ position: 'sticky', top: 0, background: 'rgba(var(--rgb-panel),0.95)' }}>
                <tr style={{ textAlign: 'left', color: 'var(--text-1)', fontSize: 11 }}>
                  <th style={{ padding: '8px 10px' }}>{t('动作')}</th>
                  <th style={{ padding: '8px 10px' }}>{t('协议')}</th>
                  <th style={{ padding: '8px 10px' }}>{t('端口')}</th>
                  <th style={{ padding: '8px 10px' }}>{t('来源')}</th>
                  <th style={{ padding: '8px 10px' }}>{t('说明')}</th>
                  <th style={{ padding: '8px 10px', width: 70 }}>{t('操作')}</th>
                </tr>
              </thead>
              <tbody>
                {rules.map((r, i) => (
                  <tr
                    key={`${r.id}-${i}`}
                    style={{ borderBottom: '1px solid rgba(var(--rgb-line),0.06)' }}
                  >
                    <td style={{ padding: '6px 10px' }}>
                      <span
                        style={{
                          color:
                            r.action === 'allow' ? 'var(--green)' : r.action === 'deny' ? 'var(--red)' : 'var(--yellow)',
                          fontWeight: 600,
                        }}
                      >
                        {actionLabel(r.action)}
                      </span>
                    </td>
                    <td style={{ padding: '6px 10px', color: 'var(--text-1)' }}>
                      {protoLabel(r.proto)}
                    </td>
                    <td style={{ padding: '6px 10px', fontFamily: 'Consolas, monospace' }}>
                      {r.port && r.port !== 'any' ? r.port : t('全部')}
                    </td>
                    <td style={{ padding: '6px 10px', fontFamily: 'Consolas, monospace' }}>
                      {r.from && r.from !== 'any' ? r.from : t('任意')}
                    </td>
                    <td style={{ padding: '6px 10px', color: 'var(--text-1)' }}>{describeRule(r)}</td>
                    <td style={{ padding: '6px 10px' }}>
                      <button
                        className="btn btn-sm btn-danger"
                        onClick={() => removeRule(r)}
                        disabled={busy}
                      >
                        {t('删除')}
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!rules.length && (
              <div style={{ padding: 30, textAlign: 'center', color: 'var(--text-1)' }}>
                {t('暂无规则，可点击上方「＋ 添加」新增 IP / 端口规则')}
              </div>
            )}
          </div>
        </>
      )}
    </div>
  )
}
