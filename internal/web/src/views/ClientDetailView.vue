<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { api, formatCurrency } from '../api'
import type { Client, Contract, PaymentDetails, Recipient } from '../types'
import PageHeader from '../components/PageHeader.vue'
import LoadingBar from '../components/LoadingBar.vue'
import Modal from '../components/Modal.vue'
import StatusChip from '../components/StatusChip.vue'
import EmptyState from '../components/EmptyState.vue'
import { useConfirm } from '../composables/useConfirm'
import { useToasts } from '../composables/useToasts'

const { confirm: confirmDialog, alert: alertDialog } = useConfirm()
const toasts = useToasts()

const route = useRoute()
const router = useRouter()
const id = computed(() => Number(route.params.id))

const loading = ref(true)
const error = ref<string | null>(null)
const client = ref<Client | null>(null)
const contracts = ref<Contract[]>([])
const recipients = ref<Recipient[]>([])
const payment = ref<PaymentDetails | null>(null)

const tab = ref<'contracts' | 'recipients' | 'payment'>('contracts')

const editOpen = ref(false)
const editForm = reactive<Partial<Client>>({})

const recipientModal = ref(false)
const recipientForm = reactive({ name: '', email: '', title: '', phone: '', is_primary: false })

const paymentForm = reactive<Partial<PaymentDetails>>({})
const paymentSaving = ref(false)
const paymentMsg = ref('')

async function load() {
  loading.value = true
  try {
    const [clients, cs, rs, p] = await Promise.all([
      api.listClients(),
      api.listContracts({ client_id: id.value }),
      api.listRecipients(id.value),
      api.getPaymentDetails(id.value),
    ])
    client.value = clients.find((c) => c.id === id.value) || null
    if (!client.value) {
      error.value = 'Client not found'
      return
    }
    contracts.value = cs
    recipients.value = rs
    payment.value = p
    Object.assign(paymentForm, p || {})
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

function openEdit() {
  if (!client.value) return
  Object.assign(editForm, client.value)
  editOpen.value = true
}

async function saveEdit() {
  try {
    await api.editClient(id.value, editForm)
    editOpen.value = false
    await load()
  } catch (e: any) {
    error.value = e.message
  }
}

async function addRecipient() {
  try {
    await api.addRecipient(id.value, { ...recipientForm })
    recipientModal.value = false
    Object.assign(recipientForm, { name: '', email: '', title: '', phone: '', is_primary: false })
    await load()
  } catch (e: any) {
    error.value = e.message
  }
}

async function removeRecipient(rid: number) {
  const ok = await confirmDialog({
    title: 'Remove recipient?',
    message: 'They will no longer receive invoices for this client.',
    confirmLabel: 'Remove',
    tone: 'danger',
  })
  if (!ok) return
  try {
    await api.removeRecipient(rid)
    await load()
  } catch (e: any) {
    await alertDialog({
      title: 'Could not remove recipient',
      message: e.message,
      tone: 'warning',
    })
  }
}

const deleting = ref(false)

async function deleteClient() {
  if (!client.value) return
  const lines: string[] = []
  if (contracts.value.length) lines.push(`${contracts.value.length} contract${contracts.value.length === 1 ? '' : 's'}`)
  if (recipients.value.length) lines.push(`${recipients.value.length} recipient${recipients.value.length === 1 ? '' : 's'}`)
  if (payment.value) lines.push('payment details')
  const detail = lines.length
    ? `This also removes: ${lines.join(', ')}, plus all time entries, invoices, and quotes for this client.`
    : 'This also removes all time entries, invoices, and quotes for this client.'

  const ok = await confirmDialog({
    title: `Delete ${client.value.name}?`,
    message: 'This cannot be undone.',
    detail,
    confirmLabel: 'Delete client',
    cancelLabel: 'Cancel',
    tone: 'danger',
  })
  if (!ok) return

  deleting.value = true
  try {
    const res = await api.deleteClient(id.value)
    toasts.push({
      tone: 'success',
      title: `Deleted ${res.name}`,
      detail: summarizeCounts(res),
    })
    router.push('/clients')
  } catch (e: any) {
    await alertDialog({
      title: 'Could not delete client',
      message: e.message,
      tone: 'warning',
    })
  } finally {
    deleting.value = false
  }
}

function summarizeCounts(r: {
  contracts: number
  time_entries: number
  invoices: number
  quotes: number
  recipients: number
}): string | undefined {
  const bits: string[] = []
  if (r.contracts) bits.push(`${r.contracts} contract${r.contracts === 1 ? '' : 's'}`)
  if (r.time_entries) bits.push(`${r.time_entries} time ${r.time_entries === 1 ? 'entry' : 'entries'}`)
  if (r.invoices) bits.push(`${r.invoices} invoice${r.invoices === 1 ? '' : 's'}`)
  if (r.quotes) bits.push(`${r.quotes} quote${r.quotes === 1 ? '' : 's'}`)
  if (r.recipients) bits.push(`${r.recipients} recipient${r.recipients === 1 ? '' : 's'}`)
  return bits.length ? `Also removed: ${bits.join(', ')}` : undefined
}

async function savePayment() {
  paymentSaving.value = true
  paymentMsg.value = ''
  try {
    await api.setPaymentDetails(id.value, paymentForm)
    paymentMsg.value = 'SAVED'
    await load()
  } catch (e: any) {
    paymentMsg.value = 'ERROR: ' + e.message
  } finally {
    paymentSaving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-if="loading">
    <LoadingBar />
  </div>

  <div v-else-if="error">
    <div class="card" style="border-color: var(--accent)">
      <div class="mono-label text-accent">[ ERROR ]</div>
      <p style="margin-top: 8px">{{ error }}</p>
      <button class="btn btn-secondary btn-sm" style="margin-top: 16px" @click="router.push('/clients')">
        ← BACK TO CLIENTS
      </button>
    </div>
  </div>

  <div v-else-if="client">
    <PageHeader
      :category="'CLIENT #' + client.id"
      :title="client.name"
      :subtitle="[client.city, client.state, client.country].filter(Boolean).join(', ') || 'No location set'"
    >
      <template #actions>
        <RouterLink to="/clients" class="btn btn-ghost">← BACK</RouterLink>
        <button class="btn btn-secondary" @click="openEdit">EDIT</button>
        <button class="btn btn-ghost btn-danger" :disabled="deleting" @click="deleteClient">
          {{ deleting ? 'DELETING...' : 'DELETE' }}
        </button>
      </template>
    </PageHeader>

    <div class="segmented" style="margin-bottom: 32px">
      <button :class="{ active: tab === 'contracts' }" @click="tab = 'contracts'">
        CONTRACTS ({{ contracts.length }})
      </button>
      <button :class="{ active: tab === 'recipients' }" @click="tab = 'recipients'">
        RECIPIENTS ({{ recipients.length }})
      </button>
      <button :class="{ active: tab === 'payment' }" @click="tab = 'payment'">PAYMENT</button>
    </div>

    <!-- Contracts tab -->
    <section v-if="tab === 'contracts'">
      <div class="section-head">
        <span class="mono-label">ACTIVE CONTRACTS</span>
        <RouterLink to="/contracts" class="btn btn-secondary btn-sm">+ NEW CONTRACT</RouterLink>
      </div>
      <EmptyState v-if="!contracts.length" title="No contracts" desc="Add one from the CONTRACTS page." />
      <table v-else class="table">
        <thead>
          <tr>
            <th>#</th>
            <th>NAME</th>
            <th class="num">RATE</th>
            <th>STATUS</th>
            <th class="num">START</th>
            <th class="num">END</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="c in contracts" :key="c.id">
            <td class="mono">{{ c.contract_number }}</td>
            <td>{{ c.name }}</td>
            <td class="num">{{ formatCurrency(c.hourly_rate, c.currency) }} / HR</td>
            <td><StatusChip :status="c.status" /></td>
            <td class="num text-disabled">{{ c.start_date.slice(0, 10) }}</td>
            <td class="num text-disabled">{{ c.end_date ? c.end_date.slice(0, 10) : '—' }}</td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- Recipients tab -->
    <section v-if="tab === 'recipients'">
      <div class="section-head">
        <span class="mono-label">INVOICE RECIPIENTS</span>
        <button class="btn btn-secondary btn-sm" @click="recipientModal = true">+ ADD RECIPIENT</button>
      </div>
      <EmptyState v-if="!recipients.length" title="No recipients" desc="Add email contacts for invoicing." />
      <table v-else class="table">
        <thead>
          <tr>
            <th>NAME</th>
            <th>EMAIL</th>
            <th>TITLE</th>
            <th>PHONE</th>
            <th>PRIMARY</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in recipients" :key="r.id">
            <td>{{ r.name }}</td>
            <td class="mono text-secondary">{{ r.email }}</td>
            <td class="text-secondary">{{ r.title || '—' }}</td>
            <td class="text-secondary mono">{{ r.phone || '—' }}</td>
            <td>
              <span v-if="r.is_primary" class="chip chip-active">PRIMARY</span>
              <span v-else class="text-disabled">—</span>
            </td>
            <td style="text-align: right">
              <button class="btn btn-ghost btn-sm" @click="removeRecipient(r.id)">REMOVE</button>
            </td>
          </tr>
        </tbody>
      </table>
    </section>

    <!-- Payment tab -->
    <section v-if="tab === 'payment'">
      <div class="section-head">
        <span class="mono-label">PAYMENT DETAILS</span>
        <span v-if="paymentMsg" class="mono-label" :class="paymentMsg === 'SAVED' ? 'text-success' : 'text-accent'">
          [ {{ paymentMsg }} ]
        </span>
      </div>
      <form class="payment-form card-outlined" @submit.prevent="savePayment">
        <div class="field">
          <label>BANK NAME</label>
          <input v-model="paymentForm.bank_name" class="input" />
        </div>
        <div class="field">
          <label>ACCOUNT NUMBER</label>
          <input v-model="paymentForm.account_number" class="input" />
        </div>
        <div class="field">
          <label>ROUTING NUMBER</label>
          <input v-model="paymentForm.routing_number" class="input" />
        </div>
        <div class="field">
          <label>SWIFT CODE</label>
          <input v-model="paymentForm.swift_code" class="input" />
        </div>
        <div class="field">
          <label>PAYMENT TERMS</label>
          <input v-model="paymentForm.payment_terms" class="input" placeholder="e.g. NET 30" />
        </div>
        <div class="field full">
          <label>NOTES</label>
          <textarea v-model="paymentForm.notes" class="textarea" rows="3" />
        </div>
        <div class="full" style="display: flex; justify-content: flex-end">
          <button class="btn btn-primary" type="submit" :disabled="paymentSaving">
            {{ paymentSaving ? 'SAVING...' : 'SAVE' }}
          </button>
        </div>
      </form>
    </section>

    <!-- Edit client modal -->
    <Modal :open="editOpen" title="Edit Client" @close="editOpen = false">
      <form class="form-grid" @submit.prevent="saveEdit">
        <div class="field">
          <label>NAME</label>
          <input v-model="editForm.name" class="input" />
        </div>
        <div class="field">
          <label>ADDRESS</label>
          <input v-model="editForm.address" class="input" />
        </div>
        <div class="row">
          <div class="field grow">
            <label>CITY</label>
            <input v-model="editForm.city" class="input" />
          </div>
          <div class="field">
            <label>STATE</label>
            <input v-model="editForm.state" class="input" style="width: 100px" />
          </div>
          <div class="field">
            <label>ZIP</label>
            <input v-model="editForm.zip_code" class="input" style="width: 120px" />
          </div>
        </div>
        <div class="field">
          <label>COUNTRY</label>
          <input v-model="editForm.country" class="input" />
        </div>
        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="editOpen = false">CANCEL</button>
          <button type="submit" class="btn btn-primary">SAVE</button>
        </div>
      </form>
    </Modal>

    <!-- Add recipient modal -->
    <Modal :open="recipientModal" title="Add Recipient" @close="recipientModal = false">
      <form class="form-grid" @submit.prevent="addRecipient">
        <div class="field">
          <label>NAME</label>
          <input v-model="recipientForm.name" class="input" required />
        </div>
        <div class="field">
          <label>EMAIL</label>
          <input v-model="recipientForm.email" class="input" type="email" required />
        </div>
        <div class="field">
          <label>TITLE</label>
          <input v-model="recipientForm.title" class="input" />
        </div>
        <div class="field">
          <label>PHONE</label>
          <input v-model="recipientForm.phone" class="input" />
        </div>
        <label class="mono-label checkbox-row">
          <input type="checkbox" v-model="recipientForm.is_primary" />
          SET AS PRIMARY
        </label>
        <div class="modal-actions">
          <button type="button" class="btn btn-ghost" @click="recipientModal = false">CANCEL</button>
          <button type="submit" class="btn btn-primary">ADD</button>
        </div>
      </form>
    </Modal>
  </div>
</template>

<style scoped>
.section-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-bottom: var(--space-sm);
  margin-bottom: var(--space-md);
  border-bottom: 1px solid var(--border);
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

.payment-form {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-md);
}

.payment-form .full {
  grid-column: 1 / -1;
}

.checkbox-row {
  display: flex;
  align-items: center;
  gap: var(--space-sm);
  cursor: pointer;
}

@media (max-width: 700px) {
  .payment-form {
    grid-template-columns: 1fr;
  }
}
</style>
