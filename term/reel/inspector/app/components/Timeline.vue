<script setup lang="ts">
import type { Lane, LaneItem } from '~/lib/lanes'
import type { Row } from '~/lib/timelineGeometry'
import type { CastEvent } from '~/lib/types'
import type { Wait } from '~/lib/waits'
import { LABEL_W, RULER_H, PAD, PAD_END, hitMarker, playheadUnit, rowHeight, sceneRows, seekAt, unitAt as toUnit, unitOf as eventUnit, xAt } from '~/lib/timelineGeometry'
import { MaximizeIcon, MinusIcon, PlusIcon } from '@lucide/vue'
import { useElementSize } from '@vueuse/core'
import { drawTimeline, drawTimelineHead } from '~/lib/timelinePaint'
import { formatSource, formatTime } from '~/lib/format'
import { laneGroups, laneItems } from '~/lib/lanes'

const { summary, lanes, cursor, seek, indexAt, hiddenLanes, laneFilter, playhead, playing, axis, collapsedGroups: collapsed } = useInspector()

const scroller = useTemplateRef<HTMLDivElement>('scroller')
const canvas = useTemplateRef<HTMLCanvasElement>('canvas')
// head is an overlay with the playhead and hover line, which move far more
// often than the lanes change, so the lanes are not drawn again for them.
const head = useTemplateRef<HTMLCanvasElement>('head')
const { width: viewW, height: viewH } = useElementSize(scroller)
const scrollX = ref(0)
const scrollY = ref(0)

const events = computed<CastEvent[]>(() => summary.value?.events ?? [])

// The x axis is time, or event index for recordings whose events bunch up,
// such as a test that writes many frames in the same millisecond.
const span = computed(() => (axis.value === 'time' ? Math.max(1, summary.value?.duration ?? 1) : Math.max(1, events.value.length)))
const zoom = ref(1)
const fitZoom = computed(() => Math.max(1e-6, (viewW.value - LABEL_W - PAD - PAD_END) / span.value))
const contentW = computed(() => LABEL_W + PAD + PAD_END + span.value * zoom.value)

function fit() {
  zoom.value = fitZoom.value
  nextTick(() => scroller.value?.scrollTo({ left: 0 }))
}

watch([summary, axis], () => nextTick(fit))
watch(viewW, (w, old) => {
  // Stay fitted while fitted, as panels resize.
  if (!old || Math.abs(zoom.value - (old - LABEL_W - PAD - PAD_END) / span.value) < 1e-9)
    zoom.value = fitZoom.value
})

const viewport = computed(() => ({ axis: axis.value, zoom: zoom.value, scrollX: scrollX.value }))
const unitOf = (event: CastEvent) => eventUnit(event, axis.value)
const xOf = (event: CastEvent) => xAt(unitOf(event), viewport.value)
const unitAt = (x: number) => toUnit(x, viewport.value)
const playheadX = () => xAt(playheadUnit(events.value, cursor.value, playhead.value, axis.value), viewport.value)

const rows = computed<Row[]>(() => {
  const out: Row[] = []
  for (const g of laneGroups) {
    const ls = lanes.value.filter(l => l.group === g && !hiddenLanes.value.has(l.id))
    if (ls.length === 0)
      continue
    out.push({ kind: 'group', group: g, count: ls.length })
    if (!collapsed.value.has(g))
      out.push(...ls.map(lane => ({ kind: 'lane' as const, lane })))
  }
  return out
})

const contentH = computed(() => RULER_H + rows.value.reduce((h, r) => h + rowHeight(r), 0) + 8)

// Marks and failures draw a faint rule through every lane, for context.
const marks = computed(() => events.value.filter(e => e.type === 'mark'))
const failures = computed(() => lanes.value.flatMap(l => laneItems(l).filter(it => it.severity === 'error')).map(it => events.value[it.i]!))

const scene = computed(() => sceneRows(rows.value, events.value, viewport.value, scrollY.value, viewW.value, viewH.value))

const hover = ref<{ x: number, y: number, ev: CastEvent, lane: Lane, item: LaneItem, wait?: Wait } | null>(null)
watch(summary, () => hover.value = null)

function draw() {
  if (canvas.value && summary.value)
    drawTimeline(canvas.value, { width: viewW.value, height: viewH.value, view: viewport.value, scene: scene.value, marks: marks.value, failures: failures.value, laneFilter: laneFilter.value, collapsed: collapsed.value })
}

function drawHead() {
  if (head.value && summary.value)
    drawTimelineHead(head.value, viewW.value, viewH.value, playheadX(), hover.value ? xOf(hover.value.ev) : undefined)
}

let frameRequested = false
function redraw() {
  if (frameRequested)
    return
  frameRequested = true
  requestAnimationFrame(() => {
    frameRequested = false
    draw()
  })
}

let headRequested = false
function redrawHead() {
  if (headRequested)
    return
  headRequested = true
  requestAnimationFrame(() => {
    headRequested = false
    drawHead()
  })
}

watch([summary, lanes, zoom, scrollX, scrollY, viewW, viewH, rows, axis, laneFilter], redraw)
watch([summary, cursor, zoom, scrollX, viewW, viewH, hover, axis, playhead], redrawHead)
onMounted(() => document.fonts?.ready.then(() => {
  redraw()
  redrawHead()
}))

function onScroll() {
  scrollX.value = scroller.value!.scrollLeft
  scrollY.value = scroller.value!.scrollTop
}

// Keep the playhead in view as it moves.
watch(cursor, (i) => {
  const ev = events.value[i]
  const s = scroller.value
  if (!ev || !s || dragging)
    return
  const x = xOf(ev)
  if (x < LABEL_W + PAD || x > viewW.value - PAD)
    s.scrollLeft = Math.max(0, unitOf(ev) * zoom.value - (viewW.value - LABEL_W) / 2)
})

// While playing, follow the playhead itself, which moves between events.
watch(playhead, () => {
  const s = scroller.value
  if (!playing.value || !s || dragging)
    return
  const x = playheadX()
  if (x < LABEL_W + PAD || x > viewW.value - PAD)
    s.scrollLeft = Math.max(0, x + scrollX.value - LABEL_W - PAD - (viewW.value - LABEL_W) / 4)
})

function zoomAt(factor: number, px: number) {
  const u = unitAt(px)
  const z = Math.min(Math.max(zoom.value * factor, fitZoom.value * 0.5), 400 / (axis.value === 'time' ? 1 : 0.05))
  zoom.value = z
  nextTick(() => {
    scroller.value!.scrollLeft = Math.max(0, u * z - (px - LABEL_W - PAD))
    onScroll()
  })
}

function onWheel(e: WheelEvent) {
  if (!(e.ctrlKey || e.metaKey))
    return
  e.preventDefault()
  const r = scroller.value!.getBoundingClientRect()
  zoomAt(Math.exp(-e.deltaY * 0.0025), Math.max(LABEL_W, e.clientX - r.left))
}

function rowAt(y: number) {
  return scene.value.find(row => y >= row.top && y < row.top + row.height)?.row
}

function hitItem(x: number, y: number) {
  const marker = hitMarker(scene.value, x, y)
  return marker && { lane: marker.lane, item: marker.item, wait: marker.kind === 'wait' ? marker.wait : undefined }
}

// seekToX moves the playhead to x, between events if that is where x is,
// and shows the last event at or before it. On the event axis, x between two
// events' slots is the matching time between theirs.
function seekToX(x: number) {
  const u = Math.max(0, unitAt(Math.max(LABEL_W, x)))
  if (axis.value === 'time') {
    seek(indexAt(u), Math.min(u, summary.value?.duration ?? u))
    return
  }
  const target = seekAt(u, events.value)
  seek(target.index, target.time)
}

let dragging = false
function local(e: MouseEvent) {
  const r = scroller.value!.getBoundingClientRect()
  return { x: e.clientX - r.left, y: e.clientY - r.top }
}

function onDown(e: MouseEvent) {
  if (e.button !== 0)
    return
  const { x, y } = local(e)
  if (x < LABEL_W) {
    const row = y >= RULER_H ? rowAt(y) : undefined
    if (row?.kind === 'lane') {
      laneFilter.value = laneFilter.value === row.lane.id ? null : row.lane.id
      return
    }
    if (row?.kind === 'group') {
      const s = new Set(collapsed.value)
      if (s.has(row.group))
        s.delete(row.group)
      else
        s.add(row.group)
      collapsed.value = s
    }
    return
  }
  const hit = hitItem(x, y)
  if (hit) {
    seek(hit.item.i)
    return
  }
  dragging = true
  seekToX(x)
  const move = (ev: MouseEvent) => seekToX(local(ev).x)
  const up = () => {
    dragging = false
    window.removeEventListener('mousemove', move)
    window.removeEventListener('mouseup', up)
  }
  window.addEventListener('mousemove', move)
  window.addEventListener('mouseup', up)
}

// overLabel is true over the lane names, which filter the event list.
const overLabel = ref(false)

function onMove(e: MouseEvent) {
  if (dragging)
    return
  const { x, y } = local(e)
  overLabel.value = x < LABEL_W && y >= RULER_H
  const hit = hitItem(x, y)
  hover.value = hit ? { x, y, ev: events.value[hit.item.i]!, ...hit } : null
}

const tooltip = computed(() => {
  const hv = hover.value
  if (!hv)
    return null
  const ev = hv.ev
  const place = { left: Math.max(0, Math.min(hv.x + 12, viewW.value - 300)), top: hv.y + 36 + 14 }
  if (hv.wait) {
    const wt = hv.wait
    const cut = [wt.start === undefined && 'started before the recording', wt.end === undefined && 'no end in the recording'].filter(Boolean).join(', ')
    return {
      what: `wait ${wt.id}`,
      detail: `${wt.what} · ${wt.outcome ?? 'unfinished'} · ${formatTime(wt.toAt - wt.fromAt)}`,
      source: cut,
      at: formatTime(wt.fromAt),
      i: wt.start ?? wt.from,
      ...place,
    }
  }
  const what = ev.type === 'annotation' ? ev.kind : ev.type
  let detail = ev.label ?? ''
  if (hv.lane.group === 'Sequences')
    detail = `${hv.item.weight} × ${hv.lane.label}`
  const source = ev.source && hv.lane.group !== 'Sequences' ? formatSource(ev.source) : ''
  return { what, detail, source, at: formatTime(ev.at), i: ev.i, ...place }
})

const zoomLabel = computed(() => `${Math.round((zoom.value / fitZoom.value) * 100)}%`)

</script>

<template>
  <div class="relative flex h-full flex-col">
    <div class="flex h-9 shrink-0 items-center gap-2 border-b px-2">
      <span class="pl-1 text-xs font-medium text-muted-foreground">Timeline</span>
      <ToggleGroup v-model="axis" type="single" variant="outline" size="sm" class="ml-2" aria-label="Axis">
        <ToggleGroupItem value="time" class="h-6 px-2 text-xs">
          Time
        </ToggleGroupItem>
        <ToggleGroupItem value="index" class="h-6 px-2 text-xs">
          Events
        </ToggleGroupItem>
      </ToggleGroup>

      <div class="ml-auto flex items-center gap-1">
        <TimelineLanes />
        <Separator orientation="vertical" class="mx-1 h-4" />
        <Tooltip>
          <TooltipTrigger as-child>
            <Button variant="ghost" size="icon-xs" aria-label="Zoom out" @click="zoomAt(1 / 1.6, (viewW + LABEL_W) / 2)">
              <MinusIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent class="flex items-center gap-2">
            Zoom out <KbdGroup><Kbd>Ctrl</Kbd><Kbd>Scroll</Kbd></KbdGroup>
          </TooltipContent>
        </Tooltip>
        <span class="tabular w-12 text-center font-mono text-[11px] text-muted-foreground">{{ zoomLabel }}</span>
        <Tooltip>
          <TooltipTrigger as-child>
            <Button variant="ghost" size="icon-xs" aria-label="Zoom in" @click="zoomAt(1.6, (viewW + LABEL_W) / 2)">
              <PlusIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent class="flex items-center gap-2">
            Zoom in <KbdGroup><Kbd>Ctrl</Kbd><Kbd>Scroll</Kbd></KbdGroup>
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger as-child>
            <Button variant="ghost" size="icon-xs" aria-label="Fit to width" @click="fit">
              <MaximizeIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>Fit to width</TooltipContent>
        </Tooltip>
      </div>
    </div>

    <div
      ref="scroller"
      class="relative min-h-0 flex-1 overflow-auto scrollbar-thin select-none"
      @scroll="onScroll"
      @wheel="onWheel"
      @mousedown="onDown"
      @mousemove="onMove"
      @mouseleave="hover = null"
    >
      <div class="sticky top-0 left-0 size-0">
        <canvas ref="canvas" class="absolute top-0 left-0" :class="hover || overLabel ? 'cursor-pointer' : 'cursor-col-resize'" />
        <canvas ref="head" class="pointer-events-none absolute top-0 left-0" />
      </div>
      <div :style="{ width: `${contentW}px`, height: `${contentH}px` }" />
    </div>

    <div
      v-if="tooltip"
      class="pointer-events-none absolute z-40 max-w-[300px] rounded-md border border-border-strong bg-popover px-2.5 py-1.5 shadow-lg"
      :style="{ left: `${tooltip.left}px`, top: `${tooltip.top}px` }"
    >
      <p class="flex items-baseline gap-2 text-xs">
        <span class="font-medium">{{ tooltip.what }}</span>
        <span class="tabular ml-auto font-mono text-[11px] text-dim">#{{ tooltip.i }} · {{ tooltip.at }}</span>
      </p>
      <p v-if="tooltip.detail" class="mt-0.5 truncate font-mono text-[11px] text-muted-foreground">
        {{ tooltip.detail }}
      </p>
      <p v-if="tooltip.source" class="mt-0.5 truncate font-mono text-[11px] text-dim">
        {{ tooltip.source }}
      </p>
    </div>
  </div>
</template>
