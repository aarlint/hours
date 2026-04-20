import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/dashboard',
  },
  {
    path: '/dashboard',
    name: 'dashboard',
    component: () => import('./views/DashboardView.vue'),
    meta: { label: 'DASHBOARD' },
  },
  {
    path: '/time',
    name: 'time',
    component: () => import('./views/TimeView.vue'),
    meta: { label: 'TIME' },
  },
  {
    path: '/clients',
    name: 'clients',
    component: () => import('./views/ClientsView.vue'),
    meta: { label: 'CLIENTS' },
  },
  {
    path: '/clients/:id',
    name: 'client-detail',
    component: () => import('./views/ClientDetailView.vue'),
    meta: { label: 'CLIENT' },
  },
  {
    path: '/contracts',
    name: 'contracts',
    component: () => import('./views/ContractsView.vue'),
    meta: { label: 'CONTRACTS' },
  },
  {
    path: '/invoices',
    name: 'invoices',
    component: () => import('./views/InvoicesView.vue'),
    meta: { label: 'INVOICES' },
  },
  {
    path: '/invoices/:number',
    name: 'invoice-detail',
    component: () => import('./views/InvoiceDetailView.vue'),
    meta: { label: 'INVOICE' },
  },
  {
    path: '/quotes',
    name: 'quotes',
    component: () => import('./views/QuotesView.vue'),
    meta: { label: 'QUOTES' },
  },
  {
    path: '/quotes/:number',
    name: 'quote-detail',
    component: () => import('./views/QuoteDetailView.vue'),
    meta: { label: 'QUOTE' },
  },
  {
    path: '/settings',
    name: 'settings',
    component: () => import('./views/SettingsView.vue'),
    meta: { label: 'SETTINGS' },
  },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})
