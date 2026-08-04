import type {
  BackgroundStartResult,
  BackgroundTask,
  Certificate,
  CertCheckResult,
  CertIssueInput,
  DeployResult,
  DnsAccount,
  DnsAccountInput,
  ExecResult,
  GeoInfo,
  Host,
  HostInput,
  InitStatus,
  MeInfo,
  NginxStatus,
  SavedCommand,
  SftpEntry,
  Website,
  WebsiteInput,
} from './types'
import { useSession } from './session'
import { tt } from './i18n'

export class ApiError extends Error {
  status: number
  needCaptcha = false
  constructor(status: number, message: string, needCaptcha = false) {
    super(message)
    this.status = status
    this.needCaptcha = needCaptcha
  }
}

/** 401 防抖：同一会话失效短时间内只触发一次全局登出，避免并发请求导致路由抖动 */
let unauthorizedNotified = false
let unauthorizedTimer: ReturnType<typeof setTimeout> | null = null

function notifyUnauthorized() {
  if (unauthorizedNotified) return
  unauthorizedNotified = true
  if (unauthorizedTimer) clearTimeout(unauthorizedTimer)
  unauthorizedTimer = setTimeout(() => {
    unauthorizedNotified = false
  }, 3000)
  useSession.getState().setAuthed(false)
}

interface RequestOpts extends RequestInit {
  /** 401 时静默处理（不触发全局登出）。用于启动时的会话探测 me()。 */
  silent401?: boolean
}

async function request<T>(path: string, init?: RequestOpts): Promise<T> {
  const headers: Record<string, string> = {}
  if (init?.body) headers['Content-Type'] = 'application/json'
  const { silent401, ...fetchInit } = init ?? {}
  const res = await fetch(path, { ...fetchInit, headers, credentials: 'same-origin' })
  // 先解析 body，401 时才能拿到 need_captcha 等业务字段
  const data = (await res.json().catch(() => ({}))) as Record<string, unknown>
  if (res.status === 401) {
    // 非静默请求 401：通知全局会话状态，路由层据此统一重定向。
    // 但登录失败（需验证码）也是 401，此时仍在登录页，不应触发全局登出。
    if (!silent401 && !data.need_captcha) notifyUnauthorized()
    const msg = typeof data.error === 'string' ? tt(data.error) : tt('unauthorized')
    throw new ApiError(res.status, msg, data.need_captcha === true)
  }
  if (!res.ok) {
    const msg = typeof data.error === 'string' ? tt(data.error) : tt('HTTP {0}', res.status)
    throw new ApiError(res.status, msg, data.need_captcha === true)
  }
  return data as T
}

/**
 * NDJSON 流式 POST：逐行回调 {"line":"..."}，以 {"ok":"..."} 成功结束、{"error":"..."} 失败结束。
 * 用于 Nginx 一键安装、证书签发/续签等长耗时操作的实时进度。
 */
function streamNDJSON<T = Record<string, unknown>>(
  path: string,
  body: unknown,
  onLine?: (line: string) => void,
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const err = (status: number, msg: string, needCaptcha = false) =>
      new ApiError(status, msg, needCaptcha)
    void (async () => {
      let res: Response
      try {
        res = await fetch(path, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
          credentials: 'same-origin',
        })
      } catch {
        reject(err(0, tt('网络错误')))
        return
      }
      if (!res.ok) {
        const data = (await res.json().catch(() => ({}))) as Record<string, unknown>
        if (res.status === 401 && !data.need_captcha) notifyUnauthorized()
        const msg = typeof data.error === 'string' ? tt(data.error) : tt('HTTP {0}', res.status)
        reject(err(res.status, msg, data.need_captcha === true))
        return
      }
      if (!res.body) {
        reject(err(res.status, tt('响应无数据流')))
        return
      }
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      let resolved = false
      try {
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          buf += decoder.decode(value, { stream: true })
          let idx: number
          while ((idx = buf.indexOf('\n')) >= 0) {
            const line = buf.slice(0, idx).trim()
            buf = buf.slice(idx + 1)
            if (!line) continue
            let data: Record<string, unknown>
            try {
              data = JSON.parse(line)
            } catch {
              continue
            }
            if (typeof data.error === 'string') {
              reject(err(res.status, tt(data.error)))
              resolved = true
              return
            }
            if (typeof data.line === 'string') {
              onLine?.(data.line)
              continue
            }
            if (data.ok) {
              resolve(data as T)
              resolved = true
              return
            }
          }
        }
        if (!resolved) reject(err(res.status, tt('响应异常结束')))
      } catch (e) {
        if (!resolved) {
          if (e instanceof DOMException && e.name === 'AbortError') reject(err(0, tt('已取消')))
          else reject(err(0, tt('网络错误')))
        }
      }
    })()
  })
}

export const api = {
  initStatus: () => request<InitStatus>('/api/init-status'),

  init: (username: string, password: string) =>
    request<{ username: string }>('/api/init', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),

  login: (username: string, password: string, captcha?: { id: string; code: string }) =>
    request<{ username: string }>('/api/login', {
      method: 'POST',
      body: JSON.stringify({
        username,
        password,
        captcha_id: captcha?.id,
        captcha_code: captcha?.code,
      }),
    }),

  logout: () => request<{ ok: string }>('/api/logout', { method: 'POST' }),

  me: () => request<MeInfo>('/api/me', { silent401: true }),

  // ---- 设置 ----
  getSettings: () => request<{ login_route: string; lang: string; hide_fm_username: string }>('/api/settings'),
  updateSettings: (opts: { login_route?: string; lang?: string; hide_fm_username?: string }) =>
    request<{ login_route: string; lang: string; hide_fm_username: string }>('/api/settings', {
      method: 'PUT',
      body: JSON.stringify(opts),
    }),
  changePassword: (oldPassword: string, newPassword: string) =>
    request<{ ok: string }>('/api/change-password', {
      method: 'POST',
      body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
    }),
  captcha: () => request<{ id: string; svg: string }>('/api/captcha'),

  listHosts: () => request<Host[]>('/api/hosts'),

  createHost: (input: HostInput) =>
    request<Host>('/api/hosts', { method: 'POST', body: JSON.stringify(input) }),

  updateHost: (id: string, input: HostInput) =>
    request<Host>(`/api/hosts/${id}`, { method: 'PUT', body: JSON.stringify(input) }),

  deleteHost: (id: string) =>
    request<{ ok: string }>(`/api/hosts/${id}`, { method: 'DELETE' }),

  /** 隐藏 / 显示桌面上某台服务器图标 */
  hideHost: (id: string, hidden: boolean) =>
    request<{ ok: string }>(`/api/hosts/${id}/hide`, {
      method: 'POST',
      body: JSON.stringify({ hidden }),
    }),

  /** 显示所有桌面隐藏的服务器 */
  showAllHosts: () =>
    request<{ ok: string }>('/api/hosts/show-all', { method: 'POST' }),

  connect: (id: string) =>
    request<{ connected: string; fingerprint: string; platform?: string }>(
      `/api/hosts/${id}/connect`,
      { method: 'POST' },
    ),

  /** 用表单参数测试 SSH 连通性（不持久化） */
  testConnect: (input: HostInput) =>
    request<{ connected: string; fingerprint: string; platform?: string }>(
      '/api/test-connect',
      { method: 'POST', body: JSON.stringify(input) },
    ),

  status: (id: string) =>
    request<{ connected: boolean; fingerprint: string; platform?: string }>(
      `/api/hosts/${id}/status`,
    ),

  exec: (id: string, command: string) =>
    request<ExecResult>(`/api/hosts/${id}/exec`, {
      method: 'POST',
      body: JSON.stringify({ command }),
    }),

  /** 批量查询主机地址（IP/域名）的地理位置，返回以地址为键的映射。 */
  geo: (hosts: string[]) =>
    request<Record<string, GeoInfo>>(`/api/geo?hosts=${encodeURIComponent(hosts.join(','))}`),

  // ---- SFTP ----
  sftpList: (id: string, dir: string) =>
    request<SftpEntry[]>(`/api/hosts/${id}/sftp/list?path=${encodeURIComponent(dir)}`),

  sftpMkdir: (id: string, path: string) =>
    request<{ ok: string }>(`/api/hosts/${id}/sftp/mkdir`, {
      method: 'POST',
      body: JSON.stringify({ path }),
    }),

  sftpRename: (id: string, oldPath: string, newPath: string) =>
    request<{ ok: string }>(`/api/hosts/${id}/sftp/rename`, {
      method: 'POST',
      body: JSON.stringify({ old_path: oldPath, new_path: newPath }),
    }),

  sftpRemove: (id: string, path: string) =>
    request<{ ok: string }>(`/api/hosts/${id}/sftp/remove`, {
      method: 'POST',
      body: JSON.stringify({ path }),
    }),

  sftpChmod: (id: string, path: string, mode: number) =>
    request<{ ok: string }>(`/api/hosts/${id}/sftp/chmod`, {
      method: 'POST',
      body: JSON.stringify({ path, mode }),
    }),

  /** 解压压缩包（zip / tar.gz 等，原位解压到归档所在目录） */
  sftpExtract: (id: string, path: string) =>
    request<{ ok: string }>(`/api/hosts/${id}/sftp/extract`, {
      method: 'POST',
      body: JSON.stringify({ path }),
    }),

  sftpRead: (id: string, path: string) =>
    request<{ name: string; content: string; mode: number; size: number }>(
      `/api/hosts/${id}/sftp/read?path=${encodeURIComponent(path)}`,
    ),

  sftpWrite: (id: string, path: string, content: string) =>
    request<{ ok: string }>(`/api/hosts/${id}/sftp/write`, {
      method: 'POST',
      body: JSON.stringify({ path, content }),
    }),

  sftpDownloadUrl: (id: string, path: string) =>
    `/api/hosts/${id}/sftp/download?path=${encodeURIComponent(path)}`,

  /** 内联预览地址（图片/视频，支持 Range 流式） */
  sftpPreviewUrl: (id: string, path: string) =>
    `/api/hosts/${id}/sftp/download?path=${encodeURIComponent(path)}&inline=1`,

  /** 粘贴：把源主机的文件/目录复制或移动到当前主机（NDJSON 流式进度）。
   *  transport: local=同服务器；relay=经 Web 服务器中转；direct=两服务器直连（无精确进度）。
   *  onProgress 收到已传输/总字节数；relay 与 local-copy 才有进度，direct 无。
   *  signal 可中止传输（用户取消）。 */
  sftpPasteStream: (
    dstId: string,
    req: {
      src_host_id: string
      src_path: string
      dst_dir: string
      mode: 'copy' | 'move'
      transport: 'local' | 'relay' | 'direct'
    },
    onProgress?: (loaded: number, total: number) => void,
    signal?: AbortSignal,
  ) =>
    new Promise<{ ok: string }>((resolve, reject) => {
      const err = (status: number, msg: string, needCaptcha = false) =>
        new ApiError(status, msg, needCaptcha)
      void (async () => {
        let res: Response
        try {
          res = await fetch(`/api/hosts/${dstId}/sftp/paste`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(req),
            credentials: 'same-origin',
            signal,
          })
        } catch (e) {
          if (e instanceof DOMException && e.name === 'AbortError') {
            reject(err(0, tt('已取消')))
          } else {
            reject(err(0, tt('网络错误')))
          }
          return
        }
        if (!res.ok) {
          const data = (await res.json().catch(() => ({}))) as Record<string, unknown>
          if (res.status === 401 && !data.need_captcha) notifyUnauthorized()
          const msg = typeof data.error === 'string' ? tt(data.error) : tt('HTTP {0}', res.status)
          reject(err(res.status, msg, data.need_captcha === true))
          return
        }
        if (!res.body) {
          reject(err(res.status, tt('响应无数据流')))
          return
        }
        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buf = ''
        let resolved = false
        try {
          for (;;) {
            const { done, value } = await reader.read()
            if (done) break
            buf += decoder.decode(value, { stream: true })
            let idx: number
            while ((idx = buf.indexOf('\n')) >= 0) {
              const line = buf.slice(0, idx).trim()
              buf = buf.slice(idx + 1)
              if (!line) continue
              let data: Record<string, unknown>
              try {
                data = JSON.parse(line)
              } catch {
                continue
              }
              if (typeof data.error === 'string') {
                reject(err(res.status, tt(data.error)))
                resolved = true
                return
              }
              if (typeof data.loaded === 'number' && typeof data.total === 'number') {
                onProgress?.(data.loaded, data.total)
              }
              if (data.ok) {
                resolve({ ok: String(data.ok) })
                resolved = true
                return
              }
            }
          }
          if (!resolved) reject(err(res.status, tt('响应异常结束')))
        } catch (e) {
          if (!resolved) {
            if (e instanceof DOMException && e.name === 'AbortError') reject(err(0, tt('已取消')))
            else reject(err(0, tt('网络错误')))
          }
        }
      })()
    }),

  sftpUpload: (
    id: string,
    path: string,
    file: File,
    onProgress?: (pct: number, loaded: number, total: number) => void,
  ) =>
    new Promise<{ ok: string }>((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('POST', `/api/hosts/${id}/sftp/upload?path=${encodeURIComponent(path)}`)
      xhr.upload.onprogress = (e) => {
        if (!onProgress) return
        if (e.lengthComputable) {
          onProgress(Math.round((e.loaded / e.total) * 100), e.loaded, e.total)
        } else {
          // 长度不可计算时按文件总大小估算
          onProgress(-1, e.loaded, file.size)
        }
      }
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          try {
            resolve(JSON.parse(xhr.responseText))
          } catch {
            resolve({ ok: 'true' })
          }
        } else {
          let msg = tt('HTTP {0}', xhr.status)
          try {
            msg = tt(JSON.parse(xhr.responseText).error || msg)
          } catch {
            /* ignore */
          }
          reject(new ApiError(xhr.status, msg))
        }
      }
      xhr.onerror = () => reject(new ApiError(0, tt('网络错误')))
      const fd = new FormData()
      fd.append('file', file)
      xhr.send(fd)
    }),

  // ---- 一键命令：保存的命令 ----
  listCommands: () => request<SavedCommand[]>('/api/commands'),
  createCommand: (name: string, command: string) =>
    request<SavedCommand>('/api/commands', {
      method: 'POST',
      body: JSON.stringify({ name, command }),
    }),
  updateCommand: (id: number, name: string, command: string) =>
    request<SavedCommand>(`/api/commands/${id}`, {
      method: 'PUT',
      body: JSON.stringify({ name, command }),
    }),
  deleteCommand: (id: number) =>
    request<{ ok: string }>(`/api/commands/${id}`, { method: 'DELETE' }),

  // ---- 一键命令：后台长期运行任务 ----
  backgroundStart: (hostIds: string[], command: string) =>
    request<BackgroundStartResult[]>('/api/background/start', {
      method: 'POST',
      body: JSON.stringify({ host_ids: hostIds, command }),
    }),
  backgroundList: () => request<BackgroundTask[]>('/api/background'),
  backgroundKill: (id: string) =>
    request<{ ok: string }>(`/api/background/${id}/kill`, { method: 'POST' }),
  backgroundLogs: (id: string, lines = 500) =>
    request<{ logs: string }>(`/api/background/${id}/logs?lines=${lines}`),

  // ---- 网站管理：站点 ----
  websiteList: (hostId: string, group?: string) =>
    request<Website[]>(
      `/api/websites?host_id=${encodeURIComponent(hostId)}${group ? `&group=${encodeURIComponent(group)}` : ''}`,
    ),
  websiteGroups: (hostId: string) =>
    request<string[]>(`/api/websites/groups?host_id=${encodeURIComponent(hostId)}`),
  createWebsite: (input: WebsiteInput) =>
    request<Website>('/api/websites', { method: 'POST', body: JSON.stringify(input) }),
  updateWebsite: (id: string, input: WebsiteInput) =>
    request<Website>(`/api/websites/${id}`, { method: 'PUT', body: JSON.stringify(input) }),
  deleteWebsite: (id: string) =>
    request<{ ok: string; warnings?: string[] }>(`/api/websites/${id}`, { method: 'DELETE' }),
  deployWebsite: (id: string, lang?: string) =>
    request<DeployResult>(`/api/websites/${id}/deploy${lang ? `?lang=${lang}` : ''}`, { method: 'POST' }),
  toggleWebsite: (id: string, enabled: boolean, lang?: string) =>
    request<DeployResult>(`/api/websites/${id}/enable${lang ? `?lang=${lang}` : ''}`, {
      method: 'POST',
      body: JSON.stringify({ enabled }),
    }),

  // ---- 网站管理：Nginx ----
  nginxStatus: (hostId: string) =>
    request<NginxStatus>(`/api/nginx/status?host_id=${encodeURIComponent(hostId)}`),
  nginxInstall: (hostId: string, onLine?: (line: string) => void) =>
    streamNDJSON<{ ok: string }>('/api/nginx/install', { host_id: hostId }, onLine),

  // ---- 网站管理：证书 ----
  certList: (hostId: string) =>
    request<Certificate[]>(`/api/certificates?host_id=${encodeURIComponent(hostId)}`),
  /** 检测某域名证书是否已安装到 /etc/nginx/ssl/<domain>/（建站表单 SSL 可用性提示） */
  certCheck: (hostId: string, domain: string) =>
    request<CertCheckResult>(
      `/api/certificates/check?host_id=${encodeURIComponent(hostId)}&domain=${encodeURIComponent(domain)}`,
    ),
  certIssue: (input: CertIssueInput, onLine?: (line: string) => void) =>
    streamNDJSON<{ ok: string; cert_id: string }>('/api/certificates/issue', input, onLine),
  certRenew: (id: string, onLine?: (line: string) => void) =>
    streamNDJSON<{ ok: string }>(`/api/certificates/${id}/renew`, {}, onLine),
  certSync: (id: string) => request<Certificate>(`/api/certificates/${id}/sync`, { method: 'POST' }),
  certDelete: (id: string) =>
    request<{ ok: string }>(`/api/certificates/${id}`, { method: 'DELETE' }),

  // ---- 网站管理：DNS 账户 ----
  dnsList: () => request<DnsAccount[]>('/api/dns-accounts'),
  dnsCreate: (input: DnsAccountInput) =>
    request<DnsAccount>('/api/dns-accounts', { method: 'POST', body: JSON.stringify(input) }),
  dnsUpdate: (id: string, input: DnsAccountInput) =>
    request<DnsAccount>(`/api/dns-accounts/${id}`, { method: 'PUT', body: JSON.stringify(input) }),
  dnsDelete: (id: string) =>
    request<{ ok: string }>(`/api/dns-accounts/${id}`, { method: 'DELETE' }),
}
