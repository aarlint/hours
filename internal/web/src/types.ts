export interface Client {
  id: number
  name: string
  address?: string
  city?: string
  state?: string
  zip_code?: string
  country?: string
  created_at: string
  updated_at: string
  active_contracts: number
}

export interface Contract {
  id: number
  client_id: number
  client_name: string
  contract_number: string
  name: string
  hourly_rate: number
  currency: string
  contract_type: string
  start_date: string
  end_date?: string | null
  status: string
  payment_terms?: string
  payment_method_id?: number | null
  notes?: string
  created_at: string
  updated_at: string
}

export interface Recipient {
  id: number
  client_id: number
  name: string
  email: string
  title?: string
  phone?: string
  is_primary: boolean
}

export interface PaymentDetails {
  id: number
  client_id: number
  bank_name?: string
  account_number?: string
  routing_number?: string
  swift_code?: string
  payment_terms?: string
  notes?: string
  updated_at: string
}

export interface PaymentMethod {
  id: number
  label: string
  bank_name?: string
  account_number?: string
  routing_number?: string
  swift_code?: string
  payment_terms?: string
  notes?: string
  is_default: boolean
  created_at: string
  updated_at: string
}

export interface PaymentMethodInput {
  label: string
  bank_name?: string
  account_number?: string
  routing_number?: string
  swift_code?: string
  payment_terms?: string
  notes?: string
  is_default?: boolean
}

export interface TimeEntry {
  id: string
  contract_id: number
  client_id: number
  client_name: string
  contract_number: string
  contract_name: string
  date: string
  hours: number
  description: string
  invoice_id?: number | null
  invoice_number?: string | null
  hourly_rate: number
  currency: string
  amount: number
  created_at: string
}

export interface Invoice {
  id: number
  invoice_number: string
  client_id: number
  client_name: string
  issue_date: string
  due_date: string
  total_amount: number
  status: 'draft' | 'pending' | 'sent' | 'paid' | 'overdue' | 'cancelled'
  pdf_path?: string
  created_at: string
}

export interface InvoiceDetails {
  invoice: Invoice
  time_entries: TimeEntry[]
  expenses: Expense[]
  total_hours: number
  total_expenses: number
}

export interface Expense {
  id: string
  client_id: number
  client_name: string
  contract_id?: number | null
  contract_number?: string
  date: string
  description: string
  amount: number
  currency: string
  category?: string
  receipt_path?: string
  invoice_id?: number | null
  invoice_number?: string
  created_at: string
}

export interface ExpenseInput {
  client_id: number
  contract_id?: number | null
  contract_number?: string
  date: string
  description: string
  amount: number
  currency?: string
  category?: string
  receipt_path?: string
}

export interface InvoicePreview {
  invoice: Invoice
  client: Client
  contracts: Contract[]
  time_entries: TimeEntry[]
  expenses: Expense[]
  total_hours: number
  total_expenses: number
  payment: PaymentDetails
  recipients: Recipient[]
  business: BusinessInfo
}

export interface QuoteLineItem {
  id: number
  quote_id: number
  description: string
  quantity: number
  unit: string
  unit_price: number
  amount: number
  sort_order: number
}

export interface Quote {
  id: number
  quote_number: string
  client_id: number
  client_name: string
  title: string
  issue_date: string
  valid_until: string
  subtotal: number
  total_amount: number
  currency: string
  status: 'draft' | 'sent' | 'accepted' | 'rejected' | 'expired' | 'converted'
  notes?: string
  pdf_path?: string
  converted_contract_id?: number | null
  created_at: string
  updated_at: string
}

export interface QuoteDetails {
  quote: Quote
  line_items: QuoteLineItem[]
}

export interface BusinessInfo {
  id: number
  business_name: string
  contact_name: string
  email: string
  phone?: string
  address?: string
  city?: string
  state?: string
  zip_code?: string
  country?: string
  tax_id?: string
  website?: string
  logo_path?: string
  invoice_prefix?: string
  export_path?: string
  updated_at: string
}

// AuthState mirrors GET /api/me. When auth is disabled by the server the
// payload is `{ auth_enabled: false }`; otherwise it's a fully populated
// user record (or 401 when no session is present).
export interface AuthState {
  auth_enabled?: boolean
  id?: number
  email?: string
  name?: string
  role?: 'admin' | 'user' | string
}

// ---------- API tokens ----------

// Every defined scope in the same order the backend's auth.AllScopes() emits.
// The UI iterates this list to render the scope-picker checkbox grid in the
// token-mint modal. Keep in sync with internal/auth/scopes.go.
export const ALL_SCOPES = [
  'clients:read', 'clients:write',
  'contracts:read', 'contracts:write',
  'time_entries:read', 'time_entries:write',
  'invoices:read', 'invoices:write',
  'quotes:read', 'quotes:write',
  'expenses:read', 'expenses:write',
  'payment_methods:read', 'payment_methods:write',
  'business_info:read', 'business_info:write',
  'recipients:read', 'recipients:write',
  'stats:read',
  'events:read',
  'data:export', 'data:import',
] as const

export type Scope = (typeof ALL_SCOPES)[number]

// ApiToken is the metadata shape returned by GET /api/tokens — the raw
// secret is gone forever after the mint response.
export interface ApiToken {
  id: number
  name: string
  scopes: string[]
  token_prefix: string
  expires_at?: string | null
  last_used_at?: string | null
  created_at: string
}

// ApiTokenWithSecret is the one-time mint response. token is the raw bearer
// the user must copy before dismissing the modal.
export interface ApiTokenWithSecret extends ApiToken {
  token: string
}

export interface CreateTokenReq {
  name: string
  scopes: string[]
  // RFC3339 datetime; null/undefined means no expiry.
  expires_at?: string | null
}

// TokenUsageSummary aggregates per-token usage stats — the shape returned by
// GET /api/tokens/{id}/usage. Matches api.tokenUsageSummaryDTO on the backend.
export interface TokenUsageSummary {
  total_calls: number
  calls_24h: number
  calls_7d: number
  calls_30d: number
  errors_24h: number
  last_call_at: string | null
  last_path: string | null
  last_method: string | null
  last_status: number | null
  by_path: { path: string; count: number; errors: number; last_at: string }[]
}

// TokenUsageEvent is one row from /api/tokens/{id}/usage/recent — newest
// first, capped at 50 entries by the server.
export interface TokenUsageEvent {
  method: string
  path: string
  status: number
  duration_ms: number
  error: string | null
  created_at: string
}

export interface Stats {
  total_clients: number
  active_contracts: number
  unbilled_hours: number
  unbilled_amount: number
  hours_this_month: number
  hours_last_month: number
  outstanding_amount: number
  paid_amount: number
  invoices_pending: number
  invoices_paid: number
  recent_entries: TimeEntry[]
}
