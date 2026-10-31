<script setup lang="ts">
const { frame, summary, highlightColor } = useInspector()

const uses = computed(() => frame.value?.colors ?? [])
// Bars scale to the busiest color other than the default, which usually
// fills most of the screen and would flatten the rest.
const most = computed(() => Math.max(1, ...uses.value.filter(u => !u.spec.startsWith('default-')).map(u => u.fg + u.bg + u.ul)))

function toggle(spec: string) {
  highlightColor.value = highlightColor.value === spec ? null : spec
}

// The 16 base colors as drawn: the program's OSC 4 colors over the cast's.
const base = computed(() => {
  const set = frame.value?.state.palette_set ?? {}
  return (summary.value?.header.palette ?? []).map((hex, i) => ({ i, hex: set[i] ?? hex, changed: i in set }))
})

function kind(spec: string) {
  if (spec.startsWith('default-'))
    return 'default'
  if (spec.startsWith('#'))
    return 'rgb'
  return Number(spec) < 16 ? 'ansi' : '256'
}
</script>

<template>
  <div>
    <InfoSection title="On screen" :count="uses.length">
      <template #actions>
        <Button v-if="highlightColor" variant="ghost" size="xs" @click="highlightColor = null">
          Clear highlight
        </Button>
      </template>
      <p class="px-3 pb-2 text-xs text-muted-foreground">
        Cells by the color they set. Pick one to highlight its cells in the frame.
      </p>
      <ul>
        <li v-for="u in uses" :key="u.spec">
          <button
            type="button"
            class="grid w-full grid-cols-[18px_128px_1fr] items-center gap-2 px-3 py-1 text-left font-mono text-xs hover:bg-muted aria-pressed:bg-primary/12"
            :aria-pressed="highlightColor === u.spec"
            @click="toggle(u.spec)"
          >
            <span class="size-[18px] rounded-[4px] border border-border-strong" :style="{ background: u.hex }" />
            <span class="flex items-baseline gap-1.5 truncate">
              {{ u.spec }}
              <span v-if="kind(u.spec) !== 'rgb'" class="text-[11px] text-muted-foreground">{{ u.hex }}</span>
              <span v-if="kind(u.spec) !== 'default'" class="font-sans text-[10.5px] text-dim">{{ kind(u.spec) }}</span>
            </span>
            <span class="flex items-center gap-2">
              <span class="relative h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                <span class="absolute inset-y-0 left-0 rounded-full bg-foreground/50" :style="{ width: `${Math.min(1, u.fg / most) * 100}%` }" />
                <span class="absolute inset-y-0 rounded-full bg-foreground/20" :style="{ left: `${Math.min(1, u.fg / most) * 100}%`, width: `${Math.min(1 - Math.min(1, u.fg / most), u.bg / most) * 100}%` }" />
              </span>
              <span class="tabular w-[92px] shrink-0 text-right text-[11px] text-dim" :title="`${u.fg} cells of text, ${u.bg} cell backgrounds, ${u.ul} underlines`">
                {{ u.fg }}<span class="text-dim/70">fg</span> {{ u.bg }}<span class="text-dim/70">bg</span><template v-if="u.ul"> {{ u.ul }}<span class="text-dim/70">ul</span></template>
              </span>
            </span>
          </button>
        </li>
      </ul>
    </InfoSection>

    <InfoSection title="Base palette">
      <div class="grid grid-cols-8 gap-1.5 px-3 pb-1">
        <Tooltip v-for="c in base" :key="c.i">
          <TooltipTrigger as-child>
            <button
              type="button"
              class="flex flex-col items-center gap-1 rounded-md p-1 hover:bg-muted aria-pressed:bg-primary/12"
              :aria-pressed="highlightColor === String(c.i)"
              @click="toggle(String(c.i))"
            >
              <span class="h-6 w-full rounded-[4px] border border-border-strong" :class="c.changed && 'ring-1 ring-warning'" :style="{ background: c.hex }" />
              <span class="font-mono text-[10px] text-dim">{{ c.i }}</span>
            </button>
          </TooltipTrigger>
          <TooltipContent class="font-mono">
            {{ c.i }} {{ c.hex }}{{ c.changed ? ' (set by OSC 4)' : '' }}
          </TooltipContent>
        </Tooltip>
      </div>
    </InfoSection>
  </div>
</template>
