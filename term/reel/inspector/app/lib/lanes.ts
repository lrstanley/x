import type { CastEvent, Summary } from './types'
import type { Wait } from './waits'
import { buildWaits } from './waits'

export type Severity = 'error' | 'warn' | undefined

export interface LaneItem {
  i: number
  // weight scales the marker: bytes for output, sequence count for tags.
  weight: number
  severity?: Severity
}

interface LaneBase {
  id: string
  label: string
  group: LaneGroup
  color: string

}

export interface EventLane extends LaneBase {
  kind: 'events'
  items: LaneItem[]
  maxWeight: number
}

export interface WaitLane extends LaneBase {
  kind: 'waits'
  waits: Wait[]
}

export type Lane = EventLane | WaitLane

export function laneItems(lane: Lane): LaneItem[] {
  return lane.kind === 'events' ? lane.items : lane.waits.map(w => ({ i: w.from, weight: 1, severity: w.severity }))
}

export function laneCount(lane: Lane): number {
  return lane.kind === 'events' ? lane.items.length : lane.waits.length
}

// Annotations are grouped by the tool that wrote them: steep's test harness,
// reel's recorder, and any other.
export type LaneGroup = 'Events' | 'steep' | 'reel' | 'Annotations' | 'Sequences'

export const laneGroups: LaneGroup[] = ['Events', 'steep', 'reel', 'Annotations', 'Sequences']

// Lane colors, tuned to read on the near-black timeline. Hue carries the
// kind of thing; red and amber are kept for failures and warnings.
export const colors = {
  mark: '#e5e5e5',
  input: '#5b8cff',
  output: '#8a8f98',
  terminal: '#2dd4bf',
  error: '#f2555a',
  warn: '#f5a524',
  annotation: '#a3a3a3',
  steep: '#c084fc',
  reel: '#38bdf8',
  query: '#38bdf8',
  sgr: '#4ade80',
  cursor: '#22d3ee',
  erase: '#fb923c',
  mode: '#facc15',
  osc: '#f472b6',
  dcs: '#e879f9',
  apc: '#e879f9',
  esc: '#94a3b8',
  control: '#fda4af',
  csi: '#93c5fd',
  other: '#737373',
} as const

export const typeColor: Record<string, string> = {
  output: colors.output,
  input: colors.input,
  mark: colors.mark,
  annotation: colors.annotation,
  resize: colors.terminal,
  focus: colors.terminal,
  scroll: colors.terminal,
  scrollback: colors.terminal,
  theme: colors.terminal,
  exit: colors.terminal,
}

export const groupColor: Record<string, string> = {
  text: colors.output,
  control: colors.control,
  sgr: colors.sgr,
  cursor: colors.cursor,
  erase: colors.erase,
  mode: colors.mode,
  query: colors.query,
  csi: colors.csi,
  esc: colors.esc,
  osc: colors.osc,
  dcs: colors.dcs,
  apc: colors.apc,
  other: colors.other,
}

const failedOutcomes = new Set(['fail', 'failed', 'failure', 'error', 'mismatch'])
const warnOutcomes = new Set(['timeout', 'canceled', 'cancelled', 'skipped'])

// severity flags annotations that record a failure or something close to it,
// and steep's "failure:n" marks.
export function severity(ev: CastEvent): Severity {
  if (ev.type === 'exit')
    return ev.label === 'status 0' ? undefined : 'warn'
  if (ev.type === 'mark')
    return ev.category === 'failure' ? 'error' : undefined
  if (ev.type !== 'annotation')
    return undefined
  const d = ev.data ?? {}
  if (ev.kind?.endsWith('.failure') || d.failed === true || d.pass === false)
    return 'error'
  const outcome = typeof d.outcome === 'string' ? d.outcome.toLowerCase() : ''
  if (failedOutcomes.has(outcome))
    return 'error'
  if (warnOutcomes.has(outcome) || d.truncated === true)
    return 'warn'
  return undefined
}

// tagColor colors a sequence lane by the token group its tag came from.
function tagColor(tag: string): string {
  const head = tag.split(' ')[0]!.toLowerCase()
  if (head in groupColor)
    return groupColor[head]!
  if (tag === 'BEL' || tag.startsWith('C0'))
    return colors.control
  return colors.csi
}

// annotationTool is the tool an annotation's kind names first, when it is
// one the inspector knows: steep or reel.
export function annotationTool(kind = ''): 'steep' | 'reel' | undefined {
  const tool = kind.slice(0, kind.indexOf('.'))
  return tool === 'steep' || tool === 'reel' ? tool : undefined
}

export function annotationColor(kind = ''): string {
  return colors[annotationTool(kind) ?? 'annotation']
}

// eventColor is the color an event is drawn in: its type's, or for an
// annotation, its tool's.
export function eventColor(ev: CastEvent): string {
  return ev.type === 'annotation' ? annotationColor(ev.kind) : typeColor[ev.type] ?? colors.other
}

export function waitColor(wait: Wait): string {
  return wait.severity === 'error' ? colors.error : wait.severity === 'warn' ? colors.warn : colors.steep
}

export function buildLanes(s: Summary): Lane[] {
  const lanes = new Map<string, EventLane>()
  const lane = (id: string, label: string, group: LaneGroup, color: string) => {
    let l = lanes.get(id)
    if (!l) {
      l = { kind: 'events', id, label, group, color, items: [], maxWeight: 1 }
      lanes.set(id, l)
    }
    return l
  }
  const push = (l: EventLane, item: LaneItem) => {
    l.items.push(item)
    l.maxWeight = Math.max(l.maxWeight, item.weight)
  }

  // Create the fixed lanes first so they keep their order.
  lane('mark', 'Marks', 'Events', colors.mark)
  lane('input', 'Input', 'Events', colors.input)
  lane('output', 'Output', 'Events', colors.output)
  lane('terminal', 'Terminal', 'Events', colors.terminal)

  for (const ev of s.events) {
    const sev = severity(ev)
    switch (ev.type) {
      case 'mark':
        push(lane('mark', 'Marks', 'Events', colors.mark), { i: ev.i, weight: 1, severity: sev })
        break
      case 'input':
        push(lane('input', 'Input', 'Events', colors.input), { i: ev.i, weight: ev.size ?? 1 })
        break
      case 'output':
        push(lane('output', 'Output', 'Events', colors.output), { i: ev.i, weight: ev.size ?? 1 })
        for (const [tag, n] of Object.entries(ev.tags ?? {}))
          push(lane(`seq:${tag}`, tag, 'Sequences', tagColor(tag)), { i: ev.i, weight: n })
        break
      case 'annotation': {
        const kind = ev.kind ?? 'unknown'
        const tool = annotationTool(kind)
        const label = tool ? kind.slice(tool.length + 1) : kind
        push(lane(`ann:${kind}`, label, tool ?? 'Annotations', annotationColor(kind)), { i: ev.i, weight: 1, severity: sev })
        break
      }
      default:
        push(lane('terminal', 'Terminal', 'Events', colors.terminal), { i: ev.i, weight: 1, severity: sev })
    }
  }

  const waits = buildWaits(s.events)
  const all: Lane[] = [...lanes.values()].filter(l => l.items.length > 0)
  if (waits.length)
    all.push({ kind: 'waits', id: 'waits', label: 'waits', group: 'steep', color: colors.steep, waits })
  const byTotal = (a: Lane, b: Lane) => laneCount(b) - laneCount(a) || a.label.localeCompare(b.label)
  return [
    ...all.filter(l => l.group === 'Events'),
    ...(['steep', 'reel', 'Annotations', 'Sequences'] as const).flatMap(g => all.filter(l => l.group === g).sort((a, b) => Number(b.kind === 'waits') - Number(a.kind === 'waits') || byTotal(a, b))),
  ]
}
