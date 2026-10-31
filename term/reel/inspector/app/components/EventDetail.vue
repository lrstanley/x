<script setup lang="ts">
import type { EventDetail, Token } from '~/lib/types'
import { ChevronRightIcon } from '@lucide/vue'
import { formatBytes, formatSource, formatTime, shortFunc } from '~/lib/format'
import { eventColor, groupColor, severity } from '~/lib/lanes'

const { generation, summary, cursor, current, detail, playing } = useInspector()

const d = shallowRef<EventDetail | null>(null)
const showText = ref(true)
const limit = ref(400)
// open holds the tokens broken out into their fields.
const open = ref(new Set<number>())

function toggle(n: number) {
  const s = new Set(open.value)
  if (!s.delete(n))
    s.add(n)
  open.value = s
}

// While playing, an event's tokens are fetched once it has been shown for a
// moment, so playback does not wait on rendering hundreds of rows per frame.
watch([generation, cursor, playing], ([recording, i, on], _, onCleanup) => {
  limit.value = 400
  open.value = new Set()
  d.value = null
  let canceled = false
  let pending: ReturnType<typeof setTimeout> | undefined
  onCleanup(() => {
    canceled = true
    clearTimeout(pending)
  })
  const ev = current.value
  if (!ev || (ev.type !== 'output' && ev.type !== 'input'))
    return
  const load = async () => {
    const got = await detail(i).catch(() => undefined)
    if (!canceled && generation.value === recording && cursor.value === i)
      d.value = got ?? null
  }
  if (on)
    pending = setTimeout(load, 250)
  else
    load()
}, { immediate: true })

const prev = computed(() => (cursor.value > 0 ? summary.value?.events[cursor.value - 1] : undefined))
const sev = computed(() => (current.value ? severity(current.value) : undefined))

const tokens = computed<Token[]>(() => (d.value?.tokens ?? []).filter(t => showText.value || t.g !== 'text'))

// Annotation fields, with strings shown as text so messages keep their
// line breaks, and everything else as JSON.
const fields = computed(() => Object.entries(current.value?.data ?? {}).map(([k, v]) => ({
  k,
  text: typeof v === 'string',
  v: typeof v === 'string' ? v : JSON.stringify(v, null, 2),
})))

const tagList = computed(() => Object.entries(current.value?.tags ?? {}).sort((a, b) => b[1] - a[1]))
</script>

<template>
  <div v-if="!current" class="px-4 py-10 text-center text-xs text-muted-foreground">
    Before the first event. Step forward with <Kbd>→</Kbd> or pick an event.
  </div>
  <div v-else>
    <div class="flex flex-col gap-2 border-b px-3 py-3">
      <div class="flex items-center gap-2">
        <span class="size-2 rounded-full" :style="{ background: eventColor(current) }" />
        <span class="font-medium">{{ current.type === 'annotation' ? current.kind : current.type }}</span>
        <Badge v-if="sev === 'error'" variant="destructive">
          failure
        </Badge>
        <Badge v-else-if="sev === 'warn'" variant="outline" class="border-warning/40 text-warning">
          warning
        </Badge>
        <span class="tabular ml-auto font-mono text-xs text-dim">#{{ current.i }}</span>
      </div>
      <dl class="tabular flex flex-wrap gap-x-4 gap-y-1 font-mono text-xs text-muted-foreground">
        <div><dt class="inline text-dim">at </dt><dd class="inline text-foreground">{{ formatTime(current.at) }}</dd></div>
        <div v-if="prev">
          <dt class="inline text-dim">
            after prev
          </dt> <dd class="inline">
            +{{ (current.at - prev.at).toFixed(0) }}ms
          </dd>
        </div>
        <div v-if="current.size !== undefined">
          <dt class="inline text-dim">
            size
          </dt> <dd class="inline">
            {{ formatBytes(current.size) }}
          </dd>
        </div>
      </dl>
      <p v-if="current.type !== 'output' && current.type !== 'annotation' && current.label" class="font-mono text-xs break-words">
        {{ current.label }}
      </p>
      <p v-if="current.source" class="font-mono text-xs break-all" :title="current.source.func">
        <span class="text-dim">source </span>{{ formatSource(current.source) }}
        <span v-if="current.source.func" class="block text-muted-foreground">{{ shortFunc(current.source.func) }}</span>
      </p>
    </div>

    <template v-if="current.type === 'annotation'">
      <InfoSection title="Fields" :count="fields.length">
        <dl>
          <InfoRow v-for="f in fields" :key="f.k" :label="f.k" mono>
            <pre class="font-mono text-xs leading-relaxed whitespace-pre-wrap break-words" :class="f.text ? 'text-foreground' : 'text-muted-foreground'">{{ f.v }}</pre>
          </InfoRow>
        </dl>
      </InfoSection>
    </template>

    <template v-if="current.type === 'output'">
      <InfoSection v-if="tagList.length" title="Sequences" :count="tagList.reduce((n, [, c]) => n + c, 0)">
        <div class="flex flex-wrap gap-1 px-3 pb-1">
          <Badge v-for="[tag, n] in tagList" :key="tag" variant="outline" class="gap-1.5 font-mono text-[11px] font-normal">
            {{ tag }}<span class="text-dim">{{ n }}</span>
          </Badge>
        </div>
      </InfoSection>
      <InfoSection title="Tokens" :count="d?.tokens?.length">
        <template #actions>
          <label class="flex items-center gap-1.5 text-[11px] font-normal">
            <Switch v-model="showText" class="scale-75" />
            text
          </label>
        </template>
        <div v-if="!d" class="px-3 pb-2">
          <Skeleton class="h-24 w-full" />
        </div>
        <ol v-else class="font-mono text-xs">
          <li
            v-for="(t, n) in tokens.slice(0, limit)"
            :key="n"
            class="border-l-2"
            :style="{ borderLeftColor: groupColor[t.g] ?? 'transparent' }"
          >
            <component
              :is="t.f?.length ? 'button' : 'div'"
              :type="t.f?.length ? 'button' : undefined"
              :aria-expanded="t.f?.length ? open.has(n) : undefined"
              class="grid w-full grid-cols-[88px_1fr] gap-2 px-3 py-[3px] text-left hover:bg-muted"
              :class="t.f?.length && 'cursor-pointer'"
              @click="t.f?.length && toggle(n)"
            >
              <span class="flex min-w-0 items-center gap-1" :class="t.g === 'text' ? 'text-dim' : 'text-foreground'" :title="t.d">
                <span class="truncate">{{ t.n ?? 'text' }}</span>
                <ChevronRightIcon v-if="t.f?.length" class="size-3 shrink-0 text-dim transition-transform" :class="open.has(n) && 'rotate-90'" />
              </span>
              <span class="min-w-0">
                <span class="break-all" :class="t.g === 'text' ? 'text-foreground/80' : 'text-muted-foreground'">{{ t.r }}</span>
                <span v-if="t.continues" class="block font-sans text-[11px] text-dim">continues in later output</span>
                <span v-if="t.d" class="block font-sans text-[11px] text-dim">{{ t.d }}</span>
              </span>
            </component>
            <dl v-if="open.has(n)" class="mx-3 mb-1.5 ml-[100px] grid grid-cols-[minmax(0,auto)_1fr] gap-x-3 gap-y-0.5 rounded-md border bg-card px-2.5 py-1.5">
              <template v-for="(f, k) in t.f" :key="k">
                <dt class="truncate text-dim">
                  {{ f.k }}
                </dt>
                <dd class="break-all text-foreground">
                  {{ f.v }}
                </dd>
              </template>
            </dl>
          </li>
        </ol>
        <div v-if="tokens.length > limit" class="px-3 pt-2">
          <Button variant="outline" size="xs" @click="limit += 1000">
            Show {{ Math.min(1000, tokens.length - limit) }} more of {{ tokens.length - limit }}
          </Button>
        </div>
      </InfoSection>
    </template>

    <InfoSection v-if="d?.raw" title="Bytes" :count="current.size">
      <pre class="max-h-80 overflow-auto px-3 pb-2 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all text-muted-foreground scrollbar-thin">{{ d.raw }}</pre>
    </InfoSection>
  </div>
</template>
