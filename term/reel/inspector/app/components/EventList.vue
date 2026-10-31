<script setup lang="ts">
import type { CastEvent, EventType } from '~/lib/types'
import type { Wait } from '~/lib/waits'
import { ListFilterIcon, SearchIcon, XIcon } from '@lucide/vue'
import { useVirtualizer } from '@tanstack/vue-virtual'
import { formatTime } from '~/lib/format'
import { annotationTool, eventColor, laneItems, severity, typeColor, waitColor } from '~/lib/lanes'

const { generation, summary, lanes, laneFilter, cursor, seek, listQuery: query, listTypes, failuresOnly } = useInspector()

const filterLane = computed(() => lanes.value.find(l => l.id === laneFilter.value))
const inLane = computed(() => filterLane.value && new Set(laneItems(filterLane.value).map(it => it.i)))

const types: EventType[] = ['output', 'input', 'mark', 'annotation', 'resize', 'focus', 'scroll', 'scrollback', 'theme', 'exit']
// shown are the types the list shows; a null filter shows them all.
const shown = computed(() => listTypes.value ?? new Set<string>(types))

const present = computed(() => {
  const counts = new Map<EventType, number>()
  for (const e of summary.value?.events ?? [])
    counts.set(e.type, (counts.get(e.type) ?? 0) + 1)
  return types.filter(t => counts.has(t)).map(t => ({ type: t, count: counts.get(t)! }))
})

function matches(e: CastEvent, q: string) {
  return (e.label ?? '').toLowerCase().includes(q)
    || (e.kind ?? '').toLowerCase().includes(q)
    || e.type.includes(q)
    || (e.tags && Object.keys(e.tags).some(t => t.toLowerCase().includes(q)))
}

const rows = computed(() => {
  const q = query.value.trim().toLowerCase()
  const all = summary.value?.events ?? []
  const allTypes = listTypes.value === null
  const lane = inLane.value
  if (!q && allTypes && !failuresOnly.value && !lane)
    return all
  return all.filter(e => shown.value.has(e.type) && (!failuresOnly.value || severity(e)) && (!lane || lane.has(e.i)) && (!q || matches(e, q)))
})

const filtered = computed(() => rows.value.length !== (summary.value?.events.length ?? 0))

const anyFilter = computed(() => !!query.value || listTypes.value !== null || failuresOnly.value || !!laneFilter.value)

function resetFilters() {
  query.value = ''
  listTypes.value = null
  failuresOnly.value = false
  laneFilter.value = null
}

function toggleType(t: EventType, on: boolean) {
  const s = new Set(shown.value)
  if (on)
    s.add(t)
  else
    s.delete(t)
  listTypes.value = types.every(x => s.has(x)) ? null : s
}

const scroller = useTemplateRef<HTMLElement>('scroller')
const virtualizer = useVirtualizer(computed(() => ({
  count: rows.value.length,
  getScrollElement: () => scroller.value ?? null,
  estimateSize: () => 26,
  overscan: 12,
})))

// Keep the selected event in view as the cursor moves elsewhere.
watch([cursor, scroller, generation], async ([i]) => {
  await nextTick()
  const at = binaryIndex(rows.value, i)
  if (at >= 0)
    virtualizer.value.scrollToIndex(at, { align: 'auto' })
}, { immediate: true })

function binaryIndex(list: CastEvent[], i: number) {
  let lo = 0
  let hi = list.length - 1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    const v = list[mid]!.i
    if (v === i)
      return mid
    if (v < i)
      lo = mid + 1
    else hi = mid - 1
  }
  return -1
}

// Each steep wait draws a line down the gutter from its start to its end,
// capped where those annotations are. A side the recording cut off fades
// out at the recording's edge.
const waits = computed(() => lanes.value.find(l => l.kind === 'waits')?.waits ?? [])
const gutterW = computed(() => (waits.value.length ? 6 + 5 * Math.max(1, ...waits.value.map(w => w.depth + 1)) : 0))

function waitSegment(w: Wait, i: number) {
  const color = waitColor(w)
  const top = i === w.start ? '50%' : '0'
  const bottom = i === w.end ? '50%' : '0'
  const fadeIn = w.start === undefined && i === w.from
  const fadeOut = w.end === undefined && i === w.to
  return {
    left: `${4 + w.depth * 5}px`,
    top,
    bottom,
    background: fadeIn || fadeOut ? `linear-gradient(${fadeIn ? 'to bottom' : 'to top'}, transparent, ${color})` : color,
  }
}

// Cache visible rows' metadata so moving the cursor only updates selection.
const visibleRows = computed(() => virtualizer.value.getVirtualItems().map((row) => {
  const event = rows.value[row.index]!
  const tool = annotationTool(event.kind)
  const sev = severity(event)
  const count = Object.values(event.tags ?? {}).reduce((a, b) => a + b, 0)
  return {
    event,
    start: row.start,
    tool,
    kind: tool ? event.kind!.slice(tool.length + 1) : event.kind,
    color: eventColor(event),
    style: sev === 'error'
      ? 'bg-destructive/12 border-l-destructive hover:bg-destructive/18'
      : sev === 'warn'
        ? 'bg-warning/10 border-l-warning hover:bg-warning/15'
        : tool === 'steep' ? 'bg-[#c084fc]/[0.05]' : '',
    tags: count ? `${count} seq` : '',
    waits: waits.value.filter(w => w.from <= event.i && event.i <= w.to),
  }
}))
</script>

<template>
  <div class="flex h-full flex-col">
    <div class="flex h-10 shrink-0 items-center gap-1.5 border-b px-2">
      <div class="relative flex-1">
        <SearchIcon class="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-dim" />
        <Input v-model="query" placeholder="Filter events" class="h-7 border-transparent bg-transparent pl-7 text-[13px] shadow-none focus-visible:border-input dark:bg-transparent" />
      </div>
      <DropdownMenu>
        <DropdownMenuTrigger as-child>
          <Button variant="ghost" size="icon-sm" aria-label="Filter by type" :class="filtered && 'text-primary'">
            <ListFilterIcon />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" class="w-52">
          <DropdownMenuGroup>
            <DropdownMenuLabel class="flex items-center justify-between">
              Event types
              <Button variant="ghost" size="xs" class="h-5 px-1.5 text-[11px] font-normal" :disabled="!anyFilter" @click="resetFilters">
                Reset
              </Button>
            </DropdownMenuLabel>
            <DropdownMenuCheckboxItem
              v-for="p in present"
              :key="p.type"
              :model-value="shown.has(p.type)"
              @update:model-value="(v: boolean) => toggleType(p.type, v)"
              @select.prevent
            >
              <span class="size-2 rounded-full" :style="{ background: typeColor[p.type] }" />
              {{ p.type }}
              <span class="tabular ml-auto font-mono text-xs text-dim">{{ p.count }}</span>
            </DropdownMenuCheckboxItem>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuCheckboxItem v-model="failuresOnly" @select.prevent>
              Failures and warnings only
            </DropdownMenuCheckboxItem>
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>

    <div v-if="filterLane" class="flex h-8 shrink-0 items-center gap-2 border-b bg-primary/8 px-3 text-xs">
      <span class="text-muted-foreground">Lane</span>
      <span class="size-2 rounded-[2px]" :style="{ background: filterLane.color }" />
      <span class="min-w-0 truncate font-mono text-foreground">{{ filterLane.group === 'steep' || filterLane.group === 'reel' ? `${filterLane.group}.` : '' }}{{ filterLane.label }}</span>
      <Button variant="ghost" size="icon-xs" class="ml-auto" aria-label="Show all lanes" @click="laneFilter = null">
        <XIcon />
      </Button>
    </div>

    <div ref="scroller" class="min-h-0 flex-1 overflow-y-auto scrollbar-thin" role="listbox" aria-label="Events">
      <div :style="{ height: `${virtualizer.getTotalSize()}px` }" class="relative">
        <button
          v-for="row in visibleRows"
          :key="row.event.i"
          type="button"
          role="option"
          :aria-selected="row.event.i === cursor"
          class="absolute inset-x-0 flex h-[26px] items-center gap-2 border-l-2 border-transparent pr-2 pl-1.5 text-left text-[12.5px] hover:bg-muted aria-selected:bg-primary/12 aria-selected:text-foreground"
          :class="row.style"
          :style="{ transform: `translateY(${row.start}px)` }"
          @click="seek(row.event.i)"
        >
          <span v-if="gutterW" aria-hidden="true" class="relative -my-px shrink-0 self-stretch" :style="{ width: `${gutterW}px` }">
            <template v-for="w in row.waits" :key="w.from">
              <span class="absolute w-[1.5px]" :style="waitSegment(w, row.event.i)" :title="`wait ${w.id}: ${w.what}, ${w.outcome ?? 'unfinished'}`" />
              <span
                v-if="row.event.i === w.start || row.event.i === w.end"
                class="absolute top-1/2 h-[1.5px] w-[4px] -translate-y-1/2"
                :style="{ left: `${4 + w.depth * 5}px`, background: waitColor(w) }"
              />
            </template>
          </span>
          <span class="tabular w-9 shrink-0 text-right font-mono text-[11px] text-dim">{{ row.event.i }}</span>
          <span class="tabular w-[58px] shrink-0 font-mono text-[11px] text-muted-foreground">{{ formatTime(row.event.at) }}</span>
          <span class="size-1.5 shrink-0 rounded-full" :style="{ background: row.color }" />
          <span
            class="w-[62px] shrink-0"
            :class="row.tool ? 'font-medium' : 'text-muted-foreground'"
            :style="row.tool && { color: row.color }"
          >{{ row.tool ?? row.event.type }}</span>
          <span class="min-w-0 flex-1 truncate font-mono text-[11.5px]" :class="row.event.type === 'mark' ? 'font-sans font-medium text-[12.5px]' : 'text-foreground/85'">
            <template v-if="row.event.type === 'annotation'">
              <span class="text-foreground">{{ row.kind }}</span>
              <span v-if="row.event.label" class="text-muted-foreground">&ensp;{{ row.event.label }}</span>
            </template>
            <template v-else>{{ row.event.label }}</template>
          </span>
          <span v-if="row.tags" class="shrink-0 font-mono text-[10.5px] text-dim">{{ row.tags }}</span>
        </button>
      </div>
      <p v-if="rows.length === 0" class="px-4 py-8 text-center text-xs text-muted-foreground">
        No events match.
      </p>
    </div>
    <div class="flex h-7 shrink-0 items-center border-t px-2.5 text-[11px] text-dim">
      <span class="tabular">{{ filtered ? `${rows.length.toLocaleString()} of ` : '' }}{{ summary?.events.length.toLocaleString() }} events</span>
    </div>
  </div>
</template>
