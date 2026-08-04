import { useEffect, useRef, useState } from 'react'
import { useT } from '../lib/i18n'

type EditableElement = HTMLInputElement | HTMLTextAreaElement

interface MenuState {
  x: number
  y: number
  elem: EditableElement
  hasSel: boolean
}

/** 将文本插入输入框/文本域光标处，并派发 input 事件同步 React 受控值 */
function insertAtCursor(elem: EditableElement, text: string) {
  const start = elem.selectionStart ?? elem.value.length
  const end = elem.selectionEnd ?? elem.value.length
  const v = elem.value
  elem.value = v.slice(0, start) + text + v.slice(end)
  const pos = start + text.length
  elem.setSelectionRange(pos, pos)
  elem.dispatchEvent(new Event('input', { bubbles: true }))
}

/** 复制降级：clipboard API 不可用时用临时 textarea + execCommand */
function clipboardWrite(text: string) {
  navigator.clipboard
    .writeText(text)
    .catch(() => {
      try {
        const ta = document.createElement('textarea')
        ta.value = text
        ta.style.position = 'fixed'
        ta.style.opacity = '0'
        document.body.appendChild(ta)
        ta.select()
        document.execCommand('copy')
        ta.remove()
      } catch {
        /* ignore */
      }
    })
}

function doAction(elem: EditableElement, action: 'cut' | 'copy' | 'paste' | 'selectAll') {
  if (action === 'selectAll') {
    elem.focus()
    elem.select()
    return
  }
  const start = elem.selectionStart ?? 0
  const end = elem.selectionEnd ?? elem.value.length
  const sel = elem.value.slice(start, end)
  if (action === 'copy') {
    if (sel) clipboardWrite(sel)
    return
  }
  if (action === 'cut') {
    if (sel) clipboardWrite(sel)
    elem.value = elem.value.slice(0, start) + elem.value.slice(end)
    elem.setSelectionRange(start, start)
    elem.dispatchEvent(new Event('input', { bubbles: true }))
    return
  }
  // paste
  navigator.clipboard
    .readText()
    .then((text) => {
      if (text && elem.isConnected) insertAtCursor(elem, text)
    })
    .catch(() => {
      /* 剪贴板不可用（非安全上下文等），静默 */
    })
}

/**
 * 全局右键菜单拦截：
 * - 任何地方右键都不弹出浏览器原生菜单（capture 阶段 e.preventDefault）
 * - 右键落在输入框/文本域（如文件管理器地址栏）时，弹出自定义小菜单：剪切/复制/粘贴/全选
 */
export function GlobalContextMenu() {
  const t = useT()
  const [menu, setMenu] = useState<MenuState | null>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const onContextMenu = (e: MouseEvent) => {
      e.preventDefault()
      const el =
        e.target instanceof Element
          ? e.target.closest<EditableElement>(
              'input:not([readonly]):not([disabled]), textarea:not([readonly]):not([disabled])',
            )
          : null
      if (el) {
        setMenu({
          x: e.clientX,
          y: e.clientY,
          elem: el,
          hasSel: (el.selectionStart ?? 0) !== (el.selectionEnd ?? 0),
        })
      } else {
        setMenu(null)
      }
    }
    // 菜单自身内部交互不关闭；其它点击/滚轮/按键/失焦关闭菜单
    const onOther = (e: Event) => {
      if (menuRef.current && e.target instanceof Node && menuRef.current.contains(e.target)) return
      setMenu(null)
    }
    const onScroll = () => setMenu(null)
    const onBlur = () => setMenu(null)

    window.addEventListener('contextmenu', onContextMenu, true)
    document.addEventListener('click', onOther, true)
    document.addEventListener('mousedown', onOther, true)
    document.addEventListener('keydown', onOther, true)
    document.addEventListener('scroll', onScroll, true)
    window.addEventListener('blur', onBlur)
    return () => {
      window.removeEventListener('contextmenu', onContextMenu, true)
      document.removeEventListener('click', onOther, true)
      document.removeEventListener('mousedown', onOther, true)
      document.removeEventListener('keydown', onOther, true)
      document.removeEventListener('scroll', onScroll, true)
      window.removeEventListener('blur', onBlur)
    }
  }, [])

  if (!menu) return null

  return (
    <div
      ref={menuRef}
      className="ctx-menu"
      style={{ left: menu.x, top: menu.y, minWidth: 150 }}
      onContextMenu={(e) => e.preventDefault()}
    >
      <div
        className={`ctx-menu-item${menu.hasSel ? '' : ' disabled'}`}
        onClick={() => {
          doAction(menu.elem, 'cut')
          setMenu(null)
        }}
      >
        {t('剪切')}
      </div>
      <div
        className={`ctx-menu-item${menu.hasSel ? '' : ' disabled'}`}
        onClick={() => {
          doAction(menu.elem, 'copy')
          setMenu(null)
        }}
      >
        {t('复制')}
      </div>
      <div
        className="ctx-menu-item"
        onClick={() => {
          doAction(menu.elem, 'paste')
          setMenu(null)
        }}
      >
        {t('粘贴')}
      </div>
      <div className="ctx-menu-sep" />
      <div
        className="ctx-menu-item"
        onClick={() => {
          doAction(menu.elem, 'selectAll')
          setMenu(null)
        }}
      >
        {t('全选')}
      </div>
    </div>
  )
}
