import { reactive } from 'vue'

export type ConfirmTone = 'default' | 'danger' | 'warning' | 'info'

export interface ConfirmOptions {
  title: string
  message?: string
  detail?: string
  confirmLabel?: string
  cancelLabel?: string
  tone?: ConfirmTone
}

export interface AlertOptions {
  title: string
  message?: string
  detail?: string
  confirmLabel?: string
  tone?: ConfirmTone
}

type Kind = 'confirm' | 'alert'

interface DialogState {
  open: boolean
  kind: Kind
  title: string
  message: string
  detail: string
  confirmLabel: string
  cancelLabel: string
  tone: ConfirmTone
}

const state = reactive<DialogState>({
  open: false,
  kind: 'confirm',
  title: '',
  message: '',
  detail: '',
  confirmLabel: 'Confirm',
  cancelLabel: 'Cancel',
  tone: 'default',
})

let resolver: ((value: boolean) => void) | null = null

function close(result: boolean) {
  state.open = false
  const r = resolver
  resolver = null
  if (r) r(result)
}

export function useConfirm() {
  function confirm(opts: ConfirmOptions): Promise<boolean> {
    if (resolver) resolver(false)
    state.kind = 'confirm'
    state.title = opts.title
    state.message = opts.message ?? ''
    state.detail = opts.detail ?? ''
    state.confirmLabel = opts.confirmLabel ?? 'Confirm'
    state.cancelLabel = opts.cancelLabel ?? 'Cancel'
    state.tone = opts.tone ?? 'default'
    state.open = true
    return new Promise<boolean>((resolve) => {
      resolver = resolve
    })
  }

  function alert(opts: AlertOptions): Promise<void> {
    if (resolver) resolver(false)
    state.kind = 'alert'
    state.title = opts.title
    state.message = opts.message ?? ''
    state.detail = opts.detail ?? ''
    state.confirmLabel = opts.confirmLabel ?? 'OK'
    state.cancelLabel = ''
    state.tone = opts.tone ?? 'default'
    state.open = true
    return new Promise<void>((resolve) => {
      resolver = () => resolve()
    })
  }

  return { state, confirm, alert, resolve: close }
}
