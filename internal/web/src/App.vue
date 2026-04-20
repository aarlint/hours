<script setup lang="ts">
import { RouterView, useRoute, useRouter } from 'vue-router'
import { computed, onMounted, ref, watch } from 'vue'
import type { Client, Contract, BusinessInfo } from './types'
import { api } from './api'
import { useToasts } from './composables/useToasts'
import { onRealtime, startRealtime, useRealtimeStatus, changeTick, isOwn } from './composables/useRealtime'
import { useShortcuts } from './composables/useShortcuts'
import { appAction } from './composables/useAppBus'
import Toasts from './components/Toasts.vue'
import Kbd from './components/Kbd.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'

const route = useRoute()
const router = useRouter()
const toasts = useToasts()
const rtStatus = useRealtimeStatus()

const theme = ref<'dark' | 'light'>(
  (localStorage.getItem('theme') as 'dark' | 'light') || 'light',
)
function applyTheme() {
  document.documentElement.setAttribute('data-theme', theme.value)
  localStorage.setItem('theme', theme.value)
}
function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : 'dark'
  applyTheme()
}

const clients = ref<Client[]>([])
const contracts = ref<Contract[]>([])
const business = ref<BusinessInfo | null>(null)
const expanded = ref<Record<number, boolean>>({})

async function loadSidebar() {
  try {
    const [cs, ks, bi] = await Promise.all([
      api.listClients(),
      api.listContracts(),
      api.getBusinessInfo().catch(() => null),
    ])
    clients.value = cs
    contracts.value = ks
    business.value = bi
  } catch {}
}

function contractsFor(clientId: number): Contract[] {
  return contracts.value.filter((c) => c.client_id === clientId)
}

function toggle(id: number) {
  expanded.value[id] = !expanded.value[id]
}

const showRightRail = computed(() => route.name === 'dashboard')

const nav = [
  { to: '/dashboard', label: 'Dashboard' },
  { to: '/time', label: 'Time log' },
  { to: '/quotes', label: 'Quotes' },
  { to: '/invoices', label: 'Invoices' },
  { to: '/clients', label: 'Clients' },
]
const views = [
  { to: '/invoices?status=pending', label: 'Pending invoices' },
  { to: '/invoices?status=overdue', label: 'Overdue invoices' },
  { to: '/contracts', label: 'Contracts' },
]

const dateBadge = computed(() => {
  const d = new Date()
  const day = d.toLocaleDateString('en-US', { weekday: 'short' })
  const mo = d.toLocaleDateString('en-US', { month: 'short' })
  return `${day} · ${mo} ${d.getDate()}`
})

onMounted(async () => {
  applyTheme()
  await loadSidebar()
  startRealtime()
})

watch(changeTick, () => {
  loadSidebar()
})

onRealtime((ev) => {
  if (ev.kind === 'time_entry.created') {
    const d = (ev.detail ?? {}) as any
    if (isOwn(d.id)) return
    toasts.push({
      tone: 'info',
      title: d.summary || 'New time entry',
      detail: d.client_name ? `${d.client_name} · ${d.contract_number ?? ''}` : undefined,
      meta: d.hours != null ? `${(+d.hours).toFixed(2)}h` : undefined,
    })
  } else if (ev.kind === 'invoice.created') {
    const d = (ev.detail ?? {}) as any
    toasts.push({
      tone: 'success',
      title: `Invoice ${d.invoice_number ?? ''} created`.trim(),
      detail: d.client_name,
      meta: d.total_amount != null ? `$${(+d.total_amount).toFixed(2)}` : undefined,
    })
  } else if (ev.kind === 'invoice.updated') {
    const d = (ev.detail ?? {}) as any
    if (d.status) {
      toasts.push({
        tone: d.status === 'paid' ? 'success' : 'info',
        title: `Invoice ${d.invoice_number ?? ''} · ${d.status}`,
        detail: d.client_name,
      })
    }
  }
})

useShortcuts([
  { key: 'k', mod: true, handler: () => appAction('command') },
  { key: 'i', mod: true, handler: () => appAction('new-invoice') },
  { key: 'slash', mod: true, handler: () => appAction('search') },
  { key: 't', alt: true, handler: () => appAction('start-timer') },
  { key: 'j', mod: true, shift: true, handler: toggleTheme },
  {
    key: 'escape',
    allowInInput: true,
    handler: () => {
      const el = document.activeElement
      if (el instanceof HTMLElement) el.blur()
    },
  },
])
</script>

<template>
  <div class="shell">
    <aside class="sidebar">
      <div class="traffic-spacer" />

      <div class="brand-row">
        <img src="/icon.svg" alt="Hours" class="brand-mark" />
        <span class="brand-tag">~/made/by/arlint.dev</span>
      </div>

      <div class="status-row">
        <span class="dot" :style="{ background: rtStatus.connected ? 'var(--billable)' : 'var(--ink-4)' }" />
        <span class="status-txt">{{ rtStatus.connected ? 'Live' : 'Offline' }}</span>
      </div>

      <div class="nav-section">
        <div class="section-label">Workspace</div>
        <RouterLink v-for="n in nav" :key="n.to" :to="n.to" class="nav-item" active-class="on">
          {{ n.label }}
        </RouterLink>
      </div>

      <div class="nav-section">
        <div class="section-label">Clients</div>
        <div v-if="!clients.length" class="empty-line">No clients yet.</div>
        <div v-for="c in clients" :key="c.id" class="tree-branch">
          <div class="tree-head" @click="toggle(c.id)">
            <span class="caret" :class="{ open: expanded[c.id] }">›</span>
            <RouterLink :to="`/clients/${c.id}`" class="tree-name" @click.stop>
              {{ c.name }}
            </RouterLink>
            <span class="tree-count">{{ contractsFor(c.id).length }}</span>
          </div>
          <div v-if="expanded[c.id]" class="tree-children">
            <RouterLink
              v-for="k in contractsFor(c.id)"
              :key="k.id"
              :to="`/contracts?client_id=${c.id}`"
              class="tree-child"
            >
              <span class="c-num">{{ k.contract_number }}</span>
              <span class="c-rate">${{ k.hourly_rate }}/h</span>
            </RouterLink>
          </div>
        </div>
      </div>

      <div class="nav-section">
        <div class="section-label">Views</div>
        <RouterLink v-for="v in views" :key="v.to" :to="v.to" class="nav-item">
          {{ v.label }}
        </RouterLink>
      </div>

      <div class="sidebar-foot">
        <div class="biz-name">{{ business?.business_name ?? 'Business' }}</div>
        <div class="biz-meta">{{ business?.email ?? 'Set in Settings' }}</div>
        <RouterLink to="/settings" class="biz-settings">Settings →</RouterLink>
      </div>
    </aside>

    <main class="main">
      <header class="topbar">
        <div class="crumbs">
          <span class="crumb">{{ dateBadge }}</span>
          <span class="crumb-sep">·</span>
          <span class="crumb ink-1">{{ (route.meta as any)?.label || route.name }}</span>
        </div>
        <div class="topbar-actions">
          <span class="hint"><Kbd>⌘</Kbd><Kbd>K</Kbd> Quick add</span>
          <button class="btn btn-ghost" @click="toggleTheme" :title="'Switch to ' + (theme === 'dark' ? 'light' : 'dark')">
            {{ theme === 'dark' ? 'Light' : 'Dark' }}
          </button>
        </div>
      </header>

      <div class="view-area">
        <RouterView v-slot="{ Component }">
          <component :is="Component" />
        </RouterView>
      </div>
    </main>

    <Toasts />
    <ConfirmDialog />
  </div>
</template>

<style>
.shell {
  display: grid;
  grid-template-columns: 248px 1fr;
  min-height: 100vh;
  background: var(--bg);
}

/* ---------- Sidebar ---------- */
.sidebar {
  background: var(--sidebar);
  border-right: 0.5px solid var(--rule);
  padding: 0 0 var(--space-md);
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  position: sticky;
  top: 0;
  height: 100vh;
}

.traffic-spacer {
  height: 28px;
  flex-shrink: 0;
  --wails-draggable: drag;
  -webkit-app-region: drag;
}

.brand-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px 4px;
  gap: 10px;
}
.brand-mark {
  width: 28px;
  height: 28px;
  border-radius: 6px;
  display: block;
  flex-shrink: 0;
}
.brand-tag {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--ink-3);
  letter-spacing: 0;
  text-transform: none;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.status-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 16px 14px;
}
.status-row .dot {
  width: 6px;
  height: 6px;
  border-radius: 6px;
}
.status-txt {
  font-family: var(--font-sans);
  font-size: 10.5px;
  color: var(--ink-3);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.nav-section {
  padding: 10px 8px;
  border-top: 0.5px solid var(--rule);
}
.section-label {
  font-family: var(--font-sans);
  font-size: 9.5px;
  letter-spacing: 0.1em;
  color: var(--ink-4);
  text-transform: uppercase;
  padding: 0 8px 8px;
  font-weight: 500;
}
.empty-line {
  padding: 4px 10px;
  font-size: 12px;
  color: var(--ink-4);
}

.nav-item {
  display: block;
  padding: 5px 10px;
  font-family: var(--font-sans);
  font-size: 12.5px;
  color: var(--ink-2);
  border-radius: 5px;
  transition: background var(--duration-fast) var(--ease-out);
}
.nav-item:hover {
  background: var(--hover);
  color: var(--ink);
}
.nav-item.on {
  background: var(--selected);
  color: var(--ink);
}

.tree-branch {
  margin-bottom: 1px;
}
.tree-head {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  cursor: pointer;
  border-radius: 5px;
}
.tree-head:hover {
  background: var(--hover);
}
.caret {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-3);
  width: 10px;
  transition: transform var(--duration-fast) var(--ease-out);
  display: inline-block;
}
.caret.open {
  transform: rotate(90deg);
}
.tree-name {
  flex: 1;
  font-size: 12.5px;
  color: var(--ink);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tree-count {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--ink-4);
}
.tree-children {
  padding: 2px 0 6px 24px;
}
.tree-child {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  padding: 3px 8px;
  font-size: 11.5px;
  border-radius: 4px;
}
.tree-child:hover {
  background: var(--hover);
}
.c-num {
  font-family: var(--font-mono);
  color: var(--ink-2);
  font-variant-numeric: tabular-nums;
}
.c-rate {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--ink-3);
  font-variant-numeric: tabular-nums;
}

.sidebar-foot {
  margin-top: auto;
  padding: 14px 16px;
  border-top: 0.5px solid var(--rule);
}
.biz-name {
  font-family: var(--font-serif);
  font-size: 14px;
  color: var(--ink);
  letter-spacing: -0.01em;
}
.biz-meta {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--ink-3);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.biz-settings {
  display: inline-block;
  margin-top: 6px;
  font-size: 11px;
  color: var(--accent);
}

/* ---------- Main / Topbar ---------- */
.main {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 24px;
  border-bottom: 0.5px solid var(--rule);
  background: var(--bg);
  position: sticky;
  top: 0;
  z-index: 30;
  backdrop-filter: saturate(140%) blur(8px);
  --wails-draggable: drag;
  -webkit-app-region: drag;
}
.crumbs {
  display: flex;
  align-items: baseline;
  gap: 6px;
  font-family: var(--font-sans);
  font-size: 11.5px;
  color: var(--ink-3);
}
.crumb-sep { color: var(--ink-4); }
.crumb.ink-1 {
  color: var(--ink);
  text-transform: capitalize;
}
.topbar-actions {
  display: flex;
  align-items: center;
  gap: var(--space-md);
  --wails-draggable: no-drag;
  -webkit-app-region: no-drag;
}
.topbar button,
.topbar a,
.topbar input {
  --wails-draggable: no-drag;
  -webkit-app-region: no-drag;
}
.hint {
  display: flex;
  align-items: center;
  gap: 4px;
  font-family: var(--font-sans);
  font-size: 11px;
  color: var(--ink-3);
}

.view-area {
  flex: 1 1 auto;
  padding: var(--space-xl) var(--space-xl) var(--space-3xl);
  max-width: 1400px;
  width: 100%;
  margin: 0 auto;
}

@media (max-width: 1000px) {
  .shell {
    grid-template-columns: 220px 1fr;
  }
}
@media (max-width: 760px) {
  .shell {
    grid-template-columns: 1fr;
  }
  .sidebar {
    position: static;
    height: auto;
    border-right: none;
    border-bottom: 0.5px solid var(--rule);
  }
  .traffic-spacer { height: 8px; }
  .view-area {
    padding: var(--space-md);
  }
}
</style>
