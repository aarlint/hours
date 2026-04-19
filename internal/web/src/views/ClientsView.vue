<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '../api'
import type { Client } from '../types'
import PageHeader from '../components/PageHeader.vue'
import Modal from '../components/Modal.vue'
import LoadingBar from '../components/LoadingBar.vue'
import EmptyState from '../components/EmptyState.vue'

const loading = ref(true)
const error = ref<string | null>(null)
const clients = ref<Client[]>([])
const search = ref('')

const modalOpen = ref(false)
const saving = ref(false)
const form = reactive({
  name: '',
  address: '',
  city: '',
  state: '',
  zip_code: '',
  country: '',
})

async function load() {
  loading.value = true
  try {
    clients.value = await api.listClients()
    error.value = null
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

function openCreate() {
  Object.assign(form, { name: '', address: '', city: '', state: '', zip_code: '', country: '' })
  error.value = null
  modalOpen.value = true
}

async function save() {
  if (!form.name.trim()) {
    error.value = 'Name required'
    return
  }
  saving.value = true
  try {
    await api.addClient({ ...form })
    modalOpen.value = false
    await load()
  } catch (e: any) {
    error.value = e.message
  } finally {
    saving.value = false
  }
}

onMounted(load)

function filtered() {
  if (!search.value) return clients.value
  const s = search.value.toLowerCase()
  return clients.value.filter(
    (c) =>
      c.name.toLowerCase().includes(s) ||
      c.city?.toLowerCase().includes(s) ||
      c.country?.toLowerCase().includes(s),
  )
}
</script>

<template>
  <div>
    <PageHeader
      category="REGISTRY"
      title="Clients"
      subtitle="Organizations you bill."
    >
      <template #actions>
        <button class="btn btn-primary" @click="openCreate">+ NEW CLIENT</button>
      </template>
    </PageHeader>

    <div class="toolbar">
      <input
        v-model="search"
        class="input input-underline"
        placeholder="SEARCH CLIENTS..."
      />
      <div class="mono-label text-disabled">
        {{ filtered().length }} / {{ clients.length }}
      </div>
    </div>

    <LoadingBar v-if="loading" />

    <EmptyState
      v-else-if="!clients.length"
      title="No clients yet"
      desc="Create your first client to start logging time."
    >
      <button class="btn btn-primary" @click="openCreate">+ NEW CLIENT</button>
    </EmptyState>

    <table v-else-if="filtered().length" class="table">
      <thead>
        <tr>
          <th>NAME</th>
          <th>LOCATION</th>
          <th class="num">CONTRACTS</th>
          <th class="num">CREATED</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="c in filtered()" :key="c.id">
          <td>
            <RouterLink :to="'/clients/' + c.id" class="client-link">
              {{ c.name }}
            </RouterLink>
          </td>
          <td class="text-secondary">
            {{ [c.city, c.state, c.country].filter(Boolean).join(', ') || '—' }}
          </td>
          <td class="num">
            <span :class="c.active_contracts > 0 ? '' : 'text-disabled'">
              {{ c.active_contracts }}
            </span>
          </td>
          <td class="num text-disabled">{{ c.created_at.slice(0, 10) }}</td>
          <td style="text-align: right">
            <RouterLink :to="'/clients/' + c.id" class="btn btn-ghost btn-sm">OPEN →</RouterLink>
          </td>
        </tr>
      </tbody>
    </table>

    <EmptyState
      v-else
      title="No matches"
      :desc="'Nothing matches \'' + search + '\''"
    />

    <Modal :open="modalOpen" title="New Client" @close="modalOpen = false">
      <form @submit.prevent="save" class="form-grid">
        <div class="field">
          <label>NAME</label>
          <input v-model="form.name" class="input" placeholder="ACME CORP" autofocus />
        </div>
        <div class="field">
          <label>ADDRESS</label>
          <input v-model="form.address" class="input" />
        </div>
        <div class="row">
          <div class="field grow">
            <label>CITY</label>
            <input v-model="form.city" class="input" />
          </div>
          <div class="field">
            <label>STATE</label>
            <input v-model="form.state" class="input" style="width: 100px" />
          </div>
          <div class="field">
            <label>ZIP</label>
            <input v-model="form.zip_code" class="input" style="width: 120px" />
          </div>
        </div>
        <div class="field">
          <label>COUNTRY</label>
          <input v-model="form.country" class="input" />
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
  align-items: center;
  justify-content: space-between;
  gap: var(--space-lg);
  margin-bottom: var(--space-lg);
}

.toolbar .input {
  max-width: 360px;
}

.client-link {
  font-weight: 500;
  color: var(--text-display);
  transition: color var(--duration-fast) var(--ease-out);
}

.client-link:hover {
  color: var(--accent);
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
</style>
