<script setup lang="ts">
import 'vue-sonner/style.css'

const { summary, boot, restore, openFile, keepFile } = useInspector()

// The address names the cast and how it is viewed, so a reload, or each of
// several tabs, returns to its own.
onMounted(() => {
  boot()
  restore()
})

const dragging = ref(false)
let depth = 0

function onDragEnter(e: DragEvent) {
  if (!e.dataTransfer?.types.includes('Files'))
    return
  depth++
  dragging.value = true
}

function onDragLeave() {
  depth = Math.max(0, depth - 1)
  if (depth === 0)
    dragging.value = false
}

function onDrop(e: DragEvent) {
  depth = 0
  dragging.value = false
  // Open the first file dropped, and keep any others with the recent casts.
  const [first, ...rest] = [...e.dataTransfer?.files ?? []]
  if (first)
    openFile(first)
  for (const f of rest)
    keepFile(f)
}
</script>

<template>
  <TooltipProvider :delay-duration="300">
    <div
      class="flex h-full flex-col overflow-hidden"
      @dragenter.prevent="onDragEnter"
      @dragover.prevent
      @dragleave="onDragLeave"
      @drop.prevent="onDrop"
    >
      <TopBar />
      <Workspace v-if="summary" class="min-h-0 flex-1" />
      <Landing v-else class="min-h-0 flex-1" />

      <div
        v-if="dragging"
        class="pointer-events-none fixed inset-3 z-50 flex items-center justify-center rounded-xl border border-dashed border-primary bg-primary/5 backdrop-blur-[2px]"
      >
        <p class="text-base font-medium">
          Drop to open the cast
        </p>
      </div>
    </div>
    <Toaster theme="dark" position="bottom-right" />
  </TooltipProvider>
</template>
