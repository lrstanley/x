<script setup lang="ts">
import { AppWindowIcon } from '@lucide/vue'
import { displayParts } from '~/lib/types'

// The parts reel can draw around and over the grid. Each starts as the cast
// calls for, from the sequences it writes; a change sticks to this cast's
// address.
const { summary, display, setDisplayPart, resetDisplay } = useInspector()

const changed = computed(() => displayParts.filter(p => display.value && display.value[p.key] !== summary.value?.display[p.key]).length)

function reason(key: string) {
  return summary.value?.display_reason?.[key]
}
</script>

<template>
  <Popover>
    <PopoverTrigger as-child>
      <Button variant="ghost" size="icon-sm" class="relative ml-0.5" aria-label="Display" title="Display: what is drawn around the screen">
        <AppWindowIcon />
        <span v-if="changed" class="absolute top-1 right-1 size-1.5 rounded-full bg-primary" />
      </Button>
    </PopoverTrigger>
    <PopoverContent align="start" class="w-80 p-0">
      <div class="flex items-center justify-between border-b px-3 py-2">
        <span class="text-xs font-medium text-muted-foreground">Drawn around the screen</span>
        <Button v-if="changed" variant="ghost" size="xs" class="h-5 px-1.5 text-[11px]" @click="resetDisplay">
          Reset
        </Button>
      </div>
      <ul class="py-1">
        <li v-for="p in displayParts" :key="p.key">
          <label class="flex cursor-pointer items-start gap-3 px-3 py-1.5 hover:bg-muted">
            <span class="min-w-0 flex-1">
              <span class="block text-[13px]">{{ p.label }}</span>
              <span class="block text-[11px] text-dim">
                <template v-if="display && display[p.key] !== summary?.display[p.key]">Changed from the cast's default</template>
                <template v-else-if="reason(p.key)">On: the cast {{ reason(p.key) }}</template>
                <template v-else>{{ p.about }}</template>
              </span>
            </span>
            <Switch
              class="mt-0.5"
              :model-value="display?.[p.key] ?? false"
              @update:model-value="(on: boolean) => setDisplayPart(p.key, on)"
            />
          </label>
        </li>
      </ul>
    </PopoverContent>
  </Popover>
</template>
