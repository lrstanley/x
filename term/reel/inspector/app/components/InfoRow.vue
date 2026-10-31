<script setup lang="ts">
defineProps<{ label: string, mono?: boolean }>()
</script>

<template>
  <div class="grid grid-cols-[112px_1fr] items-baseline gap-3 px-3 py-[5px]">
    <dt class="truncate text-muted-foreground">
      {{ label }}
    </dt>
    <dd class="min-w-0 break-words" :class="mono && 'font-mono text-xs'">
      <slot />
    </dd>
  </div>
</template>
