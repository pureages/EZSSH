import type { ComponentType } from 'react'

export interface DesktopApp {
  id: string
  name: string
  icon: string
  defaultSize: { width: number; height: number }
  /** 是否需要选择主机上下文 */
  needsHost?: boolean
  /** 是否单实例 */
  singleton?: boolean
  /** 支持的系统类型白名单；主机已探测到类型且不在此列时，右键菜单置灰 */
  platforms?: ('linux' | 'windows')[]
  /** 置灰时悬停提示文字 */
  disabledTip?: string
  component: ComponentType<AppProps>
}

export interface AppProps {
  windowId: string
  hostId: string | null
  channelId: string
  /** 目标主机系统类型（"" 表示未知/未探测） */
  platform?: string
  onTitle?: (title: string) => void
}

export const appRegistry: DesktopApp[] = []

export function registerApp(app: DesktopApp) {
  if (!appRegistry.find((a) => a.id === app.id)) {
    appRegistry.push(app)
  }
}

export function getApp(id: string): DesktopApp | undefined {
  return appRegistry.find((a) => a.id === id)
}
