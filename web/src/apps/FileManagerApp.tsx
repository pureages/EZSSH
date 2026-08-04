import { useCallback, useEffect, useRef, useState } from 'react'
import { api, ApiError } from '../lib/api'
import { setPendingTerminalCwd } from '../lib/terminalLaunch'
import { consumePendingFmCwd } from '../lib/fmLaunch'
import { newChannelId, ws } from '../lib/ws'
import { useWindowStore } from '../desktop/windowStore'
import { useClipboardStore } from '../lib/clipboardStore'
import { useT, tt } from '../lib/i18n'
import type { ClipItem } from '../lib/clipboardStore'
import type { Host, SftpEntry } from '../lib/types'
import type { AppProps } from '../desktop/appRegistry'
import { CodeEditor } from '../components/CodeEditor'

const ROOT = '/'

interface CtxMenu {
  x: number
  y: number
  entry: SftpEntry | null
  /** 快速访问条目路径（右键"快速访问"中的文件夹时设置，菜单沿用文件列表逻辑但作用于该绝对路径） */
  qaPath?: string
}

/** 预览面板状态 */
interface Preview {
  path: string
  name: string
  /** text=可编辑文本；image/video/audio=只读内联预览 */
  kind: 'text' | 'image' | 'video' | 'audio'
  content: string
  dirty: boolean
}

// 图片/视频/音频扩展名（内联预览，不支持编辑）
const IMAGE_EXTS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'svg', 'ico', 'avif'])
const VIDEO_EXTS = new Set(['mp4', 'webm', 'ogv', 'ogg', 'mov', 'm4v', 'mkv'])
const AUDIO_EXTS = new Set(['mp3', 'wav', 'm4a'])

// 压缩包扩展名（右键解压）
const ARCHIVE_RE = /\.(zip|tar|tgz|tbz2|txz)$|\.tar\.(gz|bz2|xz)$/i
const isArchive = (name: string) => ARCHIVE_RE.test(name)

function mediaKindOf(name: string): 'image' | 'video' | 'audio' | null {
  const lower = name.toLowerCase()
  const idx = lower.lastIndexOf('.')
  if (idx === -1) return null
  const ext = lower.slice(idx + 1)
  if (IMAGE_EXTS.has(ext)) return 'image'
  if (VIDEO_EXTS.has(ext)) return 'video'
  if (AUDIO_EXTS.has(ext)) return 'audio'
  return null
}

/** 主题化输入弹窗配置 */
interface InputDialogState {
  title: string
  label: string
  initial: string
  placeholder: string
  confirmText: string
  /** 校验返回值：null 表示非法（显示错误） */
  validate?: (v: string) => string | null
  resolve: (v: string | null) => void
}

/**
 * 二进制内容检测：预览不限制扩展名，而是读取后判断内容是否可编辑文本。
 * 含 NUL 字节，或前 4KB 中控制字符占比过高，视为二进制文件。
 */
function isBinaryContent(content: string): boolean {
  const sample = content.slice(0, 4096)
  if (sample.includes('\u0000')) return true
  let control = 0
  for (let i = 0; i < sample.length; i++) {
    const c = sample.charCodeAt(i)
    if (c < 9 || (c > 13 && c < 32)) control++
  }
  return control > Math.max(4, sample.length * 0.05)
}

function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function fmtTime(ts: number): string {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  return d.toLocaleString()
}

/** 剪贴板多条目展示名：超过 3 项折叠显示 */
function fmtClipNames(item: ClipItem): string {
  const names = item.items.map((i) => i.name)
  if (names.length > 3) return `${names.slice(0, 3).join(tt('、'))} ${tt('等 {0} 项', names.length)}`
  return names.join(tt('、'))
}

/**
 * 文件管理器 App：SFTP 目录浏览、上传/下载、右侧实时预览编辑、权限修改。
 */
export function FileManagerApp({ hostId, platform }: AppProps) {
  const t = useT()
  const isWin = platform === 'windows'
  const [cwd, setCwd] = useState(ROOT)
  const [entries, setEntries] = useState<SftpEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [ctx, setCtx] = useState<CtxMenu | null>(null)
  const [preview, setPreview] = useState<Preview | null>(null)
  const [previewErr, setPreviewErr] = useState('')
  const [uploading, setUploading] = useState<string | null>(null)
  const [uploadPct, setUploadPct] = useState(0)
  const [uploadBytes, setUploadBytes] = useState('')
  const [dialog, setDialog] = useState<InputDialogState | null>(null)
  const [dialogValue, setDialogValue] = useState('')
  const [dialogErr, setDialogErr] = useState('')
  // 全屏播放器（双击媒体打开）：image/video（音频在右侧面板内联播放）
  const [player, setPlayer] = useState<{ path: string; name: string; kind: 'image' | 'video' } | null>(null)
  /** 大编辑窗口（双击文本/代码文件打开），比右侧预览面板更宽更高 */
  const [bigEditor, setBigEditor] = useState<{ path: string; name: string; content: string; dirty: boolean } | null>(null)
  const [imgScale, setImgScale] = useState(1)
  const [imgRotate, setImgRotate] = useState(0)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [hi, setHi] = useState(0)
  // 历史栈的 ref（源）+ 指针 state：导航失败需在异步回调里同步修正历史（保持 hi === history.length-1）
  const historyRef = useRef<string[]>([ROOT])
  const hiRef = useRef(0)
  // 最近一次成功加载的目录：导航失败（如无权限）时回退到它，避免地址栏停在打不开的路径
  const lastGoodRef = useRef(ROOT)
  /** 回退后跳过该目录的重复加载（entries 仍是上次成功目录的内容） */
  const skipLoadRef = useRef(false)
  // 复制/剪切/粘贴
  const clipItem = useClipboardStore((s) => s.item)
  const [pasting, setPasting] = useState<string | null>(null)
  /** 粘贴进度百分比；>=0 显示进度条，-1 表示无精确进度（直连/移动） */
  const [pastePct, setPastePct] = useState(-1)
  /** 当前传输的 AbortController，用于"取消"中止 */
  const cancelRef = useRef<AbortController | null>(null)
  // 直连传输失败提示（中英对照 + 安装 sshpass）
  const [directErr, setDirectErr] = useState<{ clip: ClipItem; detail: string } | null>(null)
  const [hostNames, setHostNames] = useState<Record<string, string>>({})
  /** 主机平台映射（用于跨主机窗口的 platform 传递） */
  const [hostPlatforms, setHostPlatforms] = useState<Record<string, string>>({})
  const open = useWindowStore((s) => s.open)

  // ---- 多选 / 键盘 / 地址栏（Windows 资源管理器风格）----
  /** 选中的条目名集合（当前目录内） */
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const selectedRef = useRef<Set<string>>(new Set())
  /** shift+点击 范围选区的锚点（最近一次单击的行） */
  const anchorRef = useRef<string | null>(null)
  /** 框选矩形（viewport 坐标）；null = 未框选 */
  const [bandRect, setBandRect] = useState<{ x: number; y: number; w: number; h: number } | null>(null)
  const bandStart = useRef<{ x: number; y: number } | null>(null)
  /** 开始框选前的选中集：ctrl 追加模式在此基础上并集 */
  const preBandSel = useRef<Set<string>>(new Set())
  const rowElsRef = useRef<Map<string, HTMLTableRowElement>>(new Map())
  const rootRef = useRef<HTMLDivElement>(null)
  /** 地址栏输入值；null = 未编辑（显示当前目录） */
  const [addr, setAddr] = useState<string | null>(null)
  /** 快速访问路径列表（按主机隔离，localStorage 持久化） */
  const [quickAccess, setQuickAccess] = useState<string[]>([])
  /** 行内重命名编辑状态（null = 未在编辑） */
  const [editingName, setEditingName] = useState<{ name: string; value: string } | null>(null)
  /** 行内重命名是否已在提交中（防 Enter 后输入框卸载触发重复 blur 提交） */
  const renamingRef = useRef(false)
  /** 用户按 Esc 取消重命名（输入框卸载触发的 blur 不应再提交） */
  const renameCancelRef = useRef(false)
  /** 快速访问右键"重命名"→ 导航到父目录后待命触发的行内重命名目标 */
  const pendingRenameRef = useRef<string | null>(null)

  const updateSelected = useCallback((next: Set<string>) => {
    selectedRef.current = next
    setSelected(next)
  }, [])

  // 目录切换后清空选中与框选
  useEffect(() => {
    updateSelected(new Set())
    anchorRef.current = null
    setBandRect(null)
    bandStart.current = null
    preBandSel.current = new Set()
  }, [cwd, updateSelected])

  // 快速访问：加载（按主机隔离）
  useEffect(() => {
    if (!hostId) return
    try {
      const all = JSON.parse(localStorage.getItem('ezssh_quick_access') ?? '{}')
      if (Array.isArray(all[hostId])) setQuickAccess(all[hostId])
    } catch {
      /* ignore */
    }
  }, [hostId])

  // 快速访问：持久化
  useEffect(() => {
    if (!hostId) return
    try {
      const all = JSON.parse(localStorage.getItem('ezssh_quick_access') ?? '{}')
      all[hostId] = quickAccess
      localStorage.setItem('ezssh_quick_access', JSON.stringify(all))
    } catch {
      /* ignore */
    }
  }, [quickAccess, hostId])

  // 加载主机名与平台映射（用于展示剪贴板来源、跨服务器弹窗提示）
  useEffect(() => {
    void api
      .listHosts()
      .then((hs: Host[]) => {
        setHostNames(Object.fromEntries(hs.map((h) => [h.id, h.name])))
        setHostPlatforms(Object.fromEntries(hs.map((h) => [h.id, h.platform ?? ''])))
      })
      .catch(() => {})
  }, [])

  const load = useCallback(
    async (dir: string): Promise<boolean> => {
      if (!hostId) return false
      setLoading(true)
      setError('')
      try {
        const list = await api.sftpList(hostId, dir)
        list.sort((a, b) => Number(b.is_dir) - Number(a.is_dir) || a.name.localeCompare(b.name))
        setEntries(list)
        lastGoodRef.current = dir
        // 快速访问右键"重命名"→ 导航到父目录后，自动对该条目进入行内重命名
        if (pendingRenameRef.current) {
          const target = list.find((x) => x.name === pendingRenameRef.current)
          pendingRenameRef.current = null
          if (target) startRename(target)
        }
        return true
      } catch (e) {
        setError(e instanceof ApiError ? e.message : t('加载失败'))
        return false
      } finally {
        setLoading(false)
      }
    },
    [hostId],
  )

  // 从「网站管理 → 查看文件」跳转而来：首次渲染即消费目标目录。
  // 放在 render 期（非 useState 初始化器）保证 StrictMode 双渲染下只消费一次，
  // 且 cwd 直接以目标目录起步，避免先加载根目录再跳转造成内容与地址栏不一致。
  const mountPendingRef = useRef<string | null>(null)
  if (mountPendingRef.current === null && hostId) {
    mountPendingRef.current = consumePendingFmCwd(hostId)
  }

  useEffect(() => {
    // 来自「查看文件」的跳转：首次加载直接使用目标目录，跳过根目录加载
    if (mountPendingRef.current) {
      const pending = mountPendingRef.current
      mountPendingRef.current = null
      historyRef.current = [pending]
      hiRef.current = 0
      setHi(0)
      setCwd(pending)
      return
    }
    // 回退导航造成的重复加载：entries 仍是上次成功目录的内容，无需重新拉取
    if (skipLoadRef.current) {
      skipLoadRef.current = false
      return
    }
    let cancelled = false
    void load(cwd).then((ok) => {
      if (cancelled) return
      if (!ok && cwd !== lastGoodRef.current) {
        // 导航失败（如进入无权限目录）：回退到最近成功加载的目录，
        // 避免地址栏停留在打不开的路径、后续 join() 拼出 /root/home 这类错误路径
        const lastGood = lastGoodRef.current
        const idx = historyRef.current.lastIndexOf(lastGood)
        const nh = idx >= 0 ? historyRef.current.slice(0, idx + 1) : [lastGood]
        historyRef.current = nh
        hiRef.current = nh.length - 1
        setHi(hiRef.current)
        skipLoadRef.current = true
        setCwd(lastGood)
      }
    })
    return () => {
      cancelled = true
    }
  }, [cwd, load])

  /** 主题化输入弹窗（替代 window.prompt） */
  const askInput = (opts: Omit<InputDialogState, 'resolve'>): Promise<string | null> =>
    new Promise((resolve) => {
      setDialogValue(opts.initial)
      setDialogErr('')
      setDialog({ ...opts, resolve })
    })

  const closeDialog = (result: string | null) => {
    const d = dialog
    setDialog(null)
    d?.resolve(result)
  }

  const confirmDialog = () => {
    if (!dialog) return
    if (dialog.validate) {
      const err = dialog.validate(dialogValue)
      if (err) {
        setDialogErr(err)
        return
      }
    }
    closeDialog(dialogValue)
  }

  const navTo = (dir: string, opts?: { keepPendingRename?: boolean }) => {
    if (!opts?.keepPendingRename) pendingRenameRef.current = null
    // 用户主动导航：恢复跳过标志（若上次是失败回退留下的）
    skipLoadRef.current = false
    const nh = [...historyRef.current.slice(0, hiRef.current + 1), dir]
    historyRef.current = nh
    hiRef.current = nh.length - 1
    setHi(hiRef.current)
    setCwd(dir)
    setPreview(null)
    setPreviewErr('')
  }

  const goBack = () => {
    if (hiRef.current > 0) {
      skipLoadRef.current = false
      hiRef.current -= 1
      setHi(hiRef.current)
      setCwd(historyRef.current[hiRef.current])
    }
  }

  const goUp = () => {
    if (cwd === ROOT) return
    if (isWin) {
      // Windows 展示路径归一化（历史状态可能有重复斜杠，如 C://Users）
      const norm = cwd.replace(/\/+/g, '/')
      // 裸盘符（C:，防御性）→ C:/
      if (/^[A-Za-z]:$/.test(norm)) {
        navTo(norm + '/')
        return
      }
      // 盘符根（C:/）→ 盘符列表（/）
      if (/^[A-Za-z]:\/$/.test(norm)) {
        navTo(ROOT)
        return
      }
      // 仅盘符 + 一级目录（C:/Users）→ 盘符根 C:/
      if (/^[A-Za-z]:\/[^/]+$/.test(norm)) {
        navTo(norm.slice(0, 2) + '/')
        return
      }
      // 多级目录（C:/Users/mypureagestay/…）→ 去掉末尾一段
      const up = norm.slice(0, norm.lastIndexOf('/'))
      navTo(up || ROOT)
      return
    }
    const up = cwd.slice(0, cwd.lastIndexOf('/')) || ROOT
    navTo(up)
  }

  // Windows 盘符列表（根目录）下盘符条目按 "C:" 显示，进入时拼接为 "C:/"
  const join = (name: string) => {
    if (cwd === ROOT && isWin && name.endsWith(':')) return `${name}/`
    if (cwd === ROOT) return `/${name}`
    // 去除 cwd 尾部斜杠，避免盘符根（C:/）拼接出重复斜杠（C://Users）
    const base = cwd.replace(/\/+$/, '')
    return `${base}/${name}`
  }

  const openEntry = (e: SftpEntry) => {
    if (e.is_dir) {
      navTo(join(e.name))
      return
    }
    const media = mediaKindOf(e.name)
    if (media === 'image' || media === 'video') {
      // 双击图片/视频 → 全屏播放器
      setImgScale(1)
      setImgRotate(0)
      setPlayer({ path: join(e.name), name: e.name, kind: media })
      return
    }
    if (media === 'audio') {
      // 音频在右侧面板内联播放
      void showPreview(join(e.name), e.name)
      return
    }
    // 文本/代码文件 → 打开大编辑窗口
    void openBigEditor(join(e.name), e.name)
  }

  /** 在右侧面板加载文件：图片/视频内联预览；其余读取后做二进制检测 */
  const showPreview = async (p: string, name: string) => {
    if (!hostId) return
    setPreviewErr('')
    const media = mediaKindOf(name)
    if (media) {
      setPreview({ path: p, name, kind: media, content: '', dirty: false })
      return
    }
    try {
      const r = await api.sftpRead(hostId, p)
      if (isBinaryContent(r.content)) {
        setPreview(null)
        setPreviewErr(t('{0} 不支持的格式，无法预览', name))
        return
      }
      setPreview({ path: p, name: r.name, kind: 'text', content: r.content, dirty: false })
    } catch (err) {
      setPreview(null)
      setPreviewErr(err instanceof ApiError ? err.message : t('读取失败'))
    }
  }

  const savePreview = async () => {
    if (!preview || !hostId) return
    try {
      await api.sftpWrite(hostId, preview.path, preview.content)
      setPreview({ ...preview, dirty: false })
      void load(cwd)
    } catch (err) {
      alert(err instanceof ApiError ? err.message : t('保存失败'))
    }
  }

  /** 字数统计（Unicode 码点数，中文/emoji 准确；线性扫描避免大数组分配） */
  const textCharCount = (s: string) => {
    let n = 0
    for (let i = 0; i < s.length; i++) {
      const c = s.charCodeAt(i)
      if (c >= 0xd800 && c <= 0xdbff && i + 1 < s.length) {
        const c2 = s.charCodeAt(i + 1)
        if (c2 >= 0xdc00 && c2 <= 0xdfff) i++
      }
      n++
    }
    return n
  }
  const textLineCount = (s: string) => (s ? s.split('\n').length : 1)

  /** 双击文本/代码文件：读取后打开大编辑窗口；二进制回落到右侧预览报错 */
  const openBigEditor = async (p: string, name: string) => {
    if (!hostId) return
    try {
      const r = await api.sftpRead(hostId, p)
      if (isBinaryContent(r.content)) {
        setPreview(null)
        setPreviewErr(t('{0} 不支持的格式，无法预览', name))
        return
      }
      setBigEditor({ path: p, name: r.name, content: r.content, dirty: false })
    } catch (err) {
      alert(err instanceof ApiError ? err.message : t('读取失败'))
    }
  }

  const saveBigEditor = async () => {
    if (!bigEditor || !hostId) return
    try {
      await api.sftpWrite(hostId, bigEditor.path, bigEditor.content)
      setBigEditor({ ...bigEditor, dirty: false })
      // 若右侧预览面板正打开同一文件，同步内容与保存态
      setPreview((p) => (p && p.path === bigEditor.path ? { ...p, content: bigEditor.content, dirty: false } : p))
      void load(cwd)
    } catch (err) {
      alert(err instanceof ApiError ? err.message : t('保存失败'))
    }
  }

  /** 关闭大编辑窗口：有未保存修改时先确认 */
  const closeBigEditor = () => {
    if (bigEditor?.dirty && !window.confirm(t('文件有未保存的修改，确定关闭？'))) return
    setBigEditor(null)
  }

  const mkdir = async () => {
    if (!hostId) return
    const name = await askInput({
      title: t('新建目录'),
      label: t('目录名'),
      initial: '',
      placeholder: t('如：my-folder'),
      confirmText: t('创建'),
      validate: (v) => (v.trim() === '' ? t('目录名不能为空') : null),
    })
    if (!name) return
    try {
      await api.sftpMkdir(hostId, join(name.trim()))
      void load(cwd)
    } catch (err) {
      alert(err instanceof ApiError ? err.message : t('创建失败'))
    }
  }

  /** 新建空文档 */
  const newFile = async () => {
    if (!hostId) return
    const name = await askInput({
      title: t('新建文档'),
      label: t('文档名'),
      initial: 'newfile.txt',
      placeholder: t('如：notes.txt'),
      confirmText: t('创建'),
      validate: (v) => (v.trim() === '' ? t('文档名不能为空') : null),
    })
    if (!name) return
    const p = join(name.trim())
    try {
      await api.sftpWrite(hostId, p, '')
      void load(cwd)
    } catch (err) {
      alert(err instanceof ApiError ? err.message : t('创建失败'))
    }
  }

  /** 在当前目录打开终端（自动 cd 到此目录） */
  const openTerminalHere = async () => {
    if (!hostId) return
    // 确保 WS 已连接，让终端打开成功
    try {
      await ws.connect()
    } catch {
      /* 忽略，终端组件会自处理 */
    }
    setPendingTerminalCwd(hostId, cwd)
    open({
      id: `terminal-${Date.now().toString(36)}`,
      appId: 'terminal',
      title: hostNames[hostId] ? `${hostNames[hostId]} - ${tt('终端')}` : tt('终端'),
      titleKey: '{0} - {1}',
      titleArgs: [hostNames[hostId] || tt('终端'), tt('终端')],
      icon: '🖥️',
      hostId,
      platform: platform ?? '',
      channelId: newChannelId(),
      x: 80,
      y: 60,
      width: 760,
      height: 460,
    })
  }

  /** 行内重命名：F2 或右键「重命名」进入文件名直接编辑（Windows 资源管理器风格） */
  const startRename = (e: SftpEntry) => {
    renamingRef.current = false
    renameCancelRef.current = false
    updateSelected(new Set([e.name]))
    anchorRef.current = e.name
    setEditingName({ name: e.name, value: e.name })
  }

  /** 提交行内重命名；空名/未变忽略 */
  const commitRename = async (e: SftpEntry) => {
    if (renamingRef.current) return
    if (renameCancelRef.current) {
      // Esc 已取消：输入框卸载触发的 blur 不再提交
      renameCancelRef.current = false
      return
    }
    if (!editingName || editingName.name !== e.name) return
    const newName = editingName.value.trim()
    setEditingName(null)
    renamingRef.current = true
    if (!newName || newName === e.name || !hostId) {
      renamingRef.current = false
      return
    }
    try {
      await api.sftpRename(hostId, join(e.name), join(newName))
      void load(cwd)
    } catch (err) {
      alert(err instanceof ApiError ? err.message : t('重命名失败'))
    } finally {
      renamingRef.current = false
    }
  }

  const doChmodPath = async (p: string, initialMode: string) => {
    if (!hostId) return
    const mode = await askInput({
      title: t('修改权限'),
      label: t('权限（八进制）'),
      initial: initialMode,
      placeholder: t('如：644'),
      confirmText: t('应用'),
      validate: (v) => {
        const n = parseInt(v.trim(), 8)
        if (Number.isNaN(n) || v.trim() === '') return t('请输入合法的八进制权限')
        return null
      },
    })
    if (mode === null) return
    try {
      await api.sftpChmod(hostId, p, parseInt(mode.trim(), 8))
      void load(cwd)
    } catch (err) {
      alert(err instanceof ApiError ? err.message : t('修改失败'))
    }
  }

  const doChmod = (e: SftpEntry) =>
    void doChmodPath(join(e.name), (e.mode_num & 0o777).toString(8))

  /** 解压压缩包（原位解压到归档所在目录） */
  const doExtract = async (e: SftpEntry) => {
    if (!hostId) return
    try {
      await api.sftpExtract(hostId, join(e.name))
      void load(cwd)
    } catch (err) {
      alert(err instanceof ApiError ? err.message : t('解压失败'))
    }
  }

  /** 复制文件/文件夹的绝对路径到系统剪贴板（静默，不弹窗） */
  const copyPathText = async (p: string) => {
    try {
      await navigator.clipboard.writeText(p)
    } catch {
      // 降级：临时 textarea + execCommand
      const ta = document.createElement('textarea')
      ta.value = p
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      try {
        document.execCommand('copy')
      } catch {
        /* ignore */
      }
      ta.remove()
    }
  }

  /** 添加到快速访问（去重） */
  const addToQuickAccess = (e: SftpEntry) => {
    const p = join(e.name)
    setQuickAccess((prev) => (prev.includes(p) ? prev : [...prev, p]))
  }

  /** 从快速访问移除 */
  const removeFromQuickAccess = (p: string) => {
    setQuickAccess((prev) => prev.filter((x) => x !== p))
  }

  /** 快速访问显示名（取路径末段） */
  const qaDisplay = (p: string) => {
    const t = p.replace(/\/+$/, '')
    const i = t.lastIndexOf('/')
    return i >= 0 ? t.slice(i + 1) : t
  }

  /** 快速访问父目录（Windows 盘符根 C: 规范化为 C:/） */
  const qaParent = (p: string) => {
    const t = p.replace(/\/+$/, '')
    if (!t) return ROOT
    const i = t.lastIndexOf('/')
    if (i < 0) return ROOT
    const parent = t.slice(0, i) || ROOT
    if (/^[A-Za-z]:$/.test(parent)) return parent + '/'
    return parent
  }

  /** 快速访问条目 → 合成文件夹条目（菜单复用文件列表逻辑，但路径用 qaPath） */
  const qaEntry = (p: string): SftpEntry => ({
    name: qaDisplay(p),
    size: 0,
    mode: 'drwxr-xr-x',
    mode_num: 0o755,
    is_dir: true,
    is_link: false,
    uid: 0,
    gid: 0,
    mtime: 0,
  })

  /** 复制指定路径到剪贴板（供快速访问条目等非当前目录条目使用） */
  const copyPath = (p: string, name: string, isDir: boolean) => {
    if (!hostId) return
    useClipboardStore.getState().setItem({ hostId, items: [{ path: p, name, isDir }], action: 'copy' })
  }

  /** 剪切指定路径到剪贴板 */
  const cutPath = (p: string, name: string, isDir: boolean) => {
    if (!hostId) return
    useClipboardStore.getState().setItem({ hostId, items: [{ path: p, name, isDir }], action: 'cut' })
  }

  /** 删除指定路径（含确认） */
  const deletePath = async (p: string, name: string) => {
    if (!hostId) return
    if (!window.confirm(t('确认删除「{0}」？', name))) return
    try {
      await api.sftpRemove(hostId, p)
      if (bigEditor && bigEditor.path === p) setBigEditor(null)
      void load(cwd)
    } catch (err) {
      alert(err instanceof ApiError ? err.message : t('删除失败'))
    }
  }

  /** 快速访问条目右键「重命名」：导航到父目录后自动进入行内重命名 */
  const renameQa = (ctx: CtxMenu) => {
    if (!ctx.qaPath || ctx.qaPath === ROOT) return
    const parent = qaParent(ctx.qaPath)
    if (parent === ctx.qaPath) return
    pendingRenameRef.current = qaDisplay(ctx.qaPath)
    navTo(parent, { keepPendingRename: true })
  }

  const downloadPath = (p: string, name: string, isDir: boolean) => {
    if (!hostId) return
    const a = document.createElement('a')
    a.href = api.sftpDownloadUrl(hostId, p)
    // 目录：后端打包为 tar.gz（文件夹名.tar.gz）
    a.download = isDir ? `${name}.tar.gz` : name
    document.body.appendChild(a)
    a.click()
    a.remove()
  }

  const download = (e: SftpEntry) => downloadPath(join(e.name), e.name, e.is_dir)

  const onPickFiles = (files: FileList | null) => {
    if (!files || !hostId) return
    const file = files[0]
    if (!file) return
    setUploading(file.name)
    setUploadPct(0)
    setUploadBytes(`0 / ${fmtSize(file.size)}`)
    api
      .sftpUpload(hostId, join(file.name), file, (pct, loaded, total) => {
        setUploadPct(pct)
        if (loaded !== undefined) setUploadBytes(`${fmtSize(loaded)} / ${fmtSize(total || file.size)}`)
      })
      .then(() => {
        setUploading(null)
        void load(cwd)
      })
      .catch((err: unknown) => {
        setUploading(null)
        alert(err instanceof ApiError ? err.message : t('上传失败'))
      })
  }

  /** 复制：把当前选中项记录到全局剪贴板（可跨服务器/跨窗口粘贴） */
  const copySelected = () => {
    if (!hostId) return
    const items = entries
      .filter((e) => selectedRef.current.has(e.name))
      .map((e) => ({ path: join(e.name), name: e.name, isDir: e.is_dir }))
    if (items.length === 0) return
    useClipboardStore.getState().setItem({ hostId, items, action: 'copy' })
  }

  /** 剪切：记录选中项，粘贴成功后删除源 */
  const cutSelected = () => {
    if (!hostId) return
    const items = entries
      .filter((e) => selectedRef.current.has(e.name))
      .map((e) => ({ path: join(e.name), name: e.name, isDir: e.is_dir }))
    if (items.length === 0) return
    useClipboardStore.getState().setItem({ hostId, items, action: 'cut' })
  }

  /** 删除所有选中项（逐条删除） */
  const deleteSelected = async () => {
    if (!hostId) return
    const items = entries.filter((e) => selectedRef.current.has(e.name))
    if (items.length === 0) return
    if (!window.confirm(t('确认删除选中的 {0} 项？\n{1}', items.length, items.map((i) => i.name).join(tt('、'))))) return
    try {
      for (const e of items) {
        await api.sftpRemove(hostId, join(e.name))
      }
      if (preview && items.some((i) => i.name === preview.name)) setPreview(null)
      if (bigEditor && items.some((i) => join(i.name) === bigEditor.path)) setBigEditor(null)
      updateSelected(new Set())
      void load(cwd)
    } catch (err) {
      alert(err instanceof ApiError ? err.message : t('删除失败'))
    }
  }

  /** 粘贴：transport 为空=同服务器直接粘贴；'direct'/'relay'=跨服务器指定传输方式。
   *  多选时逐条调用后端单路径粘贴接口（后端 paste 一次处理一个源路径）。 */
  const paste = async (transport?: 'direct' | 'relay') => {
    const clip = useClipboardStore.getState().item
    if (!clip || !hostId || clip.items.length === 0) return
    const mode = clip.action === 'cut' ? 'move' : 'copy'
    const isCross = transport !== undefined
    const names = fmtClipNames(clip)
    const cutTag = clip.action === 'cut' ? t('（剪切）') : ''
    setPasting(
      isCross
        ? transport === 'direct'
          ? t('正在直连传输「{0}」{1}', names, cutTag)
          : t('正在中转传输「{0}」{1}', names, cutTag)
        : clip.action === 'cut'
          ? t('正在移动「{0}」', names)
          : t('正在复制「{0}」', names),
    )
    setPastePct(-1)
    const controller = new AbortController()
    cancelRef.current = controller
    try {
      for (const item of clip.items) {
        if (controller.signal.aborted) break
        await api.sftpPasteStream(
          hostId,
          {
            src_host_id: clip.hostId,
            src_path: item.path,
            dst_dir: cwd,
            mode,
            transport: isCross ? (transport as 'relay' | 'direct') : 'local',
          },
          (loaded, total) => {
            if (total > 0) setPastePct(Math.round((loaded / total) * 100))
          },
          controller.signal,
        )
      }
      if (clip.action === 'cut' && !controller.signal.aborted) useClipboardStore.getState().clear()
      void load(cwd)
    } catch (err) {
      if (controller.signal.aborted) {
        // 用户主动取消：静默，不弹错误
      } else {
        const msg = err instanceof ApiError ? err.message : t('粘贴失败')
        if (transport === 'direct') {
          // 直连传输失败：展示中英对照弹窗（可一键打开源终端安装 sshpass）
          setDirectErr({ clip, detail: msg })
        } else {
          alert(msg)
        }
      }
    } finally {
      cancelRef.current = null
      setPasting(null)
      setPastePct(-1)
    }
  }

  /** 打开源服务器终端并自动执行 sshpass 安装命令 */
  const installSshpass = async () => {
    const clip = directErr?.clip
    if (!clip) return
    setDirectErr(null)
    // 确保 WS 已连接，让终端打开成功
    try {
      await ws.connect()
    } catch {
      /* 忽略，终端组件会自处理 */
    }
    const installCmd =
      '(command -v apt-get && (apt-get update && apt-get install -y sshpass)) || ' +
      '(command -v yum && yum install -y sshpass) || ' +
      '(command -v dnf && dnf install -y sshpass) || ' +
      '(command -v apk && apk add sshpass) || ' +
      'echo "未检测到支持的包管理器，请手动安装 sshpass"'
    setPendingTerminalCwd(clip.hostId, '/', installCmd)
    open({
      id: `terminal-${Date.now().toString(36)}`,
      appId: 'terminal',
      title: hostNames[clip.hostId]
        ? `${hostNames[clip.hostId]} - ${tt('终端（安装 sshpass）')}`
        : tt('终端（安装 sshpass）'),
      titleKey: '{0} - {1}',
      titleArgs: [hostNames[clip.hostId] || tt('终端'), tt('终端（安装 sshpass）')],
      icon: '🖥️',
      hostId: clip.hostId,
      platform: hostPlatforms[clip.hostId] ?? '',
      channelId: newChannelId(),
      x: 80,
      y: 60,
      width: 760,
      height: 460,
    })
  }

  /** 地址栏输入规范化：只支持绝对路径；`~`/`~/x` 近似映射（SFTP 家目录通常不可直接寻址） */
  const normalizeAddr = (v: string): string => {
    let p = v.trim()
    if (!p || p === '~') return cwd
    if (p.startsWith('~/')) p = p.slice(1)
    if (isWin) {
      // Windows 展示路径：C:/Users、C:\Users、/C:/Users 一律归一为 C:/Users；/ 保留为盘符列表
      p = p.replace(/\\/g, '/').replace(/\/+/g, '/')
      if (p === '/') return ROOT
      if (p.startsWith('/')) p = p.slice(1)
      p = p.replace(/^([a-zA-Z]):/, (_, d: string) => d.toUpperCase() + ':')
      if (/^[a-zA-Z]:$/.test(p)) return p + '/'
      return p
    }
    if (!p.startsWith('/')) p = `/${p}`
    return p
  }

  /** 键盘热键：Ctrl+C/X/V 复制剪切粘贴、Delete 删除、Ctrl+A 全选、Esc 取消选择 */
  const handleKeyDown = (e: React.KeyboardEvent) => {
    const t = e.target as HTMLElement
    if (t.closest('input, textarea, select') || t.isContentEditable) return
    // 弹窗/播放器打开时忽略热键
    if (dialog || player || directErr) return
    const mod = e.ctrlKey || e.metaKey
    const key = e.key.toLowerCase()
    if (mod && key === 'c') {
      // 存在文本选区（如选中了文件名/权限文字）时交给浏览器原生复制文本，
      // 否则才是"复制选中文件"；避免在任意选中文字处 Ctrl+C 被文件复制劫持
      const sel = window.getSelection()
      if (sel && sel.toString().length > 0) return
      e.preventDefault()
      copySelected()
    } else if (mod && key === 'x') {
      e.preventDefault()
      cutSelected()
    } else if (mod && key === 'v') {
      e.preventDefault()
      void paste()
    } else if (mod && key === 'a') {
      e.preventDefault()
      updateSelected(new Set(entries.map((x) => x.name)))
    } else if (e.key === 'Delete') {
      if (selectedRef.current.size > 0) {
        e.preventDefault()
        void deleteSelected()
      }
    } else if (e.key === 'F2') {
      // F2：恰好选中一项时行内重命名（Windows 风格）
      if (selectedRef.current.size === 1) {
        e.preventDefault()
        const target = entries.find((x) => x.name === [...selectedRef.current][0])
        if (target) startRename(target)
      }
    } else if (e.key === 'Escape') {
      // 已由本组件处理（关闭右键菜单 / 取消选择），阻止全局 ESC 关闭窗口
      e.stopPropagation()
      if (ctx) setCtx(null)
      else updateSelected(new Set())
    }
  }

  // ---- 行级多选：单击 / ctrl / shift（Windows 资源管理器风格）----
  const onRowClick = (e: React.MouseEvent, entry: SftpEntry) => {
    const name = entry.name
    const cur = selectedRef.current
    if (e.ctrlKey || e.metaKey) {
      // ctrl：切换该项的选中状态
      const next = new Set(cur)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      anchorRef.current = name
      updateSelected(next)
    } else if (e.shiftKey) {
      // shift：从锚点行到本行连续选中
      const a = anchorRef.current ? entries.findIndex((x) => x.name === anchorRef.current) : -1
      const b = entries.findIndex((x) => x.name === name)
      if (a >= 0 && b >= 0) {
        const next = new Set(cur)
        for (let i = Math.min(a, b); i <= Math.max(a, b); i++) next.add(entries[i].name)
        updateSelected(next)
      } else {
        updateSelected(new Set([name]))
        anchorRef.current = name
      }
    } else {
      // 普通单击：单选 + 右侧预览
      updateSelected(new Set([name]))
      anchorRef.current = name
      if (!entry.is_dir) void showPreview(join(entry.name), entry.name)
    }
  }

  // ---- 空白处框选（rubber band）----
  /** 框选矩形存容器局部坐标（窗口 backdrop-filter 会破坏 position:fixed 定位，故用 absolute） */
  const onListPointerDown = (e: React.PointerEvent) => {
    if (e.button !== 0) return
    const t = e.target as HTMLElement
    if (t.closest('tr, button, input, a, .ctx-menu, .modal-mask')) return
    const c = (e.currentTarget as HTMLElement).getBoundingClientRect()
    bandStart.current = { x: e.clientX, y: e.clientY }
    preBandSel.current = new Set(selectedRef.current)
    setBandRect({ x: e.clientX - c.left, y: e.clientY - c.top, w: 0, h: 0 })
    e.currentTarget.setPointerCapture(e.pointerId)
  }

  const onListPointerMove = (e: React.PointerEvent) => {
    if (!bandStart.current) return
    const c = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const left = Math.min(bandStart.current.x, e.clientX)
    const top = Math.min(bandStart.current.y, e.clientY)
    setBandRect({
      x: left - c.left,
      y: top - c.top,
      w: Math.abs(e.clientX - bandStart.current.x),
      h: Math.abs(e.clientY - bandStart.current.y),
    })
    // 与行矩形求交（用 viewport 坐标，滚动自动正确）
    const band = {
      left,
      top,
      right: Math.max(bandStart.current.x, e.clientX),
      bottom: Math.max(bandStart.current.y, e.clientY),
    }
    const additive = e.ctrlKey || e.metaKey
    const next = new Set(additive ? preBandSel.current : [])
    for (const [name, el] of rowElsRef.current) {
      const r = el.getBoundingClientRect()
      if (r.left < band.right && r.right > band.left && r.top < band.bottom && r.bottom > band.top) {
        next.add(name)
      }
    }
    updateSelected(next)
  }

  const onListPointerUp = (e: React.PointerEvent) => {
    if (bandStart.current) {
      // 位移过小视为空白处单击：清除选中（ctrl 除外）
      const moved = Math.hypot(e.clientX - bandStart.current.x, e.clientY - bandStart.current.y)
      if (moved < 4 && !(e.ctrlKey || e.metaKey)) updateSelected(new Set())
    }
    bandStart.current = null
    preBandSel.current = new Set()
    setBandRect(null)
  }

  const onContextMenu = (e: React.MouseEvent, entry: SftpEntry | null) => {
    e.preventDefault()
    setCtx({ x: e.clientX, y: e.clientY, entry })
  }

  return (
    <div
      ref={rootRef}
      tabIndex={-1}
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        background: 'rgba(var(--rgb-appbg),0.85)',
        fontSize: 13,
        outline: 'none',
      }}
      onClick={() => setCtx(null)}
      onKeyDown={handleKeyDown}
      onPointerDown={(e) => {
        // 让热键作用于文件管理器：点击非输入/非弹窗/非编辑器区域时聚焦根容器
        const t = e.target as HTMLElement
        if (t.closest('input, textarea, select, .cm-editor, .ctx-menu, .modal-mask')) return
        rootRef.current?.focus()
      }}
      onContextMenu={(e) => {
        // 空白处右键（避免与行级右键冲突）
        const t = e.target as HTMLElement
        if (t.closest('tr') || t.closest('.ctx-menu')) return
        // 输入框/文本域/下拉框（地址栏等）：不弹空白菜单，交给全局右键菜单（剪切/复制/粘贴/全选）
        if (t.closest('input, textarea, select')) return
        onContextMenu(e, null)
      }}
    >
      {/* 工具栏 */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          padding: '8px 10px',
          borderBottom: '1px solid rgba(var(--rgb-line),0.15)',
        }}
      >
        <button className="btn btn-sm btn-ghost" onClick={goBack} disabled={hi === 0}>
          ←
        </button>
        <button className="btn btn-sm btn-ghost" onClick={goUp} disabled={cwd === ROOT}>
          ↑
        </button>
        <input
          value={addr ?? cwd}
          onChange={(e) => setAddr(e.target.value)}
          onFocus={(e) => {
            // 进入编辑时显示当前目录并全选（Windows 资源管理器风格）
            setAddr(cwd)
            e.target.select()
          }}
          onBlur={() => setAddr(null)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              const v = normalizeAddr(addr ?? cwd)
              setAddr(null)
              if (v !== cwd) navTo(v)
            } else if (e.key === 'Escape') {
              setAddr(null)
            }
          }}
          spellCheck={false}
          title={
            isWin
              ? t('输入绝对路径后回车跳转（如 C:/Users），/ 查看盘符列表')
              : t('输入绝对路径后回车跳转（如 /etc/nginx）')
          }
          style={{
            flex: 1,
            minWidth: 0,
            padding: '5px 10px',
            borderRadius: 6,
            border: '1px solid rgba(var(--rgb-line),0.2)',
            background: 'rgba(var(--rgb-appbg),0.5)',
            fontFamily: 'Consolas, monospace',
            color: 'var(--primary-light)',
            outline: 'none',
          }}
        />
        <button className="btn btn-sm btn-ghost" onClick={newFile}>
          {t('新建文档')}
        </button>
        <button className="btn btn-sm btn-ghost" onClick={mkdir}>
          {t('新建目录')}
        </button>
        <button className="btn btn-sm" onClick={() => fileInputRef.current?.click()}>
          {t('上传')}
        </button>
        <button className="btn btn-sm btn-ghost" onClick={() => void openTerminalHere()}>
          {t('打开终端')}
        </button>
        <input
          ref={fileInputRef}
          type="file"
          style={{ display: 'none' }}
          onChange={(e) => onPickFiles(e.target.files)}
        />
      </div>

      {/* 上传进度 */}
      {uploading && (
        <div style={{ padding: '6px 10px', color: 'var(--cyan)', fontSize: 12 }}>
          {t('⬆️ 上传中 {0}', uploading)}
          {uploadPct >= 0 ? `${uploadPct}%` : ''} {uploadBytes}
          {uploadPct > 0 && (
            <div
              style={{
                height: 4,
                marginTop: 4,
                borderRadius: 2,
                background: 'rgba(var(--rgb-line),0.2)',
                overflow: 'hidden',
              }}
            >
              <div
                style={{
                  width: `${Math.min(100, uploadPct)}%`,
                  height: '100%',
                  background: 'var(--cyan)',
                  transition: 'width 0.2s',
                }}
              />
            </div>
          )}
        </div>
      )}

      {/* 剪贴板提示条 */}
      {clipItem && !pasting && (
        <div
          style={{
            padding: '6px 10px',
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            fontSize: 12,
            color: 'var(--text-1)',
            background: 'rgba(var(--rgb-primary),0.08)',
            borderBottom: '1px solid rgba(var(--rgb-line),0.1)',
          }}
        >
          <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {clipItem.action === 'cut'
              ? t('已剪切「{0}」', fmtClipNames(clipItem))
              : t('已复制「{0}」', fmtClipNames(clipItem))}
            {clipItem.hostId !== hostId && (
              <> {t('（来自 {0}）', hostNames[clipItem.hostId] || clipItem.hostId)}</>
            )}
            {t('，可在空白处右键粘贴')}
          </span>
          <button
            className="btn btn-sm btn-ghost"
            onClick={() => useClipboardStore.getState().clear()}
          >
            {t('清除')}
          </button>
        </div>
      )}

      {/* 粘贴中提示（含进度条与取消按钮） */}
      {pasting && (
        <div
          style={{
            padding: '6px 10px',
            color: 'var(--cyan)',
            fontSize: 12,
            display: 'flex',
            alignItems: 'center',
            gap: 10,
          }}
        >
          <span style={{ flex: 1 }}>
            📋 {pasting}
            {pastePct >= 0 ? (
              <div
                style={{
                  height: 4,
                  marginTop: 4,
                  borderRadius: 2,
                  background: 'rgba(var(--rgb-line),0.2)',
                  overflow: 'hidden',
                }}
              >
                <div
                  style={{
                    width: `${Math.min(100, pastePct)}%`,
                    height: '100%',
                    background: 'var(--cyan)',
                    transition: 'width 0.2s',
                  }}
                />
              </div>
            ) : (
              <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-1)' }}>
                {t('传输中…')}
              </div>
            )}
          </span>
          <button className="btn btn-sm btn-ghost" onClick={() => cancelRef.current?.abort()}>
            {t('取消')}
          </button>
        </div>
      )}

      {/* 错误提示 */}
      {error && (
        <div style={{ padding: '8px 10px', color: 'var(--red)' }}>{error}</div>
      )}

      {/* 主体：快速访问 + 左列表 + 右预览（右侧常驻） */}
      <div style={{ flex: 1, display: 'flex', minHeight: 0 }}>
        {/* 左：快速访问 */}
        <div
          className="qa-panel"
          onContextMenu={(e) => {
            e.preventDefault()
            e.stopPropagation()
            setCtx(null)
          }}
        >
          <div className="qa-title">{t('⭐ 快速访问')}</div>
          <div className="qa-list">
            {quickAccess.map((p) => (
              <div
                key={p}
                className={`qa-item${cwd === p ? ' active' : ''}`}
                onClick={() => navTo(p)}
                onContextMenu={(e) => {
                  e.preventDefault()
                  e.stopPropagation()
                  // 复用文件列表的文件夹菜单，但路径指向该快速访问条目
                  setCtx({ x: e.clientX, y: e.clientY, entry: qaEntry(p), qaPath: p })
                }}
                title={p}
              >
                <span style={{ flexShrink: 0 }}>📁</span>
                <span
                  style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                >
                  {qaDisplay(p)}
                </span>
              </div>
            ))}
          </div>
        </div>

        {/* 中：文件列表 */}
        <div
          style={{ flex: 1, overflow: 'auto', minWidth: 0, position: 'relative' }}
          onPointerDown={onListPointerDown}
          onPointerMove={onListPointerMove}
          onPointerUp={onListPointerUp}
        >
          {loading ? (
            <div style={{ padding: 20, color: 'var(--text-1)' }}>{t('加载中…')}</div>
          ) : (
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ color: 'var(--text-1)', textAlign: 'left', fontSize: 12 }}>
                  <th style={{ padding: '6px 10px' }}>{t('名称')}</th>
                  <th style={{ padding: '6px 10px', width: 80 }}>{t('大小')}</th>
                  <th style={{ padding: '6px 10px', width: 110 }}>{t('权限')}</th>
                  <th style={{ padding: '6px 10px', width: 160 }}>{t('修改时间')}</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((e) => (
                  <tr
                    key={e.name}
                    ref={(el) => {
                      if (el) rowElsRef.current.set(e.name, el)
                      else rowElsRef.current.delete(e.name)
                    }}
                    className={selected.has(e.name) ? 'fm-row sel' : 'fm-row'}
                    onDoubleClick={() => openEntry(e)}
                    onClick={(ev) => onRowClick(ev, e)}
                    onContextMenu={(ev) => {
                      ev.preventDefault()
                      ev.stopPropagation()
                      // 右键不在当前选区内的项时，先将其单选
                      if (!selectedRef.current.has(e.name)) updateSelected(new Set([e.name]))
                      setCtx({ x: ev.clientX, y: ev.clientY, entry: e })
                    }}
                  >
                    <td style={{ padding: '6px 10px' }}>
                      {editingName?.name === e.name ? (
                        <input
                          className="fm-rename-input"
                          value={editingName.value}
                          onChange={(ev) => setEditingName({ ...editingName, value: ev.target.value })}
                          onFocus={(ev) => {
                            // Windows 风格：文件选中主名（最后一个扩展名之前），文件夹全选
                            const v = ev.target
                            const dot = e.name.lastIndexOf('.')
                            const end = !e.is_dir && dot > 0 ? dot : e.name.length
                            v.setSelectionRange(0, end)
                          }}
                          onClick={(ev) => ev.stopPropagation()}
                          onDoubleClick={(ev) => ev.stopPropagation()}
                          onPointerDown={(ev) => ev.stopPropagation()}
                          onKeyDown={(ev) => {
                            if (ev.key === 'Enter') {
                              ev.preventDefault()
                              void commitRename(e)
                            } else if (ev.key === 'Escape') {
                              ev.preventDefault()
                              renameCancelRef.current = true
                              setEditingName(null)
                            }
                          }}
                          onBlur={() => void commitRename(e)}
                          autoFocus
                        />
                      ) : (
                        <>
                          <span style={{ marginRight: 6 }}>
                            {e.is_dir ? '📁' : e.is_link ? '🔗' : '📄'}
                          </span>
                          {e.name}
                          {e.is_link && e.link_target && (
                            <span style={{ color: 'var(--text-1)', marginLeft: 6 }}>
                              → {e.link_target}
                            </span>
                          )}
                        </>
                      )}
                    </td>
                    <td style={{ padding: '6px 10px', color: 'var(--text-1)' }}>
                      {e.is_dir ? '-' : fmtSize(e.size)}
                    </td>
                    <td style={{ padding: '6px 10px', color: 'var(--text-1)' }}>
                      {e.mode}
                    </td>
                    <td style={{ padding: '6px 10px', color: 'var(--text-1)' }}>
                      {fmtTime(e.mtime)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          {/* 框选虚线框（容器局部坐标；窗口 backdrop-filter 会破坏 fixed 定位，故用 absolute） */}
          {bandRect && (
            <div
              className="rubber-band"
              style={{
                position: 'absolute',
                left: bandRect.x,
                top: bandRect.y,
                width: bandRect.w,
                height: bandRect.h,
              }}
            />
          )}
        </div>

        {/* 右：预览/编辑面板（始终显示） */}
        <div
          style={{
            width: 380,
            minWidth: 300,
            borderLeft: '1px solid rgba(var(--rgb-line),0.15)',
            display: 'flex',
            flexDirection: 'column',
            minHeight: 0,
          }}
        >
          {previewErr && !preview && (
            <div style={{ padding: 14, color: 'var(--red)', fontSize: 13 }}>
              {previewErr}
              <div style={{ marginTop: 10 }}>
                <button className="btn btn-sm btn-ghost" onClick={() => setPreviewErr('')}>
                  {t('关闭')}
                </button>
              </div>
            </div>
          )}
          {!preview && !previewErr && (
            <div
              style={{
                flex: 1,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                color: 'var(--text-1)',
                fontSize: 13,
              }}
            >
              {t('点击左侧文件查看并编辑内容')}
            </div>
          )}
            {preview && (
              <>
                <div
                  style={{
                    padding: '8px 12px',
                    borderBottom: '1px solid rgba(var(--rgb-line),0.15)',
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    gap: 8,
                    flexShrink: 0,
                  }}
                >
                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {preview.kind === 'image'
                      ? '🖼️'
                      : preview.kind === 'video'
                        ? '🎬'
                        : preview.kind === 'audio'
                          ? '🎵'
                          : '✏️'}{' '}
                    {preview.name}
                    {preview.kind === 'text' && preview.dirty && (
                      <span style={{ color: 'var(--yellow)', marginLeft: 6 }}>{t('● 未保存')}</span>
                    )}
                    {preview.kind !== 'text' && (
                      <span style={{ color: 'var(--text-1)', marginLeft: 6, fontSize: 11 }}>
                        {t('（只读预览）')}
                      </span>
                    )}
                  </span>
                  <div style={{ display: 'flex', gap: 6, flexShrink: 0 }}>
                    {preview.kind === 'text' && (
                      <button className="btn btn-sm" disabled={!preview.dirty} onClick={savePreview}>
                        {t('保存')}
                      </button>
                    )}
                    <button className="btn btn-sm btn-ghost" onClick={() => setPreview(null)}>
                      {t('关闭')}
                    </button>
                  </div>
                </div>

                {preview.kind === 'text' ? (
                  <div
                    style={{
                      flex: 1,
                      display: 'flex',
                      flexDirection: 'column',
                      minHeight: 0,
                      overflow: 'hidden',
                    }}
                  >
                    <div style={{ flex: 1, minHeight: 0, display: 'flex' }}>
                      <CodeEditor
                        value={preview.content}
                        onChange={(v) => setPreview({ ...preview, content: v, dirty: true })}
                        filename={preview.name}
                        onSave={() => void savePreview()}
                      />
                    </div>
                    <div className="editor-footer">
                      {t('共 {0} 个字 · {1} 行', textCharCount(preview.content), textLineCount(preview.content))}
                    </div>
                  </div>
                ) : preview.kind === 'image' ? (
                  <div
                    style={{
                      flex: 1,
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      overflow: 'auto',
                      background: 'rgba(var(--rgb-appbg),0.5)',
                      minHeight: 0,
                    }}
                  >
                    <img
                      src={hostId ? api.sftpPreviewUrl(hostId, preview.path) : undefined}
                      alt={preview.name}
                      style={{ maxWidth: '100%', maxHeight: '100%', objectFit: 'contain' }}
                    />
                  </div>
                ) : preview.kind === 'video' ? (
                  <div
                    style={{
                      flex: 1,
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      background: '#000',
                      minHeight: 0,
                    }}
                  >
                    <video
                      src={hostId ? api.sftpPreviewUrl(hostId, preview.path) : undefined}
                      controls
                      autoPlay={false}
                      style={{ maxWidth: '100%', maxHeight: '100%' }}
                    />
                  </div>
                ) : (
                  <div
                    style={{
                      flex: 1,
                      display: 'flex',
                      flexDirection: 'column',
                      alignItems: 'center',
                      justifyContent: 'center',
                      background: 'rgba(var(--rgb-appbg),0.5)',
                      minHeight: 0,
                      padding: 16,
                    }}
                  >
                    <audio
                      src={hostId ? api.sftpPreviewUrl(hostId, preview.path) : undefined}
                      controls
                      autoPlay
                      style={{ width: '100%', maxWidth: 360 }}
                    />
                  </div>
                )}
              </>
            )}
          </div>
      </div>

      {/* 右键菜单（行级或空白处） */}
      {ctx && (
        <div
          className="ctx-menu"
          style={{ position: 'fixed', left: ctx.x, top: ctx.y, zIndex: 999 }}
          onClick={(e) => e.stopPropagation()}
        >
          {!ctx.entry && (
            <>
              <div className="ctx-menu-item" onClick={() => { mkdir(); setCtx(null) }}>
                {t('📁 新建目录')}
              </div>
              <div className="ctx-menu-item" onClick={() => { newFile(); setCtx(null) }}>
                {t('📄 新建文档')}
              </div>
              <div className="ctx-menu-item" onClick={() => { fileInputRef.current?.click(); setCtx(null) }}>
                {t('⬆️ 上传')}
              </div>
              {clipItem && clipItem.hostId === hostId && (
                <div className="ctx-menu-item" onClick={() => { void paste(); setCtx(null) }}>
                  {t('📋 粘贴{0}', clipItem.action === 'cut' ? t('（剪切）') : '')}
                </div>
              )}
              {clipItem && clipItem.hostId !== hostId && (
                <>
                  {(() => {
                    // 直连依赖源机 sh+scp+sshpass（POSIX 链路），任一端为 Windows 时置灰不可点
                    const directBlocked = platform === 'windows' || hostPlatforms[clipItem.hostId] === 'windows'
                    return (
                      <div
                        className={`ctx-menu-item${directBlocked ? ' disabled' : ''}`}
                        title={directBlocked ? t('直连依赖源机执行 scp+sshpass（POSIX 链路），Windows 主机不可用，请用「中转」') : undefined}
                        onClick={() => {
                          if (directBlocked) return
                          void paste('direct')
                          setCtx(null)
                        }}
                      >
                        {t('⚡ 粘贴（直连）')}
                      </div>
                    )
                  })()}
                  <div className="ctx-menu-item" onClick={() => { void paste('relay'); setCtx(null) }}>
                    {t('🔄 粘贴（中转）')}
                  </div>
                </>
              )}
              <div className="ctx-menu-item" onClick={() => { void openTerminalHere(); setCtx(null) }}>
                {t('🖥️ 打开终端')}
              </div>
            </>
          )}
          {ctx.entry && (
            <>
              {!ctx.entry.is_dir && (
                <div
                  className="ctx-menu-item"
                  onClick={() => { void showPreview(join(ctx.entry!.name), ctx.entry!.name); setCtx(null) }}
                >
                  {t('✏️ 编辑')}
                </div>
              )}
              <div
                className="ctx-menu-item"
                onClick={() => {
                  if (ctx.qaPath) downloadPath(ctx.qaPath, qaDisplay(ctx.qaPath), true)
                  else download(ctx.entry!)
                  setCtx(null)
                }}
              >
                {ctx.entry.is_dir ? t('📦 下载文件夹（tar.gz）') : t('⬇️ 下载')}
              </div>
              {!ctx.entry.is_dir && isArchive(ctx.entry.name) && (
                <div className="ctx-menu-item" onClick={() => { void doExtract(ctx.entry!); setCtx(null) }}>
                  {t('📦 解压缩')}
                </div>
              )}
              <div
                className="ctx-menu-item"
                onClick={() => {
                  void copyPathText(ctx.qaPath ?? join(ctx.entry!.name))
                  setCtx(null)
                }}
              >
                {t('📋 复制绝对路径')}
              </div>
              <div className="ctx-menu-sep" />
              <div
                className="ctx-menu-item"
                onClick={() => {
                  if (ctx.qaPath) copyPath(ctx.qaPath, qaDisplay(ctx.qaPath), true)
                  else copySelected()
                  setCtx(null)
                }}
              >
                {t('📄 复制{0}', !ctx.qaPath && selected.size > 1 ? t('（{0} 项）', selected.size) : '')}
              </div>
              <div
                className="ctx-menu-item"
                onClick={() => {
                  if (ctx.qaPath) cutPath(ctx.qaPath, qaDisplay(ctx.qaPath), true)
                  else cutSelected()
                  setCtx(null)
                }}
              >
                {t('✂️ 剪切{0}', !ctx.qaPath && selected.size > 1 ? t('（{0} 项）', selected.size) : '')}
              </div>
              <div
                className="ctx-menu-item"
                onClick={() => {
                  if (ctx.qaPath) renameQa(ctx)
                  else startRename(ctx.entry!)
                  setCtx(null)
                }}
              >
                {t('🔄 重命名')}
              </div>
              <div
                className="ctx-menu-item"
                onClick={() => {
                  if (ctx.qaPath) void doChmodPath(ctx.qaPath, '755')
                  else doChmod(ctx.entry!)
                  setCtx(null)
                }}
              >
                {t('🔐 权限')}
              </div>
              <div
                className="ctx-menu-item"
                style={{ color: 'var(--red)' }}
                onClick={() => {
                  if (ctx.qaPath) void deletePath(ctx.qaPath, qaDisplay(ctx.qaPath))
                  else void deleteSelected()
                  setCtx(null)
                }}
              >
                {t('🗑 删除{0}', !ctx.qaPath && selected.size > 1 ? t('（{0} 项）', selected.size) : '')}
              </div>
              {ctx.qaPath && (
                <>
                  <div className="ctx-menu-sep" />
                  <div
                    className="ctx-menu-item"
                    style={{ color: 'var(--red)' }}
                    onClick={() => {
                      removeFromQuickAccess(ctx.qaPath!)
                      setCtx(null)
                    }}
                  >
                    {t('❌ 从快速访问中移除')}
                  </div>
                </>
              )}
              {ctx.entry.is_dir && !ctx.qaPath && (
                <>
                  <div className="ctx-menu-sep" />
                  <div
                    className="ctx-menu-item"
                    onClick={() => { addToQuickAccess(ctx.entry!); setCtx(null) }}
                  >
                    {t('⭐ 添加到快速访问')}
                  </div>
                </>
              )}
            </>
          )}
        </div>
      )}

      {/* 直连传输失败弹窗（中英对照 + 安装 sshpass） */}
      {directErr && (
        <div
          className="modal-mask"
          style={{ zIndex: 1100 }}
          onClick={() => setDirectErr(null)}
        >
          <div
            style={{
              width: 460,
              background: 'var(--bg-1)',
              border: '1px solid var(--glass-border)',
              borderRadius: 14,
              padding: 22,
              boxShadow: '0 24px 64px rgba(0,0,0,0.5)',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <h3 style={{ fontSize: 15, marginBottom: 12 }}>
              ⚠️ 直连传输失败 / Direct Transfer Failed
            </h3>
            <div style={{ fontSize: 13, lineHeight: 1.7, color: 'var(--text-1)' }}>
              <p>
                源服务器 <b>{hostNames[directErr.clip.hostId] || directErr.clip.hostId}</b>{' '}
                未安装 <b>sshpass</b> 工具。
              </p>
              <p style={{ color: 'var(--text-0)' }}>
                The source server does not have <b>sshpass</b> installed. Direct transfer runs{' '}
                <code>scp</code> on the source server, and <code>scp</code> cannot take a
                password on the command line — the <code>sshpass</code> helper is required
                (exit code 127 = command not found).
              </p>
              <p style={{ marginTop: 4 }}>
                解决方式 / Options：
              </p>
              <ul style={{ margin: '4px 0 8px 18px', color: 'var(--text-0)' }}>
                <li>点击「安装 sshpass」自动在源服务器终端执行安装命令；或</li>
                <li>改用「中转传输」，无需任何额外工具。</li>
              </ul>
              <div
                style={{
                  background: 'rgba(var(--rgb-appbg),0.6)',
                  border: '1px solid rgba(var(--rgb-line),0.15)',
                  borderRadius: 8,
                  padding: '10px 12px',
                  fontFamily: 'Consolas, monospace',
                  fontSize: 12,
                  color: 'var(--cyan)',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-all',
                }}
              >
                {`# 安装命令（按发行版二选一）\n# Install command (choose one per distro)\napt-get update && apt-get install -y sshpass\n# 或 yum/dnf:  yum install -y sshpass\n# 或 apk:      apk add sshpass`}
              </div>
              <p style={{ marginTop: 8, fontSize: 12, color: 'var(--text-1)' }}>
                错误详情 / Error detail：
                <br />
                <code style={{ color: 'var(--red)', wordBreak: 'break-all' }}>{directErr.detail}</code>
              </p>
            </div>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, marginTop: 16 }}>
              <button className="btn" onClick={() => void installSshpass()}>
                🔧 安装 sshpass / Install
              </button>
              <button className="btn btn-ghost" onClick={() => setDirectErr(null)}>
                取消 / Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 主题化输入弹窗（新建目录/文档、重命名、权限） */}
      {dialog && (
        <div
          className="modal-mask"
          style={{ zIndex: 1100 }}
          onClick={() => closeDialog(null)}
        >
          <div
            style={{
              width: 360,
              background: 'var(--bg-1)',
              border: '1px solid var(--glass-border)',
              borderRadius: 14,
              padding: 22,
              boxShadow: '0 24px 64px rgba(0,0,0,0.5)',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <h3 style={{ fontSize: 15, marginBottom: 16 }}>{dialog.title}</h3>
            <div className="field">
              <label>{dialog.label}</label>
              <input
                value={dialogValue}
                onChange={(e) => {
                  setDialogValue(e.target.value)
                  if (dialogErr) setDialogErr('')
                }}
                placeholder={dialog.placeholder}
                autoFocus
                onKeyDown={(e) => {
                  if (e.key === 'Enter') confirmDialog()
                  if (e.key === 'Escape') closeDialog(null)
                }}
              />
            </div>
            {dialogErr && (
              <div style={{ color: 'var(--red)', fontSize: 12, marginBottom: 12 }}>
                {dialogErr}
              </div>
            )}
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10 }}>
              <button className="btn btn-ghost" onClick={() => closeDialog(null)}>
                {t('取消')}
              </button>
              <button className="btn" onClick={confirmDialog}>
                {dialog.confirmText || t('确定')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 大编辑窗口：双击文本/代码文件打开 */}
      {bigEditor && (
        <div
          className="modal-mask"
          style={{ zIndex: 1250 }}
          onClick={closeBigEditor}
        >
          <div
            style={{
              width: '88%',
              height: '88%',
              display: 'flex',
              flexDirection: 'column',
              background: 'var(--bg-1)',
              border: '1px solid var(--glass-border)',
              borderRadius: 12,
              overflow: 'hidden',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            {/* 标题栏 */}
            <div
              style={{
                padding: '10px 14px',
                borderBottom: '1px solid rgba(var(--rgb-line),0.15)',
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                flexShrink: 0,
              }}
            >
              <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                ✏️ {bigEditor.name}
                {bigEditor.dirty && (
                  <span style={{ color: 'var(--yellow)', marginLeft: 6 }}>{t('● 未保存')}</span>
                )}
              </span>
              <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexShrink: 0 }}>
                <button className="btn btn-sm" disabled={!bigEditor.dirty} onClick={() => void saveBigEditor()}>
                  {t('保存 (Ctrl+S)')}
                </button>
                <button className="btn btn-sm btn-ghost" onClick={closeBigEditor}>
                  {t('✕ 关闭')}
                </button>
              </div>
            </div>
            {/* 编辑器主体 */}
            <div style={{ flex: 1, minHeight: 0, display: 'flex' }}>
              <CodeEditor
                value={bigEditor.content}
                onChange={(v) => setBigEditor({ ...bigEditor, content: v, dirty: true })}
                filename={bigEditor.name}
                onSave={() => void saveBigEditor()}
              />
            </div>
            {/* 底部：字数统计 */}
            <div className="editor-footer">
              {t('共 {0} 个字 · {1} 行', textCharCount(bigEditor.content), textLineCount(bigEditor.content))}
            </div>
          </div>
        </div>
      )}

      {/* 全屏播放器：双击图片/视频打开 */}
      {player && (
        <div
          className="modal-mask"
          style={{ zIndex: 1200, background: 'rgba(0,0,0,0.85)' }}
          onClick={() => setPlayer(null)}
        >
          <div
            style={{
              width: '92%',
              height: '92%',
              display: 'flex',
              flexDirection: 'column',
              background: 'rgba(var(--rgb-appbg),0.95)',
              border: '1px solid var(--glass-border)',
              borderRadius: 12,
              overflow: 'hidden',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            {/* 播放器标题栏 */}
            <div
              style={{
                padding: '10px 14px',
                borderBottom: '1px solid rgba(var(--rgb-line),0.15)',
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                flexShrink: 0,
              }}
            >
              <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {player.kind === 'image' ? '🖼️' : '🎬'} {player.name}
              </span>
              <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                {player.kind === 'image' && (
                  <>
                    <button
                      className="btn btn-sm btn-ghost"
                      title={t('缩小')}
                      onClick={() => setImgScale((s) => Math.max(0.1, s - 0.2))}
                    >
                      ➖
                    </button>
                    <span style={{ color: 'var(--text-1)', fontSize: 12 }}>
                      {Math.round(imgScale * 100)}%
                    </span>
                    <button
                      className="btn btn-sm btn-ghost"
                      title={t('放大')}
                      onClick={() => setImgScale((s) => Math.min(5, s + 0.2))}
                    >
                      ➕
                    </button>
                    <button
                      className="btn btn-sm btn-ghost"
                      title={t('旋转')}
                      onClick={() => setImgRotate((r) => (r + 90) % 360)}
                    >
                      🔄
                    </button>
                    <button className="btn btn-sm btn-ghost" onClick={() => { setImgScale(1); setImgRotate(0) }}>
                      {t('重置')}
                    </button>
                  </>
                )}
                <button className="btn btn-sm" onClick={() => setPlayer(null)}>
                  {t('✕ 关闭')}
                </button>
              </div>
            </div>

            {/* 播放器主体 */}
            <div
              style={{
                flex: 1,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                overflow: 'auto',
                background: '#000',
                minHeight: 0,
                position: 'relative',
              }}
              onWheel={(e) => {
                if (player.kind !== 'image') return
                e.preventDefault()
                setImgScale((s) =>
                  Math.min(5, Math.max(0.1, s + (e.deltaY < 0 ? 0.15 : -0.15))),
                )
              }}
            >
              {player.kind === 'image' ? (
                <img
                  src={hostId ? api.sftpPreviewUrl(hostId, player.path) : undefined}
                  alt={player.name}
                  style={{
                    transform: `scale(${imgScale}) rotate(${imgRotate}deg)`,
                    transition: 'transform 0.2s',
                    maxWidth: '100%',
                    maxHeight: '100%',
                    objectFit: 'contain',
                    cursor: 'zoom-in',
                  }}
                  draggable={false}
                />
              ) : (
                <video
                  src={hostId ? api.sftpPreviewUrl(hostId, player.path) : undefined}
                  controls
                  autoPlay
                  style={{ width: '100%', height: '100%', objectFit: 'contain' }}
                />
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
