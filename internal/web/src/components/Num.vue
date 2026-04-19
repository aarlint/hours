<script setup lang="ts">
defineProps<{
  size?: number | string
  weight?: number
  color?: string
}>()
</script>

<template>
  <span
    class="num"
    :style="{
      fontSize: (typeof size === 'number' ? size + 'px' : size) || undefined,
      fontWeight: weight ?? 400,
      color: color || undefined,
    }"
  >
    <slot />
  </span>
</template>

<style scoped>
.num {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.01em;
  color: var(--ink);
}
</style>
