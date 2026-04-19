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
)

type Server struct {
	db     *sql.DB
	mux    *http.ServeMux
	assets fs.FS
}

func NewServer(db *sql.DB, assets fs.FS) *Server {
	s := &Server{
		db:     db,
		mux:    http.NewServeMux(),
		assets: assets,
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

	// Dashboard
	s.mux.HandleFunc("GET /api/stats", jsonHandler(h.getStats))

	// Server-Sent Events stream
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
	InitEventBus(s.db)

	// Business info
	s.mux.HandleFunc("GET /api/business-info", jsonHandler(h.getBusinessInfo))
	s.mux.HandleFunc("PUT /api/business-info", jsonHandler(h.setBusinessInfo))

	// Clients
	s.mux.HandleFunc("GET /api/clients", jsonHandler(h.listClients))
	s.mux.HandleFunc("POST /api/clients", jsonHandler(h.addClient))
	s.mux.HandleFunc("PUT /api/clients/{id}", jsonHandler(h.editClient))

	// Recipients
	s.mux.HandleFunc("GET /api/clients/{id}/recipients", jsonHandler(h.listRecipients))
	s.mux.HandleFunc("POST /api/clients/{id}/recipients", jsonHandler(h.addRecipient))
	s.mux.HandleFunc("DELETE /api/recipients/{id}", jsonHandler(h.removeRecipient))

	// Payment details
	s.mux.HandleFunc("GET /api/clients/{id}/payment-details", jsonHandler(h.getPaymentDetails))
	s.mux.HandleFunc("PUT /api/clients/{id}/payment-details", jsonHandler(h.setPaymentDetails))

	// Contracts
	s.mux.HandleFunc("GET /api/contracts", jsonHandler(h.listContracts))
	s.mux.HandleFunc("POST /api/contracts", jsonHandler(h.addContract))

	// Time entries
	s.mux.HandleFunc("GET /api/time-entries", jsonHandler(h.searchTimeEntries))
	s.mux.HandleFunc("POST /api/time-entries", jsonHandler(h.addTimeEntry))
	s.mux.HandleFunc("POST /api/time-entries/bulk", jsonHandler(h.bulkAddTimeEntries))
	s.mux.HandleFunc("POST /api/time-entries/bulk-delete", jsonHandler(h.bulkDeleteTimeEntries))
	s.mux.HandleFunc("POST /api/time-entries/mark-invoiced", jsonHandler(h.markTimeEntriesInvoiced))
	s.mux.HandleFunc("POST /api/time-entries/unmark", jsonHandler(h.unmarkTimeEntries))
	s.mux.HandleFunc("PUT /api/time-entries/{id}", jsonHandler(h.updateTimeEntry))
	s.mux.HandleFunc("DELETE /api/time-entries/{id}", jsonHandler(h.deleteTimeEntry))

	// Invoices
	s.mux.HandleFunc("GET /api/invoices", jsonHandler(h.listInvoices))
	s.mux.HandleFunc("POST /api/invoices", jsonHandler(h.createInvoice))
	s.mux.HandleFunc("GET /api/invoices/{number}", jsonHandler(h.getInvoiceDetails))
	s.mux.HandleFunc("PATCH /api/invoices/{number}", jsonHandler(h.updateInvoiceStatus))
	s.mux.HandleFunc("DELETE /api/invoices/{number}", jsonHandler(h.deleteInvoice))
	s.mux.HandleFunc("POST /api/invoices/{number}/download", jsonHandler(h.downloadInvoice))

	// Static frontend (SPA fallback)
	if s.assets != nil {
		s.mux.HandleFunc("/", s.serveSPA)
	}
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
