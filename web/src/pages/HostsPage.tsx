import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../lib/api'
import { useSession } from '../lib/session'
import { useT, transErr } from '../lib/i18n'
import { HostFormModal } from '../components/HostFormModal'
import type { Host } from '../lib/types'

export function HostsPage() {
  const navigate = useNavigate()
  const t = useT()
  const [hosts, setHosts] = useState<Host[]>([])
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<Host | null>(null)

  const refresh = useCallback(async () => {
    try {
      setHosts(await api.listHosts())
    } catch {
      // 401 由全局会话状态统一处理（置 authed=false → 路由守卫跳登录）
      // 其它错误静默，保持页面状态
    }
  }, [])

  useEffect(() => {
    void refresh()
    const timer = setInterval(() => void refresh(), 15000)
    return () => clearInterval(timer)
  }, [refresh])

  const openCreate = () => {
    setEditing(null)
    setModalOpen(true)
  }

  const openEdit = (h: Host) => {
    setEditing(h)
    setModalOpen(true)
  }

  const remove = async (h: Host) => {
    if (!window.confirm(t('确认删除主机「{0}」？', h.name))) return
    try {
      await api.deleteHost(h.id)
      void refresh()
    } catch (e) {
      alert(transErr(e, '删除失败'))
    }
  }

  const connect = async (h: Host) => {
    try {
      const r = await api.connect(h.id)
      alert(t('连接成功\n主机指纹：{0}', r.fingerprint))
      void refresh()
    } catch (e) {
      alert(transErr(e, '连接失败'))
    }
  }

  const logout = async () => {
    await api.logout()
    useSession.getState().setAuthed(false)
    navigate(useSession.getState().loginRoute, { replace: true })
  }

  return (
    <div className="hosts-page">
      <div className="hosts-header">
        <div className="brand">{t('🛰 EZSSH · 主机管理')}</div>
        <div className="actions">
          <button className="btn btn-sm" onClick={() => navigate('/desktop')}>
            {t('🪟 进入桌面')}
          </button>
          <button className="btn btn-sm btn-ghost" onClick={openCreate}>
            {t('＋ 新增主机')}
          </button>
          <button className="btn btn-sm btn-ghost" onClick={logout}>
            {t('退出登录')}
          </button>
        </div>
      </div>

      <div className="hosts-body">
        {hosts.length === 0 ? (
          <div className="empty-tip">
            {t('还没有主机，点击右上角「新增主机」添加你的第一台服务器。')}
          </div>
        ) : (
          <table className="hosts-table">
            <thead>
              <tr>
                <th>{t('状态')}</th>
                <th>{t('名称')}</th>
                <th>{t('地址')}</th>
                <th>{t('端口')}</th>
                <th>{t('用户')}</th>
                <th>{t('认证')}</th>
                <th>{t('分组')}</th>
                <th>{t('备注')}</th>
                <th>{t('操作')}</th>
              </tr>
            </thead>
            <tbody>
              {hosts.map((h) => (
                <tr key={h.id}>
                  <td>
                    <span className={`dot ${h.connected ? 'online' : 'offline'}`} />
                    {h.connected ? t('在线') : t('离线')}
                  </td>
                  <td>{h.name}</td>
                  <td>{h.host}</td>
                  <td>{h.port}</td>
                  <td>{h.username}</td>
                  <td>{h.auth_type === 'password' ? t('密码') : t('密钥')}</td>
                  <td>{h.group_name && <span className="tag">{h.group_name}</span>}</td>
                  <td>{h.remark}</td>
                  <td>
                    <div className="row-actions">
                      <button className="btn btn-sm btn-ghost" onClick={() => connect(h)}>
                        {t('连接')}
                      </button>
                      <button className="btn btn-sm btn-ghost" onClick={() => openEdit(h)}>
                        {t('编辑')}
                      </button>
                      <button className="btn btn-sm btn-danger" onClick={() => remove(h)}>
                        {t('删除')}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <HostFormModal
        open={modalOpen}
        editing={editing}
        onClose={() => setModalOpen(false)}
        onSaved={() => void refresh()}
      />
    </div>
  )
}
