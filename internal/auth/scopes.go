// Package auth — scopes.go defines the fine-grained permission set carried by
// API tokens. Session-cookie auth implicitly grants every scope (full UI
// access). Bearer-token auth must explicitly enumerate the scopes the token
// needs; routes are wrapped with RequireScope to enforce one scope per route.
package auth

// Scope is a string of the form "<resource>:<action>". Tokens carry a set of
// scopes; each protected route declares the single scope required to call it.
// Session-cookie auth implicitly grants every scope (full UI access).
type Scope string

const (
	ScopeClientsRead         Scope = "clients:read"
	ScopeClientsWrite        Scope = "clients:write"
	ScopeContractsRead       Scope = "contracts:read"
	ScopeContractsWrite      Scope = "contracts:write"
	ScopeTimeEntriesRead     Scope = "time_entries:read"
	ScopeTimeEntriesWrite    Scope = "time_entries:write"
	ScopeInvoicesRead        Scope = "invoices:read"
	ScopeInvoicesWrite       Scope = "invoices:write"
	ScopeQuotesRead          Scope = "quotes:read"
	ScopeQuotesWrite         Scope = "quotes:write"
	ScopeExpensesRead        Scope = "expenses:read"
	ScopeExpensesWrite       Scope = "expenses:write"
	ScopePaymentMethodsRead  Scope = "payment_methods:read"
	ScopePaymentMethodsWrite Scope = "payment_methods:write"
	ScopeBusinessInfoRead    Scope = "business_info:read"
	ScopeBusinessInfoWrite   Scope = "business_info:write"
	ScopeRecipientsRead      Scope = "recipients:read"
	ScopeRecipientsWrite     Scope = "recipients:write"
	ScopeStatsRead           Scope = "stats:read"
	ScopeEventsRead          Scope = "events:read"
	ScopeDataExport          Scope = "data:export"
	ScopeDataImport          Scope = "data:import" // admin-only at the role layer
	ScopeAll                 Scope = "*"           // wildcard — grants everything
)

// AllScopes returns every defined scope (excluding "*"). Used for the UI
// "select all" affordance and for admin-issued tokens that want full access.
func AllScopes() []Scope {
	return []Scope{
		ScopeClientsRead, ScopeClientsWrite,
		ScopeContractsRead, ScopeContractsWrite,
		ScopeTimeEntriesRead, ScopeTimeEntriesWrite,
		ScopeInvoicesRead, ScopeInvoicesWrite,
		ScopeQuotesRead, ScopeQuotesWrite,
		ScopeExpensesRead, ScopeExpensesWrite,
		ScopePaymentMethodsRead, ScopePaymentMethodsWrite,
		ScopeBusinessInfoRead, ScopeBusinessInfoWrite,
		ScopeRecipientsRead, ScopeRecipientsWrite,
		ScopeStatsRead,
		ScopeEventsRead,
		ScopeDataExport, ScopeDataImport,
	}
}

// IsKnownScope reports whether s names a defined scope (or the "*" wildcard).
// Used by the token mint endpoint to reject typos before persisting them.
func IsKnownScope(s Scope) bool {
	if s == ScopeAll {
		return true
	}
	for _, k := range AllScopes() {
		if k == s {
			return true
		}
	}
	return false
}

// HasScope reports whether the provided set grants the requested scope. The
// wildcard "*" matches anything. Exposed (capitalized) so handlers in other
// packages can do scope checks without re-implementing the loop.
func HasScope(have []Scope, want Scope) bool {
	for _, s := range have {
		if s == ScopeAll || s == want {
			return true
		}
	}
	return false
}
