import { reactive } from 'vue'

export type ToastTone = 'default' | 'success' | 'warning' | 'error' | 'info'

export interface Toast {
  id: number
  tone: ToastTone
  title: string
  detail?: string
  meta?: string
  ttl: number
}

interface ToastInput {
  tone?: ToastTone
  title: string
  detail?: string
  meta?: string
  ttl?: number
}

const state = reactive<{ toasts: Toast[] }>({ toasts: [] })
let nextId = 1

export function useToasts() {
  function push(t: ToastInput): number {
    const id = nextId++
    const toast: Toast = {
      id,
      tone: t.tone ?? 'default',
      title: t.title,
      detail: t.detail,
      meta: t.meta,
      ttl: t.ttl ?? 4000,
    }
    state.toasts.push(toast)
    if (toast.ttl > 0) {
      setTimeout(() => dismiss(id), toast.ttl)
    }
    return id
  }

  function dismiss(id: number) {
    const i = state.toasts.findIndex((t) => t.id === id)
    if (i >= 0) state.toasts.splice(i, 1)
  }

  return { state, push, dismiss }
}
