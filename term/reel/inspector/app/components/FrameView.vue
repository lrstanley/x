<script setup lang="ts">
import type { Screen, ScreenCell } from '~/lib/types'
import { useElementSize, watchDebounced } from '@vueuse/core'
import { attrList, formatTime, underlineNames } from '~/lib/format'

const { generation, frame, frameScale, cursor, hoverCell, highlightColor, display, cellsAt, setScale } = useInspector()

const shownEvent = computed(() => (frame.value?.state.applied ?? 0) - 1)

const view = useTemplateRef<HTMLCanvasElement>('view')
const area = useTemplateRef<HTMLElement>('area')
const { width: areaW } = useElementSize(area)

// Frames are drawn at the scale they are shown at: the device's pixel ratio,
// less however much the frame is shrunk to fit its panel, in steps of a
// quarter rounded up so text stays sharp. Drawing more pixels than are shown
// costs time in both reel and the browser.
const fitScale = computed(() => {
  const f = frame.value
  if (!f?.width || !areaW.value)
    return null
  const dpr = window.devicePixelRatio || 1
  const natural = f.width / (f.scale ?? frameScale.value)
  const fit = Math.min(1, (areaW.value - 48) / natural)
  return Math.max(0.5, Math.min(dpr, Math.ceil(fit * dpr * 4) / 4))
})
watchDebounced(fitScale, (s) => {
  if (s && s !== frameScale.value)
    setScale(s)
}, { debounce: 150, immediate: true })
const overlay = useTemplateRef<HTMLCanvasElement>('overlay')
const pinned = ref<{ x: number, y: number } | null>(null)

// Draw each frame's pixels as reel rendered them; the overlay marks the
// hovered cell and the cells using the highlighted color.
// Draw on mount too, as a canvas mounted again starts blank.
watch(frame, f => drawFrame(f), { flush: 'post' })
onMounted(() => drawFrame(frame.value))

function drawFrame(f: typeof frame.value) {
  const c = view.value
  if (!f?.bitmap || !c || !f.width || !f.height)
    return
  if (c.width !== f.width || c.height !== f.height) {
    c.width = f.width
    c.height = f.height
  }
  const ctx = c.getContext('2d')!
  // Rounded corners leave pixels transparent, so clear what was there.
  ctx.clearRect(0, 0, c.width, c.height)
  ctx.drawImage(f.bitmap, 0, 0)
  drawOverlay()
}

// The cells, which frames leave out, are fetched only to inspect a cell or
// highlight a color, for the event the frame shows.
const needCells = computed(() => !!(hoverCell.value || pinned.value || highlightColor.value))
const cells = shallowRef<Screen | null>(null)
watch(generation, () => {
  pinned.value = null
  cells.value = null
})
watch([frame, needCells], async ([f, need], _, onCleanup) => {
  let canceled = false
  onCleanup(() => canceled = true)
  cells.value = null
  if (!need || !f)
    return
  const screen = await cellsAt(f.state.applied - 1).catch(() => undefined)
  if (!canceled)
    cells.value = screen ?? null
}, { immediate: true })

watch([hoverCell, highlightColor, pinned, cells], () => drawOverlay())

// overlayDrawn is whether the overlay has anything on it to clear.
let overlayDrawn = false

function cellSpecs(cell: ScreenCell | null | undefined) {
  const s = cell?.style
  return {
    fg: s?.fg || 'default-fg',
    bg: s?.bg || 'default-bg',
    ul: s?.underline_color || '',
  }
}

function drawOverlay() {
  const o = overlay.value
  const f = frame.value
  if (!o || !f?.width || !f.height)
    return
  if (o.width !== f.width || o.height !== f.height) {
    o.width = f.width
    o.height = f.height
  }
  const ctx = o.getContext('2d')!
  const hl = highlightColor.value
  if (!hl && !pinned.value && !hoverCell.value && !overlayDrawn)
    return
  ctx.clearRect(0, 0, o.width, o.height)
  overlayDrawn = false
  const { x: gx, y: gy, cell_w: cw, cell_h: ch } = f.layout
  const screen = cells.value
  if (hl && screen) {
    overlayDrawn = true
    ctx.fillStyle = 'rgba(0,0,0,0.55)'
    ctx.fillRect(gx, gy, f.cols * cw, f.rows * ch)
    ctx.globalCompositeOperation = 'destination-out'
    screen.cells.forEach((row, y) => row.forEach((cell, x) => {
      const s = cellSpecs(cell)
      const hasText = !!cell?.content && cell.content !== ' '
      if ((hasText && s.fg === hl) || s.bg === hl || s.ul === hl)
        ctx.fillRect(gx + x * cw, gy + y * ch, cw * Math.max(1, cell?.width ?? 1), ch)
    }))
    ctx.globalCompositeOperation = 'source-over'
  }
  for (const [c, color] of [[pinned.value, '#3b6ef6'], [hoverCell.value, 'rgba(255,255,255,0.85)']] as const) {
    if (!c)
      continue
    overlayDrawn = true
    ctx.strokeStyle = color
    ctx.lineWidth = frameScale.value
    ctx.strokeRect(gx + c.x * cw + 0.5, gy + c.y * ch + 0.5, cw - 1, ch - 1)
  }
}

function cellAt(e: MouseEvent) {
  const f = frame.value
  const c = view.value
  if (!f || !c)
    return null
  const r = c.getBoundingClientRect()
  const px = (e.clientX - r.left) * (c.width / r.width)
  const py = (e.clientY - r.top) * (c.height / r.height)
  const x = Math.floor((px - f.layout.x) / f.layout.cell_w)
  const y = Math.floor((py - f.layout.y) / f.layout.cell_h)
  if (x < 0 || y < 0 || x >= f.cols || y >= f.rows)
    return null
  return { x, y }
}

function onMove(e: MouseEvent) {
  const c = cellAt(e)
  if (c?.x !== hoverCell.value?.x || c?.y !== hoverCell.value?.y)
    hoverCell.value = c
}

function onClick(e: MouseEvent) {
  const c = cellAt(e)
  pinned.value = c && (c.x !== pinned.value?.x || c.y !== pinned.value?.y) ? c : null
}

const inspected = computed(() => {
  const c = hoverCell.value ?? pinned.value
  const f = frame.value
  if (!c || !f)
    return null
  const cell = cells.value?.cells[c.y]?.[c.x]
  const s = cellSpecs(cell)
  const resolve = (spec: string) => f.colors.find(u => u.spec === spec)?.hex
  return {
    ...c,
    content: cell?.content ?? '',
    width: cell?.width ?? 1,
    fg: s.fg,
    fgHex: resolve(s.fg) ?? f.state.fg,
    bg: s.bg,
    bgHex: resolve(s.bg) ?? f.state.bg,
    attrs: attrList(cell?.style?.attrs),
    underline: cell?.style?.underline ? underlineNames[cell.style.underline] : '',
    link: cell?.link?.url ?? '',
  }
})

const cssSize = computed(() => {
  const f = frame.value
  if (!f?.width || !f.height)
    return {}
  return { width: `${f.width / (f.scale ?? frameScale.value)}px`, aspectRatio: `${f.width} / ${f.height}` }
})

const codepoints = (s: string) => [...s].map(ch => `U+${ch.codePointAt(0)!.toString(16).toUpperCase().padStart(4, '0')}`).join(' ')
</script>

<template>
  <div class="flex h-full flex-col">
    <Transport />

    <div ref="area" class="relative min-h-0 flex-1 overflow-auto scrollbar-thin bg-[radial-gradient(rgb(255_255_255/0.06)_1px,transparent_1px)] [background-size:16px_16px]">
      <div class="flex min-h-full min-w-full items-center justify-center p-6">
        <Skeleton v-if="!frame" class="aspect-[16/10] w-[640px] max-w-full" />
        <div
          v-show="frame"
          class="relative max-w-full"
          :class="display?.chrome ? 'drop-shadow-[0_16px_32px_rgb(0_0_0/0.6)]' : 'shadow-[0_0_0_1px_rgb(255_255_255/0.08),0_24px_60px_-20px_rgb(0_0_0/0.8)]'"
          :style="cssSize"
        >
          <canvas ref="view" class="block size-full" />
          <canvas
            ref="overlay"
            class="absolute inset-0 size-full cursor-crosshair"
            @mousemove="onMove"
            @mouseleave="hoverCell = null"
            @click="onClick"
          />
        </div>
      </div>
    </div>

    <div class="flex h-7 shrink-0 items-center gap-3 overflow-hidden border-t px-3 font-mono text-[11px] text-muted-foreground">
      <template v-if="inspected">
        <span class="tabular text-foreground"><span class="text-dim">row</span> {{ inspected.y }} <span class="text-dim">col</span> {{ inspected.x }}</span>
        <span class="text-foreground">“{{ inspected.content || ' ' }}”</span>
        <span v-if="inspected.content" class="text-dim">{{ codepoints(inspected.content) }}{{ inspected.width > 1 ? ` w${inspected.width}` : '' }}</span>
        <span class="flex items-center gap-1">fg <span class="size-2.5 rounded-[2px] border border-border-strong" :style="{ background: inspected.fgHex }" />{{ inspected.fg }}</span>
        <span class="flex items-center gap-1">bg <span class="size-2.5 rounded-[2px] border border-border-strong" :style="{ background: inspected.bgHex }" />{{ inspected.bg }}</span>
        <span v-if="inspected.attrs.length">{{ inspected.attrs.join(' ') }}</span>
        <span v-if="inspected.underline">underline {{ inspected.underline }}</span>
        <span v-if="inspected.link" class="truncate text-primary">{{ inspected.link }}</span>
      </template>
      <template v-else>
        <template v-if="frame">
          <span class="tabular">{{ shownEvent < 0 ? 'Before the first event' : `After event ${shownEvent}` }}</span>
          <span class="tabular">{{ formatTime(frame.state.at) }}</span>
        </template>
        <Spinner v-if="shownEvent !== cursor" class="size-3" />
        <span v-if="frame" class="tabular">{{ frame.cols }}×{{ frame.rows }}</span>
        <span v-if="frame?.state.alt_screen">alt screen</span>
        <span class="ml-auto font-sans text-dim">Hover a cell to inspect it, click to pin</span>
      </template>
    </div>
  </div>
</template>
