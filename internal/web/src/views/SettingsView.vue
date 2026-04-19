<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import type { BusinessInfo } from '../types'
import PageHeader from '../components/PageHeader.vue'
import LoadingBar from '../components/LoadingBar.vue'
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

onMounted(load)
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
    </template>
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
</style>
