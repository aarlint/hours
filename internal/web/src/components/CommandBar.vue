<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Client, Contract } from '../types'
import { api } from '../api'
import { useToasts } from '../composables/useToasts'
import { changeTick, markOwn } from '../composables/useRealtime'
import Kbd from './Kbd.vue'
import { isoDate } from './primitives'

const props = defineProps<{
  clients: Client[]
  contracts: Contract[]
  today?: Date
}>()

const emit = defineEmits<{
  (e: 'logged', entry: { hours: number; contract_id: number; date: string; description: string }): void
}>()

const val = ref('')
const focused = ref(false)
const inputRef = ref<HTMLInputElement | null>(null)
const saving = ref(false)
const toasts = useToasts()

const selectedContract = ref<Contract | null>(null)
const openPicker = ref<'contract' | 'client' | null>(null)
const pickerFilter = ref('')
const pickerRef = ref<HTMLInputElement | null>(null)

function focus() {
  inputRef.value?.focus()
  inputRef.value?.select()
}
defineExpose({ focus })

interface Parsed {
  hours: number | null
  contract: Contract | null
  client: Client | null
  whenIso: string | null
  whenLabel: string | null
  desc: string | null
  raw: string
}

function dayOfWeek(offsetDays: number): string {
  const d = new Date(props.today ?? new Date())
  d.setDate(d.getDate() + offsetDays)
  return isoDate(d)
}

function previousWeekday(targetDow: number): string {
  const d = new Date(props.today ?? new Date())
  const today = d.getDay()
  let diff = (today - targetDow + 7) % 7
  if (diff === 0) diff = 7
  d.setDate(d.getDate() - diff)
  return isoDate(d)
}

function parseCommand(input: string): Parsed {
  const out: Parsed = {
    hours: null,
    contract: null,
    client: null,
    whenIso: null,
    whenLabel: null,
    desc: null,
    raw: input,
  }
  if (!input.trim()) return out
  const s = input.toLowerCase()

  const hM = s.match(/(\d+(?:\.\d+)?)\s*(h|hr|hrs|hour|hours)\b/)
  const mM = s.match(/(\d+)\s*(m|min|mins|minutes)\b/)
  if (hM) out.hours = parseFloat(hM[1])
  else if (mM) out.hours = Math.round(parseInt(mM[1]) / 15) * 0.25

  if (/\btoday\b/.test(s)) {
    out.whenIso = dayOfWeek(0)
    out.whenLabel = 'Today'
  } else if (/\byesterday\b/.test(s)) {
    out.whenIso = dayOfWeek(-1)
    out.whenLabel = 'Yesterday'
  } else {
    const weekdays: [RegExp, number, string][] = [
      [/\bmon(day)?\b/, 1, 'Monday'],
      [/\btue(s(day)?)?\b/, 2, 'Tuesday'],
      [/\bwed(n(esday)?)?\b/, 3, 'Wednesday'],
      [/\bthu(r(s(day)?)?)?\b/, 4, 'Thursday'],
      [/\bfri(day)?\b/, 5, 'Friday'],
      [/\bsat(urday)?\b/, 6, 'Saturday'],
      [/\bsun(day)?\b/, 0, 'Sunday'],
    ]
    for (const [rx, dow, label] of weekdays) {
      if (rx.test(s)) {
        out.whenIso = previousWeekday(dow)
        out.whenLabel = label
        break
      }
    }
  }

  const numM = input.match(/\b([A-Za-z]{1,5}[-_][A-Za-z0-9-]+)\b/)
  if (numM) {
    const n = numM[1].toUpperCase()
    const c = props.contracts.find((x) => x.contract_number.toUpperCase() === n)
    if (c) {
      out.contract = c
      out.client = props.clients.find((cl) => cl.id === c.client_id) || null
    }
  }
  if (!out.contract) {
    const cl = props.clients.find((x) => {
      const first = x.name.split(/\s+/)[0]?.toLowerCase() || ''
      return first.length > 2 && s.includes(first)
    })
    if (cl) {
      out.client = cl
      out.contract =
        props.contracts.find((x) => x.client_id === cl.id && x.status === 'active') ||
        props.contracts.find((x) => x.client_id === cl.id) ||
        null
    }
  }

  let clean = input
    .replace(/\b\d+(?:\.\d+)?\s*(h|hr|hrs|hour|hours|m|min|mins|minutes)\b/gi, '')
    .replace(/\btoday|yesterday|monday|tuesday|wednesday|thursday|friday|saturday|sunday\b/gi, '')
    .replace(/\b[A-Za-z]{1,5}[-_][A-Za-z0-9-]+\b/g, '')
    .replace(/\s+for\s+/gi, ' ')
    .replace(/^\s*(add|log)\s+/i, '')
    .trim()
  if (out.client) {
    const first = out.client.name.split(/\s+/)[0]
    if (first) clean = clean.replace(new RegExp('\\b' + first + '\\b', 'i'), '').trim()
  }
  clean = clean.replace(/\s{2,}/g, ' ').replace(/^[-—–:·.,]+\s*/, '').trim()
  if (clean.length >= 2) out.desc = clean

  return out
}

const parsed = computed(() => parseCommand(val.value))

const primaryContract = computed<Contract | null>(() => {
  const actives = props.contracts.filter((c) => c.status === 'active')
  const sorted = [...actives].sort((a, b) => b.id - a.id)
  return sorted[0] || props.contracts[0] || null
})

watch(
  primaryContract,
  (p) => {
    if (!selectedContract.value && p) selectedContract.value = p
  },
  { immediate: true },
)

watch(
  () => parsed.value.contract,
  (c) => {
    if (c) selectedContract.value = c
  },
)

const effectiveContract = computed<Contract | null>(
  () => selectedContract.value || primaryContract.value,
)
const effectiveClient = computed<Client | null>(() => {
  const c = effectiveContract.value
  if (!c) return null
  return props.clients.find((cl) => cl.id === c.client_id) || null
})

const complete = computed(
  () => parsed.value.hours != null && parsed.value.hours > 0 && effectiveContract.value != null,
)

const billableAmount = computed(() => {
  const p = parsed.value
  const k = effectiveContract.value
  if (!p.hours || !k) return null
  return p.hours * k.hourly_rate
})

async function submit() {
  if (!complete.value || saving.value) return
  const p = parsed.value
  const k = effectiveContract.value!
  const cl = effectiveClient.value
  const date = p.whenIso || isoDate(props.today ?? new Date())
  saving.value = true
  try {
    const res = await api.addTimeEntry({
      contract_id: k.id,
      hours: p.hours!,
      date,
      description: p.desc || '',
    })
    if (res?.id) markOwn(res.id)
    emit('logged', {
      hours: p.hours!,
      contract_id: k.id,
      date,
      description: p.desc || '',
    })
    toasts.push({
      tone: 'success',
      title: `Logged ${p.hours!.toFixed(2)}h`,
      detail: `${cl?.name ?? ''} · ${k.contract_number}`,
      meta: p.desc || p.whenLabel || '',
    })
    val.value = ''
    selectedContract.value = primaryContract.value
    changeTick.value++
  } catch (e: any) {
    toasts.push({ tone: 'error', title: 'Could not log entry', detail: e?.message || String(e) })
  } finally {
    saving.value = false
    nextTick(() => inputRef.value?.focus())
  }
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (openPicker.value) {
      openPicker.value = null
      return
    }
    val.value = ''
    inputRef.value?.blur()
  }
}

function togglePicker(kind: 'contract' | 'client') {
  if (openPicker.value === kind) {
    openPicker.value = null
    return
  }
  openPicker.value = kind
  pickerFilter.value = ''
  nextTick(() => pickerRef.value?.focus())
}

function pickContract(c: Contract) {
  selectedContract.value = c
  openPicker.value = null
  nextTick(() => inputRef.value?.focus())
}

function pickClient(cl: Client) {
  const next =
    props.contracts.find((k) => k.client_id === cl.id && k.status === 'active') ||
    props.contracts.find((k) => k.client_id === cl.id) ||
    null
  if (next) selectedContract.value = next
  openPicker.value = null
  nextTick(() => inputRef.value?.focus())
}

const contractOptions = computed(() => {
  const q = pickerFilter.value.trim().toLowerCase()
  const rows = props.contracts.map((c) => ({
    contract: c,
    client: props.clients.find((cl) => cl.id === c.client_id) || null,
  }))
  const filtered = q
    ? rows.filter(
        (r) =>
          r.contract.contract_number.toLowerCase().includes(q) ||
          r.contract.name?.toLowerCase().includes(q) ||
          r.client?.name.toLowerCase().includes(q),
      )
    : rows
  return filtered.sort((a, b) => {
    const as = a.contract.status === 'active' ? 0 : 1
    const bs = b.contract.status === 'active' ? 0 : 1
    if (as !== bs) return as - bs
    return b.contract.id - a.contract.id
  })
})

const clientOptions = computed(() => {
  const q = pickerFilter.value.trim().toLowerCase()
  const filtered = q
    ? props.clients.filter((c) => c.name.toLowerCase().includes(q))
    : props.clients
  return [...filtered].sort((a, b) => (b.active_contracts ?? 0) - (a.active_contracts ?? 0))
})

function onGlobalClick(e: MouseEvent) {
  if (!openPicker.value) return
  const t = e.target as HTMLElement | null
  if (!t) return
  if (t.closest('.picker-host')) return
  openPicker.value = null
}

onMounted(() => {
  document.addEventListener('mousedown', onGlobalClick)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onGlobalClick)
})

const previewOpen = computed(() => !!val.value || focused.value || openPicker.value !== null)

function onBlur(e: FocusEvent) {
  const next = e.relatedTarget as HTMLElement | null
  if (next && next.closest('.command-bar')) return
  setTimeout(() => {
    if (openPicker.value) return
    focused.value = false
  }, 0)
}
</script>

<template>
  <div class="command-bar">
    <div class="bar" :class="{ focused }">
      <span class="caret">›</span>
      <input
        ref="inputRef"
        v-model="val"
        type="text"
        placeholder="add 2h today — API rate-limit middleware"
        spellcheck="false"
        autocomplete="off"
        @focus="focused = true"
        @blur="onBlur"
        @keydown="onKey"
        @keydown.enter.prevent="submit"
      />
      <span v-if="val" class="clear" @mousedown.prevent="val = ''">×</span>
      <span class="divider" />
      <Kbd>⌘</Kbd><Kbd>K</Kbd>
    </div>

    <div v-if="previewOpen" class="preview">
      <div class="preview-label">
        {{ saving ? 'Saving…' : complete ? 'Ready — press ⏎ to log' : !parsed.hours ? 'Type hours, e.g. 2h' : 'Pick a contract' }}
      </div>
      <div class="chips">
        <div class="chip">
          <span class="chip-label">Hours</span>
          <span class="chip-val" :class="{ unresolved: parsed.hours == null }">
            {{ parsed.hours != null ? parsed.hours.toFixed(2) + 'h' : '—' }}
          </span>
        </div>

        <div class="chip picker-host">
          <span class="chip-label">Contract</span>
          <button
            type="button"
            class="chip-btn"
            :class="{ unresolved: !effectiveContract, active: openPicker === 'contract' }"
            @mousedown.prevent="togglePicker('contract')"
          >
            <span class="chip-val-text">
              {{ effectiveContract?.contract_number ?? '—' }}
            </span>
            <span class="chev">▾</span>
          </button>
          <div v-if="openPicker === 'contract'" class="picker">
            <input
              ref="pickerRef"
              v-model="pickerFilter"
              class="picker-search"
              type="text"
              placeholder="Search contracts…"
              spellcheck="false"
              autocomplete="off"
              @keydown.escape.prevent="openPicker = null"
            />
            <div class="picker-list">
              <button
                v-for="row in contractOptions"
                :key="row.contract.id"
                type="button"
                class="picker-item"
                :class="{
                  active: row.contract.id === effectiveContract?.id,
                  inactive: row.contract.status !== 'active',
                }"
                @mousedown.prevent="pickContract(row.contract)"
              >
                <span class="pi-num">{{ row.contract.contract_number }}</span>
                <span class="pi-name">{{ row.contract.name || row.client?.name }}</span>
                <span class="pi-meta">${{ row.contract.hourly_rate }}/hr</span>
              </button>
              <div v-if="!contractOptions.length" class="picker-empty">No contracts</div>
            </div>
          </div>
        </div>

        <div class="chip picker-host">
          <span class="chip-label">Client</span>
          <button
            type="button"
            class="chip-btn"
            :class="{ unresolved: !effectiveClient, active: openPicker === 'client' }"
            @mousedown.prevent="togglePicker('client')"
          >
            <span class="chip-val-text">
              {{ effectiveClient?.name ?? '—' }}
            </span>
            <span class="chev">▾</span>
          </button>
          <div v-if="openPicker === 'client'" class="picker">
            <input
              ref="pickerRef"
              v-model="pickerFilter"
              class="picker-search"
              type="text"
              placeholder="Search clients…"
              spellcheck="false"
              autocomplete="off"
              @keydown.escape.prevent="openPicker = null"
            />
            <div class="picker-list">
              <button
                v-for="cl in clientOptions"
                :key="cl.id"
                type="button"
                class="picker-item"
                :class="{ active: cl.id === effectiveClient?.id }"
                @mousedown.prevent="pickClient(cl)"
              >
                <span class="pi-name wide">{{ cl.name }}</span>
                <span class="pi-meta">{{ cl.active_contracts }} active</span>
              </button>
              <div v-if="!clientOptions.length" class="picker-empty">No clients</div>
            </div>
          </div>
        </div>

        <div class="chip">
          <span class="chip-label">When</span>
          <span class="chip-val" :class="{ unresolved: !parsed.whenLabel }">
            {{ parsed.whenLabel ?? 'Today' }}
          </span>
        </div>
        <div class="chip wide">
          <span class="chip-label">Note</span>
          <span class="chip-val note" :class="{ unresolved: !parsed.desc }">
            {{ parsed.desc || '—' }}
          </span>
        </div>
      </div>
      <div v-if="complete" class="preview-foot">
        <span class="bill">
          Billable at
          <span class="num">${{ effectiveContract!.hourly_rate }}</span
          >/hr =
          <span class="num strong">${{ billableAmount!.toFixed(2) }}</span>
        </span>
        <span class="hint"><Kbd>⏎</Kbd> Log entry</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.command-bar {
  position: relative;
}
.bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 12px;
  height: 40px;
  background: var(--surface);
  border: 0.5px solid var(--rule);
  border-radius: var(--r-md);
  transition: border-color var(--duration-fast) var(--ease-out);
}
.bar.focused {
  border-color: var(--rule-strong);
}
.caret {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--ink-3);
  user-select: none;
}
.bar input {
  flex: 1;
  border: none;
  outline: none;
  background: transparent;
  font-family: var(--font-sans);
  font-size: 13.5px;
  color: var(--ink);
  padding: 0;
}
.bar input::placeholder {
  color: var(--ink-4);
}
.clear {
  color: var(--ink-3);
  cursor: pointer;
  font-size: 14px;
  padding: 0 4px;
  user-select: none;
}
.divider {
  width: 0.5px;
  height: 18px;
  background: var(--rule);
}

.preview {
  position: absolute;
  left: 0;
  right: 0;
  top: 100%;
  margin-top: 4px;
  background: var(--surface);
  border: 0.5px solid var(--rule-strong);
  border-radius: var(--r-md);
  padding: 10px 14px;
  box-shadow: var(--shadow-pop);
  z-index: 40;
}
.preview-label {
  font-family: var(--font-sans);
  font-size: 10.5px;
  color: var(--ink-3);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  font-weight: 500;
  margin-bottom: 6px;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  align-items: stretch;
}
.chip {
  display: flex;
  flex-direction: column;
  padding-right: 18px;
  margin-right: 0;
  border-right: 0.5px solid var(--rule);
  min-width: 0;
  position: relative;
}
.chip:last-child {
  border-right: none;
}
.chip + .chip {
  padding-left: 12px;
}
.chip.wide {
  max-width: 280px;
}
.chip-label {
  font-family: var(--font-sans);
  font-size: 9.5px;
  color: var(--ink-4);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  margin-bottom: 2px;
}
.chip-val {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--ink);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.chip-val.note {
  font-family: var(--font-sans);
}
.chip-val.unresolved {
  color: var(--ink-4);
}

.chip-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: none;
  background: transparent;
  padding: 0;
  margin: 0;
  cursor: pointer;
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--ink);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  max-width: 200px;
  transition: color var(--duration-fast) var(--ease-out);
}
.chip-btn:hover {
  color: var(--accent);
}
.chip-btn.unresolved {
  color: var(--ink-4);
}
.chip-btn.active {
  color: var(--accent);
}
.chip-val-text {
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 180px;
}
.chev {
  font-size: 9px;
  color: var(--ink-4);
  line-height: 1;
}
.chip-btn.active .chev,
.chip-btn:hover .chev {
  color: var(--accent);
}

.picker {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  width: 320px;
  max-width: 90vw;
  background: var(--surface);
  border: 0.5px solid var(--rule-strong);
  border-radius: var(--r-md);
  box-shadow: var(--shadow-pop);
  z-index: 50;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.picker-search {
  border: none;
  outline: none;
  background: transparent;
  font-family: var(--font-sans);
  font-size: 12.5px;
  color: var(--ink);
  padding: 8px 12px;
  border-bottom: 0.5px solid var(--rule);
}
.picker-search::placeholder {
  color: var(--ink-4);
}
.picker-list {
  max-height: 280px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}
.picker-item {
  display: grid;
  grid-template-columns: 90px 1fr auto;
  align-items: baseline;
  gap: 10px;
  padding: 7px 12px;
  border: none;
  background: transparent;
  cursor: pointer;
  text-align: left;
  border-bottom: 0.5px solid var(--rule);
  transition: background var(--duration-fast) var(--ease-out);
}
.picker-item:last-child {
  border-bottom: none;
}
.picker-item:hover {
  background: var(--hover);
}
.picker-item.active {
  background: var(--hover);
}
.picker-item.active .pi-num {
  color: var(--accent);
}
.picker-item.inactive {
  opacity: 0.55;
}
.pi-num {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-2);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.pi-name {
  font-family: var(--font-sans);
  font-size: 12px;
  color: var(--ink);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.pi-name.wide {
  grid-column: 1 / 3;
}
.pi-meta {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--ink-3);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.picker-empty {
  padding: 16px 12px;
  text-align: center;
  font-family: var(--font-sans);
  font-size: 11px;
  color: var(--ink-4);
}

.preview-foot {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 0.5px solid var(--rule);
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.bill {
  font-family: var(--font-sans);
  font-size: 12px;
  color: var(--ink-2);
}
.num {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}
.strong {
  font-weight: 500;
  color: var(--ink);
}
.hint {
  display: flex;
  align-items: center;
  gap: 6px;
  font-family: var(--font-sans);
  font-size: 11px;
  color: var(--ink-3);
}
</style>
