import type { Lane, LaneGroup, LaneItem } from './lanes'
import type { CastEvent } from './types'
import type { Wait } from './waits'

export const LABEL_W = 184
export const RULER_H = 26
export const LANE_H = 22
export const GROUP_H = 22
export const PAD = 16
export const PAD_END = 72
export type Axis = 'time' | 'index'
export type Row = { kind: 'group', group: LaneGroup, count: number } | { kind: 'lane', lane: Lane }
export interface Viewport { axis: Axis, zoom: number, scrollX: number }
export interface Rect { x: number, y: number, width: number, height: number }
interface MarkerBase { rect: Rect, lane: Lane, item: LaneItem }
export type Marker =
  | (MarkerBase & { kind: 'event', event: CastEvent, fraction: number })
  | (MarkerBase & { kind: 'wait', wait: Wait })
export interface SceneRow { row: Row, top: number, height: number, markers: Marker[] }

export function unitOf(e: CastEvent, axis: Axis): number {
  return axis === 'time' ? e.at : e.i + 0.5
}

export function xAt(unit: number, view: Viewport): number {
  return LABEL_W + PAD + unit * view.zoom - view.scrollX
}

export function unitAt(x: number, view: Viewport): number {
  return (x + view.scrollX - LABEL_W - PAD) / view.zoom
}

export function playheadUnit(events: CastEvent[], index: number, time: number, axis: Axis): number {
  if (axis === 'time')
    return time
  const a = events[index]
  const b = events[index + 1]
  return !a ? 0 : !b || b.at <= a.at ? index + 0.5 : index + 0.5 + Math.min(1, Math.max(0, (time - a.at) / (b.at - a.at)))
}

export function seekAt(unit: number, events: CastEvent[]): { index: number, time?: number } {
  const index = Math.min(events.length - 1, Math.floor(unit - 0.5))
  const a = events[index]
  const b = events[index + 1]
  return !a ? { index: -1 } : { index, time: b ? a.at + (b.at - a.at) * Math.min(1, unit - 0.5 - index) : a.at }
}

export function rowHeight(row: Row): number {
  if (row.kind === 'group')
    return GROUP_H
  if (row.lane.kind === 'events')
    return LANE_H
  return Math.max(LANE_H, 6 + Math.max(1, ...row.lane.waits.map(w => w.depth + 1)) * 6)
}

export function niceStep(minUnits: number): number {
  const pow = 10 ** Math.floor(Math.log10(minUnits))
  for (const m of [1, 2, 5, 10]) {
    if (pow * m >= minUnits)
      return pow * m
  }
  return pow * 10
}

function firstVisible(items: LaneItem[], events: CastEvent[], unit: number, axis: Axis): number {
  let lo = 0
  let hi = items.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (unitOf(events[items[mid]!.i]!, axis) < unit)
      lo = mid + 1
    else
      hi = mid
  }
  return lo
}

// sceneRows supplies the same rectangles to painting and pointer picking.
export function sceneRows(rows: Row[], events: CastEvent[], view: Viewport, scrollY: number, width: number, height: number): SceneRow[] {
  let y = RULER_H - scrollY
  return rows.map((row) => {
    const top = y
    const rh = rowHeight(row)
    y += rh
    const markers: Marker[] = []
    if (row.kind === 'group' || top + rh < RULER_H || top > height)
      return { row, top, height: rh, markers }
    const lane = row.lane
    if (lane.kind === 'waits') {
      const cols = Math.max(1, ...lane.waits.map(w => w.depth + 1))
      const bh = Math.max(4, (rh - 6) / cols - (cols > 1 ? 2 : 0))
      for (const wait of lane.waits) {
        const x0 = xAt(view.axis === 'time' ? wait.fromAt : wait.from + 0.5, view)
        const x1 = xAt(view.axis === 'time' ? wait.toAt : wait.to + 0.5, view)
        if (x1 < LABEL_W - 4 || x0 > width + 4)
          continue
        markers.push({
          kind: 'wait', lane, item: { i: wait.from, weight: 1, severity: wait.severity }, wait,
          rect: { x: x0, y: top + 3 + wait.depth * (bh + 2), width: Math.max(2, x1 - x0), height: bh },
        })
      }
    }
    else {
      const start = firstVisible(lane.items, events, unitAt(LABEL_W - 8, view), view.axis)
      let failX = Number.NaN
      for (let k = start; k < lane.items.length; k++) {
        const item = lane.items[k]!
        const event = events[item.i]!
        const x = xAt(unitOf(event, view.axis), view)
        if (x > width + 8)
          break
        if (lane.id === 'mark') {
          if (item.severity)
            failX = x
          else if (Math.abs(x - failX) < 1)
            continue
        }
        const fraction = lane.maxWeight > 1 ? Math.log1p(item.weight) / Math.log1p(lane.maxWeight) : 1
        const bh = lane.id === 'mark' ? 10 : item.severity ? 7 : Math.max(4, fraction * (rh - 8))
        const bw = lane.id === 'mark' ? 8 : item.severity ? 7 : 1.5
        markers.push({ kind: 'event', lane, item, event, fraction, rect: { x: x - bw / 2, y: top + (rh - bh) / 2, width: bw, height: bh } })
      }
    }
    return { row, top, height: rh, markers }
  })
}

export function hitMarker(scene: SceneRow[], x: number, y: number): Marker | undefined {
  if (x < LABEL_W || y < RULER_H)
    return
  let best: Marker | undefined
  let distance = Infinity
  for (const row of scene) {
    for (const marker of row.markers) {
      const r = marker.rect
      const padX = marker.kind === 'wait' ? 3 : 6
      if (x < r.x - padX || x > r.x + r.width + padX || y < r.y || y > r.y + r.height)
        continue
      const d = Math.abs(x - (r.x + r.width / 2))
      if (d <= distance) {
        best = marker
        distance = d
      }
    }
  }
  return best
}
