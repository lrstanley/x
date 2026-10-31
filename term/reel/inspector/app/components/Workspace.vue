<script setup lang="ts">
import { useEventListener } from '@vueuse/core'
import { severity } from '~/lib/lanes'

const { step, jump, togglePlay, seek, summary } = useInspector()

const back = new Set(['ArrowLeft', 'ArrowUp', 'k'])
const forward = new Set(['ArrowRight', 'ArrowDown', 'j'])

// Keys work anywhere but in fields and widgets with their own arrow keys:
// arrows (or j/k) step through events, with shift between marks and with
// alt between failures and warnings; space plays; Home and End go to the
// ends.
useEventListener('keydown', (e: KeyboardEvent) => {
  const t = e.target instanceof Element ? e.target : document.body
  if (e.ctrlKey || e.metaKey || t.closest('input, textarea, select, [contenteditable=true], [role=tablist], [role=menu], [role=slider]'))
    return
  const dir = forward.has(e.key) ? 1 : back.has(e.key) ? -1 : 0
  if (dir) {
    e.preventDefault()
    if (e.shiftKey)
      jump(dir, ev => ev.type === 'mark')
    else if (e.altKey)
      jump(dir, ev => severity(ev) !== undefined)
    else
      step(dir)
    return
  }
  // Space plays from the event list's rows too, which are buttons, but not
  // from other buttons, which it presses.
  if (e.key === ' ' && !t.closest('button:not([role=option])')) {
    e.preventDefault()
    togglePlay()
  }
  else if (e.key === 'Home') {
    e.preventDefault()
    seek(-1)
  }
  else if (e.key === 'End') {
    e.preventDefault()
    seek((summary.value?.events.length ?? 0) - 1)
  }
})
</script>

<template>
  <ResizablePanelGroup direction="vertical" auto-save-id="reel-inspector-v" class="bg-background">
    <ResizablePanel :default-size="64" :min-size="30">
      <ResizablePanelGroup direction="horizontal" auto-save-id="reel-inspector-h">
        <ResizablePanel :default-size="24" :min-size="14" class="bg-panel">
          <EventList />
        </ResizablePanel>
        <ResizableHandle />
        <ResizablePanel :default-size="48" :min-size="25">
          <FrameView />
        </ResizablePanel>
        <ResizableHandle />
        <ResizablePanel :default-size="28" :min-size="18" class="bg-panel">
          <SidePanel />
        </ResizablePanel>
      </ResizablePanelGroup>
    </ResizablePanel>
    <ResizableHandle />
    <ResizablePanel :default-size="36" :min-size="16" class="bg-panel">
      <Timeline />
    </ResizablePanel>
  </ResizablePanelGroup>
</template>
