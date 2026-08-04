import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useT, transErr } from '../lib/i18n'
import { useEscClose } from '../lib/escClose'
import type { Host, HostInput } from '../lib/types'

interface HostForm {
  name: string
  host: string
  port: number
  username: string
  auth_type: 'password' | 'privatekey'
  password: string
  private_key: string
  group_name: string
  remark: string
  platform: '' | 'linux' | 'windows'
}

const emptyForm: HostForm = {
  name: '',
  host: '',
  port: 22,
  username: 'root',
  auth_type: 'password',
  password: '',
  private_key: '',
  group_name: '',
  remark: '',
  platform: '',
}

interface Props {
  open: boolean
  /** 传入则为编辑模式 */
  editing?: Host | null
  onClose: () => void
  onSaved: () => void
}

interface TestResult {
  ok: boolean
  message: string
}

/**
 * 新增/编辑主机表单弹窗（HostsPage 与桌面任务栏共用）。
 */
export function HostFormModal({ open, editing, onClose, onSaved }: Props) {
  const t = useT()
  const [form, setForm] = useState<HostForm>(emptyForm)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<TestResult | null>(null)
  const [msg, setMsg] = useState('')

  // ESC 关闭表单弹窗（点击遮罩不关闭，防止误触丢失输入）
  useEscClose(open, onClose)

  useEffect(() => {
    if (!open) return
    setMsg('')
    setTestResult(null)
    if (editing) {
      setForm({
        name: editing.name,
        host: editing.host,
        port: editing.port,
        username: editing.username,
        auth_type: editing.auth_type,
        password: '',
        private_key: '',
        group_name: editing.group_name,
        remark: editing.remark,
        platform: editing.platform === 'linux' || editing.platform === 'windows' ? editing.platform : '',
      })
    } else {
      setForm(emptyForm)
    }
  }, [open, editing])

  if (!open) return null

  const buildInput = (): HostInput => ({
    name: form.name.trim(),
    host: form.host.trim(),
    port: form.port || 22,
    username: form.username.trim(),
    auth_type: form.auth_type,
    password: form.auth_type === 'password' ? form.password : undefined,
    private_key: form.auth_type === 'privatekey' ? form.private_key : undefined,
    group_name: form.group_name.trim(),
    remark: form.remark.trim(),
    platform: form.platform || undefined,
  })

  const testConnect = async () => {
    setTesting(true)
    setTestResult(null)
    // 未填必填项时先提示
    if (!form.host.trim() || !form.username.trim()) {
      setTestResult({ ok: false, message: t('请先填写地址和用户名') })
      setTesting(false)
      return
    }
    try {
      const r = await api.testConnect(buildInput())
      // 用户未显式选择（自动检测）时，用探测结果回填系统类型
      setForm((prev) =>
        r.platform && !prev.platform
          ? { ...prev, platform: r.platform as '' | 'linux' | 'windows' }
          : prev,
      )
      setTestResult({
        ok: true,
        message: t('连接成功，主机指纹：{0}', r.fingerprint),
      })
    } catch (e) {
      setTestResult({
        ok: false,
        message: transErr(e, '连接失败'),
      })
    } finally {
      setTesting(false)
    }
  }

  const save = async () => {
    setSaving(true)
    setMsg('')
    try {
      const input = buildInput()
      if (editing) {
        await api.updateHost(editing.id, input)
      } else {
        await api.createHost(input)
      }
      onSaved()
      onClose()
    } catch (e) {
      setMsg(transErr(e, '保存失败'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="modal-mask">
      <div className="modal">
        <h3>{editing ? t('编辑主机 {0}', editing.name) : t('新增主机')}</h3>

        <div className="field">
          <label>{t('名称')}</label>
          <input
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder={t('如：web-01')}
            autoFocus
          />
        </div>

        <div className="field">
          <label>{t('地址（IP 或域名）')}</label>
          <input
            value={form.host}
            onChange={(e) => setForm({ ...form, host: e.target.value })}
            placeholder="192.168.1.10"
          />
        </div>

        <div className="field">
          <label>{t('端口')}</label>
          <input
            type="number"
            value={form.port}
            onChange={(e) => setForm({ ...form, port: Number(e.target.value) })}
          />
        </div>

        <div className="field">
          <label>{t('用户名')}</label>
          <input
            value={form.username}
            onChange={(e) => setForm({ ...form, username: e.target.value })}
          />
        </div>

        <div className="field">
          <label>{t('认证方式')}</label>
          <select
            value={form.auth_type}
            onChange={(e) =>
              setForm({ ...form, auth_type: e.target.value as 'password' | 'privatekey' })
            }
          >
            <option value="password">{t('密码')}</option>
            <option value="privatekey">{t('私钥')}</option>
          </select>
        </div>

        <div className="field">
          <label>{t('系统类型')}</label>
          <select
            value={form.platform}
            onChange={(e) =>
              setForm({ ...form, platform: e.target.value as '' | 'linux' | 'windows' })
            }
            title={t('自动检测：首次连接后探测并持久化；也可手动指定以跳过探测')}
          >
            <option value="">{t('自动检测')}</option>
            <option value="linux">{t('Linux')}</option>
            <option value="windows">{t('Windows')}</option>
          </select>
        </div>

        {form.auth_type === 'password' ? (
          <div className="field">
            <label>{editing ? t('密码（留空表示不修改）') : t('密码')}</label>
            <input
              type="password"
              value={form.password}
              onChange={(e) => setForm({ ...form, password: e.target.value })}
            />
          </div>
        ) : (
          <div className="field">
            <label>{editing ? t('私钥（PEM）（留空表示不修改）') : t('私钥（PEM）')}</label>
            <textarea
              value={form.private_key}
              onChange={(e) => setForm({ ...form, private_key: e.target.value })}
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
            />
          </div>
        )}

        <div className="field">
          <label>{t('分组')}</label>
          <input
            value={form.group_name}
            onChange={(e) => setForm({ ...form, group_name: e.target.value })}
            placeholder={t('如：prod / dev')}
          />
        </div>

        <div className="field">
          <label>{t('备注')}</label>
          <input
            value={form.remark}
            onChange={(e) => setForm({ ...form, remark: e.target.value })}
          />
        </div>

        <div className="error-text">{msg}</div>

        {/* 连通性测试结果 */}
        {testResult && (
          <div
            style={{
              marginTop: 4,
              marginBottom: 12,
              padding: '8px 12px',
              borderRadius: 8,
              fontSize: 13,
              background: testResult.ok ? 'rgba(34,197,94,0.12)' : 'rgba(239,68,68,0.12)',
              color: testResult.ok ? 'var(--green)' : 'var(--red)',
              wordBreak: 'break-all',
            }}
          >
            {testResult.ok ? '✅ ' : '❌ '}
            {testResult.message}
          </div>
        )}

        <div className="footer">
          <button className="btn btn-ghost" onClick={onClose}>
            {t('取消')}
          </button>
          <button className="btn btn-ghost" disabled={testing || saving} onClick={testConnect}>
            {testing ? t('测试中…') : t('🛠 测试连通性')}
          </button>
          <button className="btn" disabled={saving} onClick={save}>
            {saving ? t('保存中…') : t('保存')}
          </button>
        </div>
      </div>
    </div>
  )
}
