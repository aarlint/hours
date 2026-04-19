<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api, formatCurrency } from '../api'
import type { Client, Contract } from '../types'
import PageHeader from '../components/PageHeader.vue'
import Modal from '../components/Modal.vue'
import LoadingBar from '../components/LoadingBar.vue'
import EmptyState from '../components/EmptyState.vue'
import StatusChip from '../components/StatusChip.vue'

const loading = ref(true)
const contracts = ref<Contract[]>([])
const clients = ref<Client[]>([])
const filter = ref<'all' | 'active' | 'completed' | 'on_hold' | 'cancelled'>('active')

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
  notes: '',
})

async function load() {
  loading.value = true
  try {
    const params = filter.value === 'all' ? {} : { status: filter.value }
    const [cs, cls] = await Promise.all([api.listContracts(params), api.listClients()])
    contracts.value = cs
    clients.value = cls
  } finally {
    loading.value = false
  }
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
    await load()
  } catch (e: any) {
    error.value = e.message
  } finally {
    saving.value = false
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
          <th>STATUS</th>
          <th class="num">START</th>
          <th class="num">END</th>
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
          <td><StatusChip :status="c.status" /></td>
          <td class="num text-disabled">{{ c.start_date.slice(0, 10) }}</td>
          <td class="num text-disabled">{{ c.end_date ? c.end_date.slice(0, 10) : '—' }}</td>
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
</style>
