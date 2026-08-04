import { useEffect } from 'react'

/**
 * ESC 关闭子窗口（modal）的注册栈。
 * 各应用在弹窗显示时注册关闭回调，隐藏/卸载时注销；
 * DesktopPage 的全局 ESC 处理器只关闭"最上层"的那个（栈顶）。
 */
const closeStack: Array<() => void> = []

export function registerEscClose(fn: () => void): () => void {
  closeStack.push(fn)
  return () => {
    const i = closeStack.indexOf(fn)
    if (i >= 0) closeStack.splice(i, 1)
  }
}

/** 取栈顶（最近打开的）关闭回调，无则返回 null */
export function topEscClose(): (() => void) | null {
  return closeStack.length > 0 ? closeStack[closeStack.length - 1] : null
}

/** 弹窗在 enabled 时注册 ESC 关闭回调；卸载或 enabled 变 false 时自动注销 */
export function useEscClose(enabled: boolean, onClose: () => void) {
  useEffect(() => {
    if (!enabled) return
    return registerEscClose(onClose)
  }, [enabled, onClose])
}
