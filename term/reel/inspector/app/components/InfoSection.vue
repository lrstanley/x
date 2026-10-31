<script setup lang="ts">
defineProps<{ title: string, count?: number | string }>()
</script>

<template>
  <section class="border-b py-2 last:border-b-0">
    <h3 class="flex items-center gap-2 px-3 pt-1 pb-1.5 text-xs font-medium text-muted-foreground">
      {{ title }}
      <span v-if="count !== undefined" class="tabular font-mono text-[11px] text-dim">{{ count }}</span>
      <span class="ml-auto flex items-center gap-1"><slot name="actions" /></span>
    </h3>
    <slot />
  </section>
</template>
