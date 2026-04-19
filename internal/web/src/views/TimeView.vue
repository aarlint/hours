<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, formatCurrency, formatHours } from '../api'
import type { Client, Contract, TimeEntry } from '../types'
import PageHeader from '../components/PageHeader.vue'
import LoadingBar from '../components/LoadingBar.vue'
import EmptyState from '../components/EmptyState.vue'
import Modal from '../components/Modal.vue'
import InlineStatus from '../components/InlineStatus.vue'
import { useConfirm } from '../composables/useConfirm'

const { confirm: confirmDialog, alert: alertDialog } = useConfirm()

const loading = ref(false)
const entries = ref<TimeEntry[]>([])
const clients = ref<Client[]>([])
const contracts = ref<Contract[]>([])

const filters = reactive({
  client_id: '' as number | '',
  contract_id: '' as number | '',
  description: '',
  start_date: '',
  end_date: '',
  invoiced: '' as '' | 'true' | 'false',
  limit: 200,
})

const selected = ref<Set<string>>(new Set())

const addOpen = ref(false)
const editOpen = ref(false)
const addSaving = ref(false)
const addError = ref<string | null>(null)
const addForm = reactive({
  contract_id: 0,
  date: new Date().toISOString().slice(0, 10),
  hours: 1,
  description: '',
})
const editForm = reactive<{ id: string; hours: number; date: string; description: string }>({
  id: '',
  hours: 0,
  date: '',
  description: '',
})

const statusMsg = ref<{ kind: 'ok' | 'error'; text: string } | null>(null)

async function loadLookups() {
  const [cls, cs] = await Promise.all([
    api.listClients(),
    api.listContracts({ status: 'active' }),
  ])
  clients.value = cls
  contracts.value = cs
}

async function search() {
  loading.value = true
  try {
    const params: any = { limit: filters.limit }
    if (filters.client_id) params.client_id = filters.client_id
    if (filters.contract_id) params.contract_id = filters.contract_id
    if (filters.description) params.description = filters.description
    if (filters.start_date) params.start_date = filters.start_date
    if (filters.end_date) params.end_date = filters.end_date
    if (filters.invoiced) params.invoiced = filters.invoiced
    entries.value = await api.searchTimeEntries(params)
    selected.value = new Set()
  } finally {
    loading.value = false
  }
}

function clearFilters() {
  filters.client_id = ''
  filters.contract_id = ''
  filters.description = ''
  filters.start_date = ''
  filters.end_date = ''
  filters.invoiced = ''
  search()
}

onMounted(async () => {
  await loadLookups()
  await search()
})

watch(() => filters.client_id, () => {
  filters.contract_id = ''
})

const availableContracts = computed(() => {
  if (!filters.client_id) return contracts.value
  return contracts.value.filter((c) => c.client_id === Number(filters.client_id))
})

const totals = computed(() => {
  const hours = entries.value.reduce((s, e) => s + e.hours, 0)
  const unbilledHours = entries.value.filter((e) => !e.invoice_id).reduce((s, e) => s + e.hours, 0)
  const amount = entries.value.reduce((s, e) => s + e.amount, 0)
  return { hours, unbilledHours, amount }
})

function openAdd() {
  addForm.contract_id = contracts.value[0]?.id ?? 0
  addForm.date = new Date().toISOString().slice(0, 10)
  addForm.hours = 1
  addForm.description = ''
  addError.value = null
  addOpen.value = true
}

async function saveAdd() {
  addSaving.value = true
  addError.value = null
  try {
    await api.addTimeEntry({
      contract_id: addForm.contract_id,
      date: addForm.date,
      hours: addForm.hours,
      description: addForm.description,
    })
    addOpen.value = false
    statusMsg.value = { kind: 'ok', text: 'ENTRY ADDED' }
    setTimeout(() => (statusMsg.value = null), 2500)
    await search()
  } catch (e: any) {
    addError.value = e.message
  } finally {
    addSaving.value = false
  }
}

async function openEdit(e: TimeEntry) {
  if (e.invoice_id) {
    await alertDialog({
      title: 'Cannot edit entry',
      message: 'This entry belongs to an invoice. Update the invoice status first.',
      tone: 'warning',
    })
    return
  }
  editForm.id = e.id
  editForm.hours = e.hours
  editForm.date = e.date.slice(0, 10)
  editForm.description = e.description
  editOpen.value = true
}

async function saveEdit() {
  try {
    await api.updateTimeEntry(editForm.id, {
      hours: editForm.hours,
      date: editForm.date,
      description: editForm.description,
    })
    editOpen.value = false
    statusMsg.value = { kind: 'ok', text: 'UPDATED' }
    setTimeout(() => (statusMsg.value = null), 2500)
    await search()
  } catch (e: any) {
    statusMsg.value = { kind: 'error', text: e.message }
  }
}

async function deleteEntry(e: TimeEntry) {
  const ok = await confirmDialog({
    title: 'Delete time entry?',
    message: `This will remove a ${e.hours}h entry from ${e.date.slice(0, 10)}.`,
    detail: e.description || undefined,
    confirmLabel: 'Delete',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await api.deleteTimeEntry(e.id)
    await search()
    statusMsg.value = { kind: 'ok', text: 'DELETED' }
    setTimeout(() => (statusMsg.value = null), 2500)
  } catch (err: any) {
    statusMsg.value = { kind: 'error', text: err.message }
  }
}

function toggleSelect(id: string) {
  const s = new Set(selected.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  selected.value = s
}

function toggleSelectAll() {
  if (selected.value.size === entries.value.length) {
    selected.value = new Set()
  } else {
    selected.value = new Set(entries.value.map((e) => e.id))
  }
}

async function bulkDelete() {
  if (!selected.value.size) return
  const ok = await confirmDialog({
    title: `Delete ${selected.value.size} entries?`,
    message: 'All selected entries will be permanently removed.',
    confirmLabel: 'Delete all',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await api.bulkDeleteTimeEntries(Array.from(selected.value))
    await search()
    statusMsg.value = { kind: 'ok', text: 'DELETED' }
    setTimeout(() => (statusMsg.value = null), 2500)
  } catch (err: any) {
    statusMsg.value = { kind: 'error', text: err.message }
  }
}
</script>

<template>
  <div>
    <PageHeader
      category="LEDGER"
      title="Time"
      subtitle="Every fifteen minutes, accounted."
    >
      <template #actions>
        <InlineStatus v-if="statusMsg" :kind="statusMsg.kind" :message="statusMsg.text" />
        <button class="btn btn-primary" @click="openAdd" :disabled="!contracts.length">
          + LOG HOURS
        </button>
      </template>
    </PageHeader>

    <!-- Summary strip -->
    <section class="summary">
      <div class="summary-item">
        <span class="mono-label">TOTAL</span>
        <span class="summary-value">{{ formatHours(totals.hours) }}<small>HRS</small></span>
      </div>
      <div class="summary-item">
        <span class="mono-label">UNBILLED</span>
        <span class="summary-value">{{ formatHours(totals.unbilledHours) }}<small>HRS</small></span>
      </div>
      <div class="summary-item">
        <span class="mono-label">AMOUNT</span>
        <span class="summary-value">{{ formatCurrency(totals.amount) }}</span>
      </div>
      <div class="summary-item">
        <span class="mono-label">ENTRIES</span>
        <span class="summary-value">{{ entries.length }}</span>
      </div>
    </section>

    <!-- Filter bar -->
    <section class="filters card-outlined">
      <div class="filter-row">
        <div class="field grow">
          <label>CLIENT</label>
          <select v-model.number="filters.client_id" class="select" @change="search">
            <option value="">ALL CLIENTS</option>
            <option v-for="c in clients" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
        </div>
        <div class="field grow">
          <label>CONTRACT</label>
          <select v-model.number="filters.contract_id" class="select" @change="search">
            <option value="">ANY CONTRACT</option>
            <option v-for="c in availableContracts" :key="c.id" :value="c.id">
              {{ c.contract_number }} — {{ c.name }}
            </option>
          </select>
        </div>
        <div class="field">
          <label>INVOICED</label>
          <div class="segmented">
            <button :class="{ active: filters.invoiced === '' }" @click="filters.invoiced = ''; search()">ALL</button>
            <button :class="{ active: filters.invoiced === 'false' }" @click="filters.invoiced = 'false'; search()">
              UNBILLED
            </button>
            <button :class="{ active: filters.invoiced === 'true' }" @click="filters.invoiced = 'true'; search()">
              BILLED
            </button>
          </div>
        </div>
      </div>
      <div class="filter-row">
        <div class="field grow">
          <label>SEARCH DESCRIPTION</label>
          <input
            v-model="filters.description"
            class="input"
            placeholder="..."
            @keyup.enter="search"
          />
        </div>
        <div class="field">
          <label>FROM</label>
          <input v-model="filters.start_date" class="input" type="date" @change="search" />
        </div>
        <div class="field">
          <label>TO</label>
          <input v-model="filters.end_date" class="input" type="date" @change="search" />
        </div>
        <div class="filter-actions">
          <button class="btn btn-secondary btn-sm" @click="search">APPLY</button>
          <button class="btn btn-ghost btn-sm" @click="clearFilters">CLEAR</button>
        </div>
      </div>
    </section>

    <!-- Bulk actions -->
    <div v-if="selected.size" class="bulk-bar">
      <span class="mono-label">{{ selected.size }} SELECTED</span>
      <button class="btn btn-destructive btn-sm" @click="bulkDelete">DELETE SELECTED</button>
      <button class="btn btn-ghost btn-sm" @click="selected = new Set()">CLEAR</button>
    </div>

    <LoadingBar v-if="loading" />

    <EmptyState
      v-else-if="!entries.length"
      title="No time entries match"
      desc="Adjust filters or log new hours."
    />

    <table v-else class="table">
      <thead>
        <tr>
          <th style="width: 32px">
            <input
              type="checkbox"
              :checked="selected.size > 0 && selected.size === entries.length"
              @change="toggleSelectAll"
            />
          </th>
          <th>DATE</th>
          <th>CLIENT</th>
          <th>CONTRACT</th>
          <th class="num">HRS</th>
          <th>DESCRIPTION</th>
          <th class="num">RATE</th>
          <th class="num">AMOUNT</th>
          <th>INVOICE</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="e in entries" :key="e.id" :class="{ active: selected.has(e.id) }">
          <td>
            <input type="checkbox" :checked="selected.has(e.id)" @change="toggleSelect(e.id)" />
          </td>
          <td class="mono">{{ e.date.slice(0, 10) }}</td>
          <td>{{ e.client_name }}</td>
          <td class="mono text-secondary">{{ e.contract_number }}</td>
          <td class="num">{{ formatHours(e.hours) }}</td>
          <td class="text-secondary" style="max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">
            {{ e.description || '—' }}
          </td>
          <td class="num text-disabled">{{ formatCurrency(e.hourly_rate, e.currency) }}</td>
          <td class="num">{{ formatCurrency(e.amount, e.currency) }}</td>
          <td>
            <span v-if="e.invoice_number" class="chip chip-active mono">{{ e.invoice_number.slice(0, 12) }}</span>
            <span v-else class="chip">UNBILLED</span>
          </td>
          <td style="text-align: right; white-space: nowrap">
            <button class="btn btn-ghost btn-sm" @click="openEdit(e)" :disabled="!!e.invoice_id">EDIT</button>
            <button class="btn btn-ghost btn-sm" @click="deleteEntry(e)">DEL</button>
          </td>
        </tr>
      </tbody>
    </table>

    <!-- Add entry modal -->
    <Modal :open="addOpen" title="Log Hours" @close="addOpen = false">
      <form class="form-grid" @submit.prevent="saveAdd">
        <div class="field">
          <label>CONTRACT</label>
          <select v-model.number="addForm.contract_id" class="select" required>
            <option v-for="c in contracts" :key="c.id" :value="c.id">
              {{ c.contract_number }} · {{ c.client_name }} · {{ formatCurrency(c.hourly_rate, c.currency) }}/hr
            </option>
          </select>
        </div>
        <div class="row">
          <div class="field grow">
            <label>DATE</label>
            <input v-model="addForm.date" class="input" type="date" required />
          </div>
          <div class="field">
            <label>HOURS</label>
            <input v-model.number="addForm.hours" class="input" type="number" step="0.25" min="0.25" required />
          </div>
        </div>
        <div class="field">
          <label>DESCRIPTION</label>
          <textarea v-model="addForm.description" class="textarea" rows="3" placeholder="What did you work on?" />
        </div>
        <div v-if="addError" class="field-error">[ ERROR ] {{ addError }}</div>
        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="addOpen = false">CANCEL</button>
          <button type="submit" class="btn btn-primary" :disabled="addSaving">
            {{ addSaving ? 'SAVING...' : 'LOG' }}
          </button>
        </div>
      </form>
    </Modal>

    <!-- Edit modal -->
    <Modal :open="editOpen" title="Edit Entry" @close="editOpen = false">
      <form class="form-grid" @submit.prevent="saveEdit">
        <div class="row">
          <div class="field grow">
            <label>DATE</label>
            <input v-model="editForm.date" class="input" type="date" required />
          </div>
          <div class="field">
            <label>HOURS</label>
            <input v-model.number="editForm.hours" class="input" type="number" step="0.25" min="0.25" required />
          </div>
        </div>
        <div class="field">
          <label>DESCRIPTION</label>
          <textarea v-model="editForm.description" class="textarea" rows="3" />
        </div>
        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="editOpen = false">CANCEL</button>
          <button type="submit" class="btn btn-primary">SAVE</button>
        </div>
      </form>
    </Modal>
  </div>
</template>

<style scoped>
.summary {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--space-md);
  padding: var(--space-lg) 0;
  margin-bottom: var(--space-lg);
  border-bottom: 1px solid var(--border);
}

.summary-item {
  display: flex;
  flex-direction: column;
  gap: var(--space-sm);
}

.summary-value {
  font-family: var(--font-mono);
  font-size: 28px;
  font-variant-numeric: tabular-nums;
  color: var(--text-display);
  letter-spacing: -0.01em;
  display: flex;
  align-items: baseline;
  gap: var(--space-xs);
}

.summary-value small {
  font-size: var(--label);
  color: var(--text-secondary);
  letter-spacing: 0.08em;
}

.filters {
  display: flex;
  flex-direction: column;
  gap: var(--space-md);
  padding: var(--space-md);
  margin-bottom: var(--space-md);
}

.filter-row {
  display: flex;
  gap: var(--space-md);
  align-items: flex-end;
}

.filter-actions {
  display: flex;
  gap: var(--space-xs);
  padding-bottom: 2px;
}

.form-grid {
  display: flex;
  flex-direction: column;
  gap: var(--space-md);
}

.row {
  display: flex;
  gap: var(--space-md);
  align-items: flex-end;
}

.bulk-bar {
  display: flex;
  align-items: center;
  gap: var(--space-md);
  padding: var(--space-md);
  border: 1px solid var(--accent);
  border-radius: 8px;
  margin-bottom: var(--space-md);
  background: var(--accent-subtle);
}

@media (max-width: 900px) {
  .summary {
    grid-template-columns: repeat(2, 1fr);
  }
  .filter-row {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
