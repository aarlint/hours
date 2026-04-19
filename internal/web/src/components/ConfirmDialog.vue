<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useConfirm } from '../composables/useConfirm'
import Modal from './Modal.vue'

const { state, resolve } = useConfirm()

const confirmBtn = ref<HTMLButtonElement | null>(null)

function onConfirm() {
  resolve(true)
}
function onCancel() {
  resolve(false)
}
function onClose() {
  resolve(false)
}

watch(
  () => state.open,
  (open) => {
    if (open) nextTick(() => confirmBtn.value?.focus())
  },
)

function onKey(e: KeyboardEvent) {
  if (!state.open) return
  if (e.key === 'Enter') {
    e.preventDefault()
    onConfirm()
  }
}
</script>

<template>
  <Modal :open="state.open" :title="state.title" @close="onClose">
    <div class="confirm-body" @keydown="onKey">
      <p v-if="state.message" class="confirm-message">{{ state.message }}</p>
      <p v-if="state.detail" class="confirm-detail">{{ state.detail }}</p>

      <div class="modal-actions">
        <button
          v-if="state.kind === 'confirm'"
          type="button"
          class="btn btn-ghost"
          @click="onCancel"
        >
          {{ state.cancelLabel }}
        </button>
        <button
          ref="confirmBtn"
          type="button"
          class="btn"
          :class="{
            'btn-primary': state.tone !== 'danger',
            'btn-danger': state.tone === 'danger',
          }"
          @click="onConfirm"
        >
          {{ state.confirmLabel }}
        </button>
      </div>
    </div>
  </Modal>
</template>

<style scoped>
.confirm-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.confirm-message {
  font-family: var(--font-sans);
  font-size: 13.5px;
  color: var(--ink);
  line-height: 1.5;
  margin: 0;
}

.confirm-detail {
  font-family: var(--font-sans);
  font-size: 12px;
  color: var(--ink-3);
  line-height: 1.5;
  margin: 0;
}

.btn-danger {
  background: var(--overdue);
  color: #fff;
  border: 0.5px solid var(--overdue);
}
.btn-danger:hover {
  filter: brightness(1.08);
}
</style>
