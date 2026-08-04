export interface Host {
  id: string
  name: string
  host: string
  port: number
  username: string
  auth_type: 'password' | 'privatekey'
  group_name: string
  remark: string
  created_at: string
  updated_at: string
  connected: boolean
  fingerprint?: string
  /** 系统类型：""（未探测/自动检测）| "linux" | "windows" */
  platform?: string
  /** 桌面隐藏（设置-安全可显示全部） */
  hidden?: boolean
  /** 内置网关主机（默认播种，可删除） */
  builtin?: boolean
}

export interface HostInput {
  name: string
  host: string
  port: number
  username: string
  auth_type: 'password' | 'privatekey'
  password?: string
  private_key?: string
  group_name: string
  remark: string
  /** 系统类型：""（自动检测）| "linux" | "windows" */
  platform?: string
}

export interface InitStatus {
  initialized: boolean
  unlocked: boolean
  login_route: string
  /** 界面语言偏好（后端 settings 持久化）：zh | en */
  lang: string
  /** 网关版本号（来自后端，支持 ldflags 覆盖） */
  version: string
}

export interface MeInfo {
  username: string
  vaultUnlocked: boolean
}

export interface ExecResult {
  output: string
  error?: string
}

export interface SftpEntry {
  name: string
  size: number
  mode: string
  mode_num: number
  is_dir: boolean
  is_link: boolean
  link_target?: string
  uid: number
  gid: number
  mtime: number
}

export interface NetStat {
  iface: string
  rx_bps: number
  tx_bps: number
  rx_bytes: number
  tx_bytes: number
}

export interface DiskStat {
  mount: string
  total: number
  used: number
  pct: number
}

export interface MonitorSnapshot {
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
  disks: DiskStat[]
  net: NetStat[]
}

export interface WSMessage {
  type: string
  channelId: string
  payload?: Record<string, unknown>
}

/** 主机地址的地理位置信息（来自 GET /api/geo）。 */
export interface GeoInfo {
  ip: string
  country: string
  country_code: string
  region: string
  lat: number
  lon: number
}

/** 保存的一键命令（来自 /api/commands）。 */
export interface SavedCommand {
  id: number
  name: string
  command: string
  created_at: string
  updated_at: string
}

/** 后台任务执行结果（POST /api/background/start 逐台返回）。 */
export interface BackgroundStartResult {
  hostId: string
  hostName: string
  ok: boolean
  error?: string
  task?: { id: string; pid: number }
}

/** 后台任务视图（GET /api/background）。status: running=运行中 / exited=已退出 / unknown=主机离线或未解锁 */
export interface BackgroundTask {
  id: string
  hostId: string
  hostName: string
  pid: number
  command: string
  started: number
  status: 'running' | 'exited' | 'unknown'
  cpu: number
  mem: number
  rss: number
  start: string
}

// ---- 网站管理 ----

/** 网站（Nginx 站点记录，来自 /api/websites）。 */
export interface Website {
  id: string
  hostId: string
  hostName: string
  name: string
  group_name: string
  /** 逗号分隔的域名，第 1 个为主域名 */
  domains: string
  /** static | proxy | redirect */
  site_type: 'static' | 'proxy' | 'redirect'
  root_dir: string
  proxy_pass: string
  redirect_url: string
  ssl: boolean
  cert_id: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface WebsiteInput {
  hostId: string
  name: string
  group_name: string
  domains: string
  site_type: 'static' | 'proxy' | 'redirect'
  root_dir: string
  proxy_pass: string
  redirect_url: string
  ssl: boolean
  enabled: boolean
}

/** Nginx 安装状态（GET /api/nginx/status）。 */
export interface NginxStatus {
  installed: boolean
  version: string
  running: boolean
}

/** 站点部署结果（POST /deploy）。 */
export interface DeployResult {
  ok: boolean
  output: string
  warning?: string
  error?: string
}

/** Let's Encrypt 证书记录（来自 /api/certificates）。 */
export interface Certificate {
  id: string
  hostId: string
  hostName: string
  domain: string
  method: 'http' | 'dns'
  dns_account_id: string
  email: string
  /** issuing | active | renewing | error */
  status: string
  expires_at: string
  last_renew: string
  error: string
  created_at: string
}

export interface CertIssueInput {
  host_id: string
  website_id?: string
  domain: string
  method: 'http' | 'dns'
  dns_account_id: string
  email: string
  webroot: string
}

/** 证书可用性检测结果（GET /api/certificates/check）。 */
export interface CertCheckResult {
  installed: boolean
  expires_at: string
}

/** DNS 验证账户（Cloudflare API Token；明文不返回）。 */
export interface DnsAccount {
  id: string
  name: string
  provider: string
  has_token: boolean
  created_at: string
}

export interface DnsAccountInput {
  name: string
  provider: string
  token?: string
}
