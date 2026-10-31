<script setup lang="ts">
import { formatSource, formatTime } from '~/lib/format'

const { summary, seek } = useInspector()
const h = computed(() => summary.value!.header)

// steep records the test's setup and outcome as annotations; show them
// first, as they say most about why a recording exists.
const find = (kind: string) => summary.value?.events.find(e => e.type === 'annotation' && e.kind === kind)
const start = computed(() => find('steep.start'))
const result = computed(() => find('steep.result'))
const failures = computed(() => summary.value?.events.filter(e => e.kind === 'steep.failure') ?? [])
const effects = computed(() => summary.value?.events.filter(e => e.kind === 'reel.effect') ?? [])

const fmt = (v: unknown) => (typeof v === 'object' ? JSON.stringify(v) : String(v))
</script>

<template>
  <div>
    <InfoSection v-if="result" title="Test result">
      <dl>
        <InfoRow label="Test">
          {{ result.data?.test }}
        </InfoRow>
        <InfoRow label="Outcome">
          <Badge :variant="result.data?.failed ? 'destructive' : 'secondary'">
            {{ result.data?.failed ? `failed, ${result.data?.failures} failure${result.data?.failures === 1 ? '' : 's'}` : 'passed' }}
          </Badge>
        </InfoRow>
        <InfoRow label="Duration" mono>
          {{ formatTime(Number(result.data?.duration_ms ?? 0)) }}
        </InfoRow>
      </dl>
    </InfoSection>

    <InfoSection v-if="failures.length" title="Failures" :count="failures.length">
      <ul class="flex flex-col gap-1.5 px-3 pb-1">
        <li v-for="f in failures" :key="f.i">
          <button type="button" class="w-full rounded-md border border-destructive/30 bg-destructive/[0.06] px-2.5 py-2 text-left hover:bg-destructive/10" @click="seek(f.i)">
            <span class="tabular font-mono text-[11px] text-dim">#{{ f.i }} · {{ formatTime(f.at) }}<template v-if="f.source"> · {{ formatSource(f.source) }}</template></span>
            <span class="mt-0.5 block overflow-x-auto font-mono text-xs leading-relaxed whitespace-pre scrollbar-thin">{{ f.data?.message }}</span>
          </button>
        </li>
      </ul>
    </InfoSection>

    <InfoSection v-if="start" title="Harness">
      <dl>
        <InfoRow v-for="(v, k) in start.data" :key="k" :label="String(k)" mono>
          {{ fmt(v) }}
        </InfoRow>
      </dl>
    </InfoSection>

    <InfoSection title="Cast header">
      <dl>
        <InfoRow label="Title">
          {{ h.title || '–' }}
        </InfoRow>
        <InfoRow label="Size" mono>
          {{ h.cols }}×{{ h.rows }}
        </InfoRow>
        <InfoRow label="Scrollback" mono>
          {{ h.scrollback }} lines
        </InfoRow>
        <InfoRow label="Skip pristine">
          {{ h.skip_pristine ? 'yes' : 'no' }}
        </InfoRow>
        <InfoRow v-if="h.cell_width" label="Cell size" mono>
          {{ h.cell_width }}×{{ h.cell_height }} px
        </InfoRow>
        <InfoRow v-if="h.kitty_storage" label="Kitty storage" mono>
          {{ h.kitty_storage }}
        </InfoRow>
        <InfoRow v-for="[k, v] in [['Foreground', h.fg], ['Background', h.bg], ['Cursor', h.cursor]].filter(([, v]) => v)" :key="k" :label="k!" mono>
          <span class="flex items-center gap-1.5">
            <span class="size-3 rounded-[3px] border border-border-strong" :style="{ background: v }" />{{ v }}
          </span>
        </InfoRow>
      </dl>
    </InfoSection>

    <InfoSection v-if="effects.length" title="Export effects" :count="effects.length">
      <dl>
        <InfoRow v-for="e in effects" :key="e.i" :label="String(e.data?.effect ?? 'effect')" mono>
          {{ fmt(Object.fromEntries(Object.entries(e.data ?? {}).filter(([k]) => k !== 'effect'))) }}
        </InfoRow>
      </dl>
    </InfoSection>
  </div>
</template>
