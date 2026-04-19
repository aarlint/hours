<script setup lang="ts">
import { useToasts } from '../composables/useToasts'

const { state, dismiss } = useToasts()
</script>

<template>
  <Teleport to="body">
    <div class="toast-stack" aria-live="polite" aria-atomic="false">
      <TransitionGroup name="toast">
        <div
          v-for="t in state.toasts"
          :key="t.id"
          class="toast"
          :class="['tone-' + t.tone]"
          @click="dismiss(t.id)"
        >
          <span class="bar" />
          <div class="body">
            <div class="title">
              <span class="mark" v-if="t.tone !== 'default'" :class="['mark-' + t.tone]" />
              {{ t.title }}
            </div>
            <div v-if="t.detail" class="detail">{{ t.detail }}</div>
            <div v-if="t.meta" class="meta">{{ t.meta }}</div>
          </div>
          <span class="close">×</span>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.toast-stack {
  position: fixed;
  right: 20px;
  bottom: 20px;
  display: flex;
  flex-direction: column-reverse;
  gap: 8px;
  z-index: 400;
  max-width: 360px;
  pointer-events: none;
}

.toast {
  pointer-events: auto;
  display: flex;
  align-items: stretch;
  gap: 0;
  background: var(--surface);
  border: 0.5px solid var(--rule-strong);
  border-radius: var(--r-md);
  box-shadow: var(--shadow-pop);
  overflow: hidden;
  cursor: pointer;
  min-width: 240px;
}

.bar {
  width: 3px;
  background: var(--ink-3);
  flex-shrink: 0;
}

.tone-success .bar { background: var(--billable); }
.tone-warning .bar { background: var(--unbilled); }
.tone-error   .bar { background: var(--overdue); }
.tone-info    .bar { background: var(--accent); }

.body {
  flex: 1;
  padding: 10px 12px 10px 14px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.title {
  font-family: var(--font-sans);
  font-size: 13px;
  font-weight: 500;
  color: var(--ink);
  display: flex;
  align-items: center;
  gap: 8px;
}

.mark {
  width: 6px;
  height: 6px;
  border-radius: 6px;
  background: var(--ink-3);
}
.mark-success { background: var(--billable); }
.mark-warning { background: var(--unbilled); }
.mark-error   { background: var(--overdue); }
.mark-info    { background: var(--accent); }

.detail {
  font-size: 12px;
  color: var(--ink-2);
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
}

.meta {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--ink-3);
  margin-top: 2px;
  letter-spacing: 0;
}

.close {
  padding: 10px 12px 10px 8px;
  color: var(--ink-3);
  font-size: 14px;
  line-height: 1;
  user-select: none;
  align-self: flex-start;
}
.toast:hover .close { color: var(--ink); }

.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(8px);
}
.toast-enter-active,
.toast-leave-active {
  transition: opacity 180ms ease-out, transform 180ms ease-out;
}
</style>
