<script setup lang="ts">
import { FolderOpenIcon, XIcon } from '@lucide/vue'
import { formatBytes, formatTime } from '~/lib/format'

const { summary, fileName, fileSize, status, wasmProgress, close } = useInspector()
const picker = useTemplateRef<{ open: () => void }>('picker')

const facts = computed(() => {
  const s = summary.value
  if (!s)
    return []
  const h = s.header
  return [
    { label: 'Size', value: `${h.cols}×${h.rows}` },
    { label: 'Length', value: formatTime(s.duration) },
    { label: 'Events', value: s.events.length.toLocaleString() },
    { label: 'File', value: formatBytes(fileSize.value) },
  ]
})

const booting = computed(() => status.value === 'booting')
const progress = computed(() => {
  const { loaded, total } = wasmProgress.value
  return total ? `${Math.round((loaded / total) * 100)}%` : formatBytes(loaded)
})
</script>

<template>
  <header class="flex h-11 shrink-0 items-center gap-3 border-b bg-background px-3">
    <a href="./" class="flex items-center gap-2 pr-1" @click.prevent="close()">
      <img :src="`${useRuntimeConfig().app.baseURL}favicon.svg`" alt="" class="size-5">
      <span class="text-[13px] font-semibold tracking-tight">reel</span>
      <span class="text-[13px] text-muted-foreground">inspector</span>
    </a>

    <template v-if="summary">
      <span class="text-border-strong">/</span>
      <span class="max-w-72 truncate font-medium" :title="fileName">{{ fileName }}</span>
      <span v-if="summary.header.title && summary.header.title !== fileName" class="max-w-56 truncate text-muted-foreground">
        {{ summary.header.title }}
      </span>
      <dl class="ml-2 hidden items-center gap-4 lg:flex">
        <div v-for="f in facts" :key="f.label" class="flex items-baseline gap-1.5">
          <dt class="text-dim">
            {{ f.label }}
          </dt>
          <dd class="tabular font-mono text-xs text-muted-foreground">
            {{ f.value }}
          </dd>
        </div>
      </dl>
    </template>

    <div class="ml-auto flex items-center gap-2">
      <span v-if="booting" class="flex items-center gap-2 text-xs text-muted-foreground">
        <Spinner />
        Loading reel {{ progress }}
      </span>
      <span v-else-if="status === 'loading'" class="flex items-center gap-2 text-xs text-muted-foreground">
        <Spinner />
        Replaying
      </span>
      <FilePicker ref="picker" />
      <Button v-if="summary" variant="outline" size="sm" @click="picker?.open()">
        <FolderOpenIcon data-icon="inline-start" />
        Open
      </Button>
      <Tooltip v-if="summary">
        <TooltipTrigger as-child>
          <Button variant="ghost" size="icon-sm" aria-label="Close cast" @click="close()">
            <XIcon />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Close cast</TooltipContent>
      </Tooltip>
    </div>
  </header>
</template>
