import { onBeforeUnmount, onMounted } from 'vue'

type Handler = (e: KeyboardEvent) => void

export interface Shortcut {
  key: string          // single letter, lowercase — or 'slash', 'escape'
  mod?: boolean        // meta (⌘) or ctrl
  alt?: boolean
  shift?: boolean
  when?: () => boolean // optional guard
  handler: Handler
  allowInInput?: boolean
}

function eventMatches(e: KeyboardEvent, s: Shortcut): boolean {
  const wantMod = !!s.mod
  const hasMod = e.metaKey || e.ctrlKey
  if (wantMod !== hasMod) return false
  if (!!s.alt !== e.altKey) return false
  if (!!s.shift !== e.shiftKey) return false

  const key = e.key.toLowerCase()
  if (s.key === 'slash') return key === '/'
  if (s.key === 'escape') return key === 'escape'
  return key === s.key.toLowerCase()
}

function isTypingInto(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  const tag = el.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
  if (el.isContentEditable) return true
  return false
}

export function useShortcuts(shortcuts: Shortcut[]) {
  function onKey(e: KeyboardEvent) {
    for (const s of shortcuts) {
      if (!eventMatches(e, s)) continue
      if (s.when && !s.when()) continue
      if (!s.allowInInput && isTypingInto(e.target)) continue
      e.preventDefault()
      e.stopPropagation()
      s.handler(e)
      return
    }
  }
  onMounted(() => window.addEventListener('keydown', onKey))
  onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
}
