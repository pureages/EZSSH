import { useCallback, useEffect, useState } from 'react'
import { api, ApiError } from '../lib/api'
import { setPendingTerminalCwd } from '../lib/terminalLaunch'
import { newChannelId, ws } from '../lib/ws'
import { useWindowStore } from '../desktop/windowStore'
import type { AppProps } from '../desktop/appRegistry'
import type { BackgroundStartResult, BackgroundTask, Host, SavedCommand } from '../lib/types'
import { useT, transErr } from '../lib/i18n'

/**
 * 一键命令：保存终端命令（多行），对多台服务器前台/后台同时执行。
 * - 前台：为每台选中服务器各开一个终端窗口并自动注入命令
 * - 后台：分离式长期运行（不开终端），在「后台运行中」页签监控 CPU/MEM/RSS 并可停止/看日志
 */
export function OneClickCmdApp({ onTitle }: AppProps) {
  const t = useT()
  const [tab, setTab] = useState<'cmds' | 'bg'>('cmds')
  const [hosts, setHosts] = useState<Host[]>([])
  const [cmds, setCmds] = useState<SavedCommand[]>([])

  // 命令编辑表单
  const [cmdName, setCmdName] = useState('')
  const [cmdText, setCmdText] = useState('')
  const [editingId, setEditingId] = useState<number | null>(null)

  // 执行对话框
  const [execOpen, setExecOpen] = useState(false)
  const [execCommand, setExecCommand] = useState('')
  const [selHosts, setSelHosts] = useState<Set<string>>(new Set())
  const [execMode, setExecMode] = useState<'fg' | 'bg'>('fg')
  const [execBusy, setExecBusy] = useState(false)
  const [execResult, setExecResult] = useState<BackgroundStartResult[] | null>(null)

  // 后台任务
  const [tasks, setTasks] = useState<BackgroundTask[]>([])
  const [logTask, setLogTask] = useState<BackgroundTask | null>(null)
  const [logText, setLogText] = useState('')
  const [logLoading, setLogLoading] = useState(false)

  const [err, setErr] = useState('')

  const open = useWindowStore((s) => s.open)

  const loadCmds = useCallback(async () => {
    try {
      setCmds(await api.listCommands())
    } catch {
      /* 忽略，界面留空 */
    }
  }, [])

  useEffect(() => {
    onTitle?.(t('一键命令'))
    void api
      .listHosts()
      .then((h) => setHosts(h))
      .catch(() => {})
    void loadCmds()
  }, [onTitle, loadCmds])

  // 后台运行中页签激活时轮询
  useEffect(() => {
    if (tab !== 'bg') return
    let cancelled = false
    const tick = async () => {
      try {
        const list = await api.backgroundList()
        if (!cancelled) setTasks(list)
      } catch {
        /* 忽略 */
      }
    }
    void tick()
    const timer = setInterval(tick, 2500)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [tab])

  const saveCommand = async () => {
    const name = cmdName.trim()
    const command = cmdText.trim()
    if (!name || !command) {
      setErr(t('命令名与命令内容不能为空'))
      return
    }
    setErr('')
    try {
      if (editingId != null) await api.updateCommand(editingId, name, command)
      else await api.createCommand(name, command)
      setCmdName('')
      setCmdText('')
      setEditingId(null)
      await loadCmds()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : t('保存失败'))
    }
  }

  const editCommand = (c: SavedCommand) => {
    setEditingId(c.id)
    setCmdName(c.name)
    setCmdText(c.command)
    setErr('')
  }

  const cancelEdit = () => {
    setEditingId(null)
    setCmdName('')
    setCmdText('')
    setErr('')
  }

  const deleteCommand = async (c: SavedCommand) => {
    if (!window.confirm(t('删除命令「{0}」？', c.name))) return
    try {
      await api.deleteCommand(c.id)
      if (editingId === c.id) cancelEdit()
      await loadCmds()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : t('删除失败'))
    }
  }

  const openExec = (command: string) => {
    setExecCommand(command)
    setExecResult(null)
    setSelHosts(new Set())
    setErr('')
    setExecOpen(true)
  }

  const toggleHost = (id: string) => {
    setSelHosts((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const doExec = async () => {
    const ids = [...selHosts]
    if (ids.length === 0) {
      setErr(t('至少选择一台服务器'))
      return
    }
    setErr('')
    if (execMode === 'bg') {
      setExecBusy(true)
      try {
        const res = await api.backgroundStart(ids, execCommand)
        setExecResult(res)
        if (res.length > 0 && res.every((r) => r.ok)) {
          setExecOpen(false)
          setTab('bg')
          const list = await api.backgroundList().catch(() => [])
          setTasks(list)
        }
      } catch (e) {
        setErr(e instanceof ApiError ? e.message : t('启动失败'))
      } finally {
        setExecBusy(false)
      }
      return
    }
    // 前台：逐台打开终端并注入命令
    try {
      await ws.connect()
    } catch {
      /* 终端组件会自处理 */
    }
    let i = 0
    for (const id of ids) {
      const h = hosts.find((x) => x.id === id)
      if (!h) continue
      setPendingTerminalCwd(id, '/', execCommand)
      open({
        id: `terminal-${Date.now().toString(36)}-${i}`,
        appId: 'terminal',
        title: t('{0} - {1}', h.name, t('终端')),
        icon: '🖥️',
        hostId: id,
        platform: h.platform ?? '',
        channelId: newChannelId(),
        x: 80 + 28 * i,
        y: 60 + 28 * i,
        width: 760,
        height: 460,
      })
      i++
    }
    setExecOpen(false)
  }

  const killTask = async (task: BackgroundTask) => {
    if (!window.confirm(t('停止后台任务「{0}」PID {1}？', task.hostName, task.pid))) return
    try {
      await api.backgroundKill(task.id)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : t('停止失败'))
    }
    const list = await api.backgroundList().catch(() => [])
    setTasks(list)
  }

  const openLogs = async (task: BackgroundTask) => {
    setLogTask(task)
    setLogLoading(true)
    setLogText('')
    try {
      const res = await api.backgroundLogs(task.id)
      setLogText(res.logs || t('（无输出）'))
    } catch (e) {
      setLogText(transErr(e, '读取失败'))
    } finally {
      setLogLoading(false)
    }
  }

  const tabBtn = (key: 'cmds' | 'bg', label: string, count?: number) => (
    <button
      onClick={() => {
        setTab(key)
        setErr('')
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
      {key === 'bg' && count != null && count > 0 && (
        <span style={{ marginLeft: 6, color: '#fbbf24' }}>{count}</span>
      )}
    </button>
  )

  const statusColor = (s: BackgroundTask['status']) =>
    s === 'running' ? '#34d399' : s === 'unknown' ? '#fbbf24' : 'var(--text-1)'
  const statusLabel = (s: BackgroundTask['status']) =>
    s === 'running' ? t('运行中') : s === 'unknown' ? t('未知离线') : t('已退出')

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', background: 'rgba(var(--rgb-appbg),0.9)', color: 'var(--text-0)' }}>
      {/* 页签 */}
      <div style={{ display: 'flex', gap: 4, padding: '10px 14px 0', borderBottom: '1px solid rgba(var(--rgb-line),0.15)' }}>
        {tabBtn('cmds', t('命令'))}
        {tabBtn('bg', t('后台运行中'), tasks.length)}
      </div>

      <div style={{ flex: 1, overflow: 'auto', padding: 14 }}>
        {err && (
          <div style={{ marginBottom: 10, padding: '8px 12px', borderRadius: 8, background: 'rgba(239,68,68,0.12)', color: 'var(--red)', fontSize: 13 }}>
            {err}
          </div>
        )}

        {tab === 'cmds' && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            {/* 命令编辑区 */}
            <div style={{ background: 'rgba(var(--rgb-panel),0.6)', border: '1px solid rgba(var(--rgb-line),0.15)', borderRadius: 12, padding: 14 }}>
              <div style={{ display: 'flex', gap: 10, alignItems: 'center', marginBottom: 10 }}>
                <span style={{ fontSize: 13, color: 'var(--text-1)' }}>{t('命令名')}</span>
                <input
                  value={cmdName}
                  onChange={(e) => setCmdName(e.target.value)}
                  placeholder={editingId != null ? t('编辑命令名称') : t('如更新部署包')}
                  style={{
                    flex: 1,
                    maxWidth: 320,
                    padding: '7px 10px',
                    borderRadius: 8,
                    border: '1px solid rgba(var(--rgb-line),0.3)',
                    background: 'rgba(0,0,0,0.25)',
                    color: 'var(--text-0)',
                    fontSize: 13,
                  }}
                />
                {editingId != null && (
                  <button className="btn btn-sm btn-ghost" onClick={cancelEdit}>
                    {t('取消编辑')}
                  </button>
                )}
              </div>
              <textarea
                value={cmdText}
                onChange={(e) => setCmdText(e.target.value)}
                placeholder={t('在此输入命令（多行命令可一起保存执行）…\n例如：\ncd /opt/app\n./restart.sh')}
                rows={6}
                spellCheck={false}
                style={{
                  width: '100%',
                  boxSizing: 'border-box',
                  fontFamily: 'Consolas, Menlo, monospace',
                  fontSize: 12.5,
                  lineHeight: 1.5,
                  padding: 10,
                  borderRadius: 8,
                  border: '1px solid rgba(var(--rgb-line),0.3)',
                  background: 'rgba(0,0,0,0.25)',
                  color: 'var(--text-0)',
                  resize: 'vertical',
                }}
              />
              <div style={{ display: 'flex', gap: 10, marginTop: 12 }}>
                <button className="btn btn-sm" disabled={!cmdName.trim() || !cmdText.trim()} onClick={() => void saveCommand()}>
                  💾 {editingId != null ? t('保存修改') : t('保存命令')}
                </button>
                {cmdText.trim() && (
                  <button className="btn btn-sm btn-ghost" onClick={() => openExec(cmdText.trim())}>
                    ⚡ {t('执行…')}
                  </button>
                )}
              </div>
            </div>

            {/* 已保存命令列表 */}
            <div>
              <div style={{ fontSize: 13, color: 'var(--text-1)', marginBottom: 8 }}>
                {t('已保存命令（{0}）', cmds.length)}
              </div>
              {cmds.length === 0 ? (
                <div style={{ padding: 20, textAlign: 'center', color: 'var(--text-1)', fontSize: 13 }}>{t('还没有保存的命令')}</div>
              ) : (
                <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                  <thead>
                    <tr style={{ color: 'var(--text-1)', textAlign: 'left' }}>
                      <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>{t('名称')}</th>
                      <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>{t('命令')}</th>
                      <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>{t('更新时间')}</th>
                      <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>{t('操作')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {cmds.map((c) => (
                      <tr key={c.id} style={{ borderBottom: '1px solid rgba(var(--rgb-line),0.1)' }}>
                        <td style={{ padding: '7px 8px', fontWeight: 600, whiteSpace: 'nowrap' }}>{c.name}</td>
                        <td
                          style={{
                            padding: '7px 8px',
                            fontFamily: 'Consolas, Menlo, monospace',
                            color: 'var(--text-1)',
                            maxWidth: 360,
                            overflow: 'hidden',
                            textOverflow: 'ellipsis',
                            whiteSpace: 'nowrap',
                          }}
                          title={c.command}
                        >
                          {c.command.split('\n')[0]}
                          {c.command.includes('\n') ? ' …' : ''}
                        </td>
                        <td style={{ padding: '7px 8px', color: 'var(--text-1)', whiteSpace: 'nowrap' }}>{fmtTime(c.updated_at)}</td>
                        <td style={{ padding: '7px 8px', whiteSpace: 'nowrap' }}>
                          <button className="btn btn-sm" onClick={() => openExec(c.command)}>
                            ⚡ {t('执行')}
                          </button>
                          <button className="btn btn-sm btn-ghost" style={{ marginLeft: 6 }} onClick={() => editCommand(c)}>
                            {t('编辑')}
                          </button>
                          <button className="btn btn-sm btn-danger" style={{ marginLeft: 6 }} onClick={() => void deleteCommand(c)}>
                            {t('删除')}
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        )}

        {tab === 'bg' && (
          <div>
            {tasks.length === 0 ? (
              <div style={{ padding: 30, textAlign: 'center', color: 'var(--text-1)', fontSize: 13 }}>
                {t('暂无后台任务')}
              </div>
            ) : (
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                <thead>
                  <tr style={{ color: 'var(--text-1)', textAlign: 'left' }}>
                    <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>{t('服务器')}</th>
                    <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>{t('状态')}</th>
                    <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>PID</th>
                    <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>CPU%</th>
                    <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>MEM%</th>
                    <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>RSS</th>
                    <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>{t('启动时间')}</th>
                    <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>{t('命令')}</th>
                    <th style={{ padding: '6px 8px', borderBottom: '1px solid rgba(var(--rgb-line),0.2)' }}>{t('操作')}</th>
                  </tr>
                </thead>
                <tbody>
                  {tasks.map((task) => (
                    <tr key={task.id} style={{ borderBottom: '1px solid rgba(var(--rgb-line),0.1)' }}>
                      <td style={{ padding: '7px 8px', whiteSpace: 'nowrap', fontWeight: 600 }}>{task.hostName}</td>
                      <td style={{ padding: '7px 8px', color: statusColor(task.status), whiteSpace: 'nowrap' }}>{statusLabel(task.status)}</td>
                      <td style={{ padding: '7px 8px', fontFamily: 'Consolas, monospace', color: 'var(--text-1)' }}>{task.pid || '—'}</td>
                      <td style={{ padding: '7px 8px', fontFamily: 'Consolas, monospace' }}>{task.status === 'running' ? task.cpu.toFixed(1) : '—'}</td>
                      <td style={{ padding: '7px 8px', fontFamily: 'Consolas, monospace' }}>{task.status === 'running' ? task.mem.toFixed(1) : '—'}</td>
                      <td style={{ padding: '7px 8px', fontFamily: 'Consolas, monospace', color: 'var(--text-1)' }}>{task.status === 'running' ? fmtBytes(task.rss) : '—'}</td>
                      <td style={{ padding: '7px 8px', color: 'var(--text-1)', whiteSpace: 'nowrap' }}>{fmtStarted(task.started)}</td>
                      <td
                        style={{
                          padding: '7px 8px',
                          fontFamily: 'Consolas, Menlo, monospace',
                          color: 'var(--text-1)',
                          maxWidth: 260,
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          whiteSpace: 'nowrap',
                        }}
                        title={task.command}
                      >
                        {task.command.split('\n')[0]}
                        {task.command.includes('\n') ? ' …' : ''}
                      </td>
                      <td style={{ padding: '7px 8px', whiteSpace: 'nowrap' }}>
                        <button className="btn btn-sm" disabled={task.status !== 'running'} onClick={() => void killTask(task)}>
                          ⏹ {t('停止')}
                        </button>
                        <button className="btn btn-sm btn-ghost" style={{ marginLeft: 6 }} onClick={() => void openLogs(task)}>
                          📄 {t('日志')}
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

      {/* 执行对话框 */}
      {execOpen && (
        <div className="modal-mask" onClick={() => !execBusy && setExecOpen(false)}>
          <div className="modal" style={{ width: 560 }} onClick={(e) => e.stopPropagation()}>
            <h3>{t('执行命令')}</h3>
            <div style={{ marginBottom: 12 }}>
              <div style={{ fontSize: 12, color: 'var(--text-1)', marginBottom: 4 }}>{t('命令')}</div>
              <pre
                style={{
                  margin: 0,
                  padding: 10,
                  borderRadius: 8,
                  background: 'rgba(0,0,0,0.3)',
                  border: '1px solid rgba(var(--rgb-line),0.25)',
                  fontSize: 12,
                  maxHeight: 120,
                  overflow: 'auto',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-all',
                }}
              >
                {execCommand}
              </pre>
            </div>

            <div style={{ fontSize: 12, color: 'var(--text-1)', marginBottom: 6 }}>
              {t('目标服务器（已选 {0} / {1}）', selHosts.size, hosts.length)}
            </div>
            <div
              style={{
                border: '1px solid rgba(var(--rgb-line),0.25)',
                borderRadius: 8,
                maxHeight: 200,
                overflow: 'auto',
                padding: 4,
              }}
            >
              {hosts.length === 0 ? (
                <div style={{ padding: 12, color: 'var(--text-1)', fontSize: 13 }}>{t('暂无服务器请先在桌面添加服务器')}</div>
              ) : (
                hosts.map((h) => (
                  <label
                    key={h.id}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 8,
                      padding: '6px 8px',
                      borderRadius: 6,
                      cursor: 'pointer',
                      fontSize: 13,
                    }}
                  >
                    <input type="checkbox" checked={selHosts.has(h.id)} onChange={() => toggleHost(h.id)} />
                    <span style={{ fontWeight: 600 }}>{h.name}</span>
                    <span style={{ color: 'var(--text-1)', fontSize: 12 }}>
                      {h.username}@{h.host}:{h.port}
                    </span>
                    <span style={{ marginLeft: 'auto', fontSize: 13 }}>{h.platform === 'windows' ? '🪟' : h.platform === 'linux' ? '🐧' : ''}</span>
                  </label>
                ))
              )}
            </div>
            <div style={{ marginTop: 6, display: 'flex', gap: 8 }}>
              <button className="btn btn-sm btn-ghost" onClick={() => setSelHosts(new Set(hosts.map((h) => h.id)))}>
                {t('全选')}
              </button>
              <button className="btn btn-sm btn-ghost" onClick={() => setSelHosts(new Set())}>
                {t('清空')}
              </button>
            </div>

            <div style={{ marginTop: 14, display: 'flex', gap: 16 }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, cursor: 'pointer' }}>
                <input type="radio" checked={execMode === 'fg'} onChange={() => setExecMode('fg')} />
                {t('前台终端打开终端查看效果')}
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, cursor: 'pointer' }}>
                <input type="radio" checked={execMode === 'bg'} onChange={() => setExecMode('bg')} />
                {t('后台运行不打开终端')}
              </label>
            </div>

            {execResult && (
              <div style={{ marginTop: 12, maxHeight: 120, overflow: 'auto', fontSize: 12.5 }}>
                {execResult.map((r) => (
                  <div key={r.hostId} style={{ padding: '3px 0', color: r.ok ? '#34d399' : 'var(--red)' }}>
                    {r.ok ? t('已启动：{0}（PID {1}）', r.hostName, r.task?.pid ?? '') : t('失败：{0} {1}', r.hostName || r.hostId, r.error || t('操作失败'))}
                  </div>
                ))}
              </div>
            )}

            <div className="footer">
              <button className="btn btn-ghost" disabled={execBusy} onClick={() => setExecOpen(false)}>
                {t('取消')}
              </button>
              <button className="btn" disabled={execBusy || selHosts.size === 0} onClick={() => void doExec()}>
                {execBusy ? t('执行中…') : execMode === 'bg' ? t('后台执行') : t('前台执行')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 日志对话框 */}
      {logTask && (
        <div className="modal-mask" onClick={() => setLogTask(null)}>
          <div className="modal" style={{ width: 640 }} onClick={(e) => e.stopPropagation()}>
            <h3>{t('输出日志：{0}（PID {1}）', logTask.hostName, logTask.pid)}</h3>
            <pre
              style={{
                margin: 0,
                padding: 12,
                borderRadius: 8,
                background: 'rgba(0,0,0,0.35)',
                border: '1px solid rgba(var(--rgb-line),0.25)',
                fontSize: 12,
                lineHeight: 1.5,
                maxHeight: 420,
                overflow: 'auto',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
                color: 'var(--text-0)',
              }}
            >
              {logLoading ? t('加载中…') : logText}
            </pre>
            <div className="footer">
              <button className="btn" onClick={() => setLogTask(null)}>
                {t('关闭')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function fmtBytes(kb: number): string {
  if (kb >= 1024 * 1024) return `${(kb / 1024 / 1024).toFixed(1)} GB`
  if (kb >= 1024) return `${(kb / 1024).toFixed(1)} MB`
  return `${kb} KB`
}

function pad(n: number): string {
  return n < 10 ? `0${n}` : String(n)
}

function fmtStarted(unix: number): string {
  if (!unix) return '—'
  const d = new Date(unix * 1000)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** "2026-08-03 21:00:00" → "08-03 21:00" */
function fmtTime(sqlTime: string): string {
  if (!sqlTime) return '—'
  const m = /^\d{4}-(\d{2}-\d{2}) (\d{2}:\d{2})/.exec(sqlTime)
  return m ? `${m[1]} ${m[2]}` : sqlTime
}
