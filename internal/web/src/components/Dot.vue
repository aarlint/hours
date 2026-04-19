<script setup lang="ts">
defineProps<{
  color?: string
  size?: number
}>()
</script>

<template>
  <span
    class="dot"
    :style="{
      width: (size ?? 6) + 'px',
      height: (size ?? 6) + 'px',
      borderRadius: (size ?? 6) + 'px',
      background: color || 'var(--ink-3)',
    }"
  />
</template>

<style scoped>
.dot {
  display: inline-block;
  flex-shrink: 0;
  vertical-align: middle;
}
</style>
