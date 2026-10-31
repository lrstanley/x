<script setup lang="ts">
import { cursorShapes } from '~/lib/format'

const { frame } = useInspector()
const st = computed(() => frame.value?.state)

const yesNo = (v: boolean | undefined) => (v ? 'yes' : 'no')
</script>

<template>
  <div v-if="!st" class="p-3">
    <Skeleton class="h-40 w-full" />
  </div>
  <div v-else>
    <InfoSection title="Screen">
      <dl>
        <InfoRow label="Title">
          <span :class="!st.title && 'text-dim'">{{ st.title || 'none' }}</span>
        </InfoRow>
        <InfoRow v-if="st.working_directory" label="Directory" mono>
          {{ st.working_directory }}
        </InfoRow>
        <InfoRow label="Size" mono>
          {{ frame!.cols }}×{{ frame!.rows }}
        </InfoRow>
        <InfoRow label="Alt screen">
          {{ yesNo(st.alt_screen) }}
        </InfoRow>
        <InfoRow label="Focused">
          {{ yesNo(st.focused) }}
        </InfoRow>
        <InfoRow label="Reverse video">
          {{ yesNo(st.reverse_colors) }}
        </InfoRow>
        <InfoRow label="Scrollback" mono>
          {{ st.scrollback_len }} rows{{ st.scrollback_offset ? `, scrolled up ${st.scrollback_offset}` : '' }}
        </InfoRow>
        <InfoRow label="Generation" mono>
          {{ st.generation }}
        </InfoRow>
      </dl>
    </InfoSection>

    <InfoSection title="Cursor">
      <dl>
        <InfoRow label="Position" mono>
          {{ st.cursor.X }}, {{ st.cursor.Y }}
        </InfoRow>
        <InfoRow label="Visible">
          {{ yesNo(st.cursor.Visible) }}
        </InfoRow>
        <InfoRow label="Shape">
          {{ cursorShapes[st.cursor.Shape] ?? st.cursor.Shape }}{{ st.cursor.Blink ? ', blinking' : '' }}
        </InfoRow>
        <InfoRow label="Color" mono>
          <span class="flex items-center gap-1.5">
            <span v-if="st.cursor_color" class="size-3 rounded-[3px] border border-border-strong" :style="{ background: st.cursor_color }" />
            {{ st.cursor_color || 'renderer default' }}
          </span>
        </InfoRow>
      </dl>
    </InfoSection>

    <InfoSection title="Defaults">
      <dl>
        <InfoRow v-for="[k, v] in [['Foreground', st.fg], ['Background', st.bg]]" :key="k" :label="k!" mono>
          <span class="flex items-center gap-1.5">
            <span class="size-3 rounded-[3px] border border-border-strong" :style="{ background: v }" />{{ v }}
          </span>
        </InfoRow>
      </dl>
    </InfoSection>

    <InfoSection title="Signals">
      <dl>
        <InfoRow label="Progress" mono>
          <span :class="!st.progress && 'text-dim'">{{ st.progress || 'none' }}</span>
        </InfoRow>
        <InfoRow label="Bells" mono>
          {{ st.bells }}
        </InfoRow>
        <InfoRow label="Prompt" mono>
          <span :class="!st.prompt && 'text-dim'">{{ st.prompt || 'none' }}</span>
        </InfoRow>
        <InfoRow v-if="st.clipboard" label="Clipboard" mono>
          {{ st.clipboard }}
        </InfoRow>
      </dl>
    </InfoSection>

    <InfoSection v-if="st.prompt_marks?.length" title="Prompt marks" :count="st.prompt_marks.length">
      <dl>
        <InfoRow v-for="(m, n) in st.prompt_marks" :key="n" :label="`row ${m.Row}`" mono>
          {{ m.Ended ? (m.HasExitCode ? `exit ${m.ExitCode}` : 'ended') : 'running' }}
        </InfoRow>
      </dl>
    </InfoSection>

    <InfoSection v-if="st.notifications?.length" title="Notifications" :count="st.notifications.length">
      <ul class="flex flex-col gap-1 px-3 pb-1">
        <li v-for="(n, k) in st.notifications" :key="k" class="rounded-md border bg-card px-2.5 py-1.5">
          <p class="font-medium">
            {{ n.Title || 'Untitled' }}
          </p>
          <p class="text-xs text-muted-foreground">
            {{ n.Body }}
          </p>
        </li>
      </ul>
    </InfoSection>

    <InfoSection v-if="st.program_status?.length" title="Program status" :count="st.program_status.length">
      <dl>
        <InfoRow v-for="(p, k) in st.program_status" :key="k" :label="p.id || 'root'" mono>
          {{ p.state }}{{ p.kind ? ` · ${p.kind}` : '' }}{{ p.progress !== undefined ? ` · ${p.progress}%` : '' }}
          <span v-if="p.title || p.message" class="block font-sans text-muted-foreground">{{ [p.app, p.title, p.message].filter(Boolean).join(' — ') }}</span>
        </InfoRow>
      </dl>
    </InfoSection>

    <InfoSection v-if="st.palette_set" title="Palette set by the program" :count="Object.keys(st.palette_set).length">
      <div class="flex flex-wrap gap-1.5 px-3 pb-1">
        <span v-for="(hex, idx) in st.palette_set" :key="idx" class="flex items-center gap-1.5 rounded-md border px-1.5 py-0.5 font-mono text-[11px]">
          <span class="size-3 rounded-[3px]" :style="{ background: hex }" />{{ idx }} {{ hex }}
        </span>
      </div>
    </InfoSection>
  </div>
</template>
