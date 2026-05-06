package api

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/austin/hours-mcp/internal/auth"
	"github.com/austin/hours-mcp/internal/database"
)

type Server struct {
	db     *sql.DB
	mux    *http.ServeMux
	apiMux *http.ServeMux
	assets fs.FS
	auth   *auth.Auth
}

// AttachMCPHandler registers an http.Handler to serve the remote MCP endpoint
// at /api/mcp (and /api/mcp/...). It must be called BEFORE ListenAndServe
// returns; in practice, call it immediately after NewServerWithAuth.
//
// /api/mcp inherits the JSON 401 behaviour of the auth middleware (rather
// than the browser redirect used for SPA paths) because mcp-remote / Claude
// Desktop expect that response shape.
//
// We expose this as a setter rather than wiring the MCP server inline so the
// api package stays free of the internal/server (tool registration) package
// — internal/server already imports internal/api for shared helpers, so a
// reverse dependency would create a cycle.
func (s *Server) AttachMCPHandler(h http.Handler) {
	s.apiMux.Handle("/api/mcp", h)
	s.apiMux.Handle("/api/mcp/", h)
}

func NewServer(db *sql.DB, assets fs.FS) *Server {
	return NewServerWithAuth(db, assets, nil)
}

// NewServerWithAuth builds the server with an optional OIDC layer. Pass nil
// (or use NewServer) for the unauthenticated path used by the Wails GUI and
// by --serve when no OIDC env vars are set.
func NewServerWithAuth(db *sql.DB, assets fs.FS, a *auth.Auth) *Server {
	s := &Server{
		db:     db,
		mux:    http.NewServeMux(),
		assets: assets,
		auth:   a,
	}
	s.registerRoutes()
	return s
}

func (s *Server) Handler() http.Handler {
	return loggingMiddleware(s.mux)
}

func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("hours-mcp HTTP server listening on http://localhost%s", addr)
	return srv.ListenAndServe()
}

func (s *Server) registerRoutes() {
	h := &handlers{db: s.db}

	// Auth routes — registered before the auth middleware is attached
	// so they remain reachable for unauthenticated visitors.
	if s.auth.Enabled() {
		s.mux.HandleFunc("GET /auth/login", s.auth.LoginHandler)
		s.mux.HandleFunc("GET /auth/callback", s.auth.CallbackHandler)
		s.mux.HandleFunc("POST /auth/logout", s.auth.LogoutHandler)
	}

	// API mux — wrapped with auth middleware below if enabled.
	apiMux := http.NewServeMux()
	s.apiMux = apiMux

	if s.auth.Enabled() {
		apiMux.HandleFunc("GET /api/me", s.auth.MeHandler)
	} else {
		// Always expose /api/me so the frontend can detect "auth disabled" mode.
		apiMux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"auth_enabled":false}`))
		})
	}

	// scoped binds an http.Handler under a single scope check. When auth is
	// disabled (Wails / single-user) RequireScope is a no-op, so callers see
	// the same shape on both paths.
	scoped := func(scope auth.Scope, h http.Handler) http.Handler {
		return s.auth.RequireScope(scope)(h)
	}
	scopedFn := func(scope auth.Scope, fn http.HandlerFunc) http.Handler {
		return scoped(scope, fn)
	}

	// Dashboard
	apiMux.Handle("GET /api/stats", scopedFn(auth.ScopeStatsRead, jsonHandler(h.getStats)))

	// Server-Sent Events stream
	apiMux.Handle("GET /api/events", scopedFn(auth.ScopeEventsRead, http.HandlerFunc(s.handleEvents)))
	InitEventBus(s.db)

	// Business info
	apiMux.Handle("GET /api/business-info", scopedFn(auth.ScopeBusinessInfoRead, jsonHandler(h.getBusinessInfo)))
	apiMux.Handle("PUT /api/business-info", scopedFn(auth.ScopeBusinessInfoWrite, jsonHandler(h.setBusinessInfo)))

	// Clients
	apiMux.Handle("GET /api/clients", scopedFn(auth.ScopeClientsRead, jsonHandler(h.listClients)))
	apiMux.Handle("POST /api/clients", scopedFn(auth.ScopeClientsWrite, jsonHandler(h.addClient)))
	apiMux.Handle("PUT /api/clients/{id}", scopedFn(auth.ScopeClientsWrite, jsonHandler(h.editClient)))
	apiMux.Handle("DELETE /api/clients/{id}", scopedFn(auth.ScopeClientsWrite, jsonHandler(h.deleteClient)))

	// Recipients
	apiMux.Handle("GET /api/clients/{id}/recipients", scopedFn(auth.ScopeRecipientsRead, jsonHandler(h.listRecipients)))
	apiMux.Handle("POST /api/clients/{id}/recipients", scopedFn(auth.ScopeRecipientsWrite, jsonHandler(h.addRecipient)))
	apiMux.Handle("DELETE /api/recipients/{id}", scopedFn(auth.ScopeRecipientsWrite, jsonHandler(h.removeRecipient)))

	// Payment details (legacy per-client — kept for backward-compat;
	// new UI uses business-level /api/payment-methods below).
	apiMux.Handle("GET /api/clients/{id}/payment-details", scopedFn(auth.ScopePaymentMethodsRead, jsonHandler(h.getPaymentDetails)))
	apiMux.Handle("PUT /api/clients/{id}/payment-details", scopedFn(auth.ScopePaymentMethodsWrite, jsonHandler(h.setPaymentDetails)))

	// Payment methods (business-level — attached to contracts)
	apiMux.Handle("GET /api/payment-methods", scopedFn(auth.ScopePaymentMethodsRead, jsonHandler(h.listPaymentMethods)))
	apiMux.Handle("POST /api/payment-methods", scopedFn(auth.ScopePaymentMethodsWrite, jsonHandler(h.addPaymentMethod)))
	apiMux.Handle("PUT /api/payment-methods/{id}", scopedFn(auth.ScopePaymentMethodsWrite, jsonHandler(h.updatePaymentMethod)))
	apiMux.Handle("DELETE /api/payment-methods/{id}", scopedFn(auth.ScopePaymentMethodsWrite, jsonHandler(h.deletePaymentMethod)))

	// Contracts
	apiMux.Handle("GET /api/contracts", scopedFn(auth.ScopeContractsRead, jsonHandler(h.listContracts)))
	apiMux.Handle("POST /api/contracts", scopedFn(auth.ScopeContractsWrite, jsonHandler(h.addContract)))
	apiMux.Handle("PUT /api/contracts/{id}", scopedFn(auth.ScopeContractsWrite, jsonHandler(h.editContract)))

	// Time entries
	apiMux.Handle("GET /api/time-entries", scopedFn(auth.ScopeTimeEntriesRead, jsonHandler(h.searchTimeEntries)))
	apiMux.Handle("POST /api/time-entries", scopedFn(auth.ScopeTimeEntriesWrite, jsonHandler(h.addTimeEntry)))
	apiMux.Handle("POST /api/time-entries/bulk", scopedFn(auth.ScopeTimeEntriesWrite, jsonHandler(h.bulkAddTimeEntries)))
	apiMux.Handle("POST /api/time-entries/bulk-delete", scopedFn(auth.ScopeTimeEntriesWrite, jsonHandler(h.bulkDeleteTimeEntries)))
	apiMux.Handle("POST /api/time-entries/mark-invoiced", scopedFn(auth.ScopeTimeEntriesWrite, jsonHandler(h.markTimeEntriesInvoiced)))
	apiMux.Handle("POST /api/time-entries/unmark", scopedFn(auth.ScopeTimeEntriesWrite, jsonHandler(h.unmarkTimeEntries)))
	apiMux.Handle("PUT /api/time-entries/{id}", scopedFn(auth.ScopeTimeEntriesWrite, jsonHandler(h.updateTimeEntry)))
	apiMux.Handle("DELETE /api/time-entries/{id}", scopedFn(auth.ScopeTimeEntriesWrite, jsonHandler(h.deleteTimeEntry)))

	// Invoices
	apiMux.Handle("GET /api/invoices", scopedFn(auth.ScopeInvoicesRead, jsonHandler(h.listInvoices)))
	apiMux.Handle("POST /api/invoices", scopedFn(auth.ScopeInvoicesWrite, jsonHandler(h.createInvoice)))
	apiMux.Handle("GET /api/invoices/{number}", scopedFn(auth.ScopeInvoicesRead, jsonHandler(h.getInvoiceDetails)))
	apiMux.Handle("GET /api/invoices/{number}/preview", scopedFn(auth.ScopeInvoicesRead, jsonHandler(h.getInvoicePreview)))
	apiMux.Handle("PATCH /api/invoices/{number}", scopedFn(auth.ScopeInvoicesWrite, jsonHandler(h.updateInvoiceStatus)))
	apiMux.Handle("DELETE /api/invoices/{number}", scopedFn(auth.ScopeInvoicesWrite, jsonHandler(h.deleteInvoice)))
	apiMux.Handle("POST /api/invoices/{number}/download", scopedFn(auth.ScopeInvoicesRead, http.HandlerFunc(h.downloadInvoice)))

	// Expenses
	apiMux.Handle("GET /api/expenses", scopedFn(auth.ScopeExpensesRead, jsonHandler(h.listExpenses)))
	apiMux.Handle("POST /api/expenses", scopedFn(auth.ScopeExpensesWrite, jsonHandler(h.addExpense)))
	apiMux.Handle("PUT /api/expenses/{id}", scopedFn(auth.ScopeExpensesWrite, jsonHandler(h.updateExpense)))
	apiMux.Handle("DELETE /api/expenses/{id}", scopedFn(auth.ScopeExpensesWrite, jsonHandler(h.deleteExpense)))

	// Quotes
	apiMux.Handle("GET /api/quotes", scopedFn(auth.ScopeQuotesRead, jsonHandler(h.listQuotes)))
	apiMux.Handle("POST /api/quotes", scopedFn(auth.ScopeQuotesWrite, jsonHandler(h.createQuote)))
	apiMux.Handle("GET /api/quotes/{number}", scopedFn(auth.ScopeQuotesRead, jsonHandler(h.getQuoteDetails)))
	apiMux.Handle("PUT /api/quotes/{number}", scopedFn(auth.ScopeQuotesWrite, jsonHandler(h.updateQuote)))
	apiMux.Handle("PATCH /api/quotes/{number}", scopedFn(auth.ScopeQuotesWrite, jsonHandler(h.updateQuoteStatus)))
	apiMux.Handle("DELETE /api/quotes/{number}", scopedFn(auth.ScopeQuotesWrite, jsonHandler(h.deleteQuote)))
	apiMux.Handle("POST /api/quotes/{number}/download", scopedFn(auth.ScopeQuotesRead, http.HandlerFunc(h.downloadQuote)))
	// Convert needs both quote-write (mutates quote.status / converted_id) and
	// contract-write (creates a contract row). Chain RequireScope twice.
	apiMux.Handle("POST /api/quotes/{number}/convert",
		s.auth.RequireScope(auth.ScopeQuotesWrite)(
			s.auth.RequireScope(auth.ScopeContractsWrite)(jsonHandler(h.convertQuote)),
		),
	)

	// Data portability — JSON export of every business-level table, and
	// a destructive import that wipes the existing rows first. Import is
	// admin-only when auth is enabled (see below).
	apiMux.Handle("GET /api/export", scopedFn(auth.ScopeDataExport, http.HandlerFunc(h.exportData)))
	apiMux.Handle("POST /api/import",
		s.auth.RequireScope(auth.ScopeDataImport)(s.adminOnly(http.HandlerFunc(h.importData))),
	)

	// API tokens (session-only — bearer-authenticated requests cannot mint
	// or revoke other tokens). RequireSession returns 403 for token callers.
	apiMux.Handle("GET /api/tokens", s.auth.RequireSession()(jsonHandler(h.listAPITokens)))
	apiMux.Handle("POST /api/tokens", s.auth.RequireSession()(jsonHandler(h.createAPIToken)))
	apiMux.Handle("DELETE /api/tokens/{id}", s.auth.RequireSession()(jsonHandler(h.revokeAPIToken)))

	// Token usage metrics — also session-only so a leaked token cannot
	// inspect its own (or any) usage history.
	apiMux.Handle("GET /api/tokens/{id}/usage", s.auth.RequireSession()(jsonHandler(h.getAPITokenUsage)))
	apiMux.Handle("GET /api/tokens/{id}/usage/recent", s.auth.RequireSession()(jsonHandler(h.getAPITokenUsageRecent)))

	// Mount the API mux. When auth is enabled, every /api/* path goes
	// through the auth middleware. /api/me handles the unauth case itself
	// (it returns 401), so we exclude it from the redirect path.
	//
	// UsageRecorder is the inner wrapper: Middleware populates the user-in-
	// context, then UsageRecorder runs `next` and records to api_token_usage
	// on the way out (no-op for non-token traffic).
	if s.auth.Enabled() {
		s.mux.Handle("/api/", s.auth.Middleware(s.auth.UsageRecorder(apiMux)))
	} else {
		// Wails/local single-user mode: synthesise a DefaultUserID context
		// so every handler downstream sees a user without us having to
		// special-case the unauth path. The Wails desktop app is the only
		// caller of this branch; serve mode now requires OIDC up front.
		s.mux.Handle("/api/", localUserMiddleware(apiMux))
	}

	// Static frontend (SPA fallback). Auth-gated when enabled, but the
	// /auth/* routes above stay public because they were registered
	// directly on s.mux earlier.
	if s.assets != nil {
		if s.auth.Enabled() {
			s.mux.Handle("/", s.auth.Middleware(http.HandlerFunc(s.serveSPA)))
		} else {
			s.mux.HandleFunc("/", s.serveSPA)
		}
	}
}

// localUserMiddleware injects a synthetic User into the request context for
// the auth-disabled (Wails) path. Without it, currentUserID would return 401
// in the embedded server. Authenticated multi-tenant traffic never goes
// through here — it's gated by auth.Middleware instead.
func localUserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithLocalUser(r.Context(), database.DefaultUserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// adminOnly wraps next in the auth.RequireRole("admin") guard when auth is
// active. With auth disabled the underlying handler runs unchanged — the
// Wails GUI is single-user by definition so role checks would be theatre.
func (s *Server) adminOnly(next http.Handler) http.Handler {
	if !s.auth.Enabled() {
		return next
	}
	return s.auth.RequireRole("admin")(next)
}

func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// Prevent API paths falling through
	if strings.HasPrefix(path, "api/") {
		http.NotFound(w, r)
		return
	}

	if f, err := s.assets.Open(path); err == nil {
		f.Close()
		http.ServeFileFS(w, r, s.assets, path)
		return
	}

	// Fallback to index.html for SPA routing
	http.ServeFileFS(w, r, s.assets, "index.html")
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (sw *statusWriter) Unwrap() http.ResponseWriter { return sw.ResponseWriter }

// Shutdown gracefully stops the server
func (s *Server) Shutdown(ctx context.Context) error {
	_ = ctx
	return nil
}

func errForbidden(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusForbidden, fmt.Errorf("%s", msg))
}
