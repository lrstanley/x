<script setup lang="ts">
import { FileUpIcon, FlaskConicalIcon, HistoryIcon, XIcon } from '@lucide/vue'
import { formatBytes } from '~/lib/format'

const { status, fatal, recent, openUrl, openStored, forget } = useInspector()
const picker = useTemplateRef<{ open: () => void }>('picker')

const samples = [
  { file: 'failing-wait.cast', title: 'Failing wait', about: 'A steep test whose Wait("Atlantis") times out.' },
  { file: 'table-navigation.cast', title: 'Table navigation', about: 'A passing steep test that moves through a table with the arrow keys.' },
]

function openSample(file: string) {
  openUrl(`${useRuntimeConfig().app.baseURL}samples/${file}`)
}

const busy = computed(() => status.value === 'loading')

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
function ago(t: number) {
  const mins = Math.round((t - Date.now()) / 60_000)
  if (Math.abs(mins) < 60)
    return rtf.format(mins, 'minute')
  const hours = Math.round(mins / 60)
  if (Math.abs(hours) < 24)
    return rtf.format(hours, 'hour')
  return rtf.format(Math.round(hours / 24), 'day')
}
</script>

<template>
  <main class="overflow-y-auto scrollbar-thin">
    <div class="mx-auto flex max-w-2xl flex-col gap-10 px-6 py-16">
      <section class="flex flex-col gap-3">
        <h1 class="text-3xl font-semibold tracking-tight text-balance">
          Replay a terminal recording, one event at a time.
        </h1>
        <p class="max-w-xl text-[15px] leading-relaxed text-muted-foreground">
          Open a <code class="font-mono text-[13px] text-foreground">.cast</code> file from reel or a steep test.
          The inspector decodes it with reel itself, compiled to WebAssembly, so every frame is drawn
          exactly as an export draws it. Files never leave your browser.
        </p>
      </section>

      <Alert v-if="status === 'error'" variant="destructive">
        <AlertTitle>reel failed to start</AlertTitle>
        <AlertDescription>{{ fatal }}</AlertDescription>
      </Alert>

      <button
        type="button"
        class="group flex flex-col items-center gap-3 rounded-xl border border-dashed border-border-strong bg-card px-6 py-12 text-center transition-colors hover:border-primary/60 hover:bg-primary/[0.03] focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-60"
        :disabled="busy"
        @click="picker?.open()"
      >
        <span class="flex size-10 items-center justify-center rounded-lg border bg-muted text-muted-foreground group-hover:text-foreground">
          <Spinner v-if="busy" />
          <FileUpIcon v-else class="size-5" />
        </span>
        <span class="text-sm font-medium">{{ busy ? 'Replaying…' : 'Drop a .cast file, or choose one' }}</span>
        <span class="text-xs text-muted-foreground">asciicast v2 or v3, with reel's annotations if it has them</span>
      </button>
      <FilePicker ref="picker" />

      <section class="flex flex-col gap-2">
        <h2 class="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          <FlaskConicalIcon class="size-3.5" />
          Samples
        </h2>
        <div class="grid gap-2 sm:grid-cols-2">
          <button
            v-for="s in samples"
            :key="s.file"
            type="button"
            class="flex flex-col gap-1 rounded-lg border bg-card px-3.5 py-3 text-left transition-colors hover:border-border-strong hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring"
            :disabled="busy"
            @click="openSample(s.file)"
          >
            <span class="font-medium">{{ s.title }}</span>
            <span class="text-xs leading-snug text-muted-foreground">{{ s.about }}</span>
          </button>
        </div>
      </section>

      <section v-if="recent.length" class="flex flex-col gap-2">
        <h2 class="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          <HistoryIcon class="size-3.5" />
          Recently opened
        </h2>
        <ul class="flex flex-col overflow-hidden rounded-lg border bg-card">
          <li v-for="r in recent" :key="r.id" class="group flex items-center gap-3 border-b px-3.5 last:border-b-0 hover:bg-muted">
            <button type="button" class="flex min-w-0 flex-1 items-center gap-3 py-2.5 text-left" :disabled="busy" @click="openStored(r.id)">
              <span class="truncate font-medium">{{ r.name }}</span>
              <span class="tabular ml-auto shrink-0 font-mono text-xs text-dim">{{ formatBytes(r.size) }}</span>
              <span class="w-24 shrink-0 text-right text-xs text-muted-foreground">{{ ago(r.opened) }}</span>
            </button>
            <Button variant="ghost" size="icon-xs" class="opacity-0 group-hover:opacity-100 focus-visible:opacity-100" :aria-label="`Forget ${r.name}`" @click="forget(r.id)">
              <XIcon />
            </Button>
          </li>
        </ul>
      </section>

      <section class="flex flex-col gap-2 border-t pt-6">
        <h2 class="text-xs font-medium text-muted-foreground">
          Recording a cast
        </h2>
        <p class="text-xs leading-relaxed text-muted-foreground">
          Any steep test writes one without changes. With <code class="font-mono">failure</code>, only failing tests do:
        </p>
        <pre class="overflow-x-auto rounded-md border bg-card px-3 py-2 font-mono text-xs text-foreground scrollbar-thin">STEEP_EXPORT_MODE=failure STEEP_EXPORT='{name}.cast' go test -artifacts ./...</pre>
        <p class="text-xs leading-relaxed text-muted-foreground">
          Link straight to one with <code class="font-mono">?url=https://…/test.cast</code> if the host allows cross-origin requests.
        </p>
      </section>
    </div>
  </main>
</template>
