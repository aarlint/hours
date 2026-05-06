import type {
  ApiToken,
  ApiTokenWithSecret,
  AuthState,
  BusinessInfo,
  Client,
  Contract,
  CreateTokenReq,
  Expense,
  ExpenseInput,
  Invoice,
  InvoiceDetails,
  InvoicePreview,
  PaymentDetails,
  PaymentMethod,
  PaymentMethodInput,
  Quote,
  QuoteDetails,
  QuoteLineItem,
  Recipient,
  Stats,
  TimeEntry,
  TokenUsageSummary,
  TokenUsageEvent,
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
    credentials: 'same-origin',
  })
  if (res.status === 401 && !path.startsWith('/api/me')) {
    // Session expired or never started: kick the user to the login page so
    // OIDC can re-stamp a cookie. The auth handler then redirects them back.
    const here = window.location.pathname + window.location.search
    window.location.href = `/auth/login?return=${encodeURIComponent(here)}`
    throw new Error('not authenticated')
  }
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
  deleteClient: (id: number) =>
    request<{
      deleted: number
      name: string
      contracts: number
      time_entries: number
      invoices: number
      quotes: number
      recipients: number
    }>('DELETE', `/api/clients/${id}`),

  // Recipients
  listRecipients: (clientId: number) =>
    request<Recipient[]>('GET', `/api/clients/${clientId}/recipients`),
  addRecipient: (clientId: number, data: Partial<Recipient>) =>
    request<{ id: number }>('POST', `/api/clients/${clientId}/recipients`, data),
  removeRecipient: (id: number) =>
    request<{ deleted: number }>('DELETE', `/api/recipients/${id}`),

  // Payment (legacy per-client — kept for backward compat with existing data)
  getPaymentDetails: (clientId: number) =>
    request<PaymentDetails | null>('GET', `/api/clients/${clientId}/payment-details`),
  setPaymentDetails: (clientId: number, data: Partial<PaymentDetails>) =>
    request<{ client_id: number }>('PUT', `/api/clients/${clientId}/payment-details`, data),

  // Payment methods (business-level — attached to contracts)
  listPaymentMethods: () => request<PaymentMethod[]>('GET', '/api/payment-methods'),
  addPaymentMethod: (data: PaymentMethodInput) =>
    request<{ id: number; label: string }>('POST', '/api/payment-methods', data),
  updatePaymentMethod: (id: number, data: PaymentMethodInput) =>
    request<{ id: number }>('PUT', `/api/payment-methods/${id}`, data),
  deletePaymentMethod: (id: number) =>
    request<{
      deleted: number
      label: string
      detached_contracts: number
      detached_invoices: number
    }>('DELETE', `/api/payment-methods/${id}`),

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
  editContract: (
    id: number,
    data: {
      name?: string
      hourly_rate?: number
      currency?: string
      contract_type?: string
      end_date?: string
      status?: string
      payment_terms?: string
      payment_method_id?: number | null
      clear_payment_method?: boolean
      notes?: string
    },
  ) => request<{ id: number }>('PUT', `/api/contracts/${id}`, data),

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
  previewInvoice: (number: string) =>
    request<InvoicePreview>('GET', `/api/invoices/${encodeURIComponent(number)}/preview`),
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
  // downloadInvoice was removed — use downloadInvoiceFile() below, which
  // streams PDF bytes (web: anchor-click; Wails: native Save-As dialog).

  // Expenses
  listExpenses: (params?: {
    client_id?: number
    invoiced?: 'true' | 'false'
    start_date?: string
    end_date?: string
    category?: string
  }) => {
    const q = new URLSearchParams()
    Object.entries(params ?? {}).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== '') q.set(k, String(v))
    })
    const qs = q.toString()
    return request<Expense[]>('GET', '/api/expenses' + (qs ? '?' + qs : ''))
  },
  addExpense: (data: ExpenseInput) =>
    request<{ id: string }>('POST', '/api/expenses', data),
  updateExpense: (id: string, data: Partial<ExpenseInput>) =>
    request<{ id: string }>('PUT', `/api/expenses/${id}`, data),
  deleteExpense: (id: string) =>
    request<{ deleted: string }>('DELETE', `/api/expenses/${id}`),

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
  // downloadQuote was removed — use downloadQuoteFile() below, which
  // streams PDF bytes (web: anchor-click; Wails: native Save-As dialog).
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

  // Auth
  me: () => request<AuthState>('GET', '/api/me'),
  logout: () => request<{ ok: true }>('POST', '/auth/logout'),

  // API tokens — session-only management of personal bearer tokens.
  // POST returns the raw token exactly once; subsequent GETs only expose
  // metadata + the visible prefix.
  listApiTokens: () => request<ApiToken[]>('GET', '/api/tokens'),
  createApiToken: (body: CreateTokenReq) =>
    request<ApiTokenWithSecret>('POST', '/api/tokens', body),
  revokeApiToken: (id: number) =>
    request<{ deleted: number }>('DELETE', `/api/tokens/${id}`),

  // Per-token usage metrics. Both endpoints are session-only; bearer tokens
  // can't probe their own (or anyone else's) usage history.
  getApiTokenUsage: (id: number) =>
    request<TokenUsageSummary>('GET', `/api/tokens/${id}/usage`),
  getApiTokenUsageRecent: (id: number) =>
    request<TokenUsageEvent[]>('GET', `/api/tokens/${id}/usage/recent`),

  // Data export/import — exportData returns the parsed JSON object so the
  // caller can either offer it as a download or post it back to /api/import.
  exportData: () => request<unknown>('GET', '/api/export'),
  importData: (payload: unknown) =>
    request<{ ok: true; imported: Record<string, number> }>(
      'POST',
      '/api/import',
      payload,
    ),
}

// ---------- PDF download helpers ----------
//
// In the web (--serve) deployment we hit the streaming endpoint with fetch,
// turn the response into a Blob, and trigger an anchor click so the browser
// runs its standard "save / open" UI.
//
// In the Wails desktop app we delegate to a per-document Go binding
// (SaveInvoicePDF / SaveQuotePDF). That dispatches the same HTTP request
// through the in-process mux and pops a native Save-As dialog — necessary
// because the Wails transport returns string-only bodies and can't safely
// carry binary PDF.

function suggestedInvoiceFilename(invoiceNumber: string): string {
  const today = new Date().toISOString().slice(0, 10)
  return `invoice_${invoiceNumber}_${today}.pdf`
}

function suggestedQuoteFilename(quoteNumber: string): string {
  const today = new Date().toISOString().slice(0, 10)
  return `quote_${quoteNumber}_${today}.pdf`
}

function filenameFromContentDisposition(header: string | null, fallback: string): string {
  if (!header) return fallback
  const m = header.match(/filename="([^"]+)"/) ?? header.match(/filename=([^;]+)/)
  return m?.[1]?.trim() || fallback
}

async function browserDownload(path: string, fallbackFilename: string): Promise<{ filename: string }> {
  const res = await fetch(path, { method: 'POST', credentials: 'same-origin' })
  if (!res.ok) {
    let msg = `${res.status}`
    try {
      const data = await res.json()
      if (data?.error) msg = data.error
    } catch {}
    throw new Error(msg)
  }
  const blob = await res.blob()
  const filename = filenameFromContentDisposition(
    res.headers.get('Content-Disposition'),
    fallbackFilename,
  )
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
  return { filename }
}

export interface DownloadResult {
  /** Suggested or chosen filename (always set). */
  filename: string
  /** Absolute path the user picked in Wails Save-As, null in web mode or if cancelled. */
  chosenPath: string | null
}

export async function downloadInvoiceFile(invoiceNumber: string): Promise<DownloadResult> {
  const filename = suggestedInvoiceFilename(invoiceNumber)
  if (isWails()) {
    const app = goApp()
    if (!app.SaveInvoicePDF) throw new Error('SaveInvoicePDF binding missing — rebuild the desktop app')
    const chosen = await app.SaveInvoicePDF(invoiceNumber, filename)
    return { filename, chosenPath: chosen || null }
  }
  const { filename: actual } = await browserDownload(
    `/api/invoices/${encodeURIComponent(invoiceNumber)}/download`,
    filename,
  )
  return { filename: actual, chosenPath: null }
}

export async function downloadQuoteFile(quoteNumber: string): Promise<DownloadResult> {
  const filename = suggestedQuoteFilename(quoteNumber)
  if (isWails()) {
    const app = goApp()
    if (!app.SaveQuotePDF) throw new Error('SaveQuotePDF binding missing — rebuild the desktop app')
    const chosen = await app.SaveQuotePDF(quoteNumber, filename)
    return { filename, chosenPath: chosen || null }
  }
  const { filename: actual } = await browserDownload(
    `/api/quotes/${encodeURIComponent(quoteNumber)}/download`,
    filename,
  )
  return { filename: actual, chosenPath: null }
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
