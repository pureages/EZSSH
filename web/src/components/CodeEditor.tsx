import { useEffect, useRef } from 'react'
import { Compartment, EditorState, type Extension } from '@codemirror/state'
import {
  drawSelection,
  dropCursor,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
} from '@codemirror/view'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { search, searchKeymap } from '@codemirror/search'
import { HighlightStyle, indentUnit, StreamLanguage, syntaxHighlighting } from '@codemirror/language'
import { tags as t } from '@lezer/highlight'
import { html } from '@codemirror/lang-html'
import { css } from '@codemirror/lang-css'
import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { go } from '@codemirror/lang-go'
import { python } from '@codemirror/lang-python'
import { markdown } from '@codemirror/lang-markdown'
import { yaml } from '@codemirror/lang-yaml'
import { sql } from '@codemirror/lang-sql'
import { xml } from '@codemirror/lang-xml'
import { cpp } from '@codemirror/lang-cpp'
import { java } from '@codemirror/lang-java'
import { rust } from '@codemirror/lang-rust'
import { php } from '@codemirror/lang-php'
import { shell } from '@codemirror/legacy-modes/mode/shell'

interface CodeEditorProps {
  value: string
  onChange: (v: string) => void
  /** 文件名（用于自动识别语言高亮） */
  filename: string
  /** Ctrl+S 保存回调（可选） */
  onSave?: () => void
}

/** 主题：跟随应用 CSS 变量，深浅色主题自动适配 */
const ezTheme = EditorView.theme(
  {
    '&': {
      height: '100%',
      fontSize: '13px',
      backgroundColor: 'transparent',
      color: 'var(--text-0)',
    },
    '.cm-scroller': {
      fontFamily: 'Consolas, Menlo, monospace',
      lineHeight: '1.55',
      overflow: 'auto',
    },
    '.cm-content': {
      padding: '8px 0',
      caretColor: 'var(--primary-light)',
    },
    '.cm-line': {
      padding: '0 10px',
    },
    '&.cm-focused': { outline: 'none' },
    '.cm-gutters': {
      backgroundColor: 'transparent',
      color: 'var(--text-1)',
      borderRight: '1px solid rgba(var(--rgb-line),0.14)',
      userSelect: 'none',
      WebkitUserSelect: 'none',
    },
    '.cm-lineNumbers .cm-gutterElement': {
      padding: '0 8px 0 10px',
      minWidth: 'auto',
    },
    '.cm-activeLine': { backgroundColor: 'rgba(var(--rgb-primary),0.07)' },
    '.cm-activeLineGutter': {
      backgroundColor: 'rgba(var(--rgb-primary),0.10)',
      color: 'var(--primary-light)',
    },
    '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': {
      backgroundColor: 'rgba(var(--rgb-primary),0.28)',
    },
    '.cm-cursor': { borderLeftColor: 'var(--primary-light)' },
    '.cm-matchingBracket': {
      backgroundColor: 'rgba(var(--rgb-primary),0.25)',
      outline: '1px solid rgba(var(--rgb-primary),0.45)',
    },
    '&.cm-focused .cm-matchingBracket': {
      backgroundColor: 'rgba(var(--rgb-primary),0.30)',
    },
    '.cm-searchMatch, &.cm-focused .cm-searchMatch': {
      backgroundColor: 'rgba(var(--yellow),0.30)',
    },
    '.cm-searchMatch-selected': {
      backgroundColor: 'rgba(var(--yellow),0.55)',
    },
  },
  { dark: true },
)

/** 语法高亮配色（映射到应用主题色） */
const ezHighlight = HighlightStyle.define([
  { tag: t.comment, color: 'var(--text-1)', fontStyle: 'italic' },
  { tag: t.keyword, color: 'var(--primary-light)' },
  { tag: t.string, color: 'var(--green)' },
  { tag: t.number, color: 'var(--yellow)' },
  { tag: t.bool, color: 'var(--primary-light)' },
  { tag: t.null, color: 'var(--primary-light)' },
  { tag: t.regexp, color: 'var(--yellow)' },
  { tag: t.function(t.variableName), color: 'var(--cyan)' },
  { tag: t.definition(t.variableName), color: 'var(--cyan)' },
  { tag: t.definition(t.typeName), color: 'var(--yellow)' },
  { tag: t.typeName, color: 'var(--yellow)' },
  { tag: t.propertyName, color: 'var(--cyan)' },
  { tag: t.attributeName, color: 'var(--yellow)' },
  { tag: t.tagName, color: 'var(--primary-light)' },
  { tag: t.operator, color: 'var(--text-0)' },
  { tag: t.punctuation, color: 'var(--text-1)' },
  { tag: t.bracket, color: 'var(--text-1)' },
  { tag: t.meta, color: 'var(--text-1)' },
  { tag: t.className, color: 'var(--yellow)' },
  { tag: t.special(t.string), color: 'var(--yellow)' },
  { tag: t.heading, color: 'var(--primary-light)', fontWeight: 'bold' },
  { tag: t.link, color: 'var(--cyan)', textDecoration: 'underline' },
])

/** 语言识别：按扩展名/文件名映射 CodeMirror 语言包 */
function detectLanguage(filename: string): Extension | null {
  const lower = filename.toLowerCase()
  const dot = lower.lastIndexOf('.')
  const ext = dot >= 0 ? lower.slice(dot + 1) : ''
  const map: Record<string, () => Extension | null> = {
    html: () => html(), htm: () => html(), vue: () => html(), svelte: () => html(),
    css: () => css(), scss: () => css(), less: () => css(),
    js: () => javascript(), mjs: () => javascript(), cjs: () => javascript(),
    jsx: () => javascript({ jsx: true }),
    ts: () => javascript({ typescript: true }),
    tsx: () => javascript({ jsx: true, typescript: true }),
    json: () => json(),
    go: () => go(),
    py: () => python(), pyi: () => python(),
    md: () => markdown(), markdown: () => markdown(),
    yml: () => yaml(), yaml: () => yaml(),
    sql: () => sql(),
    xml: () => xml(), svg: () => xml(),
    c: () => cpp(), h: () => cpp(), cpp: () => cpp(), cc: () => cpp(), cxx: () => cpp(), hpp: () => cpp(), hh: () => cpp(),
    java: () => java(),
    rs: () => rust(),
    php: () => php(),
    sh: () => StreamLanguage.define(shell), bash: () => StreamLanguage.define(shell),
    conf: () => StreamLanguage.define(shell),
    ini: () => StreamLanguage.define(shell),
    log: () => null,
    txt: () => null,
  }
  if (lower === 'dockerfile') return StreamLanguage.define(shell)
  if (lower === 'makefile') return StreamLanguage.define(shell)
  if (lower === '.env' || lower === '.gitignore') return StreamLanguage.define(shell)
  const fn = map[ext]
  return fn ? fn() : null
}
/**
 * 代码/文本编辑器：行号、语法高亮、Tab 缩进（含多行）、Ctrl+S 保存、Ctrl+F 查找。
 * 基于 CodeMirror 6，主题跟随应用 CSS 变量。
 */
export function CodeEditor({ value, onChange, filename, onSave }: CodeEditorProps) {
  const hostRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)
  const onChangeRef = useRef(onChange)
  const onSaveRef = useRef(onSave)
  onChangeRef.current = onChange
  onSaveRef.current = onSave
  const langCompartment = useRef(new Compartment())

  // 挂载：创建编辑器实例（一次性）
  useEffect(() => {
    if (!hostRef.current) return
    const view = new EditorView({
      state: EditorState.create({
        doc: value,
        extensions: [
          lineNumbers(),
          highlightActiveLine(),
          highlightActiveLineGutter(),
          history(),
          drawSelection(),
          dropCursor(),
          EditorState.tabSize.of(4),
          indentUnit.of('    '),
          keymap.of([
            ...defaultKeymap,
            ...searchKeymap,
            ...historyKeymap,
            indentWithTab,
            {
              key: 'Mod-s',
              run: () => {
                onSaveRef.current?.()
                return true
              },
            },
          ]),
          search({ top: true }),
          ezTheme,
          syntaxHighlighting(ezHighlight),
          langCompartment.current.of(detectLanguage(filename) ?? []),
          EditorView.contentAttributes.of({
            spellcheck: 'false',
            autocorrect: 'off',
            autocapitalize: 'off',
          }),
          EditorView.updateListener.of((u) => {
            if (u.docChanged) onChangeRef.current(u.state.doc.toString())
          }),
        ],
      }),
      parent: hostRef.current,
    })
    viewRef.current = view
    return () => {
      view.destroy()
      viewRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 外部值同步（切换文件/保存后重置时替换全文）
  useEffect(() => {
    const view = viewRef.current
    if (!view) return
    const cur = view.state.doc.toString()
    if (cur !== value) {
      view.dispatch({ changes: { from: 0, to: cur.length, insert: value } })
    }
  }, [value])

  // 文件名变化 → 重新识别语言高亮
  useEffect(() => {
    const view = viewRef.current
    if (!view) return
    view.dispatch({
      effects: langCompartment.current.reconfigure(detectLanguage(filename) ?? []),
    })
  }, [filename])

  return <div ref={hostRef} className="code-editor" style={{ height: '100%', minHeight: 0, overflow: 'hidden' }} />
}
