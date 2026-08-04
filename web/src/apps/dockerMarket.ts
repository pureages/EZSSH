/**
 * Docker 应用市场：内置常用应用的模板目录。
 * 用户点击应用 → 查看介绍页 → 填写简单配置（端口、环境变量等）→ 一键安装。
 */

/** 字段如何映射为 docker run 参数 */
export type FieldMap =
  | { kind: 'name' } // 值 = 容器名称
  | { kind: 'port'; containerPort: number; protocol?: 'tcp' | 'udp'; envKey?: string } // 值 = 宿主机端口；envKey 存在时同时写入环境变量
  | { kind: 'env'; envKey: string } // 值 = 环境变量值
  | { kind: 'volume'; containerPath: string } // 值 = 宿主机路径
  | { kind: 'arg'; arg: string } // 值填充 {v} 生成额外参数
  | { kind: 'config' } // 值仅供 configFile 模板使用，不生成 docker 参数

export interface MarketField {
  key: string
  label: string
  type: 'text' | 'number' | 'password' | 'select'
  default: string
  placeholder?: string
  help?: string
  options?: string[]
  map: FieldMap
}

export interface MarketApp {
  id: string
  name: string
  icon: string
  tagline: string
  description: string
  image: string
  network?: 'bridge' | 'host'
  restart?: string
  privileged?: boolean
  fixedEnv?: Record<string, string>
  fixedPorts?: string[]
  fixedVolumes?: string[]
  // configFile 接收表单值并返回配置文件内容（TOML 等）；配合 configPath 由后端写入并挂载，
  // 用于无视环境变量的镜像（如 snowdreamtech/frps 只读取 /etc/frp/frps.toml）。
  configFile?: (values: Record<string, string>) => string
  configPath?: string
  fields: MarketField[]
}

/** 提交给后端的容器创建参数（对应 docker run） */
export interface ContainerSpec {
  name: string
  image: string
  ports: string[]
  env: string[]
  volumes: string[]
  extraArgs: string[]
  network: string
  privileged: boolean
  restart: string
  configFile?: string
  configPath?: string
}

export function emptySpec(): ContainerSpec {
  return {
    name: '',
    image: '',
    ports: [],
    env: [],
    volumes: [],
    extraArgs: [],
    network: 'bridge',
    privileged: false,
    restart: 'always',
  }
}

/** 根据应用模板 + 表单值构建容器创建参数 */
export function buildMarketSpec(app: MarketApp, values: Record<string, string>): ContainerSpec {
  const spec = emptySpec()
  spec.name = app.id
  spec.image = app.image
  spec.network = app.network || 'bridge'
  spec.restart = app.restart || 'always'
  spec.privileged = !!app.privileged
  spec.ports = [...(app.fixedPorts || [])]
  spec.volumes = [...(app.fixedVolumes || [])]
  for (const [k, v] of Object.entries(app.fixedEnv || {})) spec.env.push(`${k}=${v}`)

  for (const f of app.fields) {
    const val = (values[f.key] ?? f.default ?? '').trim()
    switch (f.map.kind) {
      case 'name':
        if (val) spec.name = val
        break
      case 'port':
        if (val) {
          spec.ports.push(`${val}:${f.map.containerPort}${f.map.protocol ? '/' + f.map.protocol : ''}`)
          if (f.map.envKey) spec.env.push(`${f.map.envKey}=${val}`)
        }
        break
      case 'env':
        if (val) spec.env.push(`${f.map.envKey}=${val}`)
        break
      case 'volume':
        if (val) spec.volumes.push(`${val}:${f.map.containerPath}`)
        break
      case 'arg':
        if (val) spec.extraArgs.push(f.map.arg.replace('{v}', val))
        break
      case 'config':
        // 值仅用于 configFile 模板，不生成任何 docker 参数
        break
    }
  }
  if (app.configFile) {
    spec.configFile = app.configFile(values)
    spec.configPath = app.configPath
  }
  return spec
}

/** 转义 TOML 基础字符串中的反斜杠与双引号 */
function tomlStr(s: string): string {
  return `"${s.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`
}

/** 生成表单初始值 */
export function marketDefaults(app: MarketApp): Record<string, string> {
  const v: Record<string, string> = {}
  for (const f of app.fields) v[f.key] = f.default
  return v
}

export const marketApps: MarketApp[] = [
  {
    id: 'frps',
    name: 'Frp 服务端',
    icon: '🚀',
    tagline: '内网穿透服务端（frps）',
    description:
      'frps 是 frp 的内网穿透服务端，需部署在有公网 IP 的服务器上。配合 frpc 客户端使用，可将内网服务安全地暴露到公网。安装后开放监听端口与可选的 Web 控制台。',
    image: 'snowdreamtech/frps:latest',
    network: 'bridge',
    restart: 'always',
    configPath: '/etc/frp/frps.toml',
    configFile: (v) => {
      const token = (v.token || '').trim()
      const dashPort = (v.dashboardPort || '').trim()
      const lines: string[] = []
      lines.push(`bindAddr = "0.0.0.0"`)
      lines.push(`bindPort = ${(v.bindPort || '7000').trim()}`)
      lines.push(`auth.method = "token"`)
      if (token) lines.push(`auth.token = ${tomlStr(token)}`)
      if (dashPort) {
        lines.push(`webServer.addr = "0.0.0.0"`)
        lines.push(`webServer.port = ${dashPort}`)
        const dashUser = (v.dashboardUser || '').trim()
        const dashPwd = (v.dashboardPwd || '').trim()
        if (dashUser) lines.push(`webServer.user = ${tomlStr(dashUser)}`)
        if (dashPwd) lines.push(`webServer.password = ${tomlStr(dashPwd)}`)
      }
      return lines.join('\n') + '\n'
    },
    fields: [
      {
        key: 'bindPort',
        label: '监听端口',
        type: 'number',
        default: '7000',
        placeholder: '7000',
        help: 'frpc 客户端连接服务端使用的端口',
        map: { kind: 'port', containerPort: 7000 },
      },
      {
        key: 'dashboardPort',
        label: 'Web 控制台端口',
        type: 'number',
        default: '7500',
        placeholder: '7500',
        help: '留空表示不开放控制台',
        map: { kind: 'port', containerPort: 7500 },
      },
      {
        key: 'token',
        label: '连接令牌',
        type: 'password',
        default: '',
        placeholder: '建议设置复杂令牌',
        help: 'frpc 客户端需使用相同的令牌连接',
        map: { kind: 'config' },
      },
      { key: 'dashboardUser', label: '控制台用户名', type: 'text', default: 'admin', map: { kind: 'config' } },
      { key: 'dashboardPwd', label: '控制台密码', type: 'password', default: 'admin', map: { kind: 'config' } },
    ],
  },
  {
    id: 'frpc',
    name: 'Frp 客户端',
    icon: '🔗',
    tagline: '内网穿透客户端（frpc）',
    description:
      'frpc 是 frp 的内网穿透客户端，部署在内网机器上，通过服务端（frps）将本地服务暴露到公网。部署后访问「服务端IP:公网映射端口」即可访问内网服务。',
    image: 'snowdreamtech/frpc:latest',
    network: 'bridge',
    restart: 'always',
    configPath: '/etc/frp/frpc.toml',
    configFile: (v) => {
      const token = (v.token || '').trim()
      const proxyType = (v.proxyType || 'tcp').trim()
      const lines: string[] = []
      lines.push(`serverAddr = ${tomlStr((v.serverAddr || '127.0.0.1').trim())}`)
      lines.push(`serverPort = ${(v.serverPort || '7000').trim()}`)
      lines.push(`auth.method = "token"`)
      if (token) lines.push(`auth.token = ${tomlStr(token)}`)
      lines.push('')
      lines.push('[[proxies]]')
      lines.push(`name = ${tomlStr((v.proxyName || 'web').trim())}`)
      lines.push(`type = ${tomlStr(proxyType)}`)
      lines.push(`localIP = ${tomlStr((v.localIP || '127.0.0.1').trim())}`)
      lines.push(`localPort = ${(v.localPort || '80').trim()}`)
      // tcp/udp 用 remotePort 映射公网端口；http/https 用 customDomains 转发
      if (proxyType === 'tcp' || proxyType === 'udp') {
        const remotePort = (v.remotePort || '').trim()
        if (remotePort) lines.push(`remotePort = ${remotePort}`)
      }
      return lines.join('\n') + '\n'
    },
    fields: [
      { key: 'serverAddr', label: '服务端地址', type: 'text', default: '127.0.0.1', placeholder: '公网 IP 或域名', map: { kind: 'config' } },
      { key: 'serverPort', label: '服务端端口', type: 'number', default: '7000', map: { kind: 'config' } },
      { key: 'token', label: '连接令牌', type: 'password', default: '', placeholder: '与服务端保持一致', map: { kind: 'config' } },
      { key: 'proxyName', label: '代理名称', type: 'text', default: 'web', help: '自定义代理标识', map: { kind: 'config' } },
      { key: 'proxyType', label: '代理类型', type: 'select', default: 'tcp', options: ['tcp', 'udp', 'http', 'https'], map: { kind: 'config' } },
      { key: 'localIP', label: '内网服务 IP', type: 'text', default: '127.0.0.1', map: { kind: 'config' } },
      { key: 'localPort', label: '内网服务端口', type: 'number', default: '80', map: { kind: 'config' } },
      { key: 'remotePort', label: '公网映射端口', type: 'number', default: '8080', help: '访问「服务端IP:此端口」即可访问内网服务（tcp/udp 生效）', map: { kind: 'config' } },
    ],
  },
]
