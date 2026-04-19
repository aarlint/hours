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
)

// EventBus broadcasts SSE events to all connected clients. It also runs a
// background poller against the SQLite DB so that changes made by a separate
// process (e.g. the MCP stdio binary) are detected and pushed to the web UI.

type eventMsg struct {
	Kind string
	Data any
}

type client struct {
	id int64
	ch chan eventMsg
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

func (b *eventBus) subscribe() *client {
	id := b.nextID.Add(1)
	c := &client{id: id, ch: make(chan eventMsg, 64)}
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

func (b *eventBus) broadcast(kind string, data any) {
	msg := eventMsg{Kind: kind, Data: data}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, c := range b.clients {
		select {
		case c.ch <- msg:
		default:
			// Slow consumer — drop. A heartbeat or reconnect will recover state.
		}
	}
}

// EventListener lets a host process (Wails app, tests) subscribe to every
// broadcast event in addition to the SSE fan-out.
type EventListener func(kind string, data map[string]any)

var externalListener EventListener

// SetEventListener registers a process-wide listener. Only one listener is
// supported (last one wins); pass nil to clear.
func SetEventListener(fn EventListener) {
	externalListener = fn
}

// BroadcastEvent is the package-level entry point for handlers that want to
// push a typed event to connected UIs.
func BroadcastEvent(kind string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["at"]; !ok {
		data["at"] = time.Now().UTC().Format(time.RFC3339)
	}
	bus.broadcast(kind, data)
	if externalListener != nil {
		externalListener(kind, data)
	}
}

// --- SSE handler ---

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	InitEventBus(s.db)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	cl := bus.subscribe()
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
		SELECT te.id, te.contract_id, te.date, te.hours, COALESCE(te.description, ''),
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
			contractID, clientID                             int64
			date                                             time.Time
			hours, rate                                      float64
		)
		if err := rows.Scan(&id, &contractID, &date, &hours, &descr,
			&createdAt, &contractNumber, &rate, &clientID, &clientName); err != nil {
			continue
		}
		if createdAt > st.maxCreated {
			st.maxCreated = createdAt
		}
		BroadcastEvent("time_entry.created", map[string]any{
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

// Invoices table has no updated_at, so we track status changes ourselves via an
// in-memory snapshot map keyed by invoice id.
var invoiceStatusSnap = make(map[int64]string)

func (b *eventBus) checkInvoices(ctx context.Context, st *tableState) {
	// Creations
	rows, err := b.db.QueryContext(ctx, `
		SELECT i.id, i.invoice_number, i.status, COALESCE(i.total_amount, 0),
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
				id                        int64
				invNum, status, createdAt string
				total                     float64
				clientID                  int64
				clientName                string
			)
			if err := rows.Scan(&id, &invNum, &status, &total, &createdAt, &clientID, &clientName); err != nil {
				continue
			}
			if createdAt > st.maxCreated {
				st.maxCreated = createdAt
			}
			invoiceStatusSnap[id] = status
			BroadcastEvent("invoice.created", map[string]any{
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
		SELECT i.id, i.invoice_number, i.status, COALESCE(i.total_amount, 0),
		       COALESCE(cl.id, 0), COALESCE(cl.name, '')
		FROM invoices i
		LEFT JOIN clients cl ON cl.id = i.client_id
		LIMIT 2000`)
	if err != nil {
		log.Printf("events: invoices status poll: %v", err)
		return
	}
	defer r2.Close()
	seen := make(map[int64]struct{})
	for r2.Next() {
		var (
			id             int64
			invNum, status string
			total          float64
			clientID       int64
			clientName     string
		)
		if err := r2.Scan(&id, &invNum, &status, &total, &clientID, &clientName); err != nil {
			continue
		}
		seen[id] = struct{}{}
		prev, had := invoiceStatusSnap[id]
		invoiceStatusSnap[id] = status
		if had && prev != status {
			BroadcastEvent("invoice.updated", map[string]any{
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
	for id := range invoiceStatusSnap {
		if _, ok := seen[id]; !ok {
			delete(invoiceStatusSnap, id)
		}
	}
}

func (b *eventBus) checkClients(ctx context.Context, st *tableState) {
	rows, err := b.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(created_at, ''), COALESCE(updated_at, created_at, '')
		FROM clients
		WHERE created_at > ? OR COALESCE(updated_at, created_at, '') > ?
		LIMIT 50`, safeTime(st.maxCreated), safeTime(st.maxUpdated))
	if err != nil {
		log.Printf("events: clients poll: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, created, upd string
		if err := rows.Scan(&id, &name, &created, &upd); err != nil {
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
		BroadcastEvent(kind, map[string]any{"source": "mcp", "id": id, "name": name})
	}
}

func (b *eventBus) checkContracts(ctx context.Context, st *tableState) {
	rows, err := b.db.QueryContext(ctx, `
		SELECT id, contract_number, COALESCE(name, ''), COALESCE(created_at, ''), COALESCE(updated_at, created_at, '')
		FROM contracts
		WHERE created_at > ? OR COALESCE(updated_at, created_at, '') > ?
		LIMIT 50`, safeTime(st.maxCreated), safeTime(st.maxUpdated))
	if err != nil {
		log.Printf("events: contracts poll: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var num, name, created, upd string
		if err := rows.Scan(&id, &num, &name, &created, &upd); err != nil {
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
		BroadcastEvent(kind, map[string]any{"source": "mcp", "id": id, "number": num, "name": name})
	}
}
