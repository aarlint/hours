<script setup lang="ts">
defineProps<{
  title: string
  subtitle?: string
  category?: string
}>()
</script>

<template>
  <header class="page-header">
    <div class="page-header-left">
      <span v-if="category" class="mono-label page-category">{{ category }}</span>
      <h1 class="page-title">{{ title }}</h1>
      <p v-if="subtitle" class="page-sub">{{ subtitle }}</p>
    </div>
    <div class="page-header-right">
      <slot name="actions" />
    </div>
  </header>
</template>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: var(--space-lg);
  padding-bottom: var(--space-lg);
  margin-bottom: var(--space-xl);
  border-bottom: 1px solid var(--border);
}

.page-category {
  color: var(--text-disabled);
  margin-bottom: var(--space-sm);
  display: block;
}

.page-title {
  font-family: var(--font-serif);
  font-size: 30px;
  font-weight: 400;
  line-height: 1.05;
  letter-spacing: -0.01em;
  color: var(--ink);
}

.page-sub {
  color: var(--text-secondary);
  margin-top: var(--space-sm);
  font-size: var(--body-sm);
}

.page-header-right {
  display: flex;
  gap: var(--space-sm);
  align-items: center;
}

@media (max-width: 700px) {
  .page-header {
    flex-direction: column;
    align-items: flex-start;
  }
  .page-title {
    font-size: 28px;
  }
}
</style>
