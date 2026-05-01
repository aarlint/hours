<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import type { BusinessInfo, PaymentMethod } from '../types'
import PageHeader from '../components/PageHeader.vue'
import LoadingBar from '../components/LoadingBar.vue'
import Modal from '../components/Modal.vue'
import EmptyState from '../components/EmptyState.vue'
import { useConfirm } from '../composables/useConfirm'
import { useToasts } from '../composables/useToasts'
import { isWails, pickDirectory, revealInFinder } from '../wailsShim'

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
    const blob = new Blob([JSON.stringify(data, null, 2)], {
      type: 'application/json',
    })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `hours-export-${new Date().toISOString().slice(0, 10)}.json`
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

onMounted(async () => {
  await Promise.all([load(), loadMethods()])
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
</style>
