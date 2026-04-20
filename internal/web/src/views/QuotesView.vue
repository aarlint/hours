<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api, formatCurrency } from '../api'
import type { Client, Quote } from '../types'
import PageHeader from '../components/PageHeader.vue'
import Modal from '../components/Modal.vue'
import LoadingBar from '../components/LoadingBar.vue'
import EmptyState from '../components/EmptyState.vue'
import StatusChip from '../components/StatusChip.vue'
import { useConfirm } from '../composables/useConfirm'
import { isWails, revealInFinder } from '../wailsShim'

const { confirm: confirmDialog, alert: alertDialog } = useConfirm()

type StatusFilter = 'all' | 'draft' | 'sent' | 'accepted' | 'rejected' | 'expired' | 'converted'

const loading = ref(true)
const quotes = ref<Quote[]>([])
const clients = ref<Client[]>([])
const statusFilter = ref<StatusFilter>('all')
const clientFilter = ref<number | 0>(0)

const modalOpen = ref(false)
const creating = ref(false)
const createError = ref<string | null>(null)
const downloadingNumber = ref<string | null>(null)
const nativeReveal = isWails()

type LineItemDraft = {
  description: string
  quantity: number
  unit: string
  unit_price: number
}

const form = reactive({
  client_id: 0,
  title: '',
  valid_days: 30,
  currency: 'USD',
  notes: '',
  line_items: [] as LineItemDraft[],
})

function newLineItem(): LineItemDraft {
  return { description: '', quantity: 1, unit: 'hours', unit_price: 0 }
}

const draftSubtotal = computed(() =>
  form.line_items.reduce((s, li) => s + (li.quantity || 0) * (li.unit_price || 0), 0),
)

async function load() {
  loading.value = true
  try {
    const params: any = {}
    if (statusFilter.value !== 'all') params.status = statusFilter.value
    if (clientFilter.value) params.client_id = clientFilter.value
    const [qs, cls] = await Promise.all([api.listQuotes(params), api.listClients()])
    quotes.value = qs
    clients.value = cls
  } finally {
    loading.value = false
  }
}

function openCreate() {
  Object.assign(form, {
    client_id: clients.value[0]?.id || 0,
    title: '',
    valid_days: 30,
    currency: 'USD',
    notes: '',
    line_items: [newLineItem()],
  })
  createError.value = null
  modalOpen.value = true
}

function addLine() {
  form.line_items.push(newLineItem())
}

function removeLine(i: number) {
  form.line_items.splice(i, 1)
  if (form.line_items.length === 0) form.line_items.push(newLineItem())
}

async function createQuote() {
  if (!form.client_id) {
    createError.value = 'Pick a client'
    return
  }
  if (!form.title.trim()) {
    createError.value = 'Title required'
    return
  }
  const validLines = form.line_items.filter(
    (li) => li.description.trim() && li.quantity > 0,
  )
  if (!validLines.length) {
    createError.value = 'At least one line item with a description and qty > 0'
    return
  }
  creating.value = true
  createError.value = null
  try {
    await api.createQuote({
      client_id: form.client_id,
      title: form.title.trim(),
      valid_days: form.valid_days,
      currency: form.currency,
      notes: form.notes,
      line_items: validLines.map((li) => ({
        description: li.description.trim(),
        quantity: li.quantity,
        unit: li.unit || 'hours',
        unit_price: li.unit_price,
      })),
    })
    modalOpen.value = false
    await load()
  } catch (e: any) {
    createError.value = e.message
  } finally {
    creating.value = false
  }
}

async function markStatus(q: Quote, status: string) {
  const ok = await confirmDialog({
    title: `Mark as ${status}?`,
    message: `Quote ${q.quote_number} will be updated.`,
    confirmLabel: `Mark ${status}`,
    tone: ['rejected', 'expired'].includes(status) ? 'danger' : 'default',
  })
  if (!ok) return
  try {
    await api.updateQuoteStatus(q.quote_number, status)
    await load()
  } catch (e: any) {
    await alertDialog({
      title: 'Could not update quote',
      message: e.message,
      tone: 'warning',
    })
  }
}

async function downloadQuote(q: Quote) {
  if (downloadingNumber.value) return
  downloadingNumber.value = q.quote_number
  try {
    const res = await api.downloadQuote(q.quote_number)
    if (nativeReveal) await revealInFinder(res.pdf_path)
    await load()
  } catch (e: any) {
    await alertDialog({
      title: 'Could not generate PDF',
      message: e.message,
      tone: 'warning',
    })
  } finally {
    downloadingNumber.value = null
  }
}

async function deleteQuote(q: Quote) {
  const ok = await confirmDialog({
    title: `Delete quote ${q.quote_number}?`,
    message: 'The quote and its line items will be removed. Any generated PDF will be deleted.',
    confirmLabel: 'Delete quote',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await api.deleteQuote(q.quote_number)
    await load()
  } catch (e: any) {
    await alertDialog({
      title: 'Could not delete',
      message: e.message,
      tone: 'warning',
    })
  }
}

onMounted(load)

const totalOpen = computed(() =>
  quotes.value
    .filter((q) => ['draft', 'sent'].includes(q.status))
    .reduce((s, q) => s + q.total_amount, 0),
)

const totalAccepted = computed(() =>
  quotes.value
    .filter((q) => ['accepted', 'converted'].includes(q.status))
    .reduce((s, q) => s + q.total_amount, 0),
)
</script>

<template>
  <div>
    <PageHeader
      category="BILLING"
      title="Quotes"
      subtitle="Estimates prepared for prospective or existing clients."
    >
      <template #actions>
        <button class="btn btn-primary" @click="openCreate" :disabled="!clients.length">
          + NEW QUOTE
        </button>
      </template>
    </PageHeader>

    <div class="summary">
      <div class="sum-cell">
        <div class="mono-label text-disabled">OPEN</div>
        <div class="sum-value text-accent">{{ formatCurrency(totalOpen) }}</div>
      </div>
      <div class="sum-cell">
        <div class="mono-label text-disabled">ACCEPTED</div>
        <div class="sum-value text-success">{{ formatCurrency(totalAccepted) }}</div>
      </div>
      <div class="sum-cell">
        <div class="mono-label text-disabled">COUNT</div>
        <div class="sum-value">{{ quotes.length }}</div>
      </div>
    </div>

    <div class="toolbar">
      <div class="segmented">
        <button :class="{ active: statusFilter === 'all' }" @click="statusFilter = 'all'; load()">ALL</button>
        <button :class="{ active: statusFilter === 'draft' }" @click="statusFilter = 'draft'; load()">DRAFT</button>
        <button :class="{ active: statusFilter === 'sent' }" @click="statusFilter = 'sent'; load()">SENT</button>
        <button :class="{ active: statusFilter === 'accepted' }" @click="statusFilter = 'accepted'; load()">ACCEPTED</button>
        <button :class="{ active: statusFilter === 'rejected' }" @click="statusFilter = 'rejected'; load()">REJECTED</button>
        <button :class="{ active: statusFilter === 'expired' }" @click="statusFilter = 'expired'; load()">EXPIRED</button>
        <button :class="{ active: statusFilter === 'converted' }" @click="statusFilter = 'converted'; load()">CONVERTED</button>
      </div>
      <div class="right-controls">
        <select v-model.number="clientFilter" class="select select-inline" @change="load">
          <option :value="0">ALL CLIENTS</option>
          <option v-for="c in clients" :key="c.id" :value="c.id">{{ c.name }}</option>
        </select>
      </div>
    </div>

    <LoadingBar v-if="loading" />

    <EmptyState
      v-else-if="!clients.length"
      title="No clients yet"
      desc="Create a client before preparing a quote."
    >
      <RouterLink to="/clients" class="btn btn-primary">→ CLIENTS</RouterLink>
    </EmptyState>

    <EmptyState
      v-else-if="!quotes.length"
      title="No quotes"
      desc="Draft your first estimate."
    >
      <button class="btn btn-primary" @click="openCreate">+ NEW QUOTE</button>
    </EmptyState>

    <table v-else class="table">
      <thead>
        <tr>
          <th>#</th>
          <th>CLIENT</th>
          <th>TITLE</th>
          <th class="num">ISSUED</th>
          <th class="num">VALID UNTIL</th>
          <th class="num">AMOUNT</th>
          <th>STATUS</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="q in quotes" :key="q.quote_number">
          <td>
            <RouterLink :to="'/quotes/' + encodeURIComponent(q.quote_number)" class="mono quote-link">
              {{ q.quote_number }}
            </RouterLink>
          </td>
          <td>{{ q.client_name }}</td>
          <td class="title-cell">{{ q.title }}</td>
          <td class="num text-disabled">{{ q.issue_date.slice(0, 10) }}</td>
          <td class="num text-disabled">{{ q.valid_until.slice(0, 10) }}</td>
          <td class="num">{{ formatCurrency(q.total_amount, q.currency) }}</td>
          <td><StatusChip :status="q.status" /></td>
          <td class="actions">
            <button
              class="btn btn-ghost btn-sm"
              :disabled="downloadingNumber === q.quote_number"
              @click="downloadQuote(q)"
            >
              {{ downloadingNumber === q.quote_number ? 'GENERATING…' : 'DOWNLOAD' }}
            </button>
            <button
              v-if="q.status === 'draft'"
              class="btn btn-ghost btn-sm"
              @click="markStatus(q, 'sent')"
            >
              SEND
            </button>
            <button
              v-if="q.status === 'sent'"
              class="btn btn-ghost btn-sm"
              @click="markStatus(q, 'accepted')"
            >
              ACCEPT
            </button>
            <button
              v-if="q.status === 'sent'"
              class="btn btn-ghost btn-sm btn-danger"
              @click="markStatus(q, 'rejected')"
            >
              REJECT
            </button>
            <button
              v-if="!['converted'].includes(q.status)"
              class="btn btn-ghost btn-sm btn-danger"
              @click="deleteQuote(q)"
            >
              DELETE
            </button>
          </td>
        </tr>
      </tbody>
    </table>

    <Modal :open="modalOpen" title="New quote" wide @close="modalOpen = false">
      <form class="form-grid" @submit.prevent="createQuote">
        <div class="row">
          <div class="field grow">
            <label>Client</label>
            <select v-model.number="form.client_id" class="select">
              <option :value="0" disabled>Select client</option>
              <option v-for="c in clients" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
          </div>
          <div class="field">
            <label>Valid for (days)</label>
            <input v-model.number="form.valid_days" class="input due-input" type="number" min="1" />
          </div>
          <div class="field">
            <label>Currency</label>
            <input v-model="form.currency" class="input cur-input" type="text" maxlength="6" />
          </div>
        </div>

        <div class="field">
          <label>Title</label>
          <input v-model="form.title" class="input" type="text" placeholder="e.g. Q2 backend refactor" />
        </div>

        <div class="line-items-header">
          <div class="section-title">Line items</div>
          <button type="button" class="btn btn-ghost btn-sm" @click="addLine">+ ADD LINE</button>
        </div>

        <div class="line-items">
          <div class="li-head">
            <div>DESCRIPTION</div>
            <div class="li-num">QTY</div>
            <div>UNIT</div>
            <div class="li-num">RATE</div>
            <div class="li-num">AMOUNT</div>
            <div></div>
          </div>
          <div v-for="(li, i) in form.line_items" :key="i" class="li-row">
            <input v-model="li.description" class="input" type="text" placeholder="Task or deliverable" />
            <input v-model.number="li.quantity" class="input li-input" type="number" min="0" step="0.25" />
            <input v-model="li.unit" class="input li-input" type="text" placeholder="hours" />
            <input v-model.number="li.unit_price" class="input li-input" type="number" min="0" step="1" />
            <div class="li-amount mono">
              {{ formatCurrency((li.quantity || 0) * (li.unit_price || 0), form.currency) }}
            </div>
            <button
              type="button"
              class="btn btn-ghost btn-sm btn-danger li-remove"
              :disabled="form.line_items.length === 1"
              @click="removeLine(i)"
            >
              ×
            </button>
          </div>
          <div class="li-total">
            <div class="li-total-label">TOTAL</div>
            <div class="li-total-amt mono">{{ formatCurrency(draftSubtotal, form.currency) }}</div>
          </div>
        </div>

        <div class="field">
          <label>Notes</label>
          <textarea v-model="form.notes" class="input notes" rows="3"
                    placeholder="Scope assumptions, exclusions, terms."></textarea>
        </div>

        <div v-if="createError" class="field-error">{{ createError }}</div>

        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="modalOpen = false">Close</button>
          <button type="submit" class="btn btn-primary" :disabled="creating">
            {{ creating ? 'Creating…' : 'Create quote' }}
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
  margin-bottom: var(--space-xl);
  padding: var(--space-md) var(--space-lg);
  border: 1px solid var(--border);
}
.sum-cell { display: flex; flex-direction: column; gap: 6px; }
.sum-value {
  font-family: var(--font-mono);
  font-size: 28px;
  letter-spacing: -0.01em;
  color: var(--text-display);
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: var(--space-md);
  margin-bottom: var(--space-lg);
  flex-wrap: wrap;
}
.right-controls { display: flex; gap: var(--space-sm); align-items: center; }
.select-inline { min-width: 200px; }

.quote-link {
  color: var(--text-display);
  font-weight: 500;
  transition: color var(--duration-fast) var(--ease-out);
}
.quote-link:hover { color: var(--accent); }

.title-cell {
  max-width: 280px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.actions { text-align: right; white-space: nowrap; }
.actions .btn { margin-left: var(--space-xs); }

.form-grid { display: flex; flex-direction: column; gap: var(--space-md); }
.row { display: flex; gap: var(--space-md); align-items: flex-end; }
.due-input { width: 120px; }
.cur-input { width: 90px; }

.line-items-header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-top: var(--space-sm);
}
.section-title {
  font-family: var(--font-sans);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ink-3);
  font-weight: 500;
}

.line-items {
  border: 1px solid var(--border);
  padding: var(--space-sm);
}
.li-head,
.li-row {
  display: grid;
  grid-template-columns: 3fr 80px 90px 100px 110px 32px;
  gap: 8px;
  align-items: center;
}
.li-head {
  padding: 6px 4px;
  font-family: var(--font-sans);
  font-size: 10px;
  letter-spacing: 0.06em;
  color: var(--ink-4);
  text-transform: uppercase;
  font-weight: 500;
  border-bottom: 0.5px solid var(--rule);
}
.li-row {
  padding: 6px 4px;
  border-bottom: 0.5px dashed var(--rule);
}
.li-row:last-of-type { border-bottom: none; }
.li-input {
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.li-num { text-align: right; }
.li-amount {
  text-align: right;
  font-size: 12px;
  color: var(--ink-2);
  font-variant-numeric: tabular-nums;
}
.li-remove {
  padding: 4px 8px;
  min-width: 28px;
}

.li-total {
  display: flex;
  justify-content: flex-end;
  align-items: baseline;
  gap: var(--space-md);
  padding-top: 8px;
  border-top: 0.5px solid var(--rule);
  margin-top: 4px;
}
.li-total-label {
  font-family: var(--font-sans);
  font-size: 10.5px;
  color: var(--ink-3);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  font-weight: 500;
}
.li-total-amt {
  font-size: 16px;
  color: var(--text-display);
  font-variant-numeric: tabular-nums;
}

.notes { font-family: var(--font-sans); }

@media (max-width: 760px) {
  .summary { grid-template-columns: 1fr; }
  .li-head,
  .li-row {
    grid-template-columns: 2fr 60px 70px 80px 90px 28px;
  }
}
</style>
