import type {
  BusinessInfo,
  Client,
  Contract,
  Invoice,
  InvoiceDetails,
  PaymentDetails,
  Quote,
  QuoteDetails,
  QuoteLineItem,
  Recipient,
  Stats,
  TimeEntry,
} from './types'
import { goApp, isWails } from './wailsShim'

const BASE = ''

// In the native Wails app we dispatch through a single Go binding
// (App.Request) that routes to the in-process HTTP mux. In a plain browser
// (legacy --serve mode, or vite dev) we fall back to fetch against the same
// routes. Either way, callers above see identical shapes.
async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const bodyStr = body ? JSON.stringify(body) : ''
  if (isWails()) {
    const res = await goApp().Request(method, path, bodyStr)
    if (res.status < 200 || res.status >= 300) {
      let msg = `${res.status}`
      try {
        const data = res.body ? JSON.parse(res.body) : null
        if (data?.error) msg = data.error
      } catch {}
      throw new Error(msg)
    }
    if (res.status === 204 || !res.body) return undefined as T
    return JSON.parse(res.body) as T
  }

  const res = await fetch(BASE + path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? bodyStr : undefined,
  })
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`
    try {
      const data = await res.json()
      if (data?.error) msg = data.error
    } catch {}
    throw new Error(msg)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

export const api = {
  // Stats
  getStats: () => request<Stats>('GET', '/api/stats'),

  // Business info
  getBusinessInfo: () => request<BusinessInfo | null>('GET', '/api/business-info'),
  setBusinessInfo: (data: Partial<BusinessInfo>) =>
    request<{ ok: true }>('PUT', '/api/business-info', data),

  // Clients
  listClients: () => request<Client[]>('GET', '/api/clients'),
  addClient: (data: Partial<Client>) =>
    request<{ id: number; name: string }>('POST', '/api/clients', data),
  editClient: (id: number, data: Partial<Client>) =>
    request<{ id: number }>('PUT', `/api/clients/${id}`, data),

  // Recipients
  listRecipients: (clientId: number) =>
    request<Recipient[]>('GET', `/api/clients/${clientId}/recipients`),
  addRecipient: (clientId: number, data: Partial<Recipient>) =>
    request<{ id: number }>('POST', `/api/clients/${clientId}/recipients`, data),
  removeRecipient: (id: number) =>
    request<{ deleted: number }>('DELETE', `/api/recipients/${id}`),

  // Payment
  getPaymentDetails: (clientId: number) =>
    request<PaymentDetails | null>('GET', `/api/clients/${clientId}/payment-details`),
  setPaymentDetails: (clientId: number, data: Partial<PaymentDetails>) =>
    request<{ client_id: number }>('PUT', `/api/clients/${clientId}/payment-details`, data),

  // Contracts
  listContracts: (params?: { client_id?: number; status?: string }) => {
    const q = new URLSearchParams()
    if (params?.client_id) q.set('client_id', String(params.client_id))
    if (params?.status) q.set('status', params.status)
    const qs = q.toString()
    return request<Contract[]>('GET', '/api/contracts' + (qs ? '?' + qs : ''))
  },
  addContract: (data: Partial<Contract>) =>
    request<{ id: number }>('POST', '/api/contracts', data),

  // Time entries
  searchTimeEntries: (params?: {
    client_id?: number
    contract_id?: number
    description?: string
    start_date?: string
    end_date?: string
    invoiced?: 'true' | 'false'
    limit?: number
  }) => {
    const q = new URLSearchParams()
    Object.entries(params ?? {}).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== '') q.set(k, String(v))
    })
    const qs = q.toString()
    return request<TimeEntry[]>('GET', '/api/time-entries' + (qs ? '?' + qs : ''))
  },
  addTimeEntry: (data: {
    contract_id?: number
    contract_number?: string
    hours: number
    date: string
    description?: string
  }) => request<{ id: string }>('POST', '/api/time-entries', data),
  bulkAddTimeEntries: (entries: any[]) =>
    request<{ ids: string[]; count: number }>(
      'POST',
      '/api/time-entries/bulk',
      { entries },
    ),
  updateTimeEntry: (
    id: string,
    data: { hours?: number; date?: string; description?: string },
  ) => request<{ id: string }>('PUT', `/api/time-entries/${id}`, data),
  deleteTimeEntry: (id: string) =>
    request<{ deleted: string }>('DELETE', `/api/time-entries/${id}`),
  bulkDeleteTimeEntries: (ids: string[]) =>
    request<{ deleted: number }>('POST', '/api/time-entries/bulk-delete', { ids }),
  markTimeEntriesInvoiced: (invoice_number: string, ids: string[]) =>
    request<{ marked: number }>('POST', '/api/time-entries/mark-invoiced', {
      invoice_number,
      ids,
    }),
  unmarkTimeEntries: (ids: string[]) =>
    request<{ unmarked: number }>('POST', '/api/time-entries/unmark', { ids }),

  // Invoices
  listInvoices: (params?: { client_id?: number; status?: string }) => {
    const q = new URLSearchParams()
    if (params?.client_id) q.set('client_id', String(params.client_id))
    if (params?.status) q.set('status', params.status)
    const qs = q.toString()
    return request<Invoice[]>('GET', '/api/invoices' + (qs ? '?' + qs : ''))
  },
  getInvoice: (number: string) =>
    request<InvoiceDetails>('GET', `/api/invoices/${encodeURIComponent(number)}`),
  createInvoice: (data: {
    client_id: number
    period?: string
    start_date?: string
    end_date?: string
    due_days?: number
  }) => request<any>('POST', '/api/invoices', data),
  updateInvoiceStatus: (number: string, status: string) =>
    request<{ status: string }>(
      'PATCH',
      `/api/invoices/${encodeURIComponent(number)}`,
      { status },
    ),
  deleteInvoice: (number: string) =>
    request<{ deleted: string }>(
      'DELETE',
      `/api/invoices/${encodeURIComponent(number)}`,
    ),
  downloadInvoice: (number: string) =>
    request<{ invoice_number: string; pdf_path: string; export_dir: string }>(
      'POST',
      `/api/invoices/${encodeURIComponent(number)}/download`,
    ),

  // Quotes
  listQuotes: (params?: { client_id?: number; status?: string }) => {
    const q = new URLSearchParams()
    if (params?.client_id) q.set('client_id', String(params.client_id))
    if (params?.status) q.set('status', params.status)
    const qs = q.toString()
    return request<Quote[]>('GET', '/api/quotes' + (qs ? '?' + qs : ''))
  },
  getQuote: (number: string) =>
    request<QuoteDetails>('GET', `/api/quotes/${encodeURIComponent(number)}`),
  createQuote: (data: {
    client_id: number
    title: string
    issue_date?: string
    valid_until?: string
    valid_days?: number
    currency?: string
    notes?: string
    line_items: Array<Omit<Partial<QuoteLineItem>, 'id' | 'quote_id' | 'amount' | 'sort_order'> & {
      description: string
      quantity: number
      unit_price: number
    }>
  }) => request<{ id: number; quote_number: string; total_amount: number }>('POST', '/api/quotes', data),
  updateQuote: (
    number: string,
    data: {
      title?: string
      notes?: string
      valid_until?: string
      currency?: string
      line_items?: Array<{
        description: string
        quantity: number
        unit?: string
        unit_price: number
      }>
    },
  ) =>
    request<{ quote_number: string }>(
      'PUT',
      `/api/quotes/${encodeURIComponent(number)}`,
      data,
    ),
  updateQuoteStatus: (number: string, status: string) =>
    request<{ status: string }>(
      'PATCH',
      `/api/quotes/${encodeURIComponent(number)}`,
      { status },
    ),
  deleteQuote: (number: string) =>
    request<{ deleted: string }>(
      'DELETE',
      `/api/quotes/${encodeURIComponent(number)}`,
    ),
  downloadQuote: (number: string) =>
    request<{ quote_number: string; pdf_path: string; export_dir: string }>(
      'POST',
      `/api/quotes/${encodeURIComponent(number)}/download`,
    ),
  convertQuote: (
    number: string,
    data: {
      contract_number: string
      contract_name?: string
      start_date?: string
      end_date?: string
      payment_terms?: string
    },
  ) =>
    request<{
      quote_number: string
      contract_id: number
      contract_number: string
      hourly_rate: number
    }>('POST', `/api/quotes/${encodeURIComponent(number)}/convert`, data),
}

export function formatCurrency(amount: number, currency = 'USD'): string {
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency,
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(amount)
  } catch {
    return `${currency} ${amount.toFixed(2)}`
  }
}

export function formatDate(s?: string | null): string {
  if (!s) return '—'
  const d = new Date(s)
  if (isNaN(d.getTime())) return s
  return d.toISOString().slice(0, 10)
}

export function formatHours(h: number): string {
  return h.toFixed(2)
}
