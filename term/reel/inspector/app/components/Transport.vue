<script setup lang="ts">
import type { Component } from 'vue'
import { ChevronFirstIcon, ChevronLastIcon, ChevronLeftIcon, ChevronRightIcon, FlagIcon, PauseIcon, PlayIcon, TriangleAlertIcon } from '@lucide/vue'
import { formatTime } from '~/lib/format'
import { severity } from '~/lib/lanes'

const { summary, cursor, playing, playhead, speed, seek, step, jump, togglePlay } = useInspector()

const last = computed(() => (summary.value?.events.length ?? 0) - 1)
const speeds = ['0.05', '0.25', '0.5', '1', '2', '4']
const speedModel = computed({
  get: () => String(speed.value),
  set: (v: string) => (speed.value = Number(v)),
})

interface Control { icon: Component, label: string, keys: string[], run: () => unknown }

const before: Control[] = [
  { icon: ChevronFirstIcon, label: 'Before the first event', keys: ['Home'], run: () => seek(-1) },
  { icon: FlagIcon, label: 'Previous mark', keys: ['⇧', '←'], run: () => jump(-1, e => e.type === 'mark') },
  { icon: TriangleAlertIcon, label: 'Previous failure or warning', keys: ['Alt', '←'], run: () => jump(-1, e => !!severity(e)) },
  { icon: ChevronLeftIcon, label: 'Previous event', keys: ['←'], run: () => step(-1) },
]
const after: Control[] = [
  { icon: ChevronRightIcon, label: 'Next event', keys: ['→'], run: () => step(1) },
  { icon: TriangleAlertIcon, label: 'Next failure or warning', keys: ['Alt', '→'], run: () => jump(1, e => !!severity(e)) },
  { icon: FlagIcon, label: 'Next mark', keys: ['⇧', '→'], run: () => jump(1, e => e.type === 'mark') },
  { icon: ChevronLastIcon, label: 'Last event', keys: ['End'], run: () => seek(last.value) },
]
</script>

<template>
  <div class="flex h-10 shrink-0 items-center gap-1 border-b px-2">
    <Tooltip v-for="c in before" :key="c.label">
      <TooltipTrigger as-child>
        <Button variant="ghost" size="icon-sm" :aria-label="c.label" @click="c.run">
          <component :is="c.icon" />
        </Button>
      </TooltipTrigger>
      <TooltipContent class="flex items-center gap-2">
        {{ c.label }}
        <KbdGroup><Kbd v-for="k in c.keys" :key="k">{{ k }}</Kbd></KbdGroup>
      </TooltipContent>
    </Tooltip>

    <Tooltip>
      <TooltipTrigger as-child>
        <Button size="icon-sm" :aria-label="playing ? 'Pause' : 'Play'" @click="togglePlay">
          <PauseIcon v-if="playing" />
          <PlayIcon v-else />
        </Button>
      </TooltipTrigger>
      <TooltipContent class="flex items-center gap-2">
        {{ playing ? 'Pause' : 'Play in real time' }}
        <Kbd>Space</Kbd>
      </TooltipContent>
    </Tooltip>

    <Tooltip v-for="c in after" :key="c.label">
      <TooltipTrigger as-child>
        <Button variant="ghost" size="icon-sm" :aria-label="c.label" @click="c.run">
          <component :is="c.icon" />
        </Button>
      </TooltipTrigger>
      <TooltipContent class="flex items-center gap-2">
        {{ c.label }}
        <KbdGroup><Kbd v-for="k in c.keys" :key="k">{{ k }}</Kbd></KbdGroup>
      </TooltipContent>
    </Tooltip>

    <Select v-model="speedModel">
      <SelectTrigger size="sm" class="ml-1 h-7 w-[80px] font-mono text-xs" aria-label="Playback speed">
        <SelectValue />
      </SelectTrigger>
      <SelectContent position="popper" align="start">
        <SelectGroup>
          <SelectItem v-for="s in speeds" :key="s" :value="s" class="font-mono text-xs">
            {{ s }}×
          </SelectItem>
        </SelectGroup>
      </SelectContent>
    </Select>
    <DisplayMenu />

    <div class="tabular ml-auto flex items-baseline gap-3 pr-1 font-mono text-xs whitespace-nowrap">
      <span class="text-muted-foreground">
        <span class="text-foreground">{{ formatTime(playhead) }}</span>
        / {{ formatTime(summary?.duration ?? 0) }}
      </span>
      <span class="text-dim">
        #<span class="text-muted-foreground">{{ cursor }}</span> / {{ last }}
      </span>
    </div>
  </div>
</template>
