import type { Source } from './types'

// formatTime formats ms as seconds with millisecond precision, switching to
// minutes past a minute: "0.250s", "12.004s", "1:02.345".
export function formatTime(ms: number): string {
  if (!Number.isFinite(ms))
    return '–'
  if (ms < 60_000)
    return `${(ms / 1000).toFixed(3)}s`
  const m = Math.floor(ms / 60_000)
  const s = ((ms % 60_000) / 1000).toFixed(3).padStart(6, '0')
  return `${m}:${s}`
}

// formatSource formats a source location as "file:line".
export function formatSource(src: Source): string {
  return `${src.file}:${src.line}`
}

// shortFunc drops a function name's import path: "app.TestLogin.func1".
export function shortFunc(fn: string): string {
  return fn.slice(fn.lastIndexOf('/') + 1)
}

export function formatBytes(n: number): string {
  if (n < 1024)
    return `${n} B`
  if (n < 1024 * 1024)
    return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

// Text attributes, in the order of vt.Attr's bits.
const attrNames = ['bold', 'faint', 'italic', 'blink', 'rapid blink', 'reverse', 'conceal', 'strikethrough']

export function attrList(attrs = 0): string[] {
  return attrNames.filter((_, i) => attrs & (1 << i))
}

export const underlineNames = ['none', 'single', 'double', 'curly', 'dotted', 'dashed']
export const cursorShapes = ['block', 'underline', 'bar', 'hollow block']
