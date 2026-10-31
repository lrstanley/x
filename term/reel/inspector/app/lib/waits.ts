import type { Severity } from './lanes'
import type { CastEvent } from './types'

// Wait is one of a steep test's waits: its steep.wait.start and
// steep.wait.end annotations, paired by id. A cut recording can hold only
// one of them; the missing side runs to the recording's edge.
export interface Wait {
  id: number
  // start and end are the annotations' event indexes, when present.
  start?: number
  end?: number
  // from and to are the event indexes the wait covers, and fromAt and toAt
  // its time in ms, reaching the recording's first or last event for a
  // missing side.
  from: number
  to: number
  fromAt: number
  toAt: number
  what: string
  outcome?: string
  severity: Severity
  // depth is the wait's column among the waits it overlaps.
  depth: number
}

const startKind = 'steep.wait.start'
const endKind = 'steep.wait.end'

// waitLabel says what a wait waits for, such as "text [Tokyo]".
function waitLabel(d: Record<string, unknown> = {}): string {
  const what = String(d.wait ?? 'wait')
  for (const k of ['text', 'type', 'condition', 'duration_ms']) {
    if (d[k] !== undefined)
      return `${what} ${typeof d[k] === 'string' ? d[k] : JSON.stringify(d[k])}`
  }
  return what
}

function waitSeverity(outcome?: string): Severity {
  if (!outcome)
    return 'warn'
  return outcome === 'satisfied' ? undefined : outcome === 'canceled' ? 'warn' : 'error'
}

export function buildWaits(events: CastEvent[]): Wait[] {
  const last = events.length - 1
  if (last < 0)
    return []
  const open = new Map<number, Wait>()
  const waits: Wait[] = []
  for (const ev of events) {
    if (ev.kind !== startKind && ev.kind !== endKind)
      continue
    const d = ev.data ?? {}
    const id = Number(d.id ?? 0)
    if (ev.kind === startKind) {
      const w: Wait = {
        id,
        start: ev.i,
        from: ev.i,
        to: last,
        fromAt: ev.at,
        toAt: events[last]!.at,
        what: waitLabel(d),
        severity: 'warn',
        depth: 0,
      }
      open.set(id, w)
      waits.push(w)
      continue
    }
    const w = open.get(id)
    open.delete(id)
    const outcome = typeof d.outcome === 'string' ? d.outcome : undefined
    if (w) {
      w.end = ev.i
      w.to = ev.i
      w.toAt = ev.at
      w.outcome = outcome
      w.severity = waitSeverity(outcome)
      continue
    }
    // An end whose start was cut off: it began elapsed_ms before, or at
    // the recording's start, whichever is later.
    const elapsed = Number(d.elapsed_ms ?? Number.NaN)
    const fromAt = Number.isFinite(elapsed) ? Math.max(events[0]!.at, ev.at - elapsed) : events[0]!.at
    let from = 0
    while (from < ev.i && events[from]!.at < fromAt)
      from++
    waits.push({
      id,
      end: ev.i,
      from,
      to: ev.i,
      fromAt,
      toAt: ev.at,
      what: waitLabel(d),
      outcome,
      severity: waitSeverity(outcome),
      depth: 0,
    })
  }
  waits.sort((a, b) => a.from - b.from || a.to - b.to)
  // Give overlapping waits their own columns.
  const ends: number[] = []
  for (const w of waits) {
    let col = ends.findIndex(e => e < w.from)
    if (col < 0)
      col = ends.length
    ends[col] = w.to
    w.depth = col
  }
  return waits
}
