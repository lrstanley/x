<script setup lang="ts">
import type { LaneGroup } from '~/lib/lanes'
import { SlidersHorizontalIcon } from '@lucide/vue'
import { laneCount, laneGroups } from '~/lib/lanes'

const { lanes, hiddenLanes, toggleLane } = useInspector()
const lanesByGroup = computed(() => laneGroups.map(g => ({ group: g, lanes: lanes.value.filter(l => l.group === g) })).filter(g => g.lanes.length))
function setGroup(g: LaneGroup, on: boolean) {
  const s = new Set(hiddenLanes.value)
  for (const l of lanes.value.filter(l => l.group === g)) {
    if (on)
      s.delete(l.id)
    else
      s.add(l.id)
  }
  hiddenLanes.value = s
}
</script>

<template>
  <Popover>
    <PopoverTrigger as-child>
      <Button variant="ghost" size="sm" class="h-6 text-xs">
        <SlidersHorizontalIcon data-icon="inline-start" />
        Lanes
        <span v-if="hiddenLanes.size" class="tabular font-mono text-dim">{{ lanes.length - hiddenLanes.size }}/{{ lanes.length }}</span>
      </Button>
    </PopoverTrigger>
    <PopoverContent align="end" class="max-h-[60vh] w-72 overflow-y-auto p-0 scrollbar-thin">
      <div v-for="g in lanesByGroup" :key="g.group" class="border-b py-1.5 last:border-b-0">
        <div class="flex items-center justify-between px-3 py-1">
          <span class="text-xs font-medium text-muted-foreground">{{ g.group }}</span>
          <span class="flex gap-1">
            <Button variant="ghost" size="xs" class="h-5 px-1.5 text-[11px]" @click="setGroup(g.group, true)">All</Button>
            <Button variant="ghost" size="xs" class="h-5 px-1.5 text-[11px]" @click="setGroup(g.group, false)">None</Button>
          </span>
        </div>
        <label v-for="l in g.lanes" :key="l.id" class="flex cursor-pointer items-center gap-2 px-3 py-1 hover:bg-muted">
          <Checkbox :model-value="!hiddenLanes.has(l.id)" @update:model-value="toggleLane(l.id)" />
          <span class="size-2 rounded-[2px]" :style="{ background: l.color }" />
          <span class="truncate" :class="l.group !== 'Events' && 'font-mono text-xs'">{{ l.label }}</span>
          <span class="tabular ml-auto font-mono text-[11px] text-dim">{{ laneCount(l) }}</span>
        </label>
      </div>
    </PopoverContent>
  </Popover>
</template>
