<script setup lang="ts">
defineProps<{
  size?: number | string
  color?: string
  italic?: boolean
}>()
</script>

<template>
  <span
    class="display"
    :style="{
      fontSize: (typeof size === 'number' ? size + 'px' : size) || '48px',
      color: color || undefined,
      fontStyle: italic ? 'italic' : 'normal',
    }"
  >
    <slot />
  </span>
</template>

<style scoped>
.display {
  font-family: var(--font-serif);
  font-weight: 400;
  line-height: 1;
  letter-spacing: -0.02em;
  font-variant-numeric: tabular-nums;
  color: var(--ink);
  white-space: nowrap;
}
</style>
