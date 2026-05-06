<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import type {
  ApiToken,
  ApiTokenWithSecret,
  BusinessInfo,
  PaymentMethod,
  Scope,
  TokenUsageEvent,
  TokenUsageSummary,
} from '../types'
import { ALL_SCOPES } from '../types'
import { fmtRelTime } from '../components/primitives'
import PageHeader from '../components/PageHeader.vue'
import LoadingBar from '../components/LoadingBar.vue'
import Modal from '../components/Modal.vue'
import EmptyState from '../components/EmptyState.vue'
import { useConfirm } from '../composables/useConfirm'
import { useToasts } from '../composables/useToasts'
import { isWails, pickDirectory, revealInFinder, saveTextFile } from '../wailsShim'

const loading = ref(true)
const saving = ref(false)
const msg = ref<string>('')
const existed = ref(false)

const form = reactive<Partial<BusinessInfo>>({
  business_name: '',
  contact_name: '',
  email: '',
  phone: '',
  address: '',
  city: '',
  state: '',
  zip_code: '',
  country: '',
  tax_id: '',
  website: '',
  invoice_prefix: 'INV',
  logo_path: '',
  export_path: '',
})

// ---------- Payment Methods ----------
const { confirm } = useConfirm()
const { push: toast } = useToasts()

const methods = ref<PaymentMethod[]>([])
const methodsLoading = ref(true)
const methodModalOpen = ref(false)
const methodSaving = ref(false)
const methodError = ref<string | null>(null)
const editingMethodId = ref<number | null>(null)

const methodForm = reactive({
  label: '',
  bank_name: '',
  account_number: '',
  routing_number: '',
  swift_code: '',
  payment_terms: 'Net 30',
  notes: '',
  is_default: false,
})

async function loadMethods() {
  methodsLoading.value = true
  try {
    methods.value = await api.listPaymentMethods()
  } finally {
    methodsLoading.value = false
  }
}

function resetMethodForm() {
  Object.assign(methodForm, {
    label: '',
    bank_name: '',
    account_number: '',
    routing_number: '',
    swift_code: '',
    payment_terms: 'Net 30',
    notes: '',
    is_default: methods.value.length === 0, // first method = default
  })
  methodError.value = null
}

function openAddMethod() {
  editingMethodId.value = null
  resetMethodForm()
  methodModalOpen.value = true
}

function openEditMethod(m: PaymentMethod) {
  editingMethodId.value = m.id
  Object.assign(methodForm, {
    label: m.label,
    bank_name: m.bank_name ?? '',
    account_number: m.account_number ?? '',
    routing_number: m.routing_number ?? '',
    swift_code: m.swift_code ?? '',
    payment_terms: m.payment_terms ?? '',
    notes: m.notes ?? '',
    is_default: m.is_default,
  })
  methodError.value = null
  methodModalOpen.value = true
}

async function saveMethod() {
  methodSaving.value = true
  methodError.value = null
  try {
    if (editingMethodId.value != null) {
      await api.updatePaymentMethod(editingMethodId.value, { ...methodForm })
      toast({ tone: 'success', title: 'Payment method updated', detail: methodForm.label })
    } else {
      await api.addPaymentMethod({ ...methodForm })
      toast({ tone: 'success', title: 'Payment method added', detail: methodForm.label })
    }
    methodModalOpen.value = false
    await loadMethods()
  } catch (e: any) {
    methodError.value = e.message
  } finally {
    methodSaving.value = false
  }
}

async function removeMethod(m: PaymentMethod) {
  const ok = await confirm({
    title: `Delete payment method "${m.label}"?`,
    message: 'Any contracts and invoices pointing at this method will lose their link. Existing invoice PDFs already on disk are unaffected.',
    confirmLabel: 'Delete',
    cancelLabel: 'Cancel',
    tone: 'danger',
  })
  if (!ok) return
  try {
    const res = await api.deletePaymentMethod(m.id)
    toast({
      tone: 'success',
      title: 'Payment method deleted',
      detail: res.detached_contracts || res.detached_invoices
        ? `Detached ${res.detached_contracts} contract(s), ${res.detached_invoices} invoice(s)`
        : m.label,
    })
    await loadMethods()
  } catch (e: any) {
    toast({ tone: 'error', title: 'Delete failed', detail: e.message })
  }
}

const nativePicker = computed(() => isWails())

async function browseExport() {
  const picked = await pickDirectory('Invoice export folder')
  if (picked) form.export_path = picked
}

async function revealExport() {
  if (form.export_path) await revealInFinder(form.export_path)
}

async function load() {
  loading.value = true
  try {
    const info = await api.getBusinessInfo()
    if (info) {
      Object.assign(form, info)
      existed.value = true
    }
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  msg.value = ''
  try {
    await api.setBusinessInfo(form)
    msg.value = 'SAVED'
    existed.value = true
  } catch (e: any) {
    msg.value = 'ERROR: ' + e.message
  } finally {
    saving.value = false
  }
}

function toggleTheme() {
  const el = document.documentElement
  const curr = el.getAttribute('data-theme') || 'dark'
  const next = curr === 'dark' ? 'light' : 'dark'
  el.setAttribute('data-theme', next)
  try {
    localStorage.setItem('theme', next)
  } catch {}
}

// ---------- Data export/import ----------
const exporting = ref(false)
const importing = ref(false)
const importInput = ref<HTMLInputElement | null>(null)

async function exportData() {
  exporting.value = true
  try {
    const data = await api.exportData()
    const json = JSON.stringify(data, null, 2)
    const filename = `hours-export-${new Date().toISOString().slice(0, 10)}.json`

    // In the Wails webview the browser <a download> trick silently no-ops,
    // so we route through a native save dialog binding when available.
    if (isWails()) {
      const path = await saveTextFile(filename, json)
      if (!path) {
        // user cancelled the dialog
        return
      }
      toast({
        tone: 'success',
        title: 'Export saved',
        detail: path,
      })
      // Best-effort reveal so they can see the file.
      void revealInFinder(path)
      return
    }

    const blob = new Blob([json], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
    toast({ tone: 'success', title: 'Export ready', detail: 'Saved to your downloads folder' })
  } catch (e: any) {
    toast({ tone: 'error', title: 'Export failed', detail: e.message })
  } finally {
    exporting.value = false
  }
}

function pickImportFile() {
  importInput.value?.click()
}

async function onImportFile(ev: Event) {
  const target = ev.target as HTMLInputElement
  const file = target.files?.[0]
  target.value = ''
  if (!file) return
  const ok = await confirm({
    title: 'Replace all data with this export?',
    message:
      'Every client, contract, time entry, invoice, quote and expense will be deleted and replaced by the contents of this file. This cannot be undone.',
    confirmLabel: 'Replace everything',
    cancelLabel: 'Cancel',
    tone: 'danger',
  })
  if (!ok) return
  importing.value = true
  try {
    const text = await file.text()
    const payload = JSON.parse(text)
    const res = await api.importData(payload)
    const total = Object.values(res.imported).reduce((a, b) => a + b, 0)
    toast({
      tone: 'success',
      title: 'Import complete',
      detail: `Loaded ${total} rows across ${Object.keys(res.imported).length} tables`,
    })
    setTimeout(() => window.location.reload(), 600)
  } catch (e: any) {
    toast({ tone: 'error', title: 'Import failed', detail: e.message })
  } finally {
    importing.value = false
  }
}

// ---------- API tokens ----------
//
// Tokens are personal bearer credentials minted with a chosen scope subset.
// The raw value is only ever returned by the create call; the list endpoint
// exposes metadata + a visible prefix for identification.

interface ScopeGroup {
  label: string
  read?: Scope
  write?: Scope
  scopes: Scope[]
}

// SCOPE_GROUPS pairs read/write scopes per resource so the modal renders
// "Clients [read] [write]" rows. Standalone scopes (stats:read, events:read,
// data:export, data:import) live in singleScopes below.
const SCOPE_GROUPS: ScopeGroup[] = [
  { label: 'Clients', read: 'clients:read', write: 'clients:write', scopes: ['clients:read', 'clients:write'] },
  { label: 'Contracts', read: 'contracts:read', write: 'contracts:write', scopes: ['contracts:read', 'contracts:write'] },
  { label: 'Time Entries', read: 'time_entries:read', write: 'time_entries:write', scopes: ['time_entries:read', 'time_entries:write'] },
  { label: 'Invoices', read: 'invoices:read', write: 'invoices:write', scopes: ['invoices:read', 'invoices:write'] },
  { label: 'Quotes', read: 'quotes:read', write: 'quotes:write', scopes: ['quotes:read', 'quotes:write'] },
  { label: 'Expenses', read: 'expenses:read', write: 'expenses:write', scopes: ['expenses:read', 'expenses:write'] },
  { label: 'Payment Methods', read: 'payment_methods:read', write: 'payment_methods:write', scopes: ['payment_methods:read', 'payment_methods:write'] },
  { label: 'Business Info', read: 'business_info:read', write: 'business_info:write', scopes: ['business_info:read', 'business_info:write'] },
  { label: 'Recipients', read: 'recipients:read', write: 'recipients:write', scopes: ['recipients:read', 'recipients:write'] },
]

// singleScopes are standalone (no read/write pair). Rendered separately below
// the resource grid in the modal.
const SINGLE_SCOPES: Array<{ scope: Scope; label: string; description: string }> = [
  { scope: 'stats:read', label: 'Stats', description: 'Dashboard counters and aggregates' },
  { scope: 'events:read', label: 'Events Stream', description: 'Live SSE feed of changes' },
  { scope: 'data:export', label: 'Data Export', description: 'Download a full JSON dump' },
  { scope: 'data:import', label: 'Data Import (admin)', description: 'Replace every row from an export — admin-only' },
]

const tokens = ref<ApiToken[]>([])
const tokensLoading = ref(true)
const tokenModalOpen = ref(false)
const tokenSaving = ref(false)
const tokenError = ref<string | null>(null)

const tokenForm = reactive({
  name: '',
  scopes: new Set<Scope>(),
  expires_at: '', // YYYY-MM-DD or empty
})

// secretModal carries the just-minted raw token. Shown exactly once after a
// successful create — backend never returns it again.
const secretModalOpen = ref(false)
const newSecret = ref<ApiTokenWithSecret | null>(null)
const secretCopied = ref(false)

const userRole = ref<'admin' | 'user' | string>('user')

// MCP snippet shown in the help block below the API Tokens header. Uses
// window.location.origin so anyone forking this deployment doesn't have to
// edit hard-coded URLs.
const mcpSnippet = computed(() => {
  const origin = typeof window !== 'undefined'
    ? window.location.origin
    : 'https://hours.arlint.dev'
  return `{
  "mcpServers": {
    "hours": {
      "command": "npx",
      "args": [
        "-y",
        "mcp-remote",
        "${origin}/api/mcp",
        "--header",
        "Authorization: Bearer ht_PASTE_TOKEN"
      ]
    }
  }
}`
})

const mcpCopied = ref(false)
async function copyMcpSnippet() {
  try {
    await navigator.clipboard.writeText(mcpSnippet.value)
    mcpCopied.value = true
    setTimeout(() => { mcpCopied.value = false }, 1500)
  } catch (_) {
    toast({ tone: 'error', title: 'Copy failed', detail: 'Select the snippet and copy manually.' })
  }
}

const adminOnlyScopes: Scope[] = ['data:import']

function isScopeDisabled(scope: Scope): boolean {
  if (userRole.value === 'admin') return false
  return adminOnlyScopes.includes(scope)
}

async function loadTokens() {
  tokensLoading.value = true
  try {
    tokens.value = await api.listApiTokens()
  } catch (e: any) {
    toast({ tone: 'error', title: 'Failed to load tokens', detail: e.message })
  } finally {
    tokensLoading.value = false
  }
}

function resetTokenForm() {
  tokenForm.name = ''
  tokenForm.scopes = new Set<Scope>()
  tokenForm.expires_at = ''
  tokenError.value = null
}

function openCreateToken() {
  resetTokenForm()
  tokenModalOpen.value = true
}

function toggleScope(scope: Scope) {
  if (isScopeDisabled(scope)) return
  // reactivity on Set: rebuild the object so Vue notices.
  const next = new Set(tokenForm.scopes)
  if (next.has(scope)) next.delete(scope)
  else next.add(scope)
  tokenForm.scopes = next
}

function presetReadAll() {
  const next = new Set<Scope>()
  for (const s of ALL_SCOPES) {
    if (s.endsWith(':read') || s === 'data:export') {
      if (!isScopeDisabled(s as Scope)) next.add(s as Scope)
    }
  }
  tokenForm.scopes = next
}

function presetWriteAll() {
  const next = new Set<Scope>()
  for (const s of ALL_SCOPES) {
    if (s.endsWith(':write')) {
      if (!isScopeDisabled(s as Scope)) next.add(s as Scope)
    }
  }
  tokenForm.scopes = next
}

function presetAll() {
  const next = new Set<Scope>()
  for (const s of ALL_SCOPES) {
    if (!isScopeDisabled(s as Scope)) next.add(s as Scope)
  }
  tokenForm.scopes = next
}

function presetClear() {
  tokenForm.scopes = new Set<Scope>()
}

async function submitToken() {
  tokenError.value = null
  const name = tokenForm.name.trim()
  if (!name) {
    tokenError.value = 'Name is required'
    return
  }
  const scopes = Array.from(tokenForm.scopes)
  if (scopes.length === 0) {
    tokenError.value = 'Select at least one scope'
    return
  }
  let expires_at: string | null = null
  if (tokenForm.expires_at) {
    // Browser <input type="date"> emits YYYY-MM-DD; promote to end-of-day UTC
    // RFC3339 so the backend's time.Parse(RFC3339) succeeds.
    expires_at = `${tokenForm.expires_at}T23:59:59Z`
    const parsed = new Date(expires_at)
    if (Number.isNaN(parsed.getTime()) || parsed <= new Date()) {
      tokenError.value = 'Expiry must be in the future'
      return
    }
  }
  tokenSaving.value = true
  try {
    const created = await api.createApiToken({ name, scopes, expires_at })
    tokenModalOpen.value = false
    newSecret.value = created
    secretCopied.value = false
    secretModalOpen.value = true
    await loadTokens()
  } catch (e: any) {
    tokenError.value = e.message
  } finally {
    tokenSaving.value = false
  }
}

async function copySecret() {
  if (!newSecret.value) return
  try {
    await navigator.clipboard.writeText(newSecret.value.token)
    secretCopied.value = true
    setTimeout(() => (secretCopied.value = false), 2000)
  } catch (e: any) {
    toast({ tone: 'error', title: 'Copy failed', detail: e.message })
  }
}

function dismissSecret() {
  secretModalOpen.value = false
  newSecret.value = null
}

// ---------- Per-token usage ----------
//
// The usage panel under each token row is lazy-loaded the first time the
// user clicks USAGE, then cached for ~30s so collapsing-and-reopening
// doesn't refetch unnecessarily. Two endpoints feed it: a summary
// (counters + top paths) and a recent-history list.

interface TokenUsageState {
  summary: TokenUsageSummary | null
  recent: TokenUsageEvent[]
  loading: boolean
  error: string | null
  fetchedAt: number // ms-since-epoch; 0 = never
}

const TOKEN_USAGE_TTL_MS = 30_000

const expandedTokenId = ref<number | null>(null)
const tokenUsage = reactive<Record<number, TokenUsageState>>({})

function emptyUsageState(): TokenUsageState {
  return { summary: null, recent: [], loading: false, error: null, fetchedAt: 0 }
}

async function loadTokenUsage(id: number, force = false) {
  // Important: when assigning a fresh object into a reactive() Record, the
  // proxy substitutes a reactive copy under the key. Mutating the local
  // reference directly bypasses reactivity and leaves the template stuck on
  // the previous render. Always re-read through the proxy before mutating.
  if (!tokenUsage[id]) {
    tokenUsage[id] = emptyUsageState()
  }
  const state = tokenUsage[id]
  if (!force && state.fetchedAt && Date.now() - state.fetchedAt < TOKEN_USAGE_TTL_MS) {
    return
  }
  state.loading = true
  state.error = null
  try {
    const [summary, recent] = await Promise.all([
      api.getApiTokenUsage(id),
      api.getApiTokenUsageRecent(id),
    ])
    state.summary = summary
    state.recent = recent
    state.fetchedAt = Date.now()
  } catch (e: any) {
    state.error = e?.message ?? 'failed to load usage'
  } finally {
    state.loading = false
  }
}

function toggleTokenUsage(id: number) {
  if (expandedTokenId.value === id) {
    expandedTokenId.value = null
    return
  }
  expandedTokenId.value = id
  void loadTokenUsage(id)
}

function fmtStatus(s: number | null | undefined): string {
  if (s == null) return '—'
  return String(s)
}

function statusTone(s: number | null | undefined): string {
  if (s == null) return 'text-disabled'
  if (s >= 500) return 'text-accent'
  if (s >= 400) return 'text-warning'
  return 'text-success'
}

async function revokeToken(t: ApiToken) {
  const ok = await confirm({
    title: `Revoke token "${t.name}"?`,
    message:
      'Anything authenticating with this token (Claude Desktop, scripts, integrations) will immediately stop working. This cannot be undone.',
    confirmLabel: 'Revoke',
    cancelLabel: 'Cancel',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await api.revokeApiToken(t.id)
    toast({ tone: 'success', title: 'Token revoked', detail: t.name })
    await loadTokens()
  } catch (e: any) {
    toast({ tone: 'error', title: 'Revoke failed', detail: e.message })
  }
}

function fmtDate(s?: string | null): string {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  return d.toISOString().slice(0, 10)
}

function fmtDateTime(s?: string | null): string {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  // local time, minute precision — keeps the table compact.
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

onMounted(async () => {
  // Capture role so we can disable admin-only scope checkboxes pre-emptively
  // (the server enforces it too — this is just a UX nicety).
  try {
    const me = await api.me()
    if (me?.role) userRole.value = me.role
  } catch {}
  await Promise.all([load(), loadMethods(), loadTokens()])
})
</script>

<template>
  <div>
    <PageHeader
      category="SYSTEM"
      title="Settings"
      subtitle="Business identity and defaults used when generating invoices."
    >
      <template #actions>
        <button class="btn btn-ghost" @click="toggleTheme">TOGGLE THEME</button>
        <span v-if="msg" class="mono-label" :class="msg === 'SAVED' ? 'text-success' : 'text-accent'">
          [ {{ msg }} ]
        </span>
      </template>
    </PageHeader>

    <LoadingBar v-if="loading" />

    <template v-else>
      <div v-if="!existed" class="hint-banner">
        <span class="mono-label text-warning">[ NO BUSINESS INFO SET ]</span>
        <span class="hint-text">Fill this in before generating invoices — it appears on every PDF.</span>
      </div>

      <form class="settings-grid" @submit.prevent="save">
        <section class="group">
          <div class="group-head">
            <span class="mono-label">IDENTITY</span>
            <span class="mono-label text-disabled">REQUIRED</span>
          </div>
          <div class="field">
            <label>BUSINESS NAME</label>
            <input v-model="form.business_name" class="input" required />
          </div>
          <div class="row">
            <div class="field grow">
              <label>CONTACT NAME</label>
              <input v-model="form.contact_name" class="input" />
            </div>
            <div class="field grow">
              <label>EMAIL</label>
              <input v-model="form.email" class="input" type="email" required />
            </div>
          </div>
          <div class="row">
            <div class="field grow">
              <label>PHONE</label>
              <input v-model="form.phone" class="input" />
            </div>
            <div class="field grow">
              <label>WEBSITE</label>
              <input v-model="form.website" class="input" placeholder="https://" />
            </div>
          </div>
        </section>

        <section class="group">
          <div class="group-head">
            <span class="mono-label">ADDRESS</span>
          </div>
          <div class="field">
            <label>STREET</label>
            <input v-model="form.address" class="input" />
          </div>
          <div class="row">
            <div class="field grow">
              <label>CITY</label>
              <input v-model="form.city" class="input" />
            </div>
            <div class="field">
              <label>STATE</label>
              <input v-model="form.state" class="input" style="width: 120px" />
            </div>
            <div class="field">
              <label>ZIP</label>
              <input v-model="form.zip_code" class="input" style="width: 140px" />
            </div>
          </div>
          <div class="field">
            <label>COUNTRY</label>
            <input v-model="form.country" class="input" />
          </div>
        </section>

        <section class="group">
          <div class="group-head">
            <span class="mono-label">INVOICING</span>
          </div>
          <div class="row">
            <div class="field">
              <label>INVOICE PREFIX</label>
              <input
                v-model="form.invoice_prefix"
                class="input"
                style="width: 160px"
                placeholder="INV"
              />
            </div>
            <div class="field grow">
              <label>TAX ID / EIN</label>
              <input v-model="form.tax_id" class="input" />
            </div>
          </div>
          <div class="field">
            <label>LOGO PATH (OPTIONAL)</label>
            <input v-model="form.logo_path" class="input" placeholder="/path/to/logo.png" />
            <div class="help-text mono-label text-disabled">
              ABSOLUTE PATH TO A PNG/JPG TO EMBED IN INVOICE PDFS
            </div>
          </div>
          <div class="field">
            <label>INVOICE EXPORT FOLDER</label>
            <div class="path-row">
              <input
                v-model="form.export_path"
                class="input grow"
                placeholder="~/Downloads"
              />
              <button
                v-if="nativePicker"
                type="button"
                class="btn btn-ghost"
                @click="browseExport"
              >
                BROWSE
              </button>
              <button
                v-if="nativePicker && form.export_path"
                type="button"
                class="btn btn-ghost"
                @click="revealExport"
              >
                REVEAL
              </button>
            </div>
            <div class="help-text mono-label text-disabled">
              WHERE GENERATED INVOICE PDFS ARE SAVED. DEFAULTS TO ~/DOWNLOADS.
            </div>
          </div>
        </section>

        <div class="actions">
          <button type="submit" class="btn btn-primary" :disabled="saving">
            {{ saving ? 'SAVING...' : existed ? 'SAVE CHANGES' : 'CREATE PROFILE' }}
          </button>
        </div>
      </form>

      <section class="methods-section">
        <div class="methods-head">
          <div>
            <div class="mono-label text-disabled">PAYMENT</div>
            <h2 class="methods-title">Payment Methods</h2>
            <div class="methods-sub">
              Saved bank accounts and payment destinations. Attach one to a contract
              and it will be snapshotted onto every invoice generated from it.
            </div>
          </div>
          <button class="btn btn-primary" @click="openAddMethod">+ NEW METHOD</button>
        </div>

        <LoadingBar v-if="methodsLoading" />
        <EmptyState
          v-else-if="!methods.length"
          title="No payment methods"
          desc="Add at least one method and attach it to a contract to enable invoice payment blocks."
        />

        <table v-else class="table methods-table">
          <thead>
            <tr>
              <th>LABEL</th>
              <th>BANK</th>
              <th class="mono">ACCOUNT</th>
              <th>TERMS</th>
              <th class="center">DEFAULT</th>
              <th class="num">ACTIONS</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in methods" :key="m.id">
              <td>{{ m.label }}</td>
              <td class="text-secondary">{{ m.bank_name || '—' }}</td>
              <td class="mono text-disabled">
                {{ m.account_number ? '••' + m.account_number.slice(-4) : '—' }}
              </td>
              <td class="mono text-disabled">{{ m.payment_terms || '—' }}</td>
              <td class="center">
                <span v-if="m.is_default" class="mono-label text-success">[ DEFAULT ]</span>
                <span v-else class="text-disabled">—</span>
              </td>
              <td class="num">
                <button class="btn btn-ghost btn-sm" @click="openEditMethod(m)">EDIT</button>
                <button class="btn btn-ghost btn-sm danger" @click="removeMethod(m)">DELETE</button>
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <section class="methods-section">
        <div class="methods-head">
          <div>
            <div class="mono-label text-disabled">PORTABILITY</div>
            <h2 class="methods-title">Export &amp; Import</h2>
            <div class="methods-sub">
              Move your hours, clients, contracts, invoices, quotes and expenses
              between machines or apps. Export downloads a single JSON file. Import
              <strong>replaces every row</strong> in this database with the file's
              contents — back up first if you're not sure.
            </div>
          </div>
        </div>
        <div class="io-actions">
          <button class="btn btn-primary" :disabled="exporting" @click="exportData">
            {{ exporting ? 'EXPORTING...' : 'EXPORT ALL DATA' }}
          </button>
          <button class="btn btn-ghost" :disabled="importing" @click="pickImportFile">
            {{ importing ? 'IMPORTING...' : 'IMPORT FROM FILE' }}
          </button>
          <input
            ref="importInput"
            type="file"
            accept="application/json,.json"
            style="display: none"
            @change="onImportFile"
          />
        </div>
      </section>

      <section class="methods-section">
        <div class="methods-head">
          <div>
            <div class="mono-label text-disabled">ACCESS</div>
            <h2 class="methods-title">API Tokens</h2>
            <div class="methods-sub">
              Personal bearer tokens for programmatic access — Claude Desktop,
              scripts, or any HTTP client. Each token carries the scopes you
              pick at mint time and is bound to your user; revoke any token
              individually without affecting your session.
            </div>
          </div>
          <button class="btn btn-primary" @click="openCreateToken">+ NEW TOKEN</button>
        </div>

        <details class="mcp-help">
          <summary class="mcp-help-summary">
            <span class="mono-label">CLAUDE DESKTOP / MCP SETUP</span>
            <span class="mcp-help-chevron" aria-hidden="true">▾</span>
          </summary>
          <div class="mcp-help-body">
            <p class="mcp-help-step">
              <span class="mono-label text-disabled">STEP 1</span>
              Mint a token above with the scopes Claude should have. For a
              read-only assistant pick the <code>:read</code> scopes; add
              <code>:write</code> ones if Claude should log time, create
              invoices, etc.
            </p>
            <p class="mcp-help-step">
              <span class="mono-label text-disabled">STEP 2</span>
              Open
              <code>~/Library/Application Support/Claude/claude_desktop_config.json</code>
              (macOS) or
              <code>%APPDATA%\Claude\claude_desktop_config.json</code>
              (Windows) and add the <code>hours</code> entry below. Replace
              <code>ht_PASTE_TOKEN</code> with the token from Step 1 — it's
              shown only once, so save it before closing the dialog.
            </p>
            <pre class="mcp-snippet"><code>{{ mcpSnippet }}</code><button
              type="button"
              class="btn btn-ghost btn-sm mcp-copy"
              @click="copyMcpSnippet"
            >{{ mcpCopied ? 'COPIED' : 'COPY' }}</button></pre>
            <p class="mcp-help-step">
              <span class="mono-label text-disabled">STEP 3</span>
              Fully quit Claude Desktop (Cmd+Q on macOS — closing the window
              isn't enough) and reopen. The <code>hours</code> tools should
              appear in tool search. If they don't, check
              <code>~/Library/Logs/Claude/mcp*.log</code>.
            </p>
            <p class="mcp-help-note mono-label text-disabled">
              NOTES — backup/restore tools are stdio-only and intentionally not
              exposed over HTTP. <code>npx</code> must be on Claude Desktop's
              PATH; if launches fail, use the absolute path
              (<code>which npx</code>).
            </p>
          </div>
        </details>

        <LoadingBar v-if="tokensLoading" />
        <EmptyState
          v-else-if="!tokens.length"
          title="No API tokens"
          desc="Mint a token to call the Hours API from external tools without sharing your session cookie."
        />

        <table v-else class="table tokens-table">
          <thead>
            <tr>
              <th>NAME</th>
              <th class="mono">PREFIX</th>
              <th>SCOPES</th>
              <th class="mono">CREATED</th>
              <th class="mono">LAST USED</th>
              <th class="mono">EXPIRES</th>
              <th class="num">ACTIONS</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="t in tokens" :key="t.id">
              <tr :class="{ 'tokens-row-expanded': expandedTokenId === t.id }">
                <td>{{ t.name }}</td>
                <td class="mono text-disabled">{{ t.token_prefix }}…</td>
                <td>
                  <div class="scope-chips">
                    <span
                      v-for="s in t.scopes"
                      :key="s"
                      class="scope-chip"
                      :class="{ 'scope-chip-wild': s === '*' }"
                    >{{ s }}</span>
                  </div>
                </td>
                <td class="mono text-disabled">{{ fmtDate(t.created_at) }}</td>
                <td class="mono text-disabled">{{ fmtDateTime(t.last_used_at) }}</td>
                <td class="mono text-disabled">
                  <span v-if="t.expires_at">{{ fmtDate(t.expires_at) }}</span>
                  <span v-else>never</span>
                </td>
                <td class="num">
                  <button
                    class="btn btn-ghost btn-sm"
                    :aria-expanded="expandedTokenId === t.id"
                    @click="toggleTokenUsage(t.id)"
                  >{{ expandedTokenId === t.id ? 'HIDE' : 'USAGE' }}</button>
                  <button class="btn btn-ghost btn-sm danger" @click="revokeToken(t)">REVOKE</button>
                </td>
              </tr>
              <tr v-if="expandedTokenId === t.id" class="usage-row">
                <td colspan="7">
                  <div class="usage-panel">
                    <div class="usage-panel-head">
                      <span class="mono-label text-disabled">USAGE</span>
                      <button
                        class="btn btn-ghost btn-sm"
                        :disabled="tokenUsage[t.id]?.loading"
                        @click="loadTokenUsage(t.id, true)"
                      >REFRESH</button>
                    </div>

                    <div v-if="tokenUsage[t.id]?.loading && !tokenUsage[t.id]?.summary" class="usage-loading mono-label text-disabled">
                      LOADING…
                    </div>
                    <div v-else-if="tokenUsage[t.id]?.error" class="field-error">
                      [ ERROR ] {{ tokenUsage[t.id]?.error }}
                    </div>
                    <template v-else-if="tokenUsage[t.id]?.summary">
                      <div class="usage-metrics">
                        <div class="usage-metric">
                          <div class="mono-label text-disabled">TOTAL</div>
                          <div class="usage-metric-num">{{ tokenUsage[t.id]!.summary!.total_calls }}</div>
                        </div>
                        <div class="usage-metric">
                          <div class="mono-label text-disabled">LAST 24H</div>
                          <div class="usage-metric-num">{{ tokenUsage[t.id]!.summary!.calls_24h }}</div>
                        </div>
                        <div class="usage-metric">
                          <div class="mono-label text-disabled">LAST 7D</div>
                          <div class="usage-metric-num">{{ tokenUsage[t.id]!.summary!.calls_7d }}</div>
                        </div>
                        <div class="usage-metric">
                          <div class="mono-label text-disabled">LAST 30D</div>
                          <div class="usage-metric-num">{{ tokenUsage[t.id]!.summary!.calls_30d }}</div>
                        </div>
                        <div class="usage-metric">
                          <div class="mono-label text-disabled">ERRORS 24H</div>
                          <div
                            class="usage-metric-num"
                            :class="{ 'text-warning': tokenUsage[t.id]!.summary!.errors_24h > 0 }"
                          >{{ tokenUsage[t.id]!.summary!.errors_24h }}</div>
                        </div>
                      </div>

                      <div v-if="tokenUsage[t.id]!.summary!.last_call_at" class="usage-last">
                        <span class="mono-label text-disabled">LAST</span>
                        <span class="mono usage-last-method">{{ tokenUsage[t.id]!.summary!.last_method }}</span>
                        <span class="mono usage-last-path">{{ tokenUsage[t.id]!.summary!.last_path }}</span>
                        <span class="mono" :class="statusTone(tokenUsage[t.id]!.summary!.last_status)">
                          {{ fmtStatus(tokenUsage[t.id]!.summary!.last_status) }}
                        </span>
                        <span class="text-disabled">·</span>
                        <span class="text-secondary">{{ fmtRelTime(tokenUsage[t.id]!.summary!.last_call_at) }}</span>
                      </div>
                      <div v-else class="usage-last text-disabled mono-label">
                        NO CALLS YET
                      </div>

                      <div v-if="tokenUsage[t.id]!.summary!.by_path.length" class="usage-section">
                        <div class="mono-label text-disabled usage-section-label">TOP ENDPOINTS</div>
                        <table class="usage-bypath-table">
                          <thead>
                            <tr>
                              <th>PATH</th>
                              <th class="num">COUNT</th>
                              <th class="num">ERRORS</th>
                              <th class="mono">LAST</th>
                            </tr>
                          </thead>
                          <tbody>
                            <tr v-for="row in tokenUsage[t.id]!.summary!.by_path" :key="row.path">
                              <td class="mono">{{ row.path }}</td>
                              <td class="num">{{ row.count }}</td>
                              <td class="num" :class="{ 'text-warning': row.errors > 0 }">{{ row.errors }}</td>
                              <td class="mono text-disabled">{{ fmtRelTime(row.last_at) }}</td>
                            </tr>
                          </tbody>
                        </table>
                      </div>

                      <div v-if="tokenUsage[t.id]!.recent.length" class="usage-section">
                        <details class="usage-recent-details">
                          <summary class="usage-section-summary">
                            <span class="mono-label text-disabled">RECENT ACTIVITY</span>
                            <span class="mcp-help-chevron" aria-hidden="true">▾</span>
                          </summary>
                          <ul class="usage-recent-list">
                            <li
                              v-for="(ev, idx) in tokenUsage[t.id]!.recent.slice(0, 20)"
                              :key="idx"
                              class="usage-recent-row"
                            >
                              <span class="mono usage-recent-method">{{ ev.method }}</span>
                              <span class="mono usage-recent-path">{{ ev.path }}</span>
                              <span class="mono usage-recent-status" :class="statusTone(ev.status)">{{ ev.status }}</span>
                              <span class="mono text-disabled usage-recent-dur">{{ ev.duration_ms }}ms</span>
                              <span class="text-secondary usage-recent-when">{{ fmtRelTime(ev.created_at) }}</span>
                            </li>
                          </ul>
                        </details>
                      </div>
                    </template>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </section>
    </template>

    <Modal
      :open="methodModalOpen"
      :title="editingMethodId != null ? 'Edit Payment Method' : 'New Payment Method'"
      wide
      @close="methodModalOpen = false"
    >
      <form class="form-grid" @submit.prevent="saveMethod">
        <div class="field">
          <label>LABEL</label>
          <input v-model="methodForm.label" class="input" placeholder="e.g. Chase Business Checking" required />
          <div class="help-text mono-label text-disabled">
            A SHORT NAME YOU'LL PICK FROM DROPDOWNS WHEN WIRING UP CONTRACTS
          </div>
        </div>
        <div class="row">
          <div class="field grow">
            <label>BANK NAME</label>
            <input v-model="methodForm.bank_name" class="input" />
          </div>
          <div class="field grow">
            <label>ACCOUNT NUMBER</label>
            <input v-model="methodForm.account_number" class="input" />
          </div>
        </div>
        <div class="row">
          <div class="field grow">
            <label>ROUTING NUMBER</label>
            <input v-model="methodForm.routing_number" class="input" />
          </div>
          <div class="field grow">
            <label>SWIFT / BIC</label>
            <input v-model="methodForm.swift_code" class="input" />
          </div>
        </div>
        <div class="field">
          <label>PAYMENT TERMS</label>
          <input v-model="methodForm.payment_terms" class="input" placeholder="Net 30" />
        </div>
        <div class="field">
          <label>NOTES</label>
          <textarea v-model="methodForm.notes" class="textarea" rows="2" />
        </div>
        <div class="field field-checkbox">
          <label class="checkbox-label">
            <input v-model="methodForm.is_default" type="checkbox" />
            <span>SET AS DEFAULT METHOD</span>
          </label>
        </div>
        <div v-if="methodError" class="field-error">[ ERROR ] {{ methodError }}</div>
        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="methodModalOpen = false">CANCEL</button>
          <button type="submit" class="btn btn-primary" :disabled="methodSaving">
            {{ methodSaving ? 'SAVING...' : editingMethodId != null ? 'SAVE' : 'CREATE' }}
          </button>
        </div>
      </form>
    </Modal>

    <Modal
      :open="tokenModalOpen"
      title="New API Token"
      wide
      @close="tokenModalOpen = false"
    >
      <form class="form-grid" @submit.prevent="submitToken">
        <div class="field">
          <label>NAME</label>
          <input
            v-model="tokenForm.name"
            class="input"
            placeholder="e.g. Claude Desktop, GitHub Actions, laptop"
            maxlength="100"
            required
          />
          <div class="help-text mono-label text-disabled">
            JUST FOR YOUR REFERENCE — APPEARS IN THE TABLE ABOVE
          </div>
        </div>

        <div class="field">
          <label>SCOPES</label>
          <div class="scope-presets">
            <button type="button" class="btn btn-ghost btn-sm" @click="presetReadAll">READ ALL</button>
            <button type="button" class="btn btn-ghost btn-sm" @click="presetWriteAll">WRITE ALL</button>
            <button type="button" class="btn btn-ghost btn-sm" @click="presetAll">EVERYTHING</button>
            <button type="button" class="btn btn-ghost btn-sm" @click="presetClear">CLEAR</button>
          </div>

          <div class="scope-grid">
            <div v-for="g in SCOPE_GROUPS" :key="g.label" class="scope-row">
              <div class="scope-row-label">{{ g.label }}</div>
              <label
                v-for="s in g.scopes"
                :key="s"
                class="scope-check"
                :class="{ disabled: isScopeDisabled(s) }"
              >
                <input
                  type="checkbox"
                  :checked="tokenForm.scopes.has(s)"
                  :disabled="isScopeDisabled(s)"
                  @change="toggleScope(s)"
                />
                <span class="mono-label">{{ s.endsWith(':read') ? 'READ' : 'WRITE' }}</span>
              </label>
            </div>
          </div>

          <div class="scope-row-singles">
            <label
              v-for="s in SINGLE_SCOPES"
              :key="s.scope"
              class="scope-single"
              :class="{ disabled: isScopeDisabled(s.scope) }"
            >
              <input
                type="checkbox"
                :checked="tokenForm.scopes.has(s.scope)"
                :disabled="isScopeDisabled(s.scope)"
                @change="toggleScope(s.scope)"
              />
              <div>
                <div class="scope-single-label">{{ s.label }}</div>
                <div class="scope-single-desc">{{ s.description }}</div>
              </div>
            </label>
          </div>
        </div>

        <div class="field">
          <label>EXPIRES (OPTIONAL)</label>
          <input v-model="tokenForm.expires_at" class="input" type="date" style="width: 200px" />
          <div class="help-text mono-label text-disabled">
            LEAVE BLANK FOR NO EXPIRY. THE TOKEN STOPS WORKING AT END-OF-DAY UTC ON THE CHOSEN DATE.
          </div>
        </div>

        <div v-if="tokenError" class="field-error">[ ERROR ] {{ tokenError }}</div>
        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="tokenModalOpen = false">CANCEL</button>
          <button type="submit" class="btn btn-primary" :disabled="tokenSaving">
            {{ tokenSaving ? 'CREATING...' : 'CREATE TOKEN' }}
          </button>
        </div>
      </form>
    </Modal>

    <Modal
      :open="secretModalOpen"
      title="Token created — copy it now"
      wide
      @close="dismissSecret"
    >
      <div v-if="newSecret" class="secret-body">
        <div class="secret-warning">
          <span class="mono-label text-warning">[ ONE-TIME REVEAL ]</span>
          <div class="secret-warning-text">
            This is the only time the raw token will ever be shown. After you
            close this dialog it cannot be recovered — store it somewhere safe
            (a password manager) right now.
          </div>
        </div>

        <div class="secret-meta">
          <div><span class="mono-label text-disabled">NAME</span> {{ newSecret.name }}</div>
          <div>
            <span class="mono-label text-disabled">SCOPES</span>
            <span class="scope-chips inline">
              <span v-for="s in newSecret.scopes" :key="s" class="scope-chip">{{ s }}</span>
            </span>
          </div>
        </div>

        <div class="secret-token-row">
          <code class="secret-token">{{ newSecret.token }}</code>
          <button type="button" class="btn btn-primary" @click="copySecret">
            {{ secretCopied ? 'COPIED' : 'COPY' }}
          </button>
        </div>

        <div class="secret-howto">
          <div class="mono-label text-disabled">USAGE</div>
          <pre class="secret-code"><code>Authorization: Bearer {{ newSecret.token }}</code></pre>
        </div>

        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="dismissSecret">I&rsquo;VE SAVED IT</button>
        </div>
      </div>
    </Modal>
  </div>
</template>

<style scoped>
.settings-grid {
  display: flex;
  flex-direction: column;
  gap: var(--space-2xl);
  max-width: 860px;
}

.group {
  display: flex;
  flex-direction: column;
  gap: var(--space-md);
}

.group-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  padding-bottom: var(--space-sm);
  border-bottom: 1px solid var(--border);
}

.row {
  display: flex;
  gap: var(--space-md);
  align-items: flex-end;
}

.help-text {
  margin-top: 4px;
}

.actions {
  display: flex;
  justify-content: flex-end;
  padding-top: var(--space-lg);
  border-top: 1px solid var(--border);
}

.hint-banner {
  display: flex;
  gap: var(--space-md);
  align-items: center;
  padding: var(--space-md) var(--space-lg);
  margin-bottom: var(--space-xl);
  border: 1px solid var(--warning, var(--border));
  border-left-width: 3px;
}

.hint-text {
  color: var(--text-secondary);
  font-size: 13px;
}

.path-row {
  display: flex;
  gap: var(--space-sm);
  align-items: stretch;
}

.path-row .grow {
  flex: 1;
}

.methods-section {
  margin-top: var(--space-3xl, 64px);
  padding-top: var(--space-2xl);
  border-top: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: var(--space-lg);
}

.methods-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  gap: var(--space-lg);
}

.methods-title {
  margin: 4px 0 4px;
  font-size: 28px;
  font-weight: 300;
  letter-spacing: -0.01em;
}

.methods-sub {
  color: var(--text-secondary);
  font-size: 13px;
  max-width: 640px;
  line-height: 1.5;
}

.methods-table th.center,
.methods-table td.center {
  text-align: center;
}

.btn-sm {
  padding: 4px 10px;
  font-size: 11px;
  margin-left: var(--space-xs);
}

.btn-sm.danger {
  color: var(--accent, #d71921);
}

.field-checkbox {
  flex-direction: row;
  align-items: center;
}

.checkbox-label {
  display: inline-flex;
  align-items: center;
  gap: var(--space-sm);
  cursor: pointer;
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  letter-spacing: 0.08em;
  color: var(--text-secondary);
}

.checkbox-label input {
  margin: 0;
}

.io-actions {
  display: flex;
  gap: var(--space-md);
  flex-wrap: wrap;
}

/* ---------- API tokens ---------- */

.tokens-table th.center,
.tokens-table td.center {
  text-align: center;
}

.scope-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.scope-chips.inline {
  display: inline-flex;
  margin-left: var(--space-sm);
}

.scope-chip {
  display: inline-block;
  padding: 2px 8px;
  font-family: var(--font-mono, monospace);
  font-size: 10px;
  letter-spacing: 0.06em;
  border: 1px solid var(--border);
  color: var(--text-secondary);
  border-radius: 0;
  background: transparent;
}

.scope-chip-wild {
  color: var(--accent, #d71921);
  border-color: var(--accent, #d71921);
}

/* ─── MCP / Claude Desktop setup help block ──────────────────────── */

.mcp-help {
  border: 1px solid var(--border);
  margin: var(--space-sm) 0 var(--space-md);
  background: var(--surface-2, var(--surface));
}

.mcp-help-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-sm) var(--space-md);
  cursor: pointer;
  user-select: none;
  list-style: none;
}

.mcp-help-summary::-webkit-details-marker {
  display: none;
}

.mcp-help-chevron {
  display: inline-block;
  font-size: 11px;
  color: var(--text-secondary);
  transition: transform 0.15s ease-out;
}

.mcp-help[open] .mcp-help-chevron {
  transform: rotate(180deg);
}

.mcp-help-body {
  padding: var(--space-md);
  border-top: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: var(--space-sm);
}

.mcp-help-step {
  margin: 0;
  font-size: 13px;
  line-height: 1.5;
  color: var(--text-secondary);
}

.mcp-help-step .mono-label {
  display: inline-block;
  margin-right: var(--space-xs);
}

.mcp-help-step code {
  font-family: var(--font-mono, monospace);
  font-size: 12px;
  padding: 1px 5px;
  background: var(--surface);
  border: 1px solid var(--border);
  color: var(--text-primary);
}

.mcp-snippet {
  position: relative;
  margin: 0;
  padding: var(--space-md);
  padding-right: 80px;
  background: var(--surface);
  border: 1px solid var(--border);
  font-family: var(--font-mono, monospace);
  font-size: 12px;
  line-height: 1.55;
  color: var(--text-primary);
  white-space: pre;
  overflow-x: auto;
}

.mcp-copy {
  position: absolute;
  top: var(--space-xs);
  right: var(--space-xs);
}

.mcp-help-note {
  font-size: 11px;
  line-height: 1.5;
  margin: 0;
}

.mcp-help-note code {
  font-family: var(--font-mono, monospace);
  text-transform: none;
  letter-spacing: 0;
}

.scope-presets {
  display: flex;
  gap: var(--space-xs);
  flex-wrap: wrap;
  margin-bottom: var(--space-sm);
}

.scope-grid {
  display: flex;
  flex-direction: column;
  gap: 4px;
  border: 1px solid var(--border);
  padding: var(--space-md);
  background: var(--surface-1, transparent);
}

.scope-row {
  display: grid;
  grid-template-columns: 200px auto auto;
  align-items: center;
  gap: var(--space-md);
  padding: 4px 0;
}

.scope-row + .scope-row {
  border-top: 1px solid var(--border);
}

.scope-row-label {
  font-size: 13px;
  color: var(--text-primary);
}

.scope-check {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  user-select: none;
}

.scope-check.disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.scope-check input {
  margin: 0;
}

.scope-row-singles {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-sm);
  margin-top: var(--space-md);
}

.scope-single {
  display: flex;
  align-items: flex-start;
  gap: var(--space-sm);
  padding: var(--space-sm) var(--space-md);
  border: 1px solid var(--border);
  cursor: pointer;
  user-select: none;
}

.scope-single.disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.scope-single input {
  margin-top: 2px;
}

.scope-single-label {
  font-size: 13px;
  color: var(--text-primary);
}

.scope-single-desc {
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 2px;
}

/* ---------- Secret reveal modal ---------- */

.secret-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-lg);
}

.secret-warning {
  border: 1px solid var(--warning, var(--accent, var(--border)));
  border-left-width: 3px;
  padding: var(--space-md);
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.secret-warning-text {
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.5;
}

.secret-meta {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 13px;
}

.secret-token-row {
  display: flex;
  gap: var(--space-sm);
  align-items: stretch;
}

.secret-token {
  flex: 1;
  font-family: var(--font-mono, monospace);
  font-size: 13px;
  padding: var(--space-sm) var(--space-md);
  border: 1px solid var(--border);
  background: var(--surface-1, transparent);
  word-break: break-all;
  user-select: all;
}

.secret-howto {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.secret-code {
  margin: 0;
  padding: var(--space-sm) var(--space-md);
  font-family: var(--font-mono, monospace);
  font-size: 12px;
  border: 1px solid var(--border);
  overflow-x: auto;
  white-space: pre;
}

/* ---------- Per-token usage panel ---------- */

.tokens-row-expanded > td {
  border-bottom-color: transparent;
}

.usage-row > td {
  padding: 0;
  background: var(--surface-1, var(--surface));
}

.usage-panel {
  display: flex;
  flex-direction: column;
  gap: var(--space-md);
  padding: var(--space-md) var(--space-lg);
  margin: 0 var(--space-md) var(--space-md);
  border-left: 2px solid var(--border);
  background: var(--surface-2, transparent);
}

.usage-panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.usage-loading {
  padding: var(--space-md) 0;
}

.usage-metrics {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: var(--space-md);
}

.usage-metric {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: var(--space-sm) var(--space-md);
  border: 1px solid var(--border);
  background: var(--surface);
}

.usage-metric-num {
  font-family: var(--font-mono, monospace);
  font-size: 22px;
  font-weight: 300;
  color: var(--text-primary);
  letter-spacing: -0.01em;
}

.usage-last {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-sm);
  font-size: 13px;
  padding: var(--space-sm) 0;
}

.usage-last-method {
  font-size: 11px;
  letter-spacing: 0.05em;
  color: var(--text-secondary);
  border: 1px solid var(--border);
  padding: 1px 6px;
}

.usage-last-path {
  font-size: 12px;
  color: var(--text-primary);
}

.usage-section {
  display: flex;
  flex-direction: column;
  gap: var(--space-sm);
}

.usage-section-label {
  margin-top: var(--space-sm);
}

.usage-section-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  cursor: pointer;
  user-select: none;
  padding: var(--space-xs) 0;
  list-style: none;
}

.usage-section-summary::-webkit-details-marker {
  display: none;
}

.usage-recent-details[open] .mcp-help-chevron {
  transform: rotate(180deg);
}

.usage-bypath-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}

.usage-bypath-table th,
.usage-bypath-table td {
  text-align: left;
  padding: 6px 8px;
  border-bottom: 1px solid var(--border);
}

.usage-bypath-table th {
  font-family: var(--font-mono, monospace);
  font-size: 10px;
  letter-spacing: 0.06em;
  color: var(--text-secondary);
  font-weight: 400;
}

.usage-bypath-table th.num,
.usage-bypath-table td.num {
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.usage-recent-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  border-top: 1px solid var(--border);
}

.usage-recent-row {
  display: grid;
  grid-template-columns: 50px 1fr 60px 80px 100px;
  gap: var(--space-sm);
  align-items: center;
  padding: 4px 0;
  font-size: 12px;
  border-bottom: 1px solid var(--border);
}

.usage-recent-method {
  font-size: 10px;
  letter-spacing: 0.05em;
  color: var(--text-secondary);
}

.usage-recent-path {
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.usage-recent-status {
  font-variant-numeric: tabular-nums;
  text-align: right;
}

.usage-recent-dur {
  font-variant-numeric: tabular-nums;
  text-align: right;
}

.usage-recent-when {
  font-size: 11px;
  text-align: right;
}
</style>
