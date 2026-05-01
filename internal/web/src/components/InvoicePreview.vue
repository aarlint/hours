<script setup lang="ts">
import { computed } from 'vue'
import type { InvoicePreview } from '../types'
import { formatCurrency, formatDate } from '../api'

const props = defineProps<{ data: InvoicePreview }>()

// Primary contract = the first referenced by any line item.
const primaryContract = computed(() => props.data.contracts[0] ?? null)

const currency = computed(() => primaryContract.value?.currency ?? 'USD')

const totalAmount = computed(() => props.data.invoice.total_amount)

const fromLines = computed(() => {
  const b = props.data.business
  return compact([
    b.business_name,
    b.contact_name,
    b.email,
    b.phone,
    joinAddress(b.address, b.city, b.state, b.zip_code, b.country),
    b.website,
  ])
})

const billToLines = computed(() => {
  const c = props.data.client
  const lines = compact([
    c.name,
    c.address,
    cityStateZip(c.city, c.state, c.zip_code),
    c.country,
  ])
  for (const r of props.data.recipients) {
    if (r.email) lines.push(`${r.name} <${r.email}>`)
    else if (r.name) lines.push(r.name)
  }
  return lines
})

const hasPayment = computed(() => {
  const p = props.data.payment
  return !!(
    p?.bank_name ||
    p?.account_number ||
    p?.routing_number ||
    p?.swift_code ||
    p?.payment_terms ||
    p?.notes
  )
})

const maskedAccount = computed(() => {
  const a = props.data.payment?.account_number ?? ''
  if (!a) return ''
  if (a.length <= 4) return a
  return `•••• ${a.slice(-4)}`
})

const daysUntilDue = computed(() => {
  const due = new Date(props.data.invoice.due_date)
  const now = new Date()
  return Math.ceil((due.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))
})

function compact(xs: (string | null | undefined)[]): string[] {
  return xs
    .map((x) => (x ?? '').trim())
    .filter((x) => x.length > 0)
}

function joinAddress(
  street?: string,
  city?: string,
  state?: string,
  zip?: string,
  country?: string,
): string {
  const parts: string[] = []
  if (street) parts.push(street)
  const csz = cityStateZip(city, state, zip)
  if (csz) parts.push(csz)
  if (country) parts.push(country)
  return parts.join(' · ')
}

function cityStateZip(city?: string, state?: string, zip?: string): string {
  let s = ''
  if (city) s = city
  if (state) s = s ? `${s}, ${state}` : state
  if (zip) s = s ? `${s} ${zip}` : zip
  return s
}

function formatShortDate(d: string): string {
  const dt = new Date(d)
  if (isNaN(dt.getTime())) return formatDate(d)
  return dt.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  })
}
</script>

<template>
  <div class="sheet-wrap">
    <div class="sheet">
      <!-- ── Header strip ─────────────────────────────────────── -->
      <header class="header">
        <div class="business-mark mono">
          {{ (data.business.business_name || 'UNCONFIGURED').toUpperCase() }}
        </div>
        <h1 class="wordmark display">INVOICE</h1>
      </header>

      <!-- Single red dot — the one moment of expression -->
      <div class="dot-row">
        <span class="accent-dot">•</span>
      </div>

      <div class="rule"></div>

      <!-- ── Hero: invoice number + right-side meta stack ─────── -->
      <section class="hero-band">
        <div class="hero-left">
          <div class="eyebrow">INVOICE NO.</div>
          <div class="hero-number mono">{{ data.invoice.invoice_number }}</div>
        </div>
        <div class="hero-right">
          <div class="meta-pair">
            <div class="eyebrow">ISSUED</div>
            <div class="meta-value">{{ formatShortDate(data.invoice.issue_date) }}</div>
          </div>
          <div class="meta-pair">
            <div class="eyebrow">DUE</div>
            <div class="meta-value">{{ formatShortDate(data.invoice.due_date) }}</div>
            <div
              v-if="data.invoice.status !== 'paid'"
              class="eyebrow due-flag"
              :class="daysUntilDue < 0 ? 'overdue' : daysUntilDue < 7 ? 'warn' : ''"
            >
              {{
                daysUntilDue < 0
                  ? 'OVERDUE ' + Math.abs(daysUntilDue) + 'D'
                  : daysUntilDue + 'D REMAINING'
              }}
            </div>
          </div>
          <div class="meta-pair">
            <div class="eyebrow">STATUS</div>
            <div class="meta-value status-mark">
              {{ data.invoice.status.toUpperCase() }}
            </div>
          </div>
        </div>
      </section>

      <div class="rule"></div>

      <!-- ── FROM · BILL TO ───────────────────────────────────── -->
      <section class="parties">
        <div class="party">
          <div class="eyebrow">FROM</div>
          <div
            v-for="(l, i) in fromLines"
            :key="'f' + i"
            :class="['party-line', i === 0 ? 'primary' : '']"
          >
            {{ l }}
          </div>
        </div>
        <div class="party">
          <div class="eyebrow">BILL TO</div>
          <div
            v-for="(l, i) in billToLines"
            :key="'b' + i"
            :class="['party-line', i === 0 ? 'primary' : '']"
          >
            {{ l }}
          </div>
        </div>
      </section>

      <div class="rule"></div>

      <!-- ── Contract strip ──────────────────────────────────── -->
      <section v-if="primaryContract" class="contract-strip">
        <div class="eyebrow">CONTRACT</div>
        <div class="contract-row">
          <div class="contract-name">
            <span class="mono contract-num">{{ primaryContract.contract_number }}</span>
            <span class="ink-3 separator"> · </span>
            <span>{{ primaryContract.name }}</span>
          </div>
          <div class="contract-rate mono">
            {{ formatCurrency(primaryContract.hourly_rate, primaryContract.currency) }} / HR
            <span v-if="primaryContract.payment_terms" class="ink-3">
              · {{ primaryContract.payment_terms }}
            </span>
          </div>
        </div>
      </section>

      <div v-if="primaryContract" class="rule"></div>

      <!-- ── Line items ──────────────────────────────────────── -->
      <section class="lines">
        <div class="lines-head">
          <span class="eyebrow">LINE ITEMS</span>
          <span class="eyebrow ink-3">
            · {{ data.time_entries.length }}
            {{ data.time_entries.length === 1 ? 'ENTRY' : 'ENTRIES' }}
          </span>
        </div>

        <div class="line-row head">
          <div class="col-date eyebrow">DATE</div>
          <div class="col-desc eyebrow">DESCRIPTION</div>
          <div class="col-hrs eyebrow">HRS</div>
          <div class="col-amt eyebrow">AMOUNT</div>
        </div>
        <div class="rule thin"></div>

        <div
          v-for="e in data.time_entries"
          :key="e.id"
          class="line-row"
        >
          <div class="col-date mono ink-3">{{ e.date.slice(0, 10) }}</div>
          <div class="col-desc">{{ e.description || '—' }}</div>
          <div class="col-hrs mono ink-2">{{ e.hours.toFixed(2) }}</div>
          <div class="col-amt mono">
            {{ formatCurrency(e.amount, e.currency) }}
          </div>
        </div>

        <div class="rule thin"></div>

        <div class="line-row subtotal">
          <div class="col-date"></div>
          <div class="col-desc eyebrow">SUBTOTAL</div>
          <div class="col-hrs mono ink-2">{{ data.total_hours.toFixed(2) }}</div>
          <div class="col-amt mono ink-2">{{ formatCurrency(totalAmount, currency) }}</div>
        </div>
      </section>

      <!-- ── Hero TOTAL DUE ──────────────────────────────────── -->
      <section class="total-block">
        <div class="eyebrow">TOTAL DUE</div>
        <div class="total-hero display">
          {{ formatCurrency(totalAmount, currency) }}
        </div>
        <div class="eyebrow ink-3 total-meta">
          {{ data.total_hours.toFixed(2) }} HOURS · {{ data.time_entries.length }}
          {{ data.time_entries.length === 1 ? 'ENTRY' : 'ENTRIES' }}
        </div>
      </section>

      <div class="rule"></div>

      <!-- ── Payment information ─────────────────────────────── -->
      <section v-if="hasPayment" class="payment">
        <div class="eyebrow">PAYMENT</div>
        <dl class="pay-grid">
          <template v-if="data.payment.bank_name">
            <dt class="eyebrow ink-3">BANK</dt>
            <dd class="mono">{{ data.payment.bank_name }}</dd>
          </template>
          <template v-if="maskedAccount">
            <dt class="eyebrow ink-3">ACCOUNT</dt>
            <dd class="mono">{{ maskedAccount }}</dd>
          </template>
          <template v-if="data.payment.routing_number">
            <dt class="eyebrow ink-3">ROUTING</dt>
            <dd class="mono">{{ data.payment.routing_number }}</dd>
          </template>
          <template v-if="data.payment.swift_code">
            <dt class="eyebrow ink-3">SWIFT / BIC</dt>
            <dd class="mono">{{ data.payment.swift_code }}</dd>
          </template>
          <template v-if="data.payment.payment_terms">
            <dt class="eyebrow ink-3">TERMS</dt>
            <dd class="mono">{{ data.payment.payment_terms }}</dd>
          </template>
        </dl>
        <p v-if="data.payment.notes" class="pay-notes ink-2">{{ data.payment.notes }}</p>
      </section>

      <div v-if="hasPayment" class="rule"></div>

      <!-- ── Footer ──────────────────────────────────────────── -->
      <footer class="footer">
        <div class="eyebrow">THANK YOU FOR YOUR BUSINESS</div>
        <div class="eyebrow ink-3">PAGE 1 · INV {{ data.invoice.invoice_number }}</div>
      </footer>
    </div>
  </div>
</template>

<style scoped>
/* The "paper" — US-Letter proportions, generous padding, warm off-white that
   reads as paper in both themes because we lock its colors regardless of mode. */
.sheet-wrap {
  display: flex;
  justify-content: center;
  padding: var(--space-xl) 0;
}

.sheet {
  /* US Letter at ~96dpi, scaled so it fits most modals without horizontal scroll. */
  width: 780px;
  max-width: 100%;
  min-height: 1010px;
  padding: 56px 60px 48px;
  background: #fbfaf7;
  color: #1c1b19;
  box-shadow:
    0 40px 80px rgba(0, 0, 0, 0.12),
    0 8px 24px rgba(0, 0, 0, 0.06);
  position: relative;
  font-family: var(--font-sans);
  font-size: 11px;
  line-height: 1.55;
}

/* Subtle paper tooth in the corner — a small craft detail */
.sheet::before {
  content: '';
  position: absolute;
  top: 0;
  right: 0;
  width: 40px;
  height: 40px;
  background: linear-gradient(
    225deg,
    rgba(28, 27, 25, 0.06) 0%,
    rgba(28, 27, 25, 0.03) 40%,
    transparent 100%
  );
  pointer-events: none;
}

/* ── Header ─────────────────────────────────────────── */
.header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 2px;
}

.business-mark {
  font-size: 9px;
  letter-spacing: 0.12em;
  color: #1c1b19;
  font-weight: 500;
}

.wordmark {
  font-size: 34px;
  font-weight: 400;
  letter-spacing: 0.12em;
  color: #1c1b19;
  text-transform: uppercase;
  line-height: 1;
}

.dot-row {
  display: flex;
  justify-content: flex-end;
  margin-top: -6px;
  margin-bottom: 2px;
  height: 14px;
}

.accent-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  background: #d71921;
  border-radius: 50%;
  color: transparent;
  font-size: 0;
}

/* ── Rules ──────────────────────────────────────────── */
.rule {
  height: 1px;
  background: #1c1b19;
  opacity: 0.85;
  margin: 16px 0;
}

.rule.thin {
  background: rgba(28, 27, 25, 0.25);
  margin: 8px 0;
}

/* ── Eyebrow / small caps labels ────────────────────── */
.eyebrow {
  font-family: var(--font-mono);
  font-size: 9px;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: rgba(28, 27, 25, 0.55);
  font-weight: 500;
  line-height: 1.4;
}

/* ── Hero band ──────────────────────────────────────── */
.hero-band {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 32px;
  align-items: flex-start;
}

.hero-left {
  min-width: 0;
}

.hero-number {
  font-size: 28px;
  letter-spacing: -0.02em;
  color: #1c1b19;
  margin-top: 4px;
  font-variant-numeric: tabular-nums;
  word-break: break-all;
}

.hero-right {
  display: flex;
  flex-direction: column;
  gap: 10px;
  text-align: right;
  min-width: 160px;
}

.meta-pair {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.meta-value {
  font-family: var(--font-sans);
  font-size: 12px;
  color: #1c1b19;
}

.status-mark {
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.12em;
}

.due-flag {
  margin-top: 1px;
  letter-spacing: 0.1em;
}

.due-flag.warn {
  color: #b08400;
}
.due-flag.overdue {
  color: #d71921;
}

/* ── Parties ────────────────────────────────────────── */
.parties {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 48px;
}

.party {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.party .eyebrow {
  margin-bottom: 6px;
}

.party-line {
  font-size: 11px;
  color: rgba(28, 27, 25, 0.72);
}

.party-line.primary {
  color: #1c1b19;
  font-size: 13px;
  font-weight: 500;
  letter-spacing: -0.005em;
  margin-bottom: 2px;
}

/* ── Contract strip ─────────────────────────────────── */
.contract-strip {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.contract-row {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 24px;
  align-items: baseline;
}

.contract-num {
  font-size: 11px;
  font-weight: 500;
}

.contract-name {
  font-size: 12px;
}

.contract-rate {
  font-size: 10px;
  color: rgba(28, 27, 25, 0.72);
}

.separator {
  color: rgba(28, 27, 25, 0.4);
}

/* ── Lines ──────────────────────────────────────────── */
.lines {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.lines-head {
  display: flex;
  gap: 6px;
  align-items: baseline;
  margin-bottom: 6px;
}

.line-row {
  display: grid;
  grid-template-columns: 88px 1fr 60px 110px;
  gap: 16px;
  padding: 5px 0;
  align-items: baseline;
}

.line-row.head {
  padding: 0 0 6px;
}

.line-row.subtotal {
  padding-top: 8px;
}

.col-date {
  font-size: 10px;
}
.col-desc {
  font-size: 11px;
  color: #1c1b19;
  overflow-wrap: anywhere;
}
.col-hrs {
  font-size: 10.5px;
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.col-amt {
  font-size: 11px;
  text-align: right;
  font-variant-numeric: tabular-nums;
}

/* ── Total block ────────────────────────────────────── */
.total-block {
  margin-top: 12px;
  margin-bottom: 12px;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
}

.total-hero {
  font-size: 44px;
  font-weight: 400;
  letter-spacing: -0.02em;
  line-height: 1;
  color: #1c1b19;
  font-variant-numeric: tabular-nums;
}

.total-meta {
  letter-spacing: 0.1em;
}

/* ── Payment ────────────────────────────────────────── */
.payment {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.pay-grid {
  display: grid;
  grid-template-columns: 100px 1fr;
  column-gap: 16px;
  row-gap: 3px;
  margin-top: 2px;
}

.pay-grid dt {
  font-family: var(--font-mono);
  font-size: 9px;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: rgba(28, 27, 25, 0.55);
  padding-top: 2px;
}

.pay-grid dd {
  font-size: 11px;
  font-family: var(--font-mono);
  color: #1c1b19;
}

.pay-notes {
  margin-top: 6px;
  font-size: 10.5px;
  color: rgba(28, 27, 25, 0.7);
  max-width: 540px;
}

/* ── Footer ─────────────────────────────────────────── */
.footer {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-top: auto;
  padding-top: 8px;
}

/* Color utilities scoped to the sheet (paper tones regardless of theme) */
.ink-2 {
  color: rgba(28, 27, 25, 0.72);
}
.ink-3 {
  color: rgba(28, 27, 25, 0.5);
}

/* Responsive — the sheet should scroll rather than squish below 780 */
@media (max-width: 840px) {
  .sheet {
    padding: 36px 32px;
  }
  .line-row {
    grid-template-columns: 72px 1fr 50px 90px;
    gap: 10px;
  }
  .parties {
    gap: 24px;
  }
}
</style>
