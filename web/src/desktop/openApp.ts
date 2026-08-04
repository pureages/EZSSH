import { getApp } from './appRegistry'
import { useWindowStore } from './windowStore'
import { newChannelId } from '../lib/ws'
import { useSecuritySettings } from '../lib/securitySettingsStore'
import type { Host } from '../lib/types'

/**
 * 打开一个应用窗口（几何/标题与 DesktopPage 的 openApp 完全一致）：
 * - 标题：`<服务器名> - <应用名>`；文件管理器额外显示登录用户名
 *   （可被安全设置「隐藏文件管理器用户名」关闭）
 * - 大小：应用默认尺寸按视口收敛，居中放置 + 连续打开级联错开
 *
 * 供桌面 openApp 与跨应用跳转（如网站管理 → 查看文件）共用，保证窗口行为一致。
 */
export function openAppWindow(appId: string, host?: Host) {
  const app = getApp(appId)
  if (!app) return
  const showFmUser =
    appId === 'files' &&
    !!host &&
    !useSecuritySettings.getState().hideFmUsername &&
    !!host.username
  const title = showFmUser
    ? `${host!.name} - ${app.name} - ${host!.username}`
    : host
      ? `${host.name} - ${app.name}`
      : app.name
  const titleKey = showFmUser ? '{0} - {1} - {2}' : host ? '{0} - {1}' : app.name
  const titleArgs = showFmUser
    ? [host!.name, app.name, host!.username]
    : host
      ? [host.name, app.name]
      : undefined
  const vw = window.innerWidth
  const vh = window.innerHeight
  const w = Math.min(app.defaultSize.width, vw - 16)
  const h = Math.min(app.defaultSize.height, vh - 16)
  const cascade = useWindowStore.getState().windows.length % 6
  const x = Math.max(8, Math.round((vw - w) / 2) + cascade * 24)
  const y = Math.max(8, Math.round((vh - h) / 2) + cascade * 22)
  return useWindowStore.getState().open({
    id: `${appId}-${Date.now().toString(36)}`,
    appId,
    title,
    titleKey,
    titleArgs,
    icon: app.icon,
    hostId: host?.id ?? null,
    platform: host?.platform ?? '',
    channelId: newChannelId(),
    x,
    y,
    width: w,
    height: h,
  })
}
