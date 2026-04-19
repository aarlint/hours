<script setup lang="ts">
defineProps<{
  label: string
  value: string | number
  unit?: string
  trend?: 'up' | 'down' | 'flat'
  tone?: 'default' | 'success' | 'warning' | 'accent'
  hero?: boolean
}>()
</script>

<template>
  <div class="metric" :class="{ hero }">
    <div class="mono-label metric-label">{{ label }}</div>
    <div class="metric-value-row">
      <span class="metric-value" :class="['tone-' + (tone || 'default')]">{{ value }}</span>
      <span v-if="unit" class="metric-unit mono-label">{{ unit }}</span>
      <span v-if="trend === 'up'" class="metric-trend text-success">▲</span>
      <span v-else-if="trend === 'down'" class="metric-trend text-accent">▼</span>
    </div>
    <div v-if="$slots.detail" class="metric-detail">
      <slot name="detail" />
    </div>
  </div>
</template>

<style scoped>
.metric {
  background: var(--surface);
  border: 0.5px solid var(--rule);
  border-radius: var(--r-md);
  padding: var(--space-lg);
  min-height: 120px;
  display: flex;
  flex-direction: column;
  gap: var(--space-sm);
}

.metric.hero {
  min-height: 200px;
  padding: var(--space-xl);
  grid-column: span 2;
}

.metric-label {
  color: var(--text-disabled);
}

.metric-value-row {
  display: flex;
  align-items: baseline;
  gap: var(--space-sm);
  margin-top: auto;
}

.metric-value {
  font-family: var(--font-serif);
  font-size: 36px;
  font-weight: 400;
  color: var(--ink);
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.01em;
  line-height: 1;
}

.metric.hero .metric-value {
  font-size: 56px;
  letter-spacing: -0.02em;
}

.metric-value.tone-accent {
  color: var(--overdue);
}
.metric-value.tone-success {
  color: var(--billable);
}
.metric-value.tone-warning {
  color: var(--unbilled);
}

.metric-unit {
  color: var(--text-secondary);
}

.metric-trend {
  font-family: var(--font-mono);
  font-size: var(--caption);
}

.metric-detail {
  margin-top: var(--space-sm);
  color: var(--text-secondary);
  font-size: var(--caption);
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
}

@media (max-width: 700px) {
  .metric.hero {
    grid-column: span 1;
    padding: var(--space-lg);
  }
  .metric.hero .metric-value {
    font-size: 48px;
  }
}
</style>
