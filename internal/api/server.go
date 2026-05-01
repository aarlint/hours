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
)

type Server struct {
	db     *sql.DB
	mux    *http.ServeMux
	assets fs.FS
	auth   *auth.Auth
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

	if s.auth.Enabled() {
		apiMux.HandleFunc("GET /api/me", s.auth.MeHandler)
	} else {
		// Always expose /api/me so the frontend can detect "auth disabled" mode.
		apiMux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"auth_enabled":false}`))
		})
	}

	// Dashboard
	apiMux.HandleFunc("GET /api/stats", jsonHandler(h.getStats))

	// Server-Sent Events stream
	apiMux.HandleFunc("GET /api/events", s.handleEvents)
	InitEventBus(s.db)

	// Business info
	apiMux.HandleFunc("GET /api/business-info", jsonHandler(h.getBusinessInfo))
	apiMux.HandleFunc("PUT /api/business-info", jsonHandler(h.setBusinessInfo))

	// Clients
	apiMux.HandleFunc("GET /api/clients", jsonHandler(h.listClients))
	apiMux.HandleFunc("POST /api/clients", jsonHandler(h.addClient))
	apiMux.HandleFunc("PUT /api/clients/{id}", jsonHandler(h.editClient))
	apiMux.HandleFunc("DELETE /api/clients/{id}", jsonHandler(h.deleteClient))

	// Recipients
	apiMux.HandleFunc("GET /api/clients/{id}/recipients", jsonHandler(h.listRecipients))
	apiMux.HandleFunc("POST /api/clients/{id}/recipients", jsonHandler(h.addRecipient))
	apiMux.HandleFunc("DELETE /api/recipients/{id}", jsonHandler(h.removeRecipient))

	// Payment details (legacy per-client — kept for backward-compat;
	// new UI uses business-level /api/payment-methods below).
	apiMux.HandleFunc("GET /api/clients/{id}/payment-details", jsonHandler(h.getPaymentDetails))
	apiMux.HandleFunc("PUT /api/clients/{id}/payment-details", jsonHandler(h.setPaymentDetails))

	// Payment methods (business-level — attached to contracts)
	apiMux.HandleFunc("GET /api/payment-methods", jsonHandler(h.listPaymentMethods))
	apiMux.HandleFunc("POST /api/payment-methods", jsonHandler(h.addPaymentMethod))
	apiMux.HandleFunc("PUT /api/payment-methods/{id}", jsonHandler(h.updatePaymentMethod))
	apiMux.HandleFunc("DELETE /api/payment-methods/{id}", jsonHandler(h.deletePaymentMethod))

	// Contracts
	apiMux.HandleFunc("GET /api/contracts", jsonHandler(h.listContracts))
	apiMux.HandleFunc("POST /api/contracts", jsonHandler(h.addContract))
	apiMux.HandleFunc("PUT /api/contracts/{id}", jsonHandler(h.editContract))

	// Time entries
	apiMux.HandleFunc("GET /api/time-entries", jsonHandler(h.searchTimeEntries))
	apiMux.HandleFunc("POST /api/time-entries", jsonHandler(h.addTimeEntry))
	apiMux.HandleFunc("POST /api/time-entries/bulk", jsonHandler(h.bulkAddTimeEntries))
	apiMux.HandleFunc("POST /api/time-entries/bulk-delete", jsonHandler(h.bulkDeleteTimeEntries))
	apiMux.HandleFunc("POST /api/time-entries/mark-invoiced", jsonHandler(h.markTimeEntriesInvoiced))
	apiMux.HandleFunc("POST /api/time-entries/unmark", jsonHandler(h.unmarkTimeEntries))
	apiMux.HandleFunc("PUT /api/time-entries/{id}", jsonHandler(h.updateTimeEntry))
	apiMux.HandleFunc("DELETE /api/time-entries/{id}", jsonHandler(h.deleteTimeEntry))

	// Invoices
	apiMux.HandleFunc("GET /api/invoices", jsonHandler(h.listInvoices))
	apiMux.HandleFunc("POST /api/invoices", jsonHandler(h.createInvoice))
	apiMux.HandleFunc("GET /api/invoices/{number}", jsonHandler(h.getInvoiceDetails))
	apiMux.HandleFunc("GET /api/invoices/{number}/preview", jsonHandler(h.getInvoicePreview))
	apiMux.HandleFunc("PATCH /api/invoices/{number}", jsonHandler(h.updateInvoiceStatus))
	apiMux.HandleFunc("DELETE /api/invoices/{number}", jsonHandler(h.deleteInvoice))
	apiMux.HandleFunc("POST /api/invoices/{number}/download", jsonHandler(h.downloadInvoice))

	// Expenses
	apiMux.HandleFunc("GET /api/expenses", jsonHandler(h.listExpenses))
	apiMux.HandleFunc("POST /api/expenses", jsonHandler(h.addExpense))
	apiMux.HandleFunc("PUT /api/expenses/{id}", jsonHandler(h.updateExpense))
	apiMux.HandleFunc("DELETE /api/expenses/{id}", jsonHandler(h.deleteExpense))

	// Quotes
	apiMux.HandleFunc("GET /api/quotes", jsonHandler(h.listQuotes))
	apiMux.HandleFunc("POST /api/quotes", jsonHandler(h.createQuote))
	apiMux.HandleFunc("GET /api/quotes/{number}", jsonHandler(h.getQuoteDetails))
	apiMux.HandleFunc("PUT /api/quotes/{number}", jsonHandler(h.updateQuote))
	apiMux.HandleFunc("PATCH /api/quotes/{number}", jsonHandler(h.updateQuoteStatus))
	apiMux.HandleFunc("DELETE /api/quotes/{number}", jsonHandler(h.deleteQuote))
	apiMux.HandleFunc("POST /api/quotes/{number}/download", jsonHandler(h.downloadQuote))
	apiMux.HandleFunc("POST /api/quotes/{number}/convert", jsonHandler(h.convertQuote))

	// Data portability — JSON export of every business-level table, and
	// a destructive import that wipes the existing rows first. Import is
	// admin-only when auth is enabled (see below).
	apiMux.HandleFunc("GET /api/export", h.exportData)
	apiMux.Handle("POST /api/import", s.adminOnly(http.HandlerFunc(h.importData)))

	// Mount the API mux. When auth is enabled, every /api/* path goes
	// through the auth middleware. /api/me handles the unauth case itself
	// (it returns 401), so we exclude it from the redirect path.
	if s.auth.Enabled() {
		s.mux.Handle("/api/", s.auth.Middleware(apiMux))
	} else {
		s.mux.Handle("/api/", apiMux)
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
