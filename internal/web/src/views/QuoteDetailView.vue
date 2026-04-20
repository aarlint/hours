<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api, formatCurrency } from '../api'
import type { QuoteDetails } from '../types'
import PageHeader from '../components/PageHeader.vue'
import LoadingBar from '../components/LoadingBar.vue'
import StatusChip from '../components/StatusChip.vue'
import Modal from '../components/Modal.vue'
import EmptyState from '../components/EmptyState.vue'
import { useConfirm } from '../composables/useConfirm'
import { isWails, revealInFinder } from '../wailsShim'

const { confirm: confirmDialog, alert: alertDialog } = useConfirm()
const route = useRoute()
const router = useRouter()

const number = computed(() => String(route.params.number))

const loading = ref(true)
const error = ref<string | null>(null)
const data = ref<QuoteDetails | null>(null)
const statusMsg = ref<string>('')
const downloading = ref(false)
const nativeReveal = isWails()

// Convert dialog
const convertOpen = ref(false)
const converting = ref(false)
const convertError = ref<string | null>(null)
const convertForm = reactive({
  contract_number: '',
  contract_name: '',
  start_date: new Date().toISOString().slice(0, 10),
  end_date: '',
  payment_terms: 'Net 30',
})

async function load() {
  loading.value = true
  error.value = null
  try {
    data.value = await api.getQuote(number.value)
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

async function downloadPdf() {
  if (!data.value || downloading.value) return
  downloading.value = true
  statusMsg.value = ''
  try {
    const res = await api.downloadQuote(data.value.quote.quote_number)
    statusMsg.value = 'PDF SAVED'
    if (nativeReveal) await revealInFinder(res.pdf_path)
    await load()
  } catch (e: any) {
    statusMsg.value = 'ERROR: ' + e.message
  } finally {
    downloading.value = false
  }
}

async function setStatus(s: string) {
  if (!data.value) return
  const ok = await confirmDialog({
    title: `Mark as ${s}?`,
    message: `Quote ${data.value.quote.quote_number} will be updated.`,
    confirmLabel: `Mark ${s}`,
    tone: ['rejected', 'expired'].includes(s) ? 'danger' : 'default',
  })
  if (!ok) return
  statusMsg.value = ''
  try {
    await api.updateQuoteStatus(data.value.quote.quote_number, s)
    statusMsg.value = 'SAVED'
    await load()
  } catch (e: any) {
    statusMsg.value = 'ERROR: ' + e.message
  }
}

function openConvert() {
  if (!data.value) return
  Object.assign(convertForm, {
    contract_number: '',
    contract_name: data.value.quote.title,
    start_date: new Date().toISOString().slice(0, 10),
    end_date: '',
    payment_terms: 'Net 30',
  })
  convertError.value = null
  convertOpen.value = true
}

async function submitConvert() {
  if (!data.value) return
  if (!convertForm.contract_number.trim()) {
    convertError.value = 'Contract number required'
    return
  }
  converting.value = true
  convertError.value = null
  try {
    const res = await api.convertQuote(data.value.quote.quote_number, {
      contract_number: convertForm.contract_number.trim(),
      contract_name: convertForm.contract_name.trim() || undefined,
      start_date: convertForm.start_date || undefined,
      end_date: convertForm.end_date || undefined,
      payment_terms: convertForm.payment_terms || undefined,
    })
    convertOpen.value = false
    await load()
    await alertDialog({
      title: 'Contract created',
      message: `Contract ${res.contract_number} created at ${formatCurrency(res.hourly_rate, data.value.quote.currency)}/hr.`,
      tone: 'default',
    })
  } catch (e: any) {
    convertError.value = e.message
  } finally {
    converting.value = false
  }
}

async function deleteQuote() {
  if (!data.value) return
  const ok = await confirmDialog({
    title: `Delete ${data.value.quote.quote_number}?`,
    message: 'This will delete the quote and its line items.',
    confirmLabel: 'Delete',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await api.deleteQuote(data.value.quote.quote_number)
    router.push('/quotes')
  } catch (e: any) {
    await alertDialog({ title: 'Could not delete', message: e.message, tone: 'warning' })
  }
}

onMounted(load)

const daysUntilExpiry = computed(() => {
  if (!data.value) return 0
  const d = new Date(data.value.quote.valid_until)
  const now = new Date()
  return Math.ceil((d.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))
})
</script>

<template>
  <div v-if="loading">
    <LoadingBar text="FETCHING QUOTE" />
  </div>

  <div v-else-if="error">
    <div class="card" style="border-color: var(--accent)">
      <div class="mono-label text-accent">[ ERROR ]</div>
      <p style="margin-top: 8px">{{ error }}</p>
      <button class="btn btn-secondary btn-sm" style="margin-top: 16px" @click="router.push('/quotes')">
        ← BACK
      </button>
    </div>
  </div>

  <div v-else-if="data">
    <PageHeader
      :category="'QUOTE / ' + data.quote.status.toUpperCase()"
      :title="data.quote.quote_number"
      :subtitle="data.quote.client_name + ' — ' + data.quote.title"
    >
      <template #actions>
        <RouterLink to="/quotes" class="btn btn-ghost">← BACK</RouterLink>
        <span v-if="statusMsg" class="mono-label" :class="statusMsg.startsWith('ERROR') ? 'text-accent' : 'text-success'">
          [ {{ statusMsg }} ]
        </span>
        <button
          class="btn btn-secondary"
          :disabled="downloading"
          @click="downloadPdf"
        >
          {{ downloading ? 'GENERATING…' : 'DOWNLOAD PDF' }}
        </button>
        <button
          v-if="data.quote.status === 'draft'"
          class="btn btn-primary"
          @click="setStatus('sent')"
        >
          SEND
        </button>
        <button
          v-if="data.quote.status === 'sent'"
          class="btn btn-primary"
          @click="setStatus('accepted')"
        >
          MARK ACCEPTED
        </button>
        <button
          v-if="data.quote.status === 'sent'"
          class="btn btn-ghost"
          @click="setStatus('rejected')"
        >
          MARK REJECTED
        </button>
        <button
          v-if="data.quote.status === 'accepted'"
          class="btn btn-primary"
          @click="openConvert"
        >
          → CONVERT TO CONTRACT
        </button>
        <button
          v-if="!['converted'].includes(data.quote.status)"
          class="btn btn-ghost"
          @click="deleteQuote"
        >
          DELETE
        </button>
      </template>
    </PageHeader>

    <section class="meta-grid">
      <div class="meta-cell">
        <div class="mono-label text-disabled">TOTAL</div>
        <div class="meta-value hero">{{ formatCurrency(data.quote.total_amount, data.quote.currency) }}</div>
      </div>
      <div class="meta-cell">
        <div class="mono-label text-disabled">ISSUED</div>
        <div class="meta-value-sm mono">{{ data.quote.issue_date.slice(0, 10) }}</div>
      </div>
      <div class="meta-cell">
        <div class="mono-label text-disabled">VALID UNTIL</div>
        <div class="meta-value-sm mono">{{ data.quote.valid_until.slice(0, 10) }}</div>
        <div
          v-if="['draft','sent'].includes(data.quote.status)"
          class="mono-label"
          :class="daysUntilExpiry < 0 ? 'text-accent' : daysUntilExpiry < 7 ? 'text-warning' : 'text-disabled'"
        >
          {{ daysUntilExpiry < 0 ? 'EXPIRED ' + Math.abs(daysUntilExpiry) + 'D AGO' : daysUntilExpiry + 'D REMAINING' }}
        </div>
      </div>
      <div class="meta-cell">
        <div class="mono-label text-disabled">STATUS</div>
        <StatusChip :status="data.quote.status" />
        <RouterLink
          v-if="data.quote.converted_contract_id"
          :to="'/contracts'"
          class="mono-label text-accent convert-link"
        >
          → CONTRACT #{{ data.quote.converted_contract_id }}
        </RouterLink>
      </div>
    </section>

    <section v-if="data.quote.pdf_path" class="pdf-hint">
      <span class="mono-label text-disabled">PDF →</span>
      <span class="mono">{{ data.quote.pdf_path }}</span>
      <button
        v-if="nativeReveal"
        class="btn btn-ghost btn-sm"
        @click="revealInFinder(data.quote.pdf_path!)"
      >
        REVEAL
      </button>
    </section>

    <section>
      <div class="section-head">
        <span class="mono-label">LINE ITEMS · {{ data.line_items.length }}</span>
      </div>

      <EmptyState v-if="!data.line_items.length" title="No line items" desc="This quote has no line items." />

      <table v-else class="table">
        <thead>
          <tr>
            <th>DESCRIPTION</th>
            <th class="num">QTY</th>
            <th>UNIT</th>
            <th class="num">RATE</th>
            <th class="num">AMOUNT</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="li in data.line_items" :key="li.id">
            <td>{{ li.description }}</td>
            <td class="num mono">{{ li.quantity.toFixed(2) }}</td>
            <td class="mono text-secondary">{{ li.unit }}</td>
            <td class="num text-secondary">{{ formatCurrency(li.unit_price, data.quote.currency) }}</td>
            <td class="num">{{ formatCurrency(li.amount, data.quote.currency) }}</td>
          </tr>
        </tbody>
        <tfoot>
          <tr>
            <td colspan="4" class="mono-label text-disabled" style="text-align: right">TOTAL</td>
            <td class="num" style="font-weight: 500">
              {{ formatCurrency(data.quote.total_amount, data.quote.currency) }}
            </td>
          </tr>
        </tfoot>
      </table>
    </section>

    <section v-if="data.quote.notes" class="notes-block">
      <div class="mono-label text-disabled">NOTES</div>
      <p class="notes-text">{{ data.quote.notes }}</p>
    </section>

    <Modal :open="convertOpen" title="Convert to contract" @close="convertOpen = false">
      <form class="form-grid" @submit.prevent="submitConvert">
        <p class="hint">
          Creates a new active contract under <strong>{{ data.quote.client_name }}</strong>.
          The hourly rate is derived from the quote's line items.
        </p>
        <div class="field">
          <label>Contract number</label>
          <input v-model="convertForm.contract_number" class="input" type="text" placeholder="e.g. CA-2026-001" />
        </div>
        <div class="field">
          <label>Contract name</label>
          <input v-model="convertForm.contract_name" class="input" type="text" />
        </div>
        <div class="row">
          <div class="field grow">
            <label>Start date</label>
            <input v-model="convertForm.start_date" class="input" type="date" />
          </div>
          <div class="field grow">
            <label>End date (optional)</label>
            <input v-model="convertForm.end_date" class="input" type="date" />
          </div>
        </div>
        <div class="field">
          <label>Payment terms</label>
          <input v-model="convertForm.payment_terms" class="input" type="text" placeholder="e.g. Net 30" />
        </div>

        <div v-if="convertError" class="field-error">{{ convertError }}</div>

        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="convertOpen = false">Cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="converting">
            {{ converting ? 'Creating…' : 'Create contract' }}
          </button>
        </div>
      </form>
    </Modal>
  </div>
</template>

<style scoped>
.meta-grid {
  display: grid;
  grid-template-columns: 2fr 1fr 1fr 1fr;
  gap: var(--space-md);
  margin-bottom: var(--space-xl);
  padding: var(--space-lg);
  border: 1px solid var(--border);
}
.meta-cell {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.meta-value {
  font-family: var(--font-mono);
  font-size: 22px;
  color: var(--text-display);
  letter-spacing: -0.01em;
}
.meta-value.hero {
  font-family: var(--font-serif);
  font-size: 36px;
  font-weight: 400;
  letter-spacing: -0.01em;
}
.meta-value-sm { font-size: 15px; color: var(--text-primary); }

.convert-link {
  display: inline-block;
  margin-top: 2px;
}

.pdf-hint {
  display: flex;
  gap: var(--space-sm);
  align-items: center;
  padding: var(--space-sm) var(--space-md);
  margin-bottom: var(--space-xl);
  border-left: 2px solid var(--border);
  font-size: 12px;
}

.section-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-bottom: var(--space-sm);
  margin-bottom: var(--space-md);
  border-bottom: 1px solid var(--border);
}

tfoot td {
  padding-top: var(--space-md);
  border-top: 1px solid var(--border);
}

.notes-block {
  margin-top: var(--space-xl);
  padding: var(--space-md) var(--space-lg);
  border-left: 2px solid var(--border);
}
.notes-text {
  margin-top: 6px;
  font-size: 13.5px;
  line-height: 1.55;
  color: var(--ink-2);
  white-space: pre-wrap;
}

.form-grid { display: flex; flex-direction: column; gap: var(--space-md); }
.row { display: flex; gap: var(--space-md); align-items: flex-end; }
.hint {
  font-size: 12px;
  color: var(--ink-3);
  line-height: 1.5;
  margin: 0;
}

@media (max-width: 900px) {
  .meta-grid { grid-template-columns: 1fr 1fr; }
  .meta-value.hero { font-size: 28px; }
}
</style>
