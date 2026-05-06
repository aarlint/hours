export function fmtUSD(n: number | null | undefined): string {
  if (n == null) return '—'
  return n.toLocaleString('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

export function fmtUSDShort(n: number | null | undefined): string {
  if (n == null) return '—'
  return '$' + Math.round(n).toLocaleString('en-US')
}

export function fmtInt(n: number | null | undefined): string {
  if (n == null) return '—'
  return Math.round(n).toLocaleString('en-US')
}

export function fmtHours(h: number): string {
  return h.toFixed(2)
}

export function fmtDateShort(iso: string): string {
  const d = new Date(iso.length <= 10 ? iso + 'T12:00:00' : iso)
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

export function fmtDOW(iso: string): string {
  const d = new Date(iso.length <= 10 ? iso + 'T12:00:00' : iso)
  return d.toLocaleDateString('en-US', { weekday: 'short' })
}

export function relDay(iso: string, ref = new Date()): string {
  const d = new Date(iso.length <= 10 ? iso + 'T12:00:00' : iso)
  const t = new Date(ref)
  t.setHours(12, 0, 0, 0)
  const days = Math.round((t.getTime() - d.getTime()) / 86400000)
  if (days === 0) return 'Today'
  if (days === 1) return 'Yesterday'
  if (days > 1 && days < 7) return `${days}d ago`
  if (days === -1) return 'Tomorrow'
  return fmtDateShort(iso)
}

export function isoDate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

// fmtRelTime renders a sub-day-granularity "X ago" string for a timestamp.
// Used by the API-token usage panel where we care about minutes/hours rather
// than calendar days. Returns '—' for null/empty/unparseable inputs.
//
//   <30s    → "just now"
//   <60min  → "Nm ago"
//   <24h    → "Nh ago"
//   <7d     → "Nd ago"
//   else    → fmtDateShort()
export function fmtRelTime(iso?: string | null, ref = new Date()): string {
  if (!iso) return '—'
  const d = new Date(iso.length <= 10 ? iso + 'T12:00:00' : iso)
  const t = d.getTime()
  if (Number.isNaN(t)) return iso ?? '—'
  const diffMs = ref.getTime() - t
  // Future timestamps clamp to "just now" rather than showing "-3s ago" —
  // small clock skew between server and browser shouldn't surprise users.
  if (diffMs < 30_000) return 'just now'
  const sec = Math.floor(diffMs / 1000)
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  const hr = Math.floor(sec / 3600)
  if (hr < 24) return `${hr}h ago`
  const days = Math.floor(hr / 24)
  if (days === 1) return 'yesterday'
  if (days < 7) return `${days}d ago`
  return fmtDateShort(iso)
}
