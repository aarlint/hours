<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api, formatCurrency } from '../api'
import type { Client, Invoice } from '../types'
import PageHeader from '../components/PageHeader.vue'
import Modal from '../components/Modal.vue'
import LoadingBar from '../components/LoadingBar.vue'
import EmptyState from '../components/EmptyState.vue'
import StatusChip from '../components/StatusChip.vue'
import { useConfirm } from '../composables/useConfirm'
import { isWails, revealInFinder } from '../wailsShim'

const { confirm: confirmDialog, alert: alertDialog } = useConfirm()

type StatusFilter = 'all' | 'draft' | 'pending' | 'sent' | 'paid' | 'overdue' | 'cancelled'

const loading = ref(true)
const invoices = ref<Invoice[]>([])
const clients = ref<Client[]>([])
const statusFilter = ref<StatusFilter>('all')
const clientFilter = ref<number | 0>(0)

const modalOpen = ref(false)
const creating = ref(false)
const createError = ref<string | null>(null)
const createResult = ref<{ invoice_number: string; total_amount: number; pdf_path?: string } | null>(null)
const downloadingNumber = ref<string | null>(null)
const nativeReveal = isWails()

const form = reactive({
  client_id: 0,
  period: 'this month',
  start_date: '',
  end_date: '',
  due_days: 30,
})

async function load() {
  loading.value = true
  try {
    const params: any = {}
    if (statusFilter.value !== 'all') params.status = statusFilter.value
    if (clientFilter.value) params.client_id = clientFilter.value
    const [inv, cls] = await Promise.all([api.listInvoices(params), api.listClients()])
    invoices.value = inv
    clients.value = cls
  } finally {
    loading.value = false
  }
}

function openCreate() {
  Object.assign(form, {
    client_id: clients.value[0]?.id || 0,
    period: 'this month',
    start_date: '',
    end_date: '',
    due_days: 30,
  })
  createError.value = null
  createResult.value = null
  modalOpen.value = true
}

async function createInvoice() {
  if (!form.client_id) {
    createError.value = 'Pick a client'
    return
  }
  creating.value = true
  createError.value = null
  createResult.value = null
  try {
    const payload: any = {
      client_id: form.client_id,
      due_days: form.due_days,
    }
    if (form.start_date && form.end_date) {
      payload.start_date = form.start_date
      payload.end_date = form.end_date
    } else if (form.period) {
      payload.period = form.period
    }
    const res = await api.createInvoice(payload)
    createResult.value = res
    await load()
  } catch (e: any) {
    createError.value = e.message
  } finally {
    creating.value = false
  }
}

async function markStatus(inv: Invoice, status: string) {
  const ok = await confirmDialog({
    title: `Mark as ${status}?`,
    message: `Invoice ${inv.invoice_number} will be updated.`,
    confirmLabel: `Mark ${status}`,
    tone: status === 'cancelled' ? 'danger' : 'default',
  })
  if (!ok) return
  try {
    await api.updateInvoiceStatus(inv.invoice_number, status)
    await load()
  } catch (e: any) {
    await alertDialog({
      title: 'Could not update invoice',
      message: e.message,
      tone: 'warning',
    })
  }
}

async function downloadInvoice(inv: Invoice) {
  if (downloadingNumber.value) return
  downloadingNumber.value = inv.invoice_number
  try {
    const res = await api.downloadInvoice(inv.invoice_number)
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

async function deleteInvoice(inv: Invoice) {
  const ok = await confirmDialog({
    title: `Delete invoice ${inv.invoice_number}?`,
    message: 'Any linked time entries will be unlinked and returned to unbilled. The PDF file will be removed. This cannot be undone.',
    confirmLabel: 'Delete invoice',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await api.deleteInvoice(inv.invoice_number)
    await load()
  } catch (e: any) {
    await alertDialog({
      title: 'Could not delete invoice',
      message: e.message,
      tone: 'warning',
    })
  }
}

onMounted(load)

const totalOutstanding = computed(() =>
  invoices.value
    .filter((i) => ['pending', 'sent', 'overdue'].includes(i.status))
    .reduce((s, i) => s + i.total_amount, 0),
)

const totalPaid = computed(() =>
  invoices.value
    .filter((i) => i.status === 'paid')
    .reduce((s, i) => s + i.total_amount, 0),
)
</script>

<template>
  <div>
    <PageHeader
      category="BILLING"
      title="Invoices"
      subtitle="Issued statements and their payment status."
    >
      <template #actions>
        <button class="btn btn-primary" @click="openCreate" :disabled="!clients.length">
          + NEW INVOICE
        </button>
      </template>
    </PageHeader>

    <div class="summary">
      <div class="sum-cell">
        <div class="mono-label text-disabled">OUTSTANDING</div>
        <div class="sum-value text-accent">{{ formatCurrency(totalOutstanding) }}</div>
      </div>
      <div class="sum-cell">
        <div class="mono-label text-disabled">PAID</div>
        <div class="sum-value text-success">{{ formatCurrency(totalPaid) }}</div>
      </div>
      <div class="sum-cell">
        <div class="mono-label text-disabled">COUNT</div>
        <div class="sum-value">{{ invoices.length }}</div>
      </div>
    </div>

    <div class="toolbar">
      <div class="segmented">
        <button :class="{ active: statusFilter === 'all' }" @click="statusFilter = 'all'; load()">ALL</button>
        <button :class="{ active: statusFilter === 'draft' }" @click="statusFilter = 'draft'; load()">DRAFT</button>
        <button :class="{ active: statusFilter === 'sent' }" @click="statusFilter = 'sent'; load()">SENT</button>
        <button :class="{ active: statusFilter === 'paid' }" @click="statusFilter = 'paid'; load()">PAID</button>
        <button :class="{ active: statusFilter === 'overdue' }" @click="statusFilter = 'overdue'; load()">OVERDUE</button>
        <button :class="{ active: statusFilter === 'cancelled' }" @click="statusFilter = 'cancelled'; load()">CANCELLED</button>
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
      desc="Create a client and contract before generating invoices."
    >
      <RouterLink to="/clients" class="btn btn-primary">→ CLIENTS</RouterLink>
    </EmptyState>

    <EmptyState
      v-else-if="!invoices.length"
      title="No invoices"
      desc="Generate one from unbilled time entries."
    >
      <button class="btn btn-primary" @click="openCreate">+ NEW INVOICE</button>
    </EmptyState>

    <table v-else class="table">
      <thead>
        <tr>
          <th>#</th>
          <th>CLIENT</th>
          <th class="num">ISSUED</th>
          <th class="num">DUE</th>
          <th class="num">AMOUNT</th>
          <th>STATUS</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="inv in invoices" :key="inv.invoice_number">
          <td>
            <RouterLink :to="'/invoices/' + encodeURIComponent(inv.invoice_number)" class="mono invoice-link">
              {{ inv.invoice_number }}
            </RouterLink>
          </td>
          <td>{{ inv.client_name }}</td>
          <td class="num text-disabled">{{ inv.issue_date.slice(0, 10) }}</td>
          <td class="num text-disabled">{{ inv.due_date.slice(0, 10) }}</td>
          <td class="num">{{ formatCurrency(inv.total_amount) }}</td>
          <td><StatusChip :status="inv.status" /></td>
          <td class="actions">
            <button
              class="btn btn-ghost btn-sm"
              :disabled="downloadingNumber === inv.invoice_number"
              @click="downloadInvoice(inv)"
            >
              {{ downloadingNumber === inv.invoice_number ? 'GENERATING…' : 'DOWNLOAD' }}
            </button>
            <button
              v-if="inv.status !== 'paid'"
              class="btn btn-ghost btn-sm"
              @click="markStatus(inv, 'paid')"
            >
              MARK PAID
            </button>
            <button
              v-else
              class="btn btn-ghost btn-sm"
              @click="markStatus(inv, 'pending')"
            >
              MARK PENDING
            </button>
            <button
              v-if="['draft', 'pending'].includes(inv.status)"
              class="btn btn-ghost btn-sm"
              @click="markStatus(inv, 'sent')"
            >
              SEND
            </button>
            <button
              v-if="inv.status === 'cancelled'"
              class="btn btn-ghost btn-sm btn-danger"
              @click="deleteInvoice(inv)"
            >
              DELETE
            </button>
          </td>
        </tr>
      </tbody>
    </table>

    <Modal :open="modalOpen" title="New invoice" wide @close="modalOpen = false">
      <form class="form-grid" @submit.prevent="createInvoice">
        <div class="field">
          <label>Client</label>
          <select v-model.number="form.client_id" class="select">
            <option :value="0" disabled>Select client</option>
            <option v-for="c in clients" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
        </div>

        <p class="form-hint">
          Pick a preset period, or leave it as <em>Custom range</em> to set exact dates.
          Either way, only unbilled entries are included.
        </p>

        <div class="row">
          <div class="field grow">
            <label>Period</label>
            <select v-model="form.period" class="select">
              <option value="">Custom range</option>
              <option value="this month">This month</option>
              <option value="last month">Last month</option>
              <option value="this week">This week</option>
              <option value="last week">Last week</option>
              <option value="this quarter">This quarter</option>
              <option value="last quarter">Last quarter</option>
              <option value="this year">This year</option>
              <option value="last year">Last year</option>
              <option value="all">All unbilled</option>
            </select>
          </div>
          <div class="field">
            <label>Due in (days)</label>
            <input v-model.number="form.due_days" class="input due-input" type="number" min="0" />
          </div>
        </div>

        <div class="row">
          <div class="field grow">
            <label>Start date</label>
            <input v-model="form.start_date" class="input" type="date" :disabled="!!form.period" />
          </div>
          <div class="field grow">
            <label>End date</label>
            <input v-model="form.end_date" class="input" type="date" :disabled="!!form.period" />
          </div>
        </div>

        <div v-if="createError" class="field-error">{{ createError }}</div>
        <div v-if="createResult" class="result-ok">
          <div class="result-title">Invoice created</div>
          <div class="result-line">
            <span class="mono">{{ createResult.invoice_number }}</span> · {{ formatCurrency(createResult.total_amount) }}
          </div>
          <div v-if="createResult.pdf_path" class="result-line result-pdf">
            PDF → {{ createResult.pdf_path }}
          </div>
        </div>

        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="modalOpen = false">Close</button>
          <button type="submit" class="btn btn-primary" :disabled="creating">
            {{ creating ? 'Generating…' : 'Generate invoice' }}
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

.sum-cell {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

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

.right-controls {
  display: flex;
  gap: var(--space-sm);
  align-items: center;
}

.select-inline {
  min-width: 200px;
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

.invoice-link {
  color: var(--text-display);
  font-weight: 500;
  transition: color var(--duration-fast) var(--ease-out);
}

.invoice-link:hover {
  color: var(--accent);
}

.actions {
  text-align: right;
  white-space: nowrap;
}

.actions .btn {
  margin-left: var(--space-xs);
}

.form-hint {
  font-family: var(--font-sans);
  font-size: 12px;
  color: var(--ink-3);
  line-height: 1.45;
  margin: -4px 0 2px;
}
.form-hint em { color: var(--ink-2); font-style: italic; }

.due-input { width: 120px; }

.result-ok {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  border-left: 2px solid var(--billable);
  background: var(--hover);
}
.result-title {
  font-family: var(--font-sans);
  font-size: 10.5px;
  color: var(--billable);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  font-weight: 500;
}
.result-line {
  font-size: 13px;
  color: var(--ink);
}
.result-pdf {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-3);
}

@media (max-width: 700px) {
  .summary {
    grid-template-columns: 1fr;
  }
}
</style>
