import type { WSMessage } from './types'
import { useSession } from './session'
import { tt } from './i18n'

type Handler = (msg: WSMessage) => void

const SOCKET_PATH = '/ws'
const RECONNECT_DELAY = 5000

/**
 * 单例 WebSocket 客户端：同一页面所有窗口复用一条连接。
 * 消息按 channelId 路由到对应订阅者。
 */
class WSClient {
  private ws: WebSocket | null = null
  private typeHandlers = new Map<string, Set<Handler>>()
  private channelHandlers = new Map<string, Set<Handler>>()
  private connecting: Promise<void> | null = null
  private closed = false

  async connect(): Promise<void> {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) return
    if (this.connecting) return this.connecting

    this.closed = false
    this.connecting = new Promise<void>((resolve, reject) => {
      const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
      const ws = new WebSocket(`${proto}://${window.location.host}${SOCKET_PATH}`)
      this.ws = ws

      ws.onopen = () => {
        this.connecting = null
        resolve()
      }
      ws.onerror = () => {
        this.connecting = null
        reject(new Error(tt('WebSocket 连接失败')))
      }
      ws.onmessage = (ev) => this.dispatch(ev.data as string)
      ws.onclose = () => {
        this.ws = null
        this.connecting = null
        // 已登出则不再重连（登录后会由组件重新 connect）
        if (useSession.getState().authed === false) {
          this.closed = true
          return
        }
        // 页面存活且已认证时自动重连（心跳由各终端 ping 维持）
        if (!this.closed) {
          setTimeout(() => void this.connect(), RECONNECT_DELAY)
        }
      }
    })
    return this.connecting
  }

  close() {
    this.closed = true
    this.ws?.close()
    this.ws = null
  }

  private dispatch(raw: string) {
    let msg: WSMessage
    try {
      msg = JSON.parse(raw) as WSMessage
    } catch {
      return
    }
    if (msg.channelId) {
      this.channelHandlers.get(msg.channelId)?.forEach((h) => h(msg))
    }
    this.typeHandlers.get(msg.type)?.forEach((h) => h(msg))
  }

  send(type: string, channelId: string, payload?: Record<string, unknown>) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ type, channelId, payload }))
    }
  }

  /** 订阅指定 channel 的所有消息，返回取消函数。 */
  onChannel(channelId: string, handler: Handler): () => void {
    let set = this.channelHandlers.get(channelId)
    if (!set) {
      set = new Set()
      this.channelHandlers.set(channelId, set)
    }
    set.add(handler)
    return () => set!.delete(handler)
  }

  onType(type: string, handler: Handler): () => void {
    let set = this.typeHandlers.get(type)
    if (!set) {
      set = new Set()
      this.typeHandlers.set(type, set)
    }
    set.add(handler)
    return () => set!.delete(handler)
  }
}

export const ws = new WSClient()

let seq = 0
export function newChannelId(): string {
  seq += 1
  return `ch_${Date.now().toString(36)}_${seq}`
}
