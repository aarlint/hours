package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/austin/hours-mcp/internal/auth"
	"github.com/austin/hours-mcp/internal/database"
)

// EventBus broadcasts SSE events to connected clients on a per-user basis.
// Multi-tenant: every subscriber is tagged with the userID of the session
// that opened the SSE stream, and broadcasts only fan out to subscribers
// whose userID matches.
//
// The background poller picks up changes made by other processes (e.g. the
// MCP stdio binary, which always writes as user_id=1) and pushes them only
// to subscribers belonging to that same user.

type eventMsg struct {
	Kind string
	Data any
}

type client struct {
	id     int64
	userID int64
	ch     chan eventMsg
}

type eventBus struct {
	mu      sync.RWMutex
	db      *sql.DB
	clients map[int64]*client
	nextID  atomic.Int64
	started atomic.Bool
}

var bus = &eventBus{
	clients: make(map[int64]*client),
}

// InitEventBus wires the event bus to the DB and starts the background poller.
// Safe to call more than once — subsequent calls are no-ops.
func InitEventBus(db *sql.DB) {
	if bus.started.Swap(true) {
		return
	}
	bus.db = db
	go bus.pollLoop(context.Background())
}

func (b *eventBus) subscribe(userID int64) *client {
	id := b.nextID.Add(1)
	c := &client{id: id, userID: userID, ch: make(chan eventMsg, 64)}
	b.mu.Lock()
	b.clients[id] = c
	b.mu.Unlock()
	return c
}

func (b *eventBus) unsubscribe(c *client) {
	b.mu.Lock()
	delete(b.clients, c.id)
	b.mu.Unlock()
	close(c.ch)
}

// broadcast fans the message out to every subscriber whose user_id matches.
// Pass userID=0 only for system-wide messages (none used today).
func (b *eventBus) broadcast(userID int64, kind string, data any) {
	msg := eventMsg{Kind: kind, Data: data}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, c := range b.clients {
		if c.userID != userID {
			continue
		}
		select {
		case c.ch <- msg:
		default:
			// Slow consumer — drop. A heartbeat or reconnect will recover state.
		}
	}
}

// EventListener lets a host process (Wails app, tests) subscribe to every
// broadcast event in addition to the SSE fan-out. The userID is included so
// listeners can branch on tenant, though in practice the Wails desktop app
// only ever sees DefaultUserID.
type EventListener func(userID int64, kind string, data map[string]any)

var externalListener EventListener

// SetEventListener registers a process-wide listener. Only one listener is
// supported (last one wins); pass nil to clear.
func SetEventListener(fn EventListener) {
	externalListener = fn
}

// BroadcastUserEvent is the package-level entry point for handlers that
// already know which tenant the change belongs to. Every web handler should
// route through this call so SSE subscribers in another tenant never see it.
func BroadcastUserEvent(userID int64, kind string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["at"]; !ok {
		data["at"] = time.Now().UTC().Format(time.RFC3339)
	}
	bus.broadcast(userID, kind, data)
	if externalListener != nil {
		externalListener(userID, kind, data)
	}
}

// BroadcastEvent retains the old API surface for callers that don't yet have
// a userID in scope (Wails GUI, MCP-poll fallbacks). It assumes the change
// belongs to DefaultUserID — the local single-user case.
func BroadcastEvent(kind string, data map[string]any) {
	BroadcastUserEvent(database.DefaultUserID, kind, data)
}

// --- SSE handler ---

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	InitEventBus(s.db)

	// Identify the subscriber's tenant up-front. With multi-tenant auth
	// active the middleware guarantees a user is in context; the legacy
	// auth-disabled path (Wails embed) falls back to DefaultUserID so the
	// poller still has someone to fan out to.
	var userID int64 = database.DefaultUserID
	if u, ok := auth.UserFromContext(r.Context()); ok && u != nil {
		userID = u.ID
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	cl := bus.subscribe(userID)
	defer bus.unsubscribe(cl)

	fmt.Fprintf(w, "event: hello\ndata: %s\n\n", `{"ok":true}`)
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			fmt.Fprintf(w, "event: heartbeat\ndata: {}\n\n")
			flusher.Flush()
		case msg, ok := <-cl.ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(msg.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.Kind, payload)
			flusher.Flush()
		}
	}
}

// --- Background poller ---

type tableState struct {
	maxCreated string
	maxUpdated string
	lastID     int64
}

func (b *eventBus) pollLoop(ctx context.Context) {
	if b.db == nil {
		return
	}
	tables := []struct {
		name    string
		created string
		updated string
		idCol   string
	}{
		{"time_entries", "created_at", "created_at", "id"},
		{"invoices", "created_at", "created_at", "id"},
		{"clients", "created_at", "updated_at", "id"},
		{"contracts", "created_at", "updated_at", "id"},
	}
	state := make(map[string]*tableState, len(tables))
	for _, t := range tables {
		s := &tableState{}
		row := b.db.QueryRowContext(ctx, fmt.Sprintf(
			"SELECT COALESCE(MAX(%s), ''), COALESCE(MAX(%s), '') FROM %s",
			t.created, t.updated, t.name))
		_ = row.Scan(&s.maxCreated, &s.maxUpdated)
		state[t.name] = s
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.mu.RLock()
			n := len(b.clients)
			b.mu.RUnlock()
			if n == 0 && externalListener == nil {
				continue
			}
			b.checkTimeEntries(ctx, state["time_entries"])
			b.checkInvoices(ctx, state["invoices"])
			b.checkClients(ctx, state["clients"])
			b.checkContracts(ctx, state["contracts"])
		}
	}
}

func safeTime(s string) string {
	if s == "" {
		return "1970-01-01"
	}
	return s
}

func (b *eventBus) checkTimeEntries(ctx context.Context, st *tableState) {
	rows, err := b.db.QueryContext(ctx, `
		SELECT te.id, COALESCE(te.user_id, 1), te.contract_id, te.date, te.hours, COALESCE(te.description, ''),
		       COALESCE(te.created_at, ''),
		       COALESCE(c.contract_number, ''), COALESCE(c.hourly_rate, 0),
		       COALESCE(cl.id, 0), COALESCE(cl.name, '')
		FROM time_entries te
		LEFT JOIN contracts c ON c.id = te.contract_id
		LEFT JOIN clients cl ON cl.id = c.client_id
		WHERE te.created_at > ?
		ORDER BY te.created_at ASC
		LIMIT 50`, safeTime(st.maxCreated))
	if err != nil {
		log.Printf("events: time_entries poll: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, createdAt, descr, contractNumber, clientName string
			contractID, clientID, userID                     int64
			date                                             time.Time
			hours, rate                                      float64
		)
		if err := rows.Scan(&id, &userID, &contractID, &date, &hours, &descr,
			&createdAt, &contractNumber, &rate, &clientID, &clientName); err != nil {
			continue
		}
		if createdAt > st.maxCreated {
			st.maxCreated = createdAt
		}
		BroadcastUserEvent(userID, "time_entry.created", map[string]any{
			"source":          "mcp",
			"id":              id,
			"contract_id":     contractID,
			"contract_number": contractNumber,
			"client_id":       clientID,
			"client_name":     clientName,
			"date":            date.Format("2006-01-02"),
			"hours":           hours,
			"amount":          hours * rate,
			"description":     descr,
			"summary":         fmt.Sprintf("%.2fh logged (%s)", hours, contractNumber),
		})
	}
}

// invoiceStatusKey scopes the in-memory snapshot map by tenant so two users
// with overlapping invoice ids (autoincrement still gives them distinct IDs
// in practice but better safe than sorry) don't pollute each other's diff.
type invoiceStatusKey struct {
	userID int64
	id     int64
}

// invoiceStatusSnap tracks the last-seen status per (user, invoice) pair so
// the poller can synthesise invoice.updated events even though invoices have
// no updated_at column.
var invoiceStatusSnap = make(map[invoiceStatusKey]string)

func (b *eventBus) checkInvoices(ctx context.Context, st *tableState) {
	// Creations
	rows, err := b.db.QueryContext(ctx, `
		SELECT i.id, COALESCE(i.user_id, 1), i.invoice_number, i.status, COALESCE(i.total_amount, 0),
		       COALESCE(i.created_at, ''), COALESCE(cl.id, 0), COALESCE(cl.name, '')
		FROM invoices i
		LEFT JOIN clients cl ON cl.id = i.client_id
		WHERE i.created_at > ?
		ORDER BY i.created_at ASC
		LIMIT 50`, safeTime(st.maxCreated))
	if err != nil {
		log.Printf("events: invoices create poll: %v", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var (
				id, userID                int64
				invNum, status, createdAt string
				total                     float64
				clientID                  int64
				clientName                string
			)
			if err := rows.Scan(&id, &userID, &invNum, &status, &total, &createdAt, &clientID, &clientName); err != nil {
				continue
			}
			if createdAt > st.maxCreated {
				st.maxCreated = createdAt
			}
			invoiceStatusSnap[invoiceStatusKey{userID: userID, id: id}] = status
			BroadcastUserEvent(userID, "invoice.created", map[string]any{
				"source":         "mcp",
				"id":             id,
				"invoice_number": invNum,
				"status":         status,
				"total_amount":   total,
				"client_id":      clientID,
				"client_name":    clientName,
			})
		}
	}

	// Status changes
	r2, err := b.db.QueryContext(ctx, `
		SELECT i.id, COALESCE(i.user_id, 1), i.invoice_number, i.status, COALESCE(i.total_amount, 0),
		       COALESCE(cl.id, 0), COALESCE(cl.name, '')
		FROM invoices i
		LEFT JOIN clients cl ON cl.id = i.client_id
		LIMIT 2000`)
	if err != nil {
		log.Printf("events: invoices status poll: %v", err)
		return
	}
	defer r2.Close()
	seen := make(map[invoiceStatusKey]struct{})
	for r2.Next() {
		var (
			id, userID     int64
			invNum, status string
			total          float64
			clientID       int64
			clientName     string
		)
		if err := r2.Scan(&id, &userID, &invNum, &status, &total, &clientID, &clientName); err != nil {
			continue
		}
		key := invoiceStatusKey{userID: userID, id: id}
		seen[key] = struct{}{}
		prev, had := invoiceStatusSnap[key]
		invoiceStatusSnap[key] = status
		if had && prev != status {
			BroadcastUserEvent(userID, "invoice.updated", map[string]any{
				"source":         "mcp",
				"id":             id,
				"invoice_number": invNum,
				"status":         status,
				"prev_status":    prev,
				"total_amount":   total,
				"client_id":      clientID,
				"client_name":    clientName,
			})
		}
	}
	for k := range invoiceStatusSnap {
		if _, ok := seen[k]; !ok {
			delete(invoiceStatusSnap, k)
		}
	}
}

func (b *eventBus) checkClients(ctx context.Context, st *tableState) {
	rows, err := b.db.QueryContext(ctx, `
		SELECT id, COALESCE(user_id, 1), name, COALESCE(created_at, ''), COALESCE(updated_at, created_at, '')
		FROM clients
		WHERE created_at > ? OR COALESCE(updated_at, created_at, '') > ?
		LIMIT 50`, safeTime(st.maxCreated), safeTime(st.maxUpdated))
	if err != nil {
		log.Printf("events: clients poll: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, userID int64
		var name, created, upd string
		if err := rows.Scan(&id, &userID, &name, &created, &upd); err != nil {
			continue
		}
		kind := "client.updated"
		if created > st.maxCreated {
			kind = "client.created"
			st.maxCreated = created
		}
		if upd > st.maxUpdated {
			st.maxUpdated = upd
		}
		BroadcastUserEvent(userID, kind, map[string]any{"source": "mcp", "id": id, "name": name})
	}
}

func (b *eventBus) checkContracts(ctx context.Context, st *tableState) {
	rows, err := b.db.QueryContext(ctx, `
		SELECT id, COALESCE(user_id, 1), contract_number, COALESCE(name, ''), COALESCE(created_at, ''), COALESCE(updated_at, created_at, '')
		FROM contracts
		WHERE created_at > ? OR COALESCE(updated_at, created_at, '') > ?
		LIMIT 50`, safeTime(st.maxCreated), safeTime(st.maxUpdated))
	if err != nil {
		log.Printf("events: contracts poll: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, userID int64
		var num, name, created, upd string
		if err := rows.Scan(&id, &userID, &num, &name, &created, &upd); err != nil {
			continue
		}
		kind := "contract.updated"
		if created > st.maxCreated {
			kind = "contract.created"
			st.maxCreated = created
		}
		if upd > st.maxUpdated {
			st.maxUpdated = upd
		}
		BroadcastUserEvent(userID, kind, map[string]any{"source": "mcp", "id": id, "number": num, "name": name})
	}
}
