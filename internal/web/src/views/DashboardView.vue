<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import type { Client, Contract, Stats, TimeEntry, Invoice } from '../types'
import { api } from '../api'
import { changeTick } from '../composables/useRealtime'
import { onAppAction } from '../composables/useAppBus'
import { useToasts } from '../composables/useToasts'
import CommandBar from '../components/CommandBar.vue'
import Display from '../components/Display.vue'
import Num from '../components/Num.vue'
import Dot from '../components/Dot.vue'
import StatusChip from '../components/StatusChip.vue'
import Kbd from '../components/Kbd.vue'
import { fmtDateShort, fmtDOW, fmtHours, fmtUSD, fmtUSDShort, isoDate, relDay } from '../components/primitives'

const router = useRouter()
const toasts = useToasts()

const stats = ref<Stats | null>(null)
const clients = ref<Client[]>([])
const contracts = ref<Contract[]>([])
const unbilled = ref<TimeEntry[]>([])
const recent = ref<TimeEntry[]>([])
const weekEntries = ref<TimeEntry[]>([])
const draftInvoice = ref<Invoice | null>(null)
const loading = ref(true)
const commandRef = ref<InstanceType<typeof CommandBar> | null>(null)

const today = new Date()

function startOfWeek(d: Date): Date {
  const x = new Date(d)
  x.setHours(0, 0, 0, 0)
  x.setDate(x.getDate() - x.getDay())
  return x
}

async function loadAll() {
  try {
    const [st, cs, ks, u, invs] = await Promise.all([
      api.getStats(),
      api.listClients(),
      api.listContracts(),
      api.searchTimeEntries({ invoiced: 'false', limit: 500 }),
      api.listInvoices({ status: 'draft' }).catch(() => []),
    ])
    stats.value = st
    clients.value = cs
    contracts.value = ks
    unbilled.value = u
    recent.value = st?.recent_entries ?? []
    draftInvoice.value = invs?.[0] ?? null

    const sow = startOfWeek(today)
    const eow = new Date(sow)
    eow.setDate(eow.getDate() + 7)
    weekEntries.value = await api.searchTimeEntries({
      start_date: isoDate(sow),
      end_date: isoDate(eow),
      limit: 500,
    })
  } catch (e) {
    console.error(e)
  } finally {
    loading.value = false
  }
}

onMounted(loadAll)
watch(changeTick, loadAll)

onAppAction('command', () => commandRef.value?.focus())
onAppAction('new-invoice', () => router.push('/invoices?new=1'))
onAppAction('search', () => router.push('/time'))
onAppAction('start-timer', () => {
  toasts.push({ tone: 'info', title: 'Timer not implemented yet', detail: 'Use ⌘K to quick-add instead' })
})

/* Header */
const headerDate = computed(() => {
  const d = today
  const full = d.toLocaleDateString('en-US', { weekday: 'long', month: 'long', day: 'numeric' })
  const [first, rest] = full.split(', ') as [string, string]
  return { first, rest }
})
const weekNumber = computed(() => {
  const d = today
  const jan1 = new Date(d.getFullYear(), 0, 1)
  return Math.ceil(((+d - +jan1) / 86400000 + jan1.getDay() + 1) / 7)
})

/* Stat strip */
const weekHours = computed(() => weekEntries.value.reduce((a, e) => a + e.hours, 0))
const weekValue = computed(() => weekEntries.value.reduce((a, e) => a + e.amount, 0))
const unbilledHours = computed(() => unbilled.value.reduce((a, e) => a + e.hours, 0))
const unbilledTotal = computed(() => unbilled.value.reduce((a, e) => a + e.amount, 0))

/* Week chart */
const weekDays = computed(() => {
  const days: Array<{ iso: string; hours: number; dow: string; dnum: number; isToday: boolean }> = []
  const now = today
  for (let i = 6; i >= 0; i--) {
    const d = new Date(now)
    d.setDate(d.getDate() - i)
    const iso = isoDate(d)
    const h = weekEntries.value
      .filter((e) => e.date.slice(0, 10) === iso)
      .reduce((a, e) => a + e.hours, 0)
    days.push({
      iso,
      hours: h,
      dow: d.toLocaleDateString('en-US', { weekday: 'short' }),
      dnum: d.getDate(),
      isToday: i === 0,
    })
  }
  return days
})
const maxDayHours = computed(() => Math.max(...weekDays.value.map((d) => d.hours), 8))

/* Unbilled groups by contract */
interface UnbilledGroup {
  contract: Contract
  client: Client | null
  entries: TimeEntry[]
  hours: number
  amount: number
}
const unbilledByContract = computed<UnbilledGroup[]>(() => {
  const map = new Map<number, UnbilledGroup>()
  for (const e of unbilled.value) {
    const k = e.contract_id
    const contract = contracts.value.find((c) => c.id === k)
    if (!contract) continue
    const client = clients.value.find((c) => c.id === contract.client_id) || null
    let g = map.get(k)
    if (!g) {
      g = { contract, client, entries: [], hours: 0, amount: 0 }
      map.set(k, g)
    }
    g.entries.push(e)
    g.hours += e.hours
    g.amount += e.amount
  }
  return [...map.values()].sort((a, b) => b.amount - a.amount)
})

/* Draft-invoice summary (only when stats has a pending draft invoice) */
const draftGroups = computed<UnbilledGroup[]>(() => unbilledByContract.value.slice(0, 3))

/* Recent grouped by date */
const recentByDate = computed(() => {
  const rows = [...recent.value]
    .sort((a, b) => b.date.localeCompare(a.date))
    .slice(0, 10)
  const map = new Map<string, TimeEntry[]>()
  for (const e of rows) {
    const d = e.date.slice(0, 10)
    const list = map.get(d) ?? []
    list.push(e)
    map.set(d, list)
  }
  return [...map.entries()].sort((a, b) => b[0].localeCompare(a[0]))
})

/* handlers */
function goInvoice(clientId?: number) {
  if (clientId) router.push(`/invoices?new=1&client_id=${clientId}`)
  else router.push('/invoices?new=1')
}
function onLogged() {
  loadAll()
}
</script>

<template>
  <div class="dash">
    <header class="page-head">
      <div class="head-left">
        <div class="eyebrow">Week {{ weekNumber }} · {{ new Date().toLocaleDateString('en-US', { weekday: 'long' }) }}</div>
        <div class="head-title">
          {{ headerDate.first }},<span class="head-italic">&nbsp;{{ headerDate.rest }}</span>
        </div>
      </div>
      <div class="head-actions">
        <button class="btn" @click="() => router.push('/time')">Open time log</button>
        <button class="btn btn-primary" @click="() => goInvoice()">New invoice</button>
      </div>
    </header>

    <CommandBar
      ref="commandRef"
      :clients="clients"
      :contracts="contracts"
      :today="today"
      @logged="onLogged"
    />

    <!-- Stat strip -->
    <section class="stat-strip">
      <div class="stat">
        <div class="stat-label"><Dot :color="'var(--ink-4)'" /> This week</div>
        <div class="stat-hero">
          <Display :size="44">{{ weekHours.toFixed(2) }}</Display>
          <span class="stat-unit">h</span>
        </div>
        <div class="stat-sub">{{ fmtUSDShort(weekValue) }}</div>
      </div>
      <div class="stat">
        <div class="stat-label"><Dot :color="'var(--unbilled)'" /> Unbilled</div>
        <div class="stat-hero">
          <Display :size="30" color="var(--ink-3)">$</Display>
          <Display :size="44">{{ Math.round(unbilledTotal).toLocaleString('en-US') }}</Display>
        </div>
        <div class="stat-sub">{{ unbilledHours.toFixed(2) }}h · {{ unbilled.length }} entries</div>
      </div>
      <div class="stat">
        <div class="stat-label"><Dot :color="'var(--billable)'" /> Paid MTD</div>
        <div class="stat-hero">
          <Display :size="30" color="var(--ink-3)">$</Display>
          <Display :size="44">{{ Math.round(stats?.paid_amount ?? 0).toLocaleString('en-US') }}</Display>
        </div>
        <div class="stat-sub">{{ stats?.invoices_paid ?? 0 }} invoices paid</div>
      </div>
      <div class="stat">
        <div class="stat-label">
          <Dot :color="(stats?.invoices_pending ?? 0) > 0 ? 'var(--overdue)' : 'var(--ink-4)'" />
          Outstanding
        </div>
        <div class="stat-hero">
          <Display :size="30" color="var(--ink-3)">$</Display>
          <Display :size="44">{{ Math.round(stats?.outstanding_amount ?? 0).toLocaleString('en-US') }}</Display>
        </div>
        <div class="stat-sub">{{ stats?.invoices_pending ?? 0 }} invoices pending</div>
      </div>
    </section>

    <!-- 2-col: chart + draft invoice -->
    <section class="two-col">
      <div class="card chart-card">
        <div class="card-head">
          <div>
            <div class="eyebrow">This week</div>
            <div class="card-title">Hours logged by day</div>
          </div>
          <Num size="11.5" color="var(--ink-3)">{{ weekHours.toFixed(2) }}h total</Num>
        </div>
        <div class="chart">
          <div
            v-for="r in [0.25, 0.5, 0.75, 1]"
            :key="r"
            class="gridline"
            :style="{ bottom: r * 100 + '%' }"
          />
          <div class="bars">
            <div
              v-for="d in weekDays"
              :key="d.iso"
              class="bar-col"
            >
              <div class="bar-wrap">
                <span
                  v-if="d.hours > 0"
                  class="bar-val"
                >{{ d.hours.toFixed(1) }}</span>
                <div
                  class="bar"
                  :class="{ today: d.isToday }"
                  :style="{ height: (d.hours / maxDayHours) * 100 + '%' }"
                />
              </div>
            </div>
          </div>
        </div>
        <div class="chart-x">
          <div v-for="d in weekDays" :key="d.iso" class="x-tick" :class="{ today: d.isToday }">
            <div class="x-dow">{{ d.dow }}</div>
            <div class="x-dnum">{{ d.dnum }}</div>
          </div>
        </div>
      </div>

      <div class="card draft-card">
        <div class="card-head">
          <div>
            <div class="eyebrow">Ready to invoice</div>
            <div class="card-title">{{ unbilledByContract[0]?.client?.name ?? 'Nothing pending' }}</div>
          </div>
          <StatusChip :status="'draft'" v-if="unbilledByContract.length" />
        </div>

        <div v-if="unbilledByContract.length" class="draft-total">
          <Display :size="30">{{ fmtUSD(unbilledTotal) }}</Display>
          <span class="draft-sub">{{ unbilledHours.toFixed(2) }}h · {{ unbilled.length }} entries</span>
        </div>

        <div v-if="draftGroups.length" class="draft-lines">
          <div v-for="g in draftGroups" :key="g.contract.id" class="draft-line">
            <span class="dl-num">{{ g.contract.contract_number }}</span>
            <span class="dl-name">{{ g.contract.name || g.client?.name }}</span>
            <Num size="11.5" color="var(--ink-3)">{{ g.hours.toFixed(2) }}h</Num>
            <Num size="11.5">{{ fmtUSD(g.amount) }}</Num>
          </div>
        </div>

        <div class="draft-actions">
          <button
            class="btn btn-primary"
            :disabled="!unbilledByContract.length"
            @click="() => goInvoice(unbilledByContract[0]?.client?.id)"
          >
            Create invoice →
          </button>
        </div>
      </div>
    </section>

    <!-- Unbilled table -->
    <section class="card no-pad">
      <div class="card-head padded">
        <div>
          <div class="eyebrow"><Dot :color="'var(--unbilled)'" />&nbsp;Ready to invoice</div>
          <div class="card-title">
            {{ unbilled.length }} unbilled entries across {{ unbilledByContract.length }} contracts
          </div>
        </div>
        <button
          class="btn"
          :disabled="!unbilledByContract.length"
          @click="() => goInvoice()"
        >
          Bulk invoice
        </button>
      </div>
      <table class="table unbilled-table">
        <thead>
          <tr>
            <th>Contract</th>
            <th>Client</th>
            <th>Engagement</th>
            <th class="num">Hours</th>
            <th class="num">Rate</th>
            <th class="num">Amount</th>
            <th class="num"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!unbilledByContract.length">
            <td colspan="7" class="empty-row">Nothing unbilled. Great.</td>
          </tr>
          <tr v-for="r in unbilledByContract" :key="r.contract.id">
            <td class="mono">{{ r.contract.contract_number }}</td>
            <td>{{ r.client?.name ?? '—' }}</td>
            <td class="text-secondary">{{ r.contract.name }}</td>
            <td class="num">{{ r.hours.toFixed(2) }}h</td>
            <td class="num text-secondary">${{ r.contract.hourly_rate }}</td>
            <td class="num" style="font-weight: 500">{{ fmtUSD(r.amount) }}</td>
            <td class="num">
              <a class="link-accent" @click.prevent="() => goInvoice(r.client?.id)">Invoice →</a>
            </td>
          </tr>
        </tbody>
        <tfoot v-if="unbilledByContract.length">
          <tr>
            <td colspan="3" class="footer-label">Total unbilled</td>
            <td class="num">{{ unbilledHours.toFixed(2) }}h</td>
            <td class="num"></td>
            <td class="num" colspan="2">
              <Display :size="20">{{ fmtUSD(unbilledTotal) }}</Display>
            </td>
          </tr>
        </tfoot>
      </table>
    </section>

    <!-- Recent activity -->
    <section class="card no-pad">
      <div class="card-head padded">
        <div class="card-title">Recent activity</div>
        <a class="link-accent" @click.prevent="() => router.push('/time')">View time log →</a>
      </div>
      <div v-if="!recentByDate.length" class="empty-row" style="padding: 40px">
        No activity yet.
      </div>
      <div v-for="[date, entries] in recentByDate" :key="date" class="date-group">
        <div class="date-head">
          <div class="date-left">
            <span class="date-rel">{{ relDay(date) }}</span>
            <span class="date-iso">{{ fmtDateShort(date) }} · {{ fmtDOW(date) }}</span>
          </div>
          <Num size="11" color="var(--ink-2)">{{ entries.reduce((a, e) => a + e.hours, 0).toFixed(2) }}h total</Num>
        </div>
        <div v-for="e in entries" :key="e.id" class="entry-row">
          <span class="er-num">{{ e.contract_number }}</span>
          <span class="er-client">{{ e.client_name }}</span>
          <span class="er-desc">{{ e.description || '—' }}</span>
          <Num size="12">{{ e.hours.toFixed(2) }}h</Num>
          <Num size="12" color="var(--ink-3)">{{ fmtUSD(e.amount) }}</Num>
          <span class="er-status">
            <StatusChip v-if="e.invoice_number" :status="'invoiced'" />
            <span v-else class="er-unbilled">Unbilled</span>
          </span>
        </div>
      </div>
    </section>

    <footer class="page-foot">
      <span class="foot-hint"><Kbd>⌘</Kbd><Kbd>K</Kbd> Quick add</span>
      <span class="foot-hint"><Kbd>⌘</Kbd><Kbd>I</Kbd> New invoice</span>
      <span class="foot-hint"><Kbd>⌘</Kbd><Kbd>/</Kbd> Search</span>
    </footer>
  </div>
</template>

<style scoped>
.dash {
  display: flex;
  flex-direction: column;
  gap: var(--space-lg);
}

/* Page head */
.page-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
}
.eyebrow {
  font-family: var(--font-sans);
  font-size: 10.5px;
  color: var(--ink-3);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: 4px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.head-title {
  font-family: var(--font-serif);
  font-size: 30px;
  color: var(--ink);
  letter-spacing: -0.01em;
  line-height: 1;
  white-space: nowrap;
}
.head-italic {
  color: var(--ink-3);
  font-style: italic;
}
.head-actions {
  display: flex;
  gap: 8px;
}

/* Stat strip */
.stat-strip {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  background: var(--surface);
  border: 0.5px solid var(--rule);
  border-radius: var(--r-md);
}
.stat {
  padding: 16px 20px;
  border-right: 0.5px solid var(--rule);
}
.stat:last-child {
  border-right: none;
}
.stat-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-family: var(--font-sans);
  font-size: 10.5px;
  color: var(--ink-3);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: 10px;
  font-weight: 500;
}
.stat-hero {
  display: flex;
  align-items: baseline;
  gap: 3px;
}
.stat-unit {
  font-family: var(--font-serif);
  font-size: 20px;
  color: var(--ink-3);
  font-style: italic;
}
.stat-sub {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-3);
  margin-top: 6px;
  font-variant-numeric: tabular-nums;
}

/* Two-col */
.two-col {
  display: grid;
  grid-template-columns: 1.35fr 1fr;
  gap: var(--space-md);
}
.card.no-pad { padding: 0; }
.card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 14px;
}
.card-head.padded {
  padding: 14px 20px;
  border-bottom: 0.5px solid var(--rule);
  margin-bottom: 0;
}
.card-title {
  font-family: var(--font-serif);
  font-size: 18px;
  color: var(--ink);
  letter-spacing: -0.01em;
  line-height: 1.2;
}

/* Week chart */
.chart-card { display: flex; flex-direction: column; }
.chart {
  flex: 1;
  position: relative;
  min-height: 160px;
}
.gridline {
  position: absolute;
  left: 0;
  right: 0;
  height: 0.5px;
  background: var(--rule);
}
.bars {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: flex-end;
  padding: 0 6px;
}
.bar-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  height: 100%;
}
.bar-wrap {
  flex: 1;
  width: 100%;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  align-items: center;
  position: relative;
  padding-bottom: 0;
}
.bar-val {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--ink-2);
  font-variant-numeric: tabular-nums;
  margin-bottom: 4px;
}
.bar {
  width: 26px;
  background: var(--ink-2);
  border-radius: 2px 2px 0 0;
  min-height: 2px;
  transition: height var(--duration-medium) var(--ease-out);
}
.bar.today { background: var(--ink); }
.chart-x {
  display: flex;
  border-top: 0.5px solid var(--rule);
  padding: 8px 6px 0;
  margin-top: 8px;
}
.x-tick {
  flex: 1;
  text-align: center;
}
.x-dow {
  font-family: var(--font-sans);
  font-size: 10.5px;
  color: var(--ink-3);
}
.x-tick.today .x-dow { color: var(--ink); font-weight: 500; }
.x-dnum {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--ink-4);
  margin-top: 1px;
}

/* Draft card */
.draft-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.draft-total {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.draft-sub {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-3);
  font-variant-numeric: tabular-nums;
}
.draft-lines {
  border-top: 0.5px solid var(--rule);
  padding-top: 10px;
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.draft-line {
  display: flex;
  align-items: baseline;
  gap: 10px;
  font-size: 12px;
}
.dl-num {
  font-family: var(--font-mono);
  color: var(--ink-3);
  font-size: 11px;
  width: 100px;
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
}
.dl-name {
  flex: 1;
  color: var(--ink-2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.draft-actions {
  margin-top: auto;
  display: flex;
  gap: 8px;
}

/* Tables */
.unbilled-table th,
.unbilled-table td {
  padding: 9px 20px;
}
.unbilled-table .footer-label {
  font-family: var(--font-sans);
  font-size: 11px;
  color: var(--ink-3);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  text-align: right;
  background: var(--surface-alt);
}
.unbilled-table tfoot td {
  background: var(--surface-alt);
  border-top: 0.5px solid var(--rule-strong);
}
.empty-row {
  padding: 32px;
  text-align: center;
  color: var(--ink-3);
  font-size: 12px;
}
.link-accent {
  color: var(--accent);
  cursor: pointer;
  font-size: 11.5px;
  font-weight: 500;
}
.link-accent:hover {
  text-decoration: underline;
}

/* Recent */
.date-group {
  display: flex;
  flex-direction: column;
}
.date-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 7px 20px;
  background: var(--surface-alt);
  border-bottom: 0.5px solid var(--rule);
  border-top: 0.5px solid var(--rule);
}
.date-group:first-child .date-head { border-top: none; }
.date-left {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.date-rel {
  font-family: var(--font-sans);
  font-size: 11.5px;
  color: var(--ink);
  font-weight: 500;
}
.date-iso {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--ink-3);
}
.entry-row {
  display: grid;
  grid-template-columns: 100px 180px 1fr 70px 80px 90px;
  align-items: center;
  gap: 12px;
  padding: 8px 20px;
  border-bottom: 0.5px solid var(--rule);
  font-size: 12px;
}
.entry-row:last-child { border-bottom: none; }
.entry-row:hover { background: var(--hover); }
.er-num {
  font-family: var(--font-mono);
  color: var(--ink-3);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.er-client { color: var(--ink); }
.er-desc {
  color: var(--ink-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.entry-row :deep(.num) { text-align: right; }
.er-status { text-align: right; }
.er-unbilled {
  font-family: var(--font-sans);
  font-size: 10.5px;
  color: var(--unbilled);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

/* Foot */
.page-foot {
  display: flex;
  gap: var(--space-lg);
  padding: var(--space-md) 0;
  color: var(--ink-3);
}
.foot-hint {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-family: var(--font-sans);
  font-size: 11px;
}

/* Responsive */
@media (max-width: 1100px) {
  .stat-strip { grid-template-columns: repeat(2, 1fr); }
  .stat:nth-child(2) { border-right: none; }
  .two-col { grid-template-columns: 1fr; }
  .entry-row { grid-template-columns: 80px 120px 1fr 60px 70px 70px; gap: 8px; }
}
@media (max-width: 700px) {
  .stat-strip { grid-template-columns: 1fr; }
  .stat { border-right: none; border-bottom: 0.5px solid var(--rule); }
  .stat:last-child { border-bottom: none; }
  .entry-row { grid-template-columns: 1fr; gap: 4px; }
  .head-title { font-size: 24px; }
}
</style>
