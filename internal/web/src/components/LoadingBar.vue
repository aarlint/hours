<script setup lang="ts">
defineProps<{ text?: string }>()
</script>

<template>
  <div class="loading-bar">
    <div class="bar">
      <span v-for="i in 12" :key="i" />
    </div>
    <div class="loading">{{ text || 'LOADING' }}</div>
  </div>
</template>

<style scoped>
.loading-bar {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-md);
  padding: var(--space-3xl) var(--space-xl);
}

.bar {
  display: flex;
  gap: 2px;
  width: 160px;
}

.bar span {
  flex: 1;
  height: 10px;
  background: var(--rule);
  animation: pulse-seg 1.2s ease-in-out infinite;
}

.bar span:nth-child(1) { animation-delay: 0s; }
.bar span:nth-child(2) { animation-delay: 0.08s; }
.bar span:nth-child(3) { animation-delay: 0.16s; }
.bar span:nth-child(4) { animation-delay: 0.24s; }
.bar span:nth-child(5) { animation-delay: 0.32s; }
.bar span:nth-child(6) { animation-delay: 0.40s; }
.bar span:nth-child(7) { animation-delay: 0.48s; }
.bar span:nth-child(8) { animation-delay: 0.56s; }
.bar span:nth-child(9) { animation-delay: 0.64s; }
.bar span:nth-child(10) { animation-delay: 0.72s; }
.bar span:nth-child(11) { animation-delay: 0.80s; }
.bar span:nth-child(12) { animation-delay: 0.88s; }

@keyframes pulse-seg {
  0%, 100% { background: var(--rule); }
  50% { background: var(--ink); }
}
</style>
