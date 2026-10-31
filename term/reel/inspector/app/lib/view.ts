// A page's view of a cast: what it shows and how, kept in its address so a
// reload, or another tab on another cast, keeps its own. Only settings that
// differ from the defaults are written.

export type Axis = 'time' | 'index'

export interface View {
  // q is the event list's filter text, types the event types it shows (null
  // for all), and fail whether it shows failures and warnings only.
  q: string
  types: string[] | null
  fail: boolean
  // lane is the timeline lane the event list shows only.
  lane: string | null
  // hide are the timeline's hidden lanes, and collapsed its collapsed groups.
  hide: string[]
  collapsed: string[]
  axis: Axis
  speed: number
  tab: string
  // display are the parts drawn around the grid that differ from what the
  // cast turns on by default.
  display: Record<string, boolean>
}

export function defaultView(): View {
  return { q: '', types: null, fail: false, lane: null, hide: [], collapsed: [], axis: 'time', speed: 1, tab: 'event', display: {} }
}

const list = (s: string | null) => (s ? s.split(',').filter(Boolean) : [])

export function readView(p: URLSearchParams): View {
  const v = defaultView()
  v.q = p.get('q') ?? ''
  v.types = p.has('types') ? list(p.get('types')) : null
  v.fail = p.get('fail') === '1'
  v.lane = p.get('lane')
  v.hide = list(p.get('hide'))
  v.collapsed = list(p.get('collapsed'))
  v.axis = p.get('axis') === 'index' ? 'index' : 'time'
  const speed = Number(p.get('speed'))
  v.speed = Number.isFinite(speed) && speed > 0 ? speed : 1
  v.tab = p.get('tab') || 'event'
  for (const part of list(p.get('display'))) {
    const [k, on] = part.split(':')
    if (k && (on === '0' || on === '1'))
      v.display[k] = on === '1'
  }
  return v
}

export function writeView(v: View, p: URLSearchParams) {
  const d = defaultView()
  const put = (k: string, value: string, unset: boolean) => (unset ? p.delete(k) : p.set(k, value))
  put('q', v.q, !v.q)
  put('types', (v.types ?? []).join(','), v.types === null)
  put('fail', '1', !v.fail)
  put('lane', v.lane ?? '', !v.lane)
  put('hide', v.hide.join(','), !v.hide.length)
  put('collapsed', v.collapsed.join(','), !v.collapsed.length)
  put('axis', v.axis, v.axis === d.axis)
  put('speed', String(v.speed), v.speed === d.speed)
  put('tab', v.tab, v.tab === d.tab)
  const display = Object.entries(v.display).map(([k, on]) => `${k}:${on ? 1 : 0}`)
  put('display', display.join(','), !display.length)
}
