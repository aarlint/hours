<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api, formatCurrency, formatHours } from '../api'
import type { InvoiceDetails } from '../types'
import PageHeader from '../components/PageHeader.vue'
import LoadingBar from '../components/LoadingBar.vue'
import StatusChip from '../components/StatusChip.vue'
import EmptyState from '../components/EmptyState.vue'
import { isWails, revealInFinder } from '../wailsShim'

const route = useRoute()
const router = useRouter()
const number = computed(() => String(route.params.number))

const loading = ref(true)
const error = ref<string | null>(null)
const data = ref<InvoiceDetails | null>(null)
const statusMsg = ref<string>('')
const downloading = ref(false)
const nativeReveal = isWails()

async function load() {
  loading.value = true
  error.value = null
  try {
    data.value = await api.getInvoice(number.value)
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
    const res = await api.downloadInvoice(data.value.invoice.invoice_number)
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
  statusMsg.value = ''
  try {
    await api.updateInvoiceStatus(data.value.invoice.invoice_number, s)
    statusMsg.value = 'SAVED'
    await load()
  } catch (e: any) {
    statusMsg.value = 'ERROR: ' + e.message
  }
}

onMounted(load)

const daysUntilDue = computed(() => {
  if (!data.value) return 0
  const due = new Date(data.value.invoice.due_date)
  const now = new Date()
  return Math.ceil((due.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))
})
</script>

<template>
  <div v-if="loading">
    <LoadingBar text="FETCHING INVOICE" />
  </div>

  <div v-else-if="error">
    <div class="card" style="border-color: var(--accent)">
      <div class="mono-label text-accent">[ ERROR ]</div>
      <p style="margin-top: 8px">{{ error }}</p>
      <button class="btn btn-secondary btn-sm" style="margin-top: 16px" @click="router.push('/invoices')">
        ← BACK
      </button>
    </div>
  </div>

  <div v-else-if="data">
    <PageHeader
      :category="'INVOICE / ' + data.invoice.status.toUpperCase()"
      :title="data.invoice.invoice_number"
      :subtitle="data.invoice.client_name"
    >
      <template #actions>
        <RouterLink to="/invoices" class="btn btn-ghost">← BACK</RouterLink>
        <span v-if="statusMsg" class="mono-label" :class="statusMsg.startsWith('ERROR') ? 'text-accent' : 'text-success'">
          [ {{ statusMsg }} ]
        </span>
        <button
          class="btn btn-secondary"
          :disabled="downloading"
          @click="downloadPdf"
        >
          {{ downloading ? 'GENERATING...' : 'DOWNLOAD PDF' }}
        </button>
        <button
          v-if="data.invoice.status !== 'paid'"
          class="btn btn-primary"
          @click="setStatus('paid')"
        >
          MARK PAID
        </button>
        <button
          v-else
          class="btn btn-secondary"
          @click="setStatus('pending')"
        >
          MARK PENDING
        </button>
        <button
          v-if="['draft', 'pending'].includes(data.invoice.status)"
          class="btn btn-secondary"
          @click="setStatus('sent')"
        >
          MARK SENT
        </button>
        <button
          v-if="data.invoice.status !== 'cancelled' && data.invoice.status !== 'paid'"
          class="btn btn-ghost"
          @click="setStatus('cancelled')"
        >
          CANCEL
        </button>
      </template>
    </PageHeader>

    <section class="meta-grid">
      <div class="meta-cell">
        <div class="mono-label text-disabled">TOTAL</div>
        <div class="meta-value hero">{{ formatCurrency(data.invoice.total_amount) }}</div>
      </div>
      <div class="meta-cell">
        <div class="mono-label text-disabled">HOURS</div>
        <div class="meta-value">{{ formatHours(data.total_hours) }}</div>
      </div>
      <div class="meta-cell">
        <div class="mono-label text-disabled">ISSUED</div>
        <div class="meta-value-sm mono">{{ data.invoice.issue_date.slice(0, 10) }}</div>
      </div>
      <div class="meta-cell">
        <div class="mono-label text-disabled">DUE</div>
        <div class="meta-value-sm mono">{{ data.invoice.due_date.slice(0, 10) }}</div>
        <div
          v-if="data.invoice.status !== 'paid'"
          class="mono-label"
          :class="daysUntilDue < 0 ? 'text-accent' : daysUntilDue < 7 ? 'text-warning' : 'text-disabled'"
        >
          {{ daysUntilDue < 0 ? 'OVERDUE ' + Math.abs(daysUntilDue) + 'D' : daysUntilDue + 'D REMAINING' }}
        </div>
      </div>
      <div class="meta-cell">
        <div class="mono-label text-disabled">STATUS</div>
        <StatusChip :status="data.invoice.status" />
      </div>
    </section>

    <section v-if="data.invoice.pdf_path" class="pdf-hint">
      <span class="mono-label text-disabled">PDF →</span>
      <span class="mono">{{ data.invoice.pdf_path }}</span>
      <button
        v-if="nativeReveal"
        class="btn btn-ghost btn-sm"
        @click="revealInFinder(data.invoice.pdf_path!)"
      >
        REVEAL
      </button>
    </section>

    <section>
      <div class="section-head">
        <span class="mono-label">LINE ITEMS · {{ data.time_entries.length }} ENTRIES</span>
      </div>

      <EmptyState v-if="!data.time_entries.length" title="No line items" desc="This invoice has no time entries attached." />

      <table v-else class="table">
        <thead>
          <tr>
            <th class="num">DATE</th>
            <th>CONTRACT</th>
            <th>DESCRIPTION</th>
            <th class="num">HRS</th>
            <th class="num">RATE</th>
            <th class="num">AMOUNT</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in data.time_entries" :key="e.id">
            <td class="num mono text-disabled">{{ e.date.slice(0, 10) }}</td>
            <td class="mono text-secondary">{{ e.contract_number }}</td>
            <td>{{ e.description || '—' }}</td>
            <td class="num">{{ formatHours(e.hours) }}</td>
            <td class="num text-secondary">{{ formatCurrency(e.hourly_rate, e.currency) }}</td>
            <td class="num">{{ formatCurrency(e.amount, e.currency) }}</td>
          </tr>
        </tbody>
        <tfoot>
          <tr>
            <td colspan="3" class="mono-label text-disabled" style="text-align: right">TOTAL</td>
            <td class="num">{{ formatHours(data.total_hours) }}</td>
            <td></td>
            <td class="num" style="font-weight: 500">{{ formatCurrency(data.invoice.total_amount) }}</td>
          </tr>
        </tfoot>
      </table>
    </section>
  </div>
</template>

<style scoped>
.meta-grid {
  display: grid;
  grid-template-columns: 2fr 1fr 1fr 1fr 1fr;
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

.meta-value-sm {
  font-size: 15px;
  color: var(--text-primary);
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

@media (max-width: 900px) {
  .meta-grid {
    grid-template-columns: 1fr 1fr;
  }
  .meta-value.hero {
    font-size: 28px;
  }
}
</style>
