<script setup lang="ts">
defineProps<{ kind?: 'ok' | 'error' | 'info'; message: string }>()
</script>

<template>
  <div class="inline-status" :class="['kind-' + (kind || 'info')]">
    <span class="bracket">[</span>
    <span class="text">{{ message }}</span>
    <span class="bracket">]</span>
  </div>
</template>

<style scoped>
.inline-status {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-family: var(--font-mono);
  font-size: var(--caption);
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.kind-ok { color: var(--billable); }
.kind-error { color: var(--overdue); }
.kind-info { color: var(--ink-2); }
</style>
