import { reactive } from 'vue'

type Target = 'command' | 'search' | 'new-invoice' | 'start-timer'

const handlers = reactive<Record<Target, Array<() => void>>>({
  command: [],
  search: [],
  'new-invoice': [],
  'start-timer': [],
})

export function onAppAction(target: Target, fn: () => void) {
  handlers[target].push(fn)
  return () => {
    const i = handlers[target].indexOf(fn)
    if (i >= 0) handlers[target].splice(i, 1)
  }
}

export function appAction(target: Target) {
  for (const fn of handlers[target]) {
    try {
      fn()
    } catch {}
  }
}
