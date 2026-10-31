<script setup lang="ts">
const { openFile } = useInspector()
const input = useTemplateRef<HTMLInputElement>('input')

function onChange() {
  const file = input.value?.files?.[0]
  if (file)
    openFile(file)
  if (input.value)
    input.value.value = ''
}

defineExpose({ open: () => input.value?.click() })
</script>

<template>
  <input ref="input" type="file" accept=".cast,application/x-asciicast,application/json,text/plain" class="hidden" @change="onChange">
</template>
