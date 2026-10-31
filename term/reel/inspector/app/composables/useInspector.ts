import type { Lane } from '~/lib/lanes'
import type { CastEvent, Display, Frame, Screen, Summary } from '~/lib/types'
import type { RecentCast } from '~/lib/recentCasts'
import type { Rpc } from '~/lib/workerProtocol'
import type { View } from '~/lib/view'
import { createWorkerClient } from '~/lib/workerClient'
import { createFrameScheduler, InspectorSession, targetKey } from '~/lib/frameScheduler'
import { deleteCast, listRecent, readCast, storeCast } from '~/lib/recentCasts'
import { toast } from 'vue-sonner'
import { buildLanes } from '~/lib/lanes'
import { defaultView, readView, writeView } from '~/lib/view'

// One inspector per page: the state lives at module scope and every component
// shares it through useInspector().

type Status = 'booting' | 'idle' | 'loading' | 'ready' | 'error'

// Source is where the open cast came from, which the address names: a cast
// kept in this browser, by id, or one fetched from a URL.
type Source = { kind: 'cast', id: string } | { kind: 'url', url: string }

const status = ref<Status>('booting')
const fatal = ref('')
const wasmProgress = ref({ loaded: 0, total: 0 })
const fileName = ref('')
const fileSize = ref(0)
const summary = shallowRef<Summary | null>(null)
const lanes = shallowRef<Lane[]>([])
const cursor = ref(-1)
const frame = shallowRef<Frame | null>(null)
const frameScale = ref(1)
const generation = ref(0)
let session = new InspectorSession(0)
const playing = ref(false)
const speed = ref(1)
const hiddenLanes = ref(new Set<string>())
// laneFilter is the id of the lane the event list shows only, or null.
const laneFilter = ref<string | null>(null)
// playhead is the time in ms the timeline's playhead is at: the playback
// clock while playing, so it moves smoothly at the playback speed, where a
// pause left it, and the selected event's time after a seek.
const playhead = ref(0)
const axis = ref<'time' | 'index'>('time')
const highlightColor = ref<string | null>(null)
const source = shallowRef<Source | null>(null)
// The event list's filters, the timeline's collapsed groups, and the
// inspector's tab, which the address keeps with the rest of the view.
const listQuery = ref('')
const listTypes = ref<Set<string> | null>(null)
const failuresOnly = ref(false)
const collapsedGroups = ref(new Set<string>())
const panelTab = ref('event')
// displayOverrides are the parts drawn around the grid turned on or off
// against what the cast turns on by default; display is the result.
const displayOverrides = ref<Record<string, boolean>>({})
const display = computed<Display | null>(() => summary.value && { ...summary.value.display, ...displayOverrides.value })
const hoverCell = ref<{ x: number, y: number } | null>(null)
const recent = ref<RecentCast[]>([])

let client: ReturnType<typeof createWorkerClient> | undefined
const rpc: Rpc = (op, args) => {
  boot()
  return client!.rpc(op, args)
}

function boot() {
  if (client)
    return
  client = createWorkerClient(useRuntimeConfig().app.baseURL, {
    ready: () => {
      if (status.value === 'booting')
        status.value = 'idle'
    },
    progress: value => wasmProgress.value = value,
    fatal: (err) => {
      invalidate()
      status.value = 'error'
      fatal.value = err.message
    },
  })
  refreshRecent()
  window.addEventListener('popstate', () => restore())
  window.addEventListener('focus', refreshRecent)
}

let recentRead = 0
async function refreshRecent() {
  const read = ++recentRead
  const rows = await listRecent().catch(() => [])
  if (read === recentRead)
    recent.value = rows
}

async function remember(name: string, bytes: Uint8Array) {
  try {
    await storeCast(name, bytes)
    await refreshRecent()
  }
  catch (err) {
    toast.warning(`Couldn't keep ${name} in this browser`, { description: (err as Error).message })
  }
}

async function keepFile(file: File) {
  await remember(file.name, new Uint8Array(await file.arrayBuffer()))
}

async function forget(id: string) {
  await deleteCast(id).catch(() => {})
  await refreshRecent()
}

// Invalidate before any asynchronous read, including file and network reads.
function invalidate() {
  clearTimeout(urlTimer)
  stop()
  generation.value++
  session = new InspectorSession(generation.value)
  frame.value?.bitmap.close()
  frame.value = null
  summary.value = null
  lanes.value = []
  cursor.value = -1
  playhead.value = 0
  source.value = null
  hoverCell.value = null
  highlightColor.value = null
  return session
}

function beginOpen() {
  const next = invalidate()
  status.value = 'loading'
  void rpc('unload', [next.generation]).catch(() => {})
  return next
}

interface OpenOptions {
  // at is the event to show; the first by default.
  at?: number
  // view restores a view, such as an address's; a newly opened cast gets
  // the default filters, and keeps the axis, speed, and tab.
  view?: View
  // source is where the cast came from; by default, this browser's store.
  source?: Source
  // restoring is true when the address asked for the cast, so it is not
  // added to the history again.
  restoring?: boolean
}

async function openBytes(active: InspectorSession, name: string, bytes: Uint8Array, opts: OpenOptions = {}) {
  if (session !== active)
    return
  try {
    frameScale.value = Math.min(2, Math.max(1, Math.round(window.devicePixelRatio || 1)))
    const s = await rpc('load', [active.generation, bytes, frameScale.value])
    if (session !== active)
      return
    summary.value = s
    lanes.value = buildLanes(s)
    applyView(opts.view ?? { ...defaultView(), axis: axis.value, speed: speed.value, tab: panelTab.value })
    fileName.value = name
    fileSize.value = bytes.length
    frame.value = null
    status.value = 'ready'
    source.value = opts.source ?? { kind: 'cast', id: name }
    seek(opts.at ?? 0)
    writeUrl(!opts.restoring)
    remember(name, bytes)
  }
  catch (err) {
    if (session !== active)
      return
    status.value = 'idle'
    toast.error(`Couldn't open ${name}`, { description: (err as Error).message })
  }
}

async function openFile(file: File) {
  const active = beginOpen()
  try {
    await openBytes(active, file.name, new Uint8Array(await file.arrayBuffer()))
  }
  catch (err) {
    openFailed(active, `Couldn't read ${file.name}`, err)
  }
}

function openFailed(active: InspectorSession, message: string, err: unknown) {
  if (session !== active || fatal.value)
    return
  status.value = 'idle'
  toast.error(message, { description: (err as Error).message })
}

async function openStored(id: string, opts: OpenOptions = {}) {
  const active = beginOpen()
  try {
    const stored = await readCast(id)
    if (session !== active)
      return
    if (!stored) {
      toast.error(`${id} is no longer stored in this browser`)
      close(opts.restoring)
      return
    }
    await openBytes(active, stored.name, stored.bytes, { ...opts, source: { kind: 'cast', id: stored.name } })
  }
  catch (err) {
    openFailed(active, `Couldn't open ${id}`, err)
  }
}

async function openUrl(url: string, opts: OpenOptions = {}) {
  const active = beginOpen()
  try {
    const res = await fetch(url)
    if (!res.ok)
      throw new Error(`${res.status} ${res.statusText}`)
    const name = decodeURIComponent(new URL(url, location.href).pathname.split('/').pop() || 'cast')
    await openBytes(active, name, new Uint8Array(await res.arrayBuffer()), { ...opts, source: { kind: 'url', url } })
  }
  catch (err) {
    openFailed(active, `Couldn't fetch ${url}`, err)
  }
}

function close(restoring = false) {
  const next = invalidate()
  void rpc('unload', [next.generation]).catch(() => {})
  status.value = fatal.value ? 'error' : 'idle'
  writeUrl(!restoring)
}

// The view and the address. Every setting the view holds is written to the
// address as it changes, so a reload or Back returns to it, and each tab
// keeps its own.
function currentView(): View {
  return {
    q: listQuery.value,
    types: listTypes.value && [...listTypes.value],
    fail: failuresOnly.value,
    lane: laneFilter.value,
    hide: [...hiddenLanes.value],
    collapsed: [...collapsedGroups.value],
    axis: axis.value,
    speed: speed.value,
    tab: panelTab.value,
    display: { ...displayOverrides.value },
  }
}

function applyView(v: View) {
  listQuery.value = v.q
  listTypes.value = v.types && new Set(v.types)
  failuresOnly.value = v.fail
  laneFilter.value = v.lane
  hiddenLanes.value = new Set(v.hide)
  collapsedGroups.value = new Set(v.collapsed)
  axis.value = v.axis
  speed.value = v.speed
  panelTab.value = v.tab
  displayOverrides.value = { ...v.display }
}

// Overrides matching the cast's default are omitted from its address.
async function setDisplayPart(key: keyof Display, on: boolean) {
  const next = { ...displayOverrides.value }
  if (on === summary.value?.display[key])
    delete next[key]
  else
    next[key] = on
  displayOverrides.value = next
  renderFrames()
}

async function resetDisplay() {
  displayOverrides.value = {}
  renderFrames()
}

// setScale draws frames at scale from now on, such as to match the size
// the page shows them at, so no more pixels are drawn than are seen.
function setScale(scale: number) {
  if (!summary.value || scale === frameScale.value)
    return
  frameScale.value = scale
  renderFrames()
}

function urlFor(): string {
  const src = source.value
  if (!src || !summary.value)
    return location.pathname
  const p = new URLSearchParams()
  if (src.kind === 'url')
    p.set('url', src.url)
  else
    p.set('cast', src.id)
  writeView(currentView(), p)
  return `${location.pathname}?${p.toString().replace(/%2C/g, ',').replace(/%3A/g, ':').replace(/%2F/g, '/')}#e=${cursor.value}`
}

let urlTimer: ReturnType<typeof setTimeout> | undefined

// writeUrl writes the address now, as a new history entry when push is set,
// such as for another cast; scheduleUrl writes it shortly, in place, so
// scrubbing doesn't flood the history.
function writeUrl(push = false) {
  clearTimeout(urlTimer)
  const url = urlFor()
  if (url === location.pathname + location.search + location.hash)
    return
  if (push)
    history.pushState(null, '', url)
  else
    history.replaceState(null, '', url)
}

function scheduleUrl() {
  clearTimeout(urlTimer)
  urlTimer = setTimeout(() => writeUrl(), 150)
}

watch([cursor, listQuery, listTypes, failuresOnly, laneFilter, hiddenLanes, collapsedGroups, axis, speed, panelTab, displayOverrides], () => {
  if (summary.value)
    scheduleUrl()
})

// restore opens what the address names, with its view: on load, and when
// Back or Forward changes it.
async function restore() {
  const p = new URLSearchParams(location.search)
  const m = /^#e=(-?\d+)$/.exec(location.hash)
  const view = readView(p)
  const opts: OpenOptions = { at: m ? Number(m[1]) : undefined, view, restoring: true }
  const url = p.get('url')
  const id = p.get('cast')
  const same = source.value && (url ? source.value.kind === 'url' && source.value.url === url : source.value.kind === 'cast' && source.value.id === id)
  if (same && summary.value) {
    applyView(view)
    renderFrames()
    if (opts.at !== undefined)
      seek(opts.at)
    return
  }
  if (url)
    await openUrl(url, opts)
  else if (id)
    await openStored(id, opts)
  else
    close(true)
}

function frameTarget(index = cursor.value) {
  if (!summary.value || !display.value || status.value !== 'ready')
    return null
  return { generation: session.generation, index, scale: frameScale.value, display: { ...display.value } }
}

const scheduler = createFrameScheduler(rpc, () => frameTarget(), (next) => {
  frame.value?.bitmap.close()
  frame.value = next
}, err => toast.error('Replay failed', { description: err.message }))
const renderFrames = scheduler.render

function cellsAt(i: number): Promise<Screen | undefined> {
  const active = session
  const target = frameTarget(i)
  if (!target)
    return Promise.resolve(undefined)
  const key = targetKey(target)
  if (active.cells?.key !== key) {
    active.cells = { key, screen: rpc('cells', [active.generation, i, { scale: target.scale, display: target.display }]).then((response) => {
      const current = frameTarget(i)
      return session === active && current && targetKey(current) === key ? response.screen : undefined
    }) }
  }
  return active.cells.screen
}

// seek moves to event i. at is a time in ms at or after it to put the
// playhead at, such as where a drag on the timeline is; without it, the
// playhead snaps to the event. While playing, playback carries on from
// there.
function seek(i: number, at?: number) {
  const s = summary.value
  if (!s)
    return
  cursor.value = Math.max(-1, Math.min(s.events.length - 1, Math.round(i)))
  const eventAt = cursor.value < 0 ? 0 : s.events[cursor.value]!.at
  if (playing.value && !ticking)
    rebase(at ?? eventAt)
  if (!playing.value)
    playhead.value = at ?? eventAt
  renderFrames()
}

// indexAt returns the last event at or before t ms, or -1.
function indexAt(t: number): number {
  const ev = summary.value?.events ?? []
  let lo = 0
  let hi = ev.length - 1
  let ans = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (ev[mid]!.at <= t) {
      ans = mid
      lo = mid + 1
    }
    else {
      hi = mid - 1
    }
  }
  return ans
}

function step(n: number) {
  seek(cursor.value + n)
}

// jump moves to the next or previous event the predicate accepts.
function jump(dir: 1 | -1, pred: (e: CastEvent) => boolean) {
  const ev = summary.value?.events ?? []
  for (let i = cursor.value + dir; i >= 0 && i < ev.length; i += dir) {
    if (pred(ev[i]!)) {
      seek(i)
      return true
    }
  }
  return false
}

// Playback walks a clock through the events' times, showing the last event
// due at each animation frame. A seek from elsewhere, or a speed change,
// rebases the clock so playback carries on from there.
let raf = 0
let ticking = false
let clock = { at: 0, t0: 0 }

function rebase(at: number) {
  clock = { at, t0: performance.now() }
}

function play() {
  const s = summary.value
  if (!s || s.events.length === 0)
    return
  if (cursor.value >= s.events.length - 1)
    seek(-1)
  playing.value = true
  // Resume where a pause left the playhead, if it is still past the event
  // shown; any seek since put it on that event.
  const eventAt = cursor.value < 0 ? 0 : s.events[cursor.value]!.at
  const next = s.events[cursor.value + 1]
  rebase(playhead.value > eventAt && (!next || playhead.value < next.at) ? playhead.value : eventAt)
  const tick = () => {
    if (!playing.value)
      return
    const t = clock.at + (performance.now() - clock.t0) * speed.value
    playhead.value = Math.min(t, s.duration)
    const i = indexAt(t)
    if (i !== cursor.value) {
      ticking = true
      seek(i)
      ticking = false
    }
    if (i >= s.events.length - 1) {
      stop()
      return
    }
    raf = requestAnimationFrame(tick)
  }
  raf = requestAnimationFrame(tick)
}

watch(speed, (_, old) => {
  if (playing.value)
    clock = { at: clock.at + (performance.now() - clock.t0) * old, t0: performance.now() }
})

function stop() {
  playing.value = false
  cancelAnimationFrame(raf)
}

function togglePlay() {
  if (playing.value)
    stop()
  else
    play()
}

async function detail(i: number) {
  const active = session
  if (!active.details.has(i))
    active.details.set(i, rpc('detail', [active.generation, i]))
  const result = await active.details.get(i)!
  return session === active ? result : undefined
}

function toggleLane(id: string) {
  const s = new Set(hiddenLanes.value)
  if (s.has(id))
    s.delete(id)
  else
    s.add(id)
  hiddenLanes.value = s
}

const current = computed(() => (cursor.value >= 0 ? summary.value?.events[cursor.value] : undefined))

export function useInspector() {
  return {
    generation,
    status,
    fatal,
    wasmProgress,
    fileName,
    fileSize,
    summary,
    lanes,
    cursor,
    current,
    frame,
    frameScale,
    playing,
    speed,
    hiddenLanes,
    laneFilter,
    playhead,
    axis,
    highlightColor,
    hoverCell,
    recent,
    boot,
    openFile,
    openUrl,
    openStored,
    forget,
    keepFile,
    close,
    seek,
    step,
    jump,
    indexAt,
    restore,
    listQuery,
    listTypes,
    failuresOnly,
    collapsedGroups,
    panelTab,
    display,
    setDisplayPart,
    resetDisplay,
    cellsAt,
    setScale,
    togglePlay,
    detail,
    toggleLane,
  }
}
