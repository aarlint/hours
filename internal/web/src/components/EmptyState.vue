<script setup lang="ts">
defineProps<{ title: string; desc?: string }>()
</script>

<template>
  <div class="empty">
    <div class="dots dot-grid-subtle" />
    <div class="empty-title">{{ title }}</div>
    <div v-if="desc" class="empty-desc">{{ desc }}</div>
    <div class="empty-actions"><slot /></div>
  </div>
</template>

<style scoped>
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: var(--space-4xl) var(--space-xl);
  text-align: center;
  gap: var(--space-md);
}

.dots {
  width: 120px;
  height: 40px;
  margin-bottom: var(--space-sm);
  opacity: 0.6;
}

.empty-actions:not(:empty) {
  margin-top: var(--space-md);
}
</style>
