<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, formatCurrency } from '../api'
import type { Client, Contract, Expense } from '../types'
import PageHeader from '../components/PageHeader.vue'
import LoadingBar from '../components/LoadingBar.vue'
import EmptyState from '../components/EmptyState.vue'
import Modal from '../components/Modal.vue'
import InlineStatus from '../components/InlineStatus.vue'
import { useConfirm } from '../composables/useConfirm'

const { confirm: confirmDialog } = useConfirm()

const loading = ref(false)
const expenses = ref<Expense[]>([])
const clients = ref<Client[]>([])
const contracts = ref<Contract[]>([])

const filters = reactive({
  client_id: '' as number | '',
  invoiced: '' as '' | 'true' | 'false',
  category: '',
  start_date: '',
  end_date: '',
})

const addOpen = ref(false)
const editOpen = ref(false)
const saving = ref(false)
const formError = ref<string | null>(null)
const addForm = reactive({
  client_id: 0,
  contract_id: null as number | null,
  date: new Date().toISOString().slice(0, 10),
  description: '',
  amount: 0,
  currency: 'USD',
  category: '',
  receipt_path: '',
})
const editForm = reactive<{
  id: string
  date: string
  description: string
  amount: number
  currency: string
  category: string
  receipt_path: string
}>({
  id: '',
  date: '',
  description: '',
  amount: 0,
  currency: 'USD',
  category: '',
  receipt_path: '',
})

const statusMsg = ref<{ kind: 'ok' | 'error'; text: string } | null>(null)

async function loadLookups() {
  const [cls, cs] = await Promise.all([api.listClients(), api.listContracts()])
  clients.value = cls
  contracts.value = cs
}

async function search() {
  loading.value = true
  try {
    const params: any = {}
    if (filters.client_id) params.client_id = filters.client_id
    if (filters.invoiced) params.invoiced = filters.invoiced
    if (filters.category) params.category = filters.category
    if (filters.start_date) params.start_date = filters.start_date
    if (filters.end_date) params.end_date = filters.end_date
    expenses.value = await api.listExpenses(params)
  } finally {
    loading.value = false
  }
}

function clearFilters() {
  filters.client_id = ''
  filters.invoiced = ''
  filters.category = ''
  filters.start_date = ''
  filters.end_date = ''
  search()
}

onMounted(async () => {
  await loadLookups()
  await search()
})

const totals = computed(() => {
  const all = expenses.value.reduce((s, e) => s + e.amount, 0)
  const unbilled = expenses.value
    .filter((e) => !e.invoice_id)
    .reduce((s, e) => s + e.amount, 0)
  return { all, unbilled, count: expenses.value.length }
})

const availableContracts = computed(() => {
  if (!addForm.client_id) return []
  return contracts.value.filter((c) => c.client_id === Number(addForm.client_id))
})

function openAdd() {
  addForm.client_id = clients.value[0]?.id ?? 0
  addForm.contract_id = null
  addForm.date = new Date().toISOString().slice(0, 10)
  addForm.description = ''
  addForm.amount = 0
  addForm.currency = 'USD'
  addForm.category = ''
  addForm.receipt_path = ''
  formError.value = null
  addOpen.value = true
}

async function saveAdd() {
  saving.value = true
  formError.value = null
  try {
    await api.addExpense({
      client_id: addForm.client_id,
      contract_id: addForm.contract_id || undefined,
      date: addForm.date,
      description: addForm.description,
      amount: addForm.amount,
      currency: addForm.currency || 'USD',
      category: addForm.category || undefined,
      receipt_path: addForm.receipt_path || undefined,
    })
    addOpen.value = false
    await search()
    statusMsg.value = { kind: 'ok', text: 'ADDED' }
    setTimeout(() => (statusMsg.value = null), 2500)
  } catch (err: any) {
    formError.value = err.message || String(err)
  } finally {
    saving.value = false
  }
}

function openEdit(e: Expense) {
  editForm.id = e.id
  editForm.date = e.date
  editForm.description = e.description
  editForm.amount = e.amount
  editForm.currency = e.currency
  editForm.category = e.category || ''
  editForm.receipt_path = e.receipt_path || ''
  formError.value = null
  editOpen.value = true
}

async function saveEdit() {
  saving.value = true
  formError.value = null
  try {
    await api.updateExpense(editForm.id, {
      client_id: 0,
      date: editForm.date,
      description: editForm.description,
      amount: editForm.amount,
      currency: editForm.currency,
      category: editForm.category,
      receipt_path: editForm.receipt_path,
    })
    editOpen.value = false
    await search()
    statusMsg.value = { kind: 'ok', text: 'UPDATED' }
    setTimeout(() => (statusMsg.value = null), 2500)
  } catch (err: any) {
    formError.value = err.message || String(err)
  } finally {
    saving.value = false
  }
}

async function deleteExpense(e: Expense) {
  const ok = await confirmDialog({
    title: 'Delete expense?',
    message: `${e.description} — ${formatCurrency(e.amount, e.currency)}`,
    confirmLabel: 'Delete',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await api.deleteExpense(e.id)
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
      title="Expenses"
      subtitle="Pass-through costs you bill to clients."
    >
      <template #actions>
        <InlineStatus v-if="statusMsg" :kind="statusMsg.kind" :message="statusMsg.text" />
        <button class="btn btn-primary" @click="openAdd" :disabled="!clients.length">
          + ADD EXPENSE
        </button>
      </template>
    </PageHeader>

    <!-- Summary strip -->
    <section class="summary">
      <div class="summary-item">
        <span class="mono-label">TOTAL</span>
        <span class="summary-value">{{ formatCurrency(totals.all) }}</span>
      </div>
      <div class="summary-item">
        <span class="mono-label">UNBILLED</span>
        <span class="summary-value">{{ formatCurrency(totals.unbilled) }}</span>
      </div>
      <div class="summary-item">
        <span class="mono-label">COUNT</span>
        <span class="summary-value">{{ totals.count }}</span>
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
        <div class="field">
          <label>INVOICED</label>
          <div class="segmented">
            <button :class="{ active: filters.invoiced === '' }" @click="filters.invoiced = ''; search()">ALL</button>
            <button :class="{ active: filters.invoiced === 'false' }" @click="filters.invoiced = 'false'; search()">UNBILLED</button>
            <button :class="{ active: filters.invoiced === 'true' }" @click="filters.invoiced = 'true'; search()">BILLED</button>
          </div>
        </div>
        <div class="field">
          <label>CATEGORY</label>
          <input v-model="filters.category" class="input" placeholder="travel, software..." @keyup.enter="search" />
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

    <LoadingBar v-if="loading" />

    <EmptyState
      v-else-if="!expenses.length"
      title="No expenses yet"
      desc="Log a billable expense to include it on the next invoice."
    />

    <table v-else class="table">
      <thead>
        <tr>
          <th>DATE</th>
          <th>CLIENT</th>
          <th>CATEGORY</th>
          <th>DESCRIPTION</th>
          <th class="num">AMOUNT</th>
          <th>INVOICE</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="e in expenses" :key="e.id">
          <td class="mono">{{ e.date }}</td>
          <td>{{ e.client_name }}</td>
          <td class="mono text-secondary">{{ e.category || '—' }}</td>
          <td class="text-secondary" style="max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">
            {{ e.description }}
          </td>
          <td class="num">{{ formatCurrency(e.amount, e.currency) }}</td>
          <td>
            <span v-if="e.invoice_number" class="chip chip-active mono">{{ e.invoice_number.slice(0, 12) }}</span>
            <span v-else class="chip">UNBILLED</span>
          </td>
          <td style="text-align: right; white-space: nowrap">
            <button class="btn btn-ghost btn-sm" @click="openEdit(e)" :disabled="!!e.invoice_id">EDIT</button>
            <button class="btn btn-ghost btn-sm" @click="deleteExpense(e)" :disabled="!!e.invoice_id">DEL</button>
          </td>
        </tr>
      </tbody>
    </table>

    <!-- Add modal -->
    <Modal :open="addOpen" title="Add Expense" @close="addOpen = false">
      <form class="form-grid" @submit.prevent="saveAdd">
        <div class="field">
          <label>CLIENT</label>
          <select v-model.number="addForm.client_id" class="select" required>
            <option v-for="c in clients" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
        </div>
        <div class="field">
          <label>CONTRACT (OPTIONAL)</label>
          <select v-model.number="addForm.contract_id" class="select">
            <option :value="null">— NONE —</option>
            <option v-for="c in availableContracts" :key="c.id" :value="c.id">
              {{ c.contract_number }} — {{ c.name }}
            </option>
          </select>
        </div>
        <div class="row">
          <div class="field grow">
            <label>DATE</label>
            <input v-model="addForm.date" class="input" type="date" required />
          </div>
          <div class="field">
            <label>AMOUNT</label>
            <input v-model.number="addForm.amount" class="input" type="number" step="0.01" min="0.01" required />
          </div>
          <div class="field">
            <label>CURRENCY</label>
            <input v-model="addForm.currency" class="input" maxlength="3" style="width: 70px" />
          </div>
        </div>
        <div class="field">
          <label>CATEGORY</label>
          <input v-model="addForm.category" class="input" placeholder="travel, lodging, software..." />
        </div>
        <div class="field">
          <label>DESCRIPTION</label>
          <textarea v-model="addForm.description" class="textarea" rows="2" placeholder="What was this for?" required />
        </div>
        <div class="field">
          <label>RECEIPT PATH (OPTIONAL)</label>
          <input v-model="addForm.receipt_path" class="input" placeholder="/path/to/receipt.pdf" />
        </div>
        <div v-if="formError" class="field-error">[ ERROR ] {{ formError }}</div>
        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="addOpen = false">CANCEL</button>
          <button type="submit" class="btn btn-primary" :disabled="saving">
            {{ saving ? 'SAVING...' : 'ADD' }}
          </button>
        </div>
      </form>
    </Modal>

    <!-- Edit modal -->
    <Modal :open="editOpen" title="Edit Expense" @close="editOpen = false">
      <form class="form-grid" @submit.prevent="saveEdit">
        <div class="row">
          <div class="field grow">
            <label>DATE</label>
            <input v-model="editForm.date" class="input" type="date" required />
          </div>
          <div class="field">
            <label>AMOUNT</label>
            <input v-model.number="editForm.amount" class="input" type="number" step="0.01" min="0.01" required />
          </div>
          <div class="field">
            <label>CURRENCY</label>
            <input v-model="editForm.currency" class="input" maxlength="3" style="width: 70px" />
          </div>
        </div>
        <div class="field">
          <label>CATEGORY</label>
          <input v-model="editForm.category" class="input" />
        </div>
        <div class="field">
          <label>DESCRIPTION</label>
          <textarea v-model="editForm.description" class="textarea" rows="2" required />
        </div>
        <div class="field">
          <label>RECEIPT PATH</label>
          <input v-model="editForm.receipt_path" class="input" />
        </div>
        <div v-if="formError" class="field-error">[ ERROR ] {{ formError }}</div>
        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="editOpen = false">CANCEL</button>
          <button type="submit" class="btn btn-primary" :disabled="saving">
            {{ saving ? 'SAVING...' : 'SAVE' }}
          </button>
        </div>
      </form>
    </Modal>
  </div>
</template>

<style scoped>
.summary {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
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
  flex-wrap: wrap;
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
</style>
