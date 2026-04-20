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
  total_hours: number
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
