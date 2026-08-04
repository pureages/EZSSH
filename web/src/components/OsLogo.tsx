/**
 * 系统 logo：根据发行版 ID 显示对应的 OS 图标（web/public/os/*.svg）。
 * 未识别或加载中时回退为桌面图标 A（🖥️）。
 */

/** /etc/os-release 的 ID（或常见变体） -> public/os 下的文件名 */
const DISTRO_MAP: Record<string, string> = {
  ubuntu: 'ubuntu',
  debian: 'debian',
  centos: 'centos',
  fedora: 'fedora',
  arch: 'archlinux',
  manjaro: 'manjaro',
  linuxmint: 'linuxmint',
  rocky: 'rockylinux',
  almalinux: 'almalinux',
  rhel: 'redhat',
  redhat: 'redhat',
  raspbian: 'raspberrypi',
  alpine: 'alpinelinux',
  kali: 'kalilinux',
  // 前缀匹配处理 opensuse-leap / opensuse-tumbleweed 等变体
  opensuse: 'opensuse',
  // Windows 服务器（后端 monitor.hwinfo 对 Windows 目标机固定返回 distro=windows）
  windows: 'windows',
}

/** 将发行版 ID 映射到 logo 文件路径；无法识别返回 null。 */
export function osLogoPath(distro: string): string | null {
  const id = (distro || '').toLowerCase().trim()
  if (!id) return null
  if (DISTRO_MAP[id]) return `/os/${DISTRO_MAP[id]}.svg`
  for (const [key, file] of Object.entries(DISTRO_MAP)) {
    if (id.startsWith(key)) return `/os/${file}.svg`
  }
  return null
}

/** 图标 A：默认桌面图标（🖥️）。 */
export function BaseIcon({ size = 30 }: { size?: number }) {
  return <span style={{ fontSize: size, lineHeight: 1, display: 'inline-block' }}>🖥️</span>
}

export function OsLogo({
  distro,
  forceBase = false,
  size = 30,
}: {
  distro?: string
  /** 强制显示图标 A（桌面刷新闪烁期间） */
  forceBase?: boolean
  size?: number
}) {
  const path = forceBase ? null : osLogoPath(distro || '')
  if (!path) return <BaseIcon size={size} />
  return (
    <img
      src={path}
      alt={distro || 'os'}
      draggable={false}
      style={{ width: size, height: size, objectFit: 'contain', display: 'block' }}
    />
  )
}
