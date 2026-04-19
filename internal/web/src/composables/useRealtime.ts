import { onBeforeUnmount, reactive, ref } from 'vue'
import { isWails, runtime } from '../wailsShim'

export type RealtimeKind =
  | 'time_entry.created'
  | 'time_entry.updated'
  | 'time_entry.deleted'
  | 'invoice.created'
  | 'invoice.updated'
  | 'invoice.deleted'
  | 'client.created'
  | 'client.updated'
  | 'contract.created'
  | 'contract.updated'

export interface RealtimeEvent {
  kind: RealtimeKind
  at: string
  summary?: string
  detail?: Record<string, unknown>
}

type Listener = (ev: RealtimeEvent) => void

const listeners = new Set<Listener>()
const connectionState = reactive({
  connected: false,
  attempts: 0,
  lastHeartbeat: 0,
})

let source: EventSource | null = null
let reconnectTimer: number | undefined
let started = false
const wailsOffFns: Array<() => void> = []

const KINDS: RealtimeKind[] = [
  'time_entry.created',
  'time_entry.updated',
  'time_entry.deleted',
  'invoice.created',
  'invoice.updated',
  'invoice.deleted',
  'client.created',
  'client.updated',
  'contract.created',
  'contract.updated',
]

function dispatch(ev: RealtimeEvent) {
  for (const fn of listeners) {
    try {
      fn(ev)
    } catch (err) {
      console.error('[realtime] listener error', err)
    }
  }
}

function connectWails() {
  const r = runtime()
  connectionState.connected = true
  connectionState.attempts = 0
  connectionState.lastHeartbeat = Date.now()

  wailsOffFns.push(
    r.EventsOn('hello', () => {
      connectionState.lastHeartbeat = Date.now()
    }),
  )

  for (const kind of KINDS) {
    wailsOffFns.push(
      r.EventsOn(kind, (data: any) => {
        connectionState.lastHeartbeat = Date.now()
        dispatch({
          kind,
          at: data?.at ?? new Date().toISOString(),
          summary: data?.summary,
          detail: data,
        })
      }),
    )
  }
}

function connectSSE() {
  if (source) return
  try {
    source = new EventSource('/api/events')
  } catch (err) {
    console.error('[realtime] failed to open SSE', err)
    scheduleReconnect()
    return
  }

  source.onopen = () => {
    connectionState.connected = true
    connectionState.attempts = 0
  }
  source.onerror = () => {
    connectionState.connected = false
    if (source) {
      source.close()
      source = null
    }
    scheduleReconnect()
  }
  source.addEventListener('hello', (ev) => {
    try {
      const data = JSON.parse((ev as MessageEvent).data)
      connectionState.lastHeartbeat = Date.now()
      dispatch({ kind: 'time_entry.created', at: new Date().toISOString(), detail: data })
    } catch {}
  })
  source.addEventListener('heartbeat', () => {
    connectionState.lastHeartbeat = Date.now()
  })

  for (const kind of KINDS) {
    source.addEventListener(kind, (ev) => {
      try {
        const msg = ev as MessageEvent
        const data = JSON.parse(msg.data)
        dispatch({ kind, at: data.at ?? new Date().toISOString(), summary: data.summary, detail: data })
      } catch (err) {
        console.error('[realtime] parse error', err)
      }
    })
  }
}

function scheduleReconnect() {
  if (reconnectTimer !== undefined) return
  const attempt = ++connectionState.attempts
  const delay = Math.min(30_000, 1000 * Math.pow(1.6, Math.min(attempt, 8)))
  reconnectTimer = window.setTimeout(() => {
    reconnectTimer = undefined
    connectSSE()
  }, delay)
}

export function startRealtime() {
  if (started) return
  started = true
  if (isWails()) {
    connectWails()
  } else {
    connectSSE()
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible' && !source) connectSSE()
    })
  }
}

export function onRealtime(fn: Listener) {
  listeners.add(fn)
  onBeforeUnmount(() => listeners.delete(fn))
}

export function useRealtimeStatus() {
  return connectionState
}

export const changeTick = ref(0)

listeners.add(() => {
  changeTick.value++
})

const ownIds = new Set<string>()
export function markOwn(id: string) {
  ownIds.add(id)
  setTimeout(() => ownIds.delete(id), 15_000)
}
export function isOwn(id: string | undefined | null): boolean {
  if (!id) return false
  return ownIds.has(id)
}
