import { useCallback, useEffect, useRef, useState } from 'react'
import { ws } from '../lib/ws'
import { api } from '../lib/api'
import { useT, tt, transErr } from '../lib/i18n'
import type { AppProps } from '../desktop/appRegistry'
import type { Host } from '../lib/types'

interface DownloadTask {
  id: string
  hostId: string
  url: string
  dir: string
  name: string
  gid: string
  status: string
  completed: number
  total: number
  speed: number
  error: string
  createdAt: number
}

const reqSeq = { n: 0 }

/** 字节 -> "1.2G" / "345M" / "12K" */
function fmtBytes(b: number): string {
  if (!b || b < 0) return '0 B'
  if (b >= 1024 ** 3) return `${(b / 1024 ** 3).toFixed(2)} G`
  if (b >= 1024 ** 2) return `${(b / 1024 ** 2).toFixed(1)} M`
  if (b >= 1024) return `${(b / 1024).toFixed(1)} K`
  return `${Math.round(b)} B`
}

/** B/s 速率 -> "12.3M/s" */
function fmtSpeed(bps: number): string {
  if (bps >= 1024 * 1024) return `${(bps / 1024 / 1024).toFixed(1)} M/s`
  if (bps >= 1024) return `${(bps / 1024).toFixed(1)} K/s`
  return `${Math.round(bps)} B/s`
}

const STATUS_COLOR: Record<string, string> = {
  waiting: 'var(--yellow)',
  active: 'var(--green)',
  paused: '#7dd3fc',
  complete: 'var(--green)',
  removed: 'var(--text-1)',
  error: 'var(--red)',
}

/** 任务来源（保留旧种子任务的展示，新任务均为直链） */
function sourceType(task: DownloadTask): string {
  const u = (task.url || '').toLowerCase()
  if (task.gid && u === '' && task.name) return tt('种子')
  return tt('直链')
}

/** 从链接取默认文件名（直链取最后一段路径） */
function basenameOf(u: string): string {
  const t = u.trim().replace(/\/+$/, '')
  const i = t.lastIndexOf('/')
  let b = i >= 0 ? t.slice(i + 1) : t
  try {
    b = decodeURIComponent(b)
  } catch {
    /* 保留原样 */
  }
  return b
}

/**
 * 直链下载 App：让任意一台服务器把直链下载到自己的磁盘。
 * 参考格式：目标服务器 | 下载链接 | 保存目录。
 */
export function DownloadApp({ hostId, onTitle }: AppProps) {
  const t = useT()
  const STATUS_TEXT: Record<string, string> = {
    waiting: t('等待中'),
    active: t('下载中'),
    paused: t('已暂停'),
    complete: t('已完成'),
    removed: t('已移除'),
    error: t('错误'),
  }
  const [hosts, setHosts] = useState<Host[]>([])
  const [targetId, setTargetId] = useState(hostId || '')
  const targetIdRef = useRef(targetId)

  // 表单
  const [url, setUrl] = useState('')
  const [dir, setDir] = useState('/root/Downloads')
  const [fileName, setFileName] = useState('')
  const fileNameTouched = useRef(false)

  // 保存目录浏览器
  const [dirBrowser, setDirBrowser] = useState<null | {
    path: string
    parent: string
    entries: { name: string; isDir: boolean }[]
    loading: boolean
    err: string
  }>(null)
  const [dirJump, setDirJump] = useState('')

  // aria2 安装状态
  const [aria, setAria] = useState<{ installed: boolean; version: string; checking: boolean }>({
    installed: false,
    version: '',
    checking: true,
  })
  const [installing, setInstalling] = useState(false)
  const [installOutput, setInstallOutput] = useState('')

  // 任务列表
  const [tasks, setTasks] = useState<DownloadTask[]>([])
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  useEffect(() => {
    targetIdRef.current = targetId
  }, [targetId])

  useEffect(() => {
    if (onTitle) onTitle(t('直链下载'))
  }, [onTitle])

  // 首次挂载：连接 WS + 拉取主机列表
  useEffect(() => {
    void ws.connect().catch(() => {})
    api
      .listHosts()
      .then((list) => {
        setHosts(list)
        if (!targetIdRef.current && list.length > 0) setTargetId(list[0].id)
      })
      .catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const sendReq = useCallback(
    (type: string, payload: Record<string, unknown> = {}, timeout = 30000): Promise<any> => {
      return new Promise((resolve, reject) => {
        const id = `req_${++reqSeq.n}`
        ws.send(type, id, { ...payload, hostId: targetIdRef.current })
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )

  // 检测目标服务器 aria2 安装状态
  const checkAria = useCallback(async () => {
    if (!targetIdRef.current) return
    setAria((a) => ({ ...a, checking: true }))
    try {
      const r = await sendReq('download.check')
      const st = (r as any).status as { installed: boolean; version: string } | undefined
      setAria({
        installed: Boolean(st?.installed),
        version: String(st?.version || ''),
        checking: false,
      })
    } catch (e) {
      setAria({ installed: false, version: '', checking: false })
      setError(transErr(e, '检测 aria2 失败'))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sendReq])

  // 拉取任务列表
  const refresh = useCallback(async () => {
    if (!targetIdRef.current) return
    try {
      const r = await sendReq('download.list')
      setTasks((r.tasks as DownloadTask[]) || [])
    } catch (e) {
      setError(transErr(e, '加载任务列表失败'))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sendReq])

  // 切换目标主机：重新检测 aria2 + 刷新任务，并每 2s 自动刷新进度
  useEffect(() => {
    if (!targetId) return
    setError('')
    setNotice('')
    void checkAria()
    void refresh()
    const timer = setInterval(() => void refresh(), 2000)
    return () => clearInterval(timer)
  }, [targetId, checkAria, refresh])

  // 一键安装 aria2（流式输出）
  const installAria = async () => {
    if (!window.confirm(t('将在目标服务器上通过系统包管理器（apt/dnf/yum/apk）安装 aria2，确认开始？'))) return
    setInstalling(true)
    setInstallOutput('')
    setError('')
    try {
      await new Promise<void>((resolve, reject) => {
        const channelId = `di_${Date.now().toString(36)}_${++reqSeq.n}`
        let unsub: () => void = () => {}
        const timer = setTimeout(() => {
          unsub()
          reject(new Error(t('安装超时，请稍后在终端中查看安装状态')))
        }, 600000)
        unsub = ws.onChannel(channelId, (msg) => {
          if (msg.type === 'download.install.output') {
            setInstallOutput((o) => o + String((msg.payload as any)?.line || '') + '\n')
          } else if (msg.type === 'download.install.done') {
            clearTimeout(timer)
            unsub()
            const p = msg.payload as any
            if (p?.ok) resolve()
            else reject(new Error(p?.message || t('安装失败')))
          }
        })
        ws.send('download.install', channelId, { hostId: targetIdRef.current })
      })
      await checkAria()
      setInstalling(false)
    } catch (e) {
      setError(transErr(e, '安装失败'))
      setInstalling(false)
    }
  }

  // 链接变化时自动填充默认文件名（未手动编辑过才覆盖）
  const onUrlChange = (v: string) => {
    setUrl(v)
    if (!fileNameTouched.current && v.trim()) {
      setFileName(basenameOf(v))
    }
  }
  const onFileNameChange = (v: string) => {
    fileNameTouched.current = true
    setFileName(v)
  }

  // ---- 保存目录浏览器 ----
  const loadDir = useCallback(
    async (p: string) => {
      try {
        const r = await sendReq('download.listdir', { path: p }, 20000)
        setDirBrowser({ path: r.path, parent: r.parent, entries: r.entries || [], loading: false, err: '' })
        setDirJump(r.path)
      } catch (e) {
        setDirBrowser((d) => (d ? { ...d, loading: false, err: transErr(e, '读取目录失败') } : d))
      }
    },
    [sendReq],
  )

  const openDirBrowser = () => {
    const p = dir.trim() || '/'
    setDirBrowser({ path: p, parent: '', entries: [], loading: true, err: '' })
    setDirJump(p)
    void loadDir(p)
  }

  const navDir = (name: string) => {
    if (!dirBrowser) return
    const p = dirBrowser.path === '/' ? '/' + name : dirBrowser.path + '/' + name
    setDirBrowser({ ...dirBrowser, loading: true, err: '' })
    void loadDir(p)
  }

  const goParent = () => {
    if (!dirBrowser || !dirBrowser.parent || dirBrowser.parent === dirBrowser.path) return
    setDirBrowser({ ...dirBrowser, loading: true, err: '' })
    void loadDir(dirBrowser.parent)
  }

  const pickDir = () => {
    if (!dirBrowser) return
    setDir(dirBrowser.path)
    setDirBrowser(null)
  }

  // 新建下载任务
  const addTask = async () => {
    if (!targetIdRef.current) {
      setError(t('请选择目标服务器'))
      return
    }
    const urlText = url.trim()
    if (!urlText) {
      setError(t('请填写下载链接（直链）'))
      return
    }
    if (!dir.trim()) {
      setError(t('请填写保存目录'))
      return
    }
    try {
      // 首次触发时后端可能要先冷启动远端 aria2 daemon（SSH 拉起 + RPC 握手），
      // 比默认 30s 长，这里放宽到 60s，与后端 Add 的 60s 上下文一致。
      await sendReq(
        'download.add',
        {
          url: urlText,
          dir: dir.trim(),
          name: fileName.trim(),
        },
        60000,
      )
      setNotice(t('任务已创建，开始下载'))
      setError('')
      setUrl('')
      setFileName('')
      fileNameTouched.current = false
      void refresh()
    } catch (e) {
      setError(transErr(e, '创建任务失败'))
    }
  }

  // 暂停 / 继续 / 取消
  const taskAction = async (act: 'pause' | 'resume' | 'cancel', task: DownloadTask) => {
    if (act === 'cancel' && !window.confirm(t('确认取消「{0}」并清理已下载的文件？', task.name))) return
    try {
      await sendReq(`download.${act}`, { id: task.id })
      void refresh()
    } catch (e) {
      alert(transErr(e, '操作失败'))
    }
  }

  const renderActions = (task: DownloadTask) => {
    if (task.status === 'active') {
      return (
        <>
          <button className="btn btn-sm btn-ghost" onClick={() => taskAction('pause', task)}>
            {t('暂停')}
          </button>
          <button className="btn btn-sm btn-danger" style={{ marginLeft: 4 }} onClick={() => taskAction('cancel', task)}>
            {t('取消')}
          </button>
        </>
      )
    }
    if (task.status === 'paused' || task.status === 'waiting') {
      return (
        <>
          <button className="btn btn-sm btn-ghost" onClick={() => taskAction('resume', task)}>
            {t('继续')}
          </button>
          <button className="btn btn-sm btn-danger" style={{ marginLeft: 4 }} onClick={() => taskAction('cancel', task)}>
            {t('取消')}
          </button>
        </>
      )
    }
    if (task.status === 'error' || task.status === 'removed') {
      return (
        <button className="btn btn-sm btn-danger" onClick={() => taskAction('cancel', task)}>
          {t('移除')}
        </button>
      )
    }
    return null
  }

  return (
    <div
      style={{
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
        background: 'rgba(var(--rgb-appbg),0.85)',
        fontSize: 12,
      }}
    >
      {/* 状态栏 + 手动刷新：与 aria2 状态同行，避免独占一行的突兀 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          padding: '6px 12px',
          borderBottom: '1px solid rgba(var(--rgb-line),0.15)',
          flexWrap: 'wrap',
        }}
      >
        {aria.checking ? (
          <span style={{ color: 'var(--text-1)', fontSize: 11 }}>{t('检测 aria2 状态…')}</span>
        ) : aria.installed && aria.version ? (
          <span style={{ color: 'var(--text-1)', fontSize: 11 }}>{t('✅ aria2 {0}', aria.version)}</span>
        ) : null}
        <div style={{ flex: 1 }} />
        <button className="btn btn-sm btn-ghost" onClick={() => void refresh()}>
          {t('刷新')}
        </button>
      </div>

      {error && <div style={{ padding: '6px 12px', color: 'var(--red)', wordBreak: 'break-all' }}>{error}</div>}
      {notice && <div style={{ padding: '6px 12px', color: 'var(--green)' }}>{notice}</div>}

      {/* aria2 未安装提示 */}
      {!aria.installed && !aria.checking && (
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
            {t('⚠️ 目标服务器尚未安装 aria2')}
          </div>
          {installing ? (
            <>
              <div style={{ color: 'var(--text-1)', marginBottom: 6 }}>{t('正在执行安装，请耐心等待…')}</div>
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
            <button className="btn btn-sm" onClick={installAria}>
              {t('一键安装 aria2')}
            </button>
          )}
        </div>
      )}
      {/* 新建任务表单 */}
      <div style={{ padding: 12, borderBottom: '1px solid rgba(var(--rgb-line),0.15)', flexShrink: 0 }}>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
            gap: 10,
            alignItems: 'end',
          }}
        >
          <div className="field" style={{ marginBottom: 0 }}>
            <label>{t('目标服务器（保存到磁盘的主机）')}</label>
            <select value={targetId} onChange={(e) => setTargetId(e.target.value)} disabled={hosts.length === 0}>
              {hosts.length === 0 && <option value="">{t('（无服务器，请先添加）')}</option>}
              {hosts.map((h) => (
                <option key={h.id} value={h.id}>
                  {t('{0}（{1}@{2}）', h.name, h.username, h.host)}
                </option>
              ))}
            </select>
          </div>

          <div className="field" style={{ marginBottom: 0 }}>
            <label>{t('下载链接（直链）')}</label>
            <input
              value={url}
              onChange={(e) => onUrlChange(e.target.value)}
              placeholder={t('https://… 或 http://…')}
            />
          </div>

          <div className="field" style={{ marginBottom: 0 }}>
            <label>{t('保存目录')}</label>
            <div style={{ display: 'flex', gap: 6 }}>
              <input
                value={dir}
                onChange={(e) => setDir(e.target.value)}
                placeholder="/root/Downloads"
                style={{ flex: 1, minWidth: 0, width: 'auto' }}
              />
              <button
                type="button"
                className="btn btn-sm btn-ghost"
                style={{ flexShrink: 0 }}
                onClick={openDirBrowser}
              >
                {t('浏览…')}
              </button>
            </div>
          </div>

          <div className="field" style={{ marginBottom: 0 }}>
            <label>{t('保存文件名')}</label>
            <input
              value={fileName}
              onChange={(e) => onFileNameChange(e.target.value)}
              placeholder={t('留空则自动取链接文件名')}
            />
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 10, flexWrap: 'wrap' }}>
          <div style={{ flex: 1 }} />
          <button
            className="btn btn-sm"
            onClick={addTask}
            disabled={!aria.installed || aria.checking}
          >
            {t('▶ 开始下载')}
          </button>
        </div>
      </div>

      {/* 任务列表 */}
      <div style={{ flex: 1, overflow: 'auto' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead style={{ position: 'sticky', top: 0, background: 'rgba(var(--rgb-panel),0.95)' }}>
            <tr style={{ textAlign: 'left', color: 'var(--text-1)', fontSize: 11 }}>
              <th style={{ padding: '8px 10px' }}>{t('文件')}</th>
              <th style={{ padding: '8px 10px' }}>{t('来源')}</th>
              <th style={{ padding: '8px 10px' }}>{t('状态')}</th>
              <th style={{ padding: '8px 10px', width: 220 }}>{t('进度')}</th>
              <th style={{ padding: '8px 10px' }}>{t('大小')}</th>
              <th style={{ padding: '8px 10px' }}>{t('速度')}</th>
              <th style={{ padding: '8px 10px' }}>{t('保存到')}</th>
              <th style={{ padding: '8px 10px' }}>{t('操作')}</th>
            </tr>
          </thead>
          <tbody>
            {tasks.map((task) => {
              const pct = task.total > 0 ? Math.min(100, Math.round((task.completed / task.total) * 100)) : 0
              const indeterminate = task.status === 'active' && task.total <= 0
              return (
                <tr key={task.id} style={{ borderBottom: '1px solid rgba(var(--rgb-line),0.06)' }}>
                  <td style={{ padding: '6px 10px', maxWidth: 240 }}>
                    <div style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={task.name}>
                      {task.name}
                    </div>
                    <div style={{ color: 'var(--text-1)', fontSize: 11, fontFamily: 'Consolas, monospace' }}>
                      {task.id.slice(0, 18)}
                    </div>
                  </td>
                  <td style={{ padding: '6px 10px', color: 'var(--text-1)' }}>{sourceType(task)}</td>
                  <td style={{ padding: '6px 10px' }}>
                    <span style={{ color: STATUS_COLOR[task.status] || 'var(--text-0)', fontWeight: 600 }}>
                      {STATUS_TEXT[task.status] || task.status}
                    </span>
                    {task.error && (
                      <div style={{ color: 'var(--red)', fontSize: 11, maxWidth: 180 }} title={task.error}>
                        {task.error.slice(0, 60)}
                      </div>
                    )}
                  </td>
                  <td style={{ padding: '6px 10px' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <div
                        style={{
                          flex: 1,
                          height: 6,
                          borderRadius: 3,
                          background: 'rgba(var(--rgb-line),0.15)',
                          overflow: 'hidden',
                        }}
                      >
                        <div
                          style={{
                            height: '100%',
                            width: indeterminate ? '30%' : `${pct}%`,
                            borderRadius: 3,
                            background: indeterminate
                              ? 'linear-gradient(90deg, rgba(34,197,94,0.2), rgba(34,197,94,0.9))'
                              : 'var(--green)',
                            transition: 'width 0.4s',
                            animation: indeterminate ? 'termBlink 1.2s step-start infinite' : undefined,
                          }}
                        />
                      </div>
                      <span style={{ color: 'var(--text-1)', fontSize: 11, width: 38, textAlign: 'right', fontFamily: 'Consolas, Menlo, monospace' }}>
                        {indeterminate ? '--%' : `${pct}%`}
                      </span>
                    </div>
                  </td>
                  <td style={{ padding: '6px 10px', color: 'var(--text-1)', fontFamily: 'Consolas, Menlo, monospace' }}>
                    {task.total > 0 ? `${fmtBytes(task.completed)} / ${fmtBytes(task.total)}` : fmtBytes(task.completed)}
                  </td>
                  <td style={{ padding: '6px 10px', color: task.speed > 0 ? '#34d399' : 'var(--text-1)', fontFamily: 'Consolas, Menlo, monospace' }}>
                    {task.status === 'active' ? fmtSpeed(task.speed) : '—'}
                  </td>
                  <td style={{ padding: '6px 10px', color: 'var(--text-1)', maxWidth: 180 }}>
                    <div style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={task.dir}>
                      {task.dir}
                    </div>
                  </td>
                  <td style={{ padding: '6px 10px', whiteSpace: 'nowrap' }}>{renderActions(task)}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
        {tasks.length === 0 && (
          <div style={{ padding: 30, textAlign: 'center', color: 'var(--text-1)' }}>
            {targetId ? t('暂无下载任务，请在上方新建') : t('请先在上方选择目标服务器')}
          </div>
        )}
      </div>

      {/* 保存目录浏览器弹窗 */}
      {dirBrowser && (
        <div className="modal-mask" onClick={() => setDirBrowser(null)}>
          <div className="modal" style={{ width: 560 }} onClick={(e) => e.stopPropagation()}>
            <h3>{t('📁 选择保存目录')}</h3>
            <div className="field" style={{ display: 'flex', gap: 6, alignItems: 'center', marginBottom: 0 }}>
              <input
                value={dirJump}
                onChange={(e) => setDirJump(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && dirJump.trim()) void loadDir(dirJump.trim())
                }}
                placeholder={t('输入路径后回车跳转')}
                style={{ flex: 1, minWidth: 0, width: 'auto' }}
              />
              <button
                type="button"
                className="btn btn-sm btn-ghost"
                style={{ flexShrink: 0 }}
                onClick={() => dirJump.trim() && void loadDir(dirJump.trim())}
              >
                {t('跳转')}
              </button>
            </div>
            <div
              style={{
                fontSize: 12,
                color: 'var(--text-1)',
                margin: '6px 0 8px',
                fontFamily: 'Consolas, monospace',
                wordBreak: 'break-all',
              }}
            >
              📍 {dirBrowser.path}
            </div>
            {dirBrowser.err && <div style={{ color: 'var(--red)', marginBottom: 8, fontSize: 12 }}>{dirBrowser.err}</div>}
            <div
              style={{
                border: '1px solid var(--glass-border)',
                borderRadius: 8,
                overflow: 'auto',
                maxHeight: 320,
                background: 'rgba(var(--rgb-appbg),0.5)',
              }}
            >
              {dirBrowser.loading ? (
                <div style={{ padding: 20, textAlign: 'center', color: 'var(--text-1)' }}>{t('加载中…')}</div>
              ) : (
                <>
                  {dirBrowser.parent && dirBrowser.parent !== dirBrowser.path && (
                    <div
                      className="fm-row"
                      style={{ padding: '8px 10px', display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}
                      onClick={goParent}
                    >
                      <span>📂</span>
                      <span>{t('..（上级目录）')}</span>
                    </div>
                  )}
                  {dirBrowser.entries.map((e) => (
                    <div
                      key={e.name}
                      className="fm-row"
                      style={{ padding: '8px 10px', display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}
                      onClick={() => navDir(e.name)}
                    >
                      <span>📂</span>
                      <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={e.name}>
                        {e.name}
                      </span>
                    </div>
                  ))}
                  {dirBrowser.entries.length === 0 && (
                    <div style={{ padding: 20, textAlign: 'center', color: 'var(--text-1)' }}>{t('（没有子目录）')}</div>
                  )}
                </>
              )}
            </div>
            <div className="footer">
              <button className="btn btn-sm btn-ghost" onClick={() => setDirBrowser(null)}>
                {t('取消')}
              </button>
              <button className="btn btn-sm" onClick={pickDir} disabled={dirBrowser.loading}>
                {t('选择此目录')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
