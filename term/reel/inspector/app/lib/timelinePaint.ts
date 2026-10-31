import type { LaneGroup } from './lanes'
import type { Axis, Marker, SceneRow, Viewport } from './timelineGeometry'
import type { CastEvent } from './types'
import { formatTime } from './format'
import { colors, laneCount, waitColor } from './lanes'
import { LABEL_W, PAD, RULER_H, niceStep, unitAt as toUnit, unitOf, xAt } from './timelineGeometry'

interface PaintModel {
  width: number
  height: number
  view: Viewport
  scene: SceneRow[]
  marks: CastEvent[]
  failures: CastEvent[]
  laneFilter: string | null
  collapsed: Set<string>
}

// Groups of a tool's annotations carry its color.
const groupAccent: Partial<Record<LaneGroup, string>> = { steep: colors.steep, reel: colors.reel }

function tint(hex: string, alpha: number) {
  const n = Number.parseInt(hex.slice(1), 16)
  return `rgba(${n >> 16},${(n >> 8) & 255},${n & 255},${alpha})`
}

const theme = {
  bg: '#0a0a0a',
  labelBg: '#0a0a0a',
  line: 'rgba(255,255,255,0.06)',
  lineStrong: 'rgba(255,255,255,0.12)',
  text: '#a3a3a3',
  dim: '#737373',
  fg: '#f5f5f5',
  primary: '#3b6ef6',
}

// sized fits c to the view at the device's pixel ratio, and returns its
// context scaled to CSS pixels.
function sized(c: HTMLCanvasElement, w: number, h: number) {
  const dpr = window.devicePixelRatio || 1
  if (c.width !== Math.round(w * dpr) || c.height !== Math.round(h * dpr)) {
    c.width = Math.round(w * dpr)
    c.height = Math.round(h * dpr)
    c.style.width = `${w}px`
    c.style.height = `${h}px`
  }
  const ctx = c.getContext('2d')!
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
  return ctx
}

// drawTimelineHead draws the playhead, with its handle on the ruler, and the line
// under the pointer.
export function drawTimelineHead(c: HTMLCanvasElement, w: number, h: number, cx: number, hoverX?: number) {
  const ctx = sized(c, w, h)
  ctx.clearRect(0, 0, w, h)
  ctx.save()
  ctx.beginPath()
  ctx.rect(LABEL_W, 0, w - LABEL_W, h)
  ctx.clip()
  if (hoverX !== undefined) {
    const x = Math.round(hoverX) + 0.5
    ctx.fillStyle = 'rgba(255,255,255,0.25)'
    ctx.fillRect(x, RULER_H, 1, h)
  }
  ctx.fillStyle = theme.primary
  ctx.fillRect(cx - 0.5, RULER_H, 1.5, h)
  ctx.beginPath()
  ctx.moveTo(cx - 5, 0)
  ctx.lineTo(cx + 5.5, 0)
  ctx.lineTo(cx + 5.5, 6)
  ctx.lineTo(cx + 0.25, 11)
  ctx.lineTo(cx - 5, 6)
  ctx.fill()
  ctx.fillRect(cx - 0.5, 11, 1.5, RULER_H - 11)
  ctx.restore()
}

function drawWait(ctx: CanvasRenderingContext2D, marker: Extract<Marker, { kind: 'wait' }>) {
  const { wait: wt, rect: { x: x0, y, width, height: bh } } = marker
  const color = waitColor(wt)
  const fill = ctx.createLinearGradient(x0, 0, x0 + width, 0)
  fill.addColorStop(0, tint(color, wt.start === undefined ? 0 : 0.28))
  fill.addColorStop(Math.min(0.5, 24 / width), tint(color, 0.28))
  fill.addColorStop(Math.max(0.5, 1 - 24 / width), tint(color, 0.28))
  fill.addColorStop(1, tint(color, wt.end === undefined ? 0 : 0.28))
  ctx.fillStyle = fill
  ctx.beginPath()
  ctx.roundRect(x0, y, width, bh, 3)
  ctx.fill()
  ctx.fillStyle = color
  if (wt.start !== undefined)
    ctx.fillRect(x0, y, 2, bh)
  if (wt.end !== undefined)
    ctx.fillRect(x0 + width - 2, y, 2, bh)
  if (width > 36 && bh >= 10) {
    ctx.save()
    ctx.beginPath()
    ctx.rect(Math.max(x0, LABEL_W) + 5, y, width - 10, bh)
    ctx.clip()
    ctx.font = '400 10.5px "JetBrains Mono Variable", ui-monospace, monospace'
    ctx.fillStyle = theme.fg
    ctx.fillText(wt.what, Math.max(x0, LABEL_W) + 6, y + bh / 2)
    ctx.restore()
  }
}

function tickLabel(v: number, axis: Axis) {
  if (axis === 'index')
    return String(Math.round(v))
  if (v === 0)
    return '0'
  return v < 1000 ? `${Math.round(v)}ms` : formatTime(v).replace(/\.?0+s$/, 's')
}

export function drawTimeline(c: HTMLCanvasElement, model: PaintModel) {
  const { width: w, height: h, view, scene, marks, failures, laneFilter, collapsed } = model
  const unitAt = (x: number) => toUnit(x, view)
  const xOf = (event: CastEvent) => xAt(unitOf(event, view.axis), view)
  const ctx = sized(c, w, h)
  ctx.fillStyle = theme.bg
  ctx.fillRect(0, 0, w, h)
  ctx.font = '500 11px "Inter Variable", system-ui, sans-serif'
  ctx.textBaseline = 'middle'

  const u0 = unitAt(LABEL_W)
  const u1 = unitAt(w)

  const step = niceStep(90 / view.zoom)
  ctx.save()
  ctx.beginPath()
  ctx.rect(LABEL_W, 0, w - LABEL_W, h)
  ctx.clip()
  for (let v = Math.max(0, Math.floor(u0 / step) * step); v <= u1; v += step) {
    const x = Math.round(LABEL_W + PAD + v * view.zoom - view.scrollX) + 0.5
    ctx.fillStyle = theme.line
    ctx.fillRect(x, RULER_H, 1, h - RULER_H)
  }
  for (const m of marks) {
    const x = Math.round(xOf(m)) + 0.5
    if (x >= LABEL_W && x <= w) {
      ctx.fillStyle = 'rgba(229,229,229,0.10)'
      ctx.fillRect(x, RULER_H, 1, h)
    }
  }
  for (const f of failures) {
    const x = Math.round(xOf(f)) + 0.5
    if (x >= LABEL_W && x <= w) {
      ctx.fillStyle = 'rgba(242,85,90,0.28)'
      ctx.fillRect(x, RULER_H, 1, h)
    }
  }

  scene.forEach(({ row, top, height: rh, markers }) => {
    if (top + rh < RULER_H || top > h)
      return
    if (row.kind === 'group') {
      const accent = groupAccent[row.group]
      ctx.fillStyle = accent ? tint(accent, 0.06) : 'rgba(255,255,255,0.025)'
      ctx.fillRect(LABEL_W, top, w - LABEL_W, rh)
      return
    }
    const lane = row.lane
    const accent = groupAccent[lane.group]
    if (accent) {
      ctx.fillStyle = tint(accent, 0.025)
      ctx.fillRect(LABEL_W, top, w - LABEL_W, rh)
    }
    let lastX = -Infinity
    for (const marker of markers) {
      if (marker.kind === 'wait') {
        drawWait(ctx, marker)
        continue
      }
      const { item: it, event: ev, rect: r, fraction: frac } = marker
      const x = r.x + r.width / 2
      const mid = r.y + r.height / 2
      ctx.fillStyle = it.severity === 'error' ? colors.error : it.severity === 'warn' ? colors.warn : lane.color
      if (lane.id === 'mark') {
        ctx.beginPath()
        ctx.moveTo(x, mid - 5)
        ctx.lineTo(x + 4, mid)
        ctx.lineTo(x, mid + 5)
        ctx.lineTo(x - 4, mid)
        ctx.fill()
        if (x - lastX > 60) {
          ctx.fillStyle = theme.text
          ctx.fillText(ev.label ?? '', x + 7, mid, 120)
          lastX = x
        }
      }
      else if (it.severity) {
        ctx.beginPath()
        ctx.arc(x, mid, 3.5, 0, Math.PI * 2)
        ctx.fill()
      }
      else {
        ctx.globalAlpha = 0.55 + 0.45 * frac
        ctx.fillRect(r.x, r.y, r.width, r.height)
        ctx.globalAlpha = 1
      }
    }
    ctx.fillStyle = theme.line
    ctx.fillRect(LABEL_W, top + rh - 0.5, w - LABEL_W, 0.5)
  })

  ctx.restore()

  // Ruler, drawn last so lanes scroll under it.
  ctx.fillStyle = theme.bg
  ctx.fillRect(0, 0, w, RULER_H)
  ctx.fillStyle = theme.lineStrong
  ctx.fillRect(0, RULER_H - 0.5, w, 0.5)
  ctx.save()
  ctx.beginPath()
  ctx.rect(LABEL_W, 0, w - LABEL_W, RULER_H)
  ctx.clip()
  ctx.font = '400 10.5px "JetBrains Mono Variable", ui-monospace, monospace'
  for (let v = Math.max(0, Math.floor(u0 / step) * step); v <= u1; v += step) {
    const x = Math.round(LABEL_W + PAD + v * view.zoom - view.scrollX) + 0.5
    ctx.fillStyle = theme.lineStrong
    ctx.fillRect(x, RULER_H - 6, 1, 6)
    ctx.fillStyle = theme.dim
    ctx.fillText(tickLabel(v, view.axis), x + 4, RULER_H / 2 - 1)
  }
  ctx.restore()

  // Lane labels, pinned to the left.
  ctx.fillStyle = theme.labelBg
  ctx.fillRect(0, RULER_H, LABEL_W, h - RULER_H)
  ctx.save()
  ctx.beginPath()
  ctx.rect(0, RULER_H, LABEL_W, h - RULER_H)
  ctx.clip()
  scene.forEach(({ row, top, height: rh }) => {
    if (top + rh < RULER_H || top > h)
      return
    const mid = top + rh / 2
    if (row.kind === 'group') {
      const accent = groupAccent[row.group]
      ctx.fillStyle = accent ? tint(accent, 0.08) : 'rgba(255,255,255,0.025)'
      ctx.fillRect(0, top, LABEL_W, rh)
      if (accent) {
        ctx.fillStyle = accent
        ctx.fillRect(0, top, 2, rh)
      }
      ctx.fillStyle = theme.dim
      ctx.font = '400 10px system-ui, sans-serif'
      ctx.fillText(collapsed.has(row.group) ? '▸' : '▾', 10, mid)
      ctx.font = '600 11px "Inter Variable", system-ui, sans-serif'
      ctx.fillStyle = accent ?? theme.text
      ctx.fillText(row.group, 22, mid)
      ctx.fillStyle = theme.dim
      ctx.font = '400 10.5px "JetBrains Mono Variable", ui-monospace, monospace'
      ctx.textAlign = 'right'
      ctx.fillText(String(row.count), LABEL_W - 12, mid)
      ctx.textAlign = 'left'
      return
    }
    const lane = row.lane
    if (lane.id === laneFilter) {
      ctx.fillStyle = tint(colors.input, 0.18)
      ctx.fillRect(0, top, LABEL_W, rh)
    }
    const accent = groupAccent[lane.group]
    if (accent) {
      ctx.fillStyle = accent
      ctx.fillRect(0, top, 2, rh)
    }
    ctx.fillStyle = lane.color
    ctx.fillRect(22, mid - 3, 6, 6)
    ctx.font = lane.group === 'Events' ? '500 12px "Inter Variable", system-ui, sans-serif' : '400 11.5px "JetBrains Mono Variable", ui-monospace, monospace'
    ctx.fillStyle = theme.fg
    ctx.fillText(lane.label, 36, mid, LABEL_W - 36 - 44)
    ctx.font = '400 10.5px "JetBrains Mono Variable", ui-monospace, monospace'
    ctx.fillStyle = theme.dim
    ctx.textAlign = 'right'
    ctx.fillText(String(laneCount(lane)), LABEL_W - 12, mid)
    ctx.textAlign = 'left'
  })
  ctx.restore()
  ctx.fillStyle = theme.lineStrong
  ctx.fillRect(LABEL_W - 0.5, 0, 0.5, h)
}

