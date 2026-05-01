<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api, formatCurrency } from '../api'
import type { Client, Contract, PaymentMethod } from '../types'
import PageHeader from '../components/PageHeader.vue'
import Modal from '../components/Modal.vue'
import LoadingBar from '../components/LoadingBar.vue'
import EmptyState from '../components/EmptyState.vue'
import StatusChip from '../components/StatusChip.vue'
import { useToasts } from '../composables/useToasts'

const loading = ref(true)
const contracts = ref<Contract[]>([])
const clients = ref<Client[]>([])
const paymentMethods = ref<PaymentMethod[]>([])
const filter = ref<'all' | 'active' | 'completed' | 'on_hold' | 'cancelled'>('active')

const { push: toast } = useToasts()

// Create modal ------------------------------------------------------------
const modalOpen = ref(false)
const saving = ref(false)
const error = ref<string | null>(null)
const form = reactive({
  client_id: 0,
  contract_number: '',
  name: '',
  hourly_rate: 100,
  currency: 'USD',
  contract_type: 'hourly',
  start_date: new Date().toISOString().slice(0, 10),
  end_date: '',
  payment_terms: 'Net 30',
  payment_method_id: null as number | null,
  notes: '',
})

// Edit modal --------------------------------------------------------------
const editModalOpen = ref(false)
const editSaving = ref(false)
const editError = ref<string | null>(null)
const editingId = ref<number | null>(null)
const editForm = reactive({
  name: '',
  hourly_rate: 0,
  currency: 'USD',
  contract_type: 'hourly',
  end_date: '',
  status: 'active',
  payment_terms: '',
  payment_method_id: null as number | null,
  notes: '',
})

async function load() {
  loading.value = true
  try {
    const params = filter.value === 'all' ? {} : { status: filter.value }
    const [cs, cls, pms] = await Promise.all([
      api.listContracts(params),
      api.listClients(),
      api.listPaymentMethods(),
    ])
    contracts.value = cs
    clients.value = cls
    paymentMethods.value = pms
  } finally {
    loading.value = false
  }
}

function defaultPaymentMethodId(): number | null {
  const def = paymentMethods.value.find((m) => m.is_default)
  return def?.id ?? null
}

function paymentMethodLabel(id?: number | null): string {
  if (id == null) return '—'
  return paymentMethods.value.find((m) => m.id === id)?.label ?? '—'
}

function openCreate() {
  Object.assign(form, {
    client_id: clients.value[0]?.id || 0,
    contract_number: '',
    name: '',
    hourly_rate: 100,
    currency: 'USD',
    contract_type: 'hourly',
    start_date: new Date().toISOString().slice(0, 10),
    end_date: '',
    payment_terms: 'Net 30',
    payment_method_id: defaultPaymentMethodId(),
    notes: '',
  })
  error.value = null
  modalOpen.value = true
}

async function save() {
  saving.value = true
  error.value = null
  try {
    await api.addContract({ ...form } as any)
    modalOpen.value = false
    toast({ tone: 'success', title: 'Contract created', detail: form.name })
    await load()
  } catch (e: any) {
    error.value = e.message
  } finally {
    saving.value = false
  }
}

function openEdit(c: Contract) {
  editingId.value = c.id
  Object.assign(editForm, {
    name: c.name,
    hourly_rate: c.hourly_rate,
    currency: c.currency,
    contract_type: c.contract_type,
    end_date: c.end_date ? c.end_date.slice(0, 10) : '',
    status: c.status,
    payment_terms: c.payment_terms ?? '',
    payment_method_id: c.payment_method_id ?? null,
    notes: c.notes ?? '',
  })
  editError.value = null
  editModalOpen.value = true
}

async function saveEdit() {
  if (editingId.value == null) return
  editSaving.value = true
  editError.value = null
  try {
    const payload: any = {
      name: editForm.name,
      hourly_rate: editForm.hourly_rate,
      currency: editForm.currency,
      contract_type: editForm.contract_type,
      status: editForm.status,
      payment_terms: editForm.payment_terms,
      notes: editForm.notes,
    }
    if (editForm.end_date) payload.end_date = editForm.end_date
    if (editForm.payment_method_id == null) {
      payload.clear_payment_method = true
    } else {
      payload.payment_method_id = editForm.payment_method_id
    }
    await api.editContract(editingId.value, payload)
    editModalOpen.value = false
    toast({ tone: 'success', title: 'Contract updated', detail: editForm.name })
    await load()
  } catch (e: any) {
    editError.value = e.message
  } finally {
    editSaving.value = false
  }
}

onMounted(load)

const totalRate = computed(() =>
  contracts.value.reduce((sum, c) => sum + (c.status === 'active' ? c.hourly_rate : 0), 0),
)
</script>

<template>
  <div>
    <PageHeader
      category="AGREEMENTS"
      title="Contracts"
      subtitle="Rate-bearing billing agreements."
    >
      <template #actions>
        <button class="btn btn-primary" @click="openCreate" :disabled="!clients.length">
          + NEW CONTRACT
        </button>
      </template>
    </PageHeader>

    <div class="toolbar">
      <div class="segmented">
        <button :class="{ active: filter === 'all' }" @click="filter = 'all'; load()">ALL</button>
        <button :class="{ active: filter === 'active' }" @click="filter = 'active'; load()">ACTIVE</button>
        <button :class="{ active: filter === 'completed' }" @click="filter = 'completed'; load()">DONE</button>
        <button :class="{ active: filter === 'on_hold' }" @click="filter = 'on_hold'; load()">HOLD</button>
        <button :class="{ active: filter === 'cancelled' }" @click="filter = 'cancelled'; load()">CANCELLED</button>
      </div>
      <div class="mono-label text-disabled">
        {{ contracts.length }} {{ contracts.length === 1 ? 'CONTRACT' : 'CONTRACTS' }}
      </div>
    </div>

    <LoadingBar v-if="loading" />

    <EmptyState
      v-else-if="!clients.length"
      title="No clients yet"
      desc="Create a client first before adding contracts."
    >
      <RouterLink to="/clients" class="btn btn-primary">→ CLIENTS</RouterLink>
    </EmptyState>

    <EmptyState v-else-if="!contracts.length" title="No contracts" desc="Add a contract to start billing." />

    <table v-else class="table">
      <thead>
        <tr>
          <th>#</th>
          <th>NAME</th>
          <th>CLIENT</th>
          <th class="num">RATE</th>
          <th>TYPE</th>
          <th>TERMS</th>
          <th>PAYMENT</th>
          <th>STATUS</th>
          <th class="num">START</th>
          <th class="num">END</th>
          <th class="num">ACTIONS</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="c in contracts" :key="c.id">
          <td class="mono">{{ c.contract_number }}</td>
          <td>{{ c.name }}</td>
          <td class="text-secondary">
            <RouterLink :to="'/clients/' + c.client_id" class="mini-link">{{ c.client_name }}</RouterLink>
          </td>
          <td class="num">{{ formatCurrency(c.hourly_rate, c.currency) }}</td>
          <td class="mono text-disabled">{{ c.contract_type }}</td>
          <td class="mono text-disabled">{{ c.payment_terms || '—' }}</td>
          <td
            class="text-secondary"
            :class="{ 'text-disabled': c.payment_method_id == null }"
          >
            {{ paymentMethodLabel(c.payment_method_id) }}
          </td>
          <td><StatusChip :status="c.status" /></td>
          <td class="num text-disabled">{{ c.start_date.slice(0, 10) }}</td>
          <td class="num text-disabled">{{ c.end_date ? c.end_date.slice(0, 10) : '—' }}</td>
          <td class="num">
            <button class="btn btn-ghost btn-sm" @click="openEdit(c)">EDIT</button>
          </td>
        </tr>
      </tbody>
    </table>

    <Modal :open="modalOpen" title="New Contract" wide @close="modalOpen = false">
      <form class="form-grid" @submit.prevent="save">
        <div class="row">
          <div class="field grow">
            <label>CLIENT</label>
            <select v-model="form.client_id" class="select">
              <option v-for="c in clients" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
          </div>
          <div class="field">
            <label>CONTRACT #</label>
            <input v-model="form.contract_number" class="input" placeholder="C-2026-001" required />
          </div>
        </div>
        <div class="field">
          <label>NAME / DESCRIPTION</label>
          <input v-model="form.name" class="input" required />
        </div>
        <div class="row">
          <div class="field">
            <label>RATE</label>
            <input v-model.number="form.hourly_rate" class="input" type="number" step="0.01" min="0" required />
          </div>
          <div class="field">
            <label>CURRENCY</label>
            <select v-model="form.currency" class="select">
              <option>USD</option>
              <option>EUR</option>
              <option>GBP</option>
              <option>CAD</option>
              <option>AUD</option>
            </select>
          </div>
          <div class="field grow">
            <label>TYPE</label>
            <select v-model="form.contract_type" class="select">
              <option>hourly</option>
              <option>fixed</option>
              <option>retainer</option>
            </select>
          </div>
        </div>
        <div class="row">
          <div class="field grow">
            <label>START DATE</label>
            <input v-model="form.start_date" class="input" type="date" required />
          </div>
          <div class="field grow">
            <label>END DATE (OPTIONAL)</label>
            <input v-model="form.end_date" class="input" type="date" />
          </div>
        </div>
        <div class="field">
          <label>PAYMENT TERMS</label>
          <input v-model="form.payment_terms" class="input" />
        </div>
        <div class="field">
          <label>PAYMENT METHOD</label>
          <select v-model="form.payment_method_id" class="select">
            <option :value="null">— NONE —</option>
            <option v-for="m in paymentMethods" :key="m.id" :value="m.id">
              {{ m.label }}{{ m.is_default ? ' (default)' : '' }}
            </option>
          </select>
          <div v-if="!paymentMethods.length" class="help-text mono-label text-disabled">
            NO METHODS YET — ADD ONE IN SETTINGS → PAYMENT METHODS
          </div>
        </div>
        <div class="field">
          <label>NOTES</label>
          <textarea v-model="form.notes" class="textarea" rows="2" />
        </div>
        <div v-if="error" class="field-error">[ ERROR ] {{ error }}</div>
        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="modalOpen = false">CANCEL</button>
          <button type="submit" class="btn btn-primary" :disabled="saving">
            {{ saving ? 'SAVING...' : 'CREATE' }}
          </button>
        </div>
      </form>
    </Modal>

    <Modal :open="editModalOpen" title="Edit Contract" wide @close="editModalOpen = false">
      <form class="form-grid" @submit.prevent="saveEdit">
        <div class="field">
          <label>NAME / DESCRIPTION</label>
          <input v-model="editForm.name" class="input" required />
        </div>
        <div class="row">
          <div class="field">
            <label>RATE</label>
            <input
              v-model.number="editForm.hourly_rate"
              class="input"
              type="number"
              step="0.01"
              min="0"
              required
            />
          </div>
          <div class="field">
            <label>CURRENCY</label>
            <select v-model="editForm.currency" class="select">
              <option>USD</option>
              <option>EUR</option>
              <option>GBP</option>
              <option>CAD</option>
              <option>AUD</option>
            </select>
          </div>
          <div class="field grow">
            <label>TYPE</label>
            <select v-model="editForm.contract_type" class="select">
              <option>hourly</option>
              <option>fixed</option>
              <option>retainer</option>
            </select>
          </div>
        </div>
        <div class="row">
          <div class="field grow">
            <label>STATUS</label>
            <select v-model="editForm.status" class="select">
              <option>active</option>
              <option>completed</option>
              <option>on_hold</option>
              <option>cancelled</option>
            </select>
          </div>
          <div class="field grow">
            <label>END DATE (OPTIONAL)</label>
            <input v-model="editForm.end_date" class="input" type="date" />
          </div>
        </div>
        <div class="field">
          <label>PAYMENT TERMS</label>
          <input v-model="editForm.payment_terms" class="input" />
        </div>
        <div class="field">
          <label>PAYMENT METHOD</label>
          <select v-model="editForm.payment_method_id" class="select">
            <option :value="null">— NONE —</option>
            <option v-for="m in paymentMethods" :key="m.id" :value="m.id">
              {{ m.label }}{{ m.is_default ? ' (default)' : '' }}
            </option>
          </select>
          <div class="help-text mono-label text-disabled">
            SNAPSHOTTED ONTO EACH NEW INVOICE GENERATED FROM THIS CONTRACT
          </div>
        </div>
        <div class="field">
          <label>NOTES</label>
          <textarea v-model="editForm.notes" class="textarea" rows="2" />
        </div>
        <div v-if="editError" class="field-error">[ ERROR ] {{ editError }}</div>
        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="editModalOpen = false">CANCEL</button>
          <button type="submit" class="btn btn-primary" :disabled="editSaving">
            {{ editSaving ? 'SAVING...' : 'SAVE' }}
          </button>
        </div>
      </form>
    </Modal>
  </div>
</template>

<style scoped>
.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: var(--space-lg);
  gap: var(--space-md);
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

.mini-link {
  color: var(--text-secondary);
  transition: color var(--duration-fast) var(--ease-out);
}

.mini-link:hover {
  color: var(--text-primary);
}

.btn-sm {
  padding: 4px 10px;
  font-size: 11px;
}

.help-text {
  margin-top: 4px;
}
</style>
