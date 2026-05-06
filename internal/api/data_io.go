package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// userScopedTables maps each business-level table to the WHERE clause that
// scopes it to a single user. Most tables carry user_id directly; recipients
// and quote_line_items reach through to a parent row.
var userScopedTables = map[string]string{
	"business_info":    "user_id = ?",
	"clients":          "user_id = ?",
	"recipients":       "client_id IN (SELECT id FROM clients WHERE user_id = ?)",
	"payment_methods":  "user_id = ?",
	"payment_details":  "client_id IN (SELECT id FROM clients WHERE user_id = ?)",
	"contracts":        "user_id = ?",
	"quotes":           "user_id = ?",
	"quote_line_items": "quote_id IN (SELECT id FROM quotes WHERE user_id = ?)",
	"invoices":         "user_id = ?",
	"time_entries":     "user_id = ?",
	"expenses":         "user_id = ?",
}

// exportData walks every row of every business-level table belonging to the
// authenticated user and dumps it to the response as a single JSON document.
// Multi-tenant: the export only contains the caller's own rows.
//
// Schema/migrations and sessions are excluded — the importer recreates the
// schema via runMigrations on startup, and sessions/users belong to the
// host instance, not the data being moved.
func (h *handlers) exportData(w http.ResponseWriter, r *http.Request) {
	userID, err := currentUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	out := map[string]any{
		"version":     1,
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"tables":      map[string]any{},
	}
	tables := []string{
		"business_info",
		"clients",
		"recipients",
		"payment_methods",
		"payment_details",
		"contracts",
		"quotes",
		"quote_line_items",
		"invoices",
		"time_entries",
		"expenses",
	}
	tableOut := out["tables"].(map[string]any)
	for _, t := range tables {
		rows, err := dumpTableForUser(h.db, t, userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError,
				fmt.Errorf("dump %s: %w", t, err))
			return
		}
		tableOut[t] = rows
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(
		"Content-Disposition",
		fmt.Sprintf(`attachment; filename="hours-export-%s.json"`,
			time.Now().Format("20060102-150405")),
	)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		// Headers are already sent — best we can do is log via the wrapper.
		return
	}
}

// dumpTableForUser returns []map[col]value for every row in the table that
// belongs to the given user, preserving SQLite's column order. Skips silently
// if the table doesn't exist (legacy payment_details after migration, etc.).
func dumpTableForUser(db *sql.DB, table string, userID int64) ([]map[string]any, error) {
	if !tableExists(db, table) {
		return []map[string]any{}, nil
	}
	whereClause, ok := userScopedTables[table]
	if !ok {
		// Defensive: tables we haven't whitelisted are excluded entirely.
		return []map[string]any{}, nil
	}
	q := fmt.Sprintf(`SELECT * FROM %q WHERE %s`, table, whereClause)
	rows, err := db.Query(q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, 64)
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			v := vals[i]
			// SQLite returns []byte for TEXT — coerce so the JSON is readable.
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			row[c] = v
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func tableExists(db *sql.DB, name string) bool {
	var n string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&n)
	return err == nil
}

// importPayload mirrors the export shape. The importer is permissive about
// which tables are present so a partial export can still be loaded.
type importPayload struct {
	Version int                         `json:"version"`
	Tables  map[string][]map[string]any `json:"tables"`
}

// importData wipes the *current user's* data tables and replaces them from
// the uploaded JSON. This is destructive and irreversible — gated to the
// admin role when auth is enabled. We run inside a single transaction so a
// partial import doesn't leave the DB half-overwritten.
//
// Multi-tenant: any user_id field present in the payload is ignored — every
// inserted row is forcibly stamped with the caller's user_id so an admin
// can't accidentally import data into another tenant. Wipe is similarly
// scoped — other tenants' rows remain untouched.
func (h *handlers) importData(w http.ResponseWriter, r *http.Request) {
	userID, err := currentUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	defer r.Body.Close()
	var payload importPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}
	if payload.Version == 0 || len(payload.Tables) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("missing version or tables"))
		return
	}

	// Order matters because of foreign keys: parents before children when
	// inserting, children before parents when wiping.
	insertOrder := []string{
		"business_info",
		"clients",
		"recipients",
		"payment_methods",
		"payment_details",
		"contracts",
		"quotes",
		"quote_line_items",
		"invoices",
		"time_entries",
		"expenses",
	}
	wipeOrder := []string{
		"expenses",
		"time_entries",
		"invoices",
		"quote_line_items",
		"quotes",
		"contracts",
		"payment_details",
		"payment_methods",
		"recipients",
		"clients",
		"business_info",
	}

	tx, err := h.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	// Disable FK enforcement while we rebuild — sqlite checks them
	// transactionally but only at COMMIT for deferred FKs we don't have.
	if _, err := tx.Exec(`PRAGMA defer_foreign_keys = ON`); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, t := range wipeOrder {
		if !tableExists(h.db, t) {
			continue
		}
		whereClause, ok := userScopedTables[t]
		if !ok {
			continue
		}
		q := fmt.Sprintf(`DELETE FROM %q WHERE %s`, t, whereClause)
		if _, err := tx.Exec(q, userID); err != nil {
			writeError(w, http.StatusInternalServerError,
				fmt.Errorf("wipe %s: %w", t, err))
			return
		}
	}

	// Tables whose rows carry a direct user_id column — we forcibly stamp
	// the caller's user_id onto each row regardless of what's in the JSON.
	directUserIDTables := map[string]bool{
		"business_info":   true,
		"clients":         true,
		"payment_methods": true,
		"contracts":       true,
		"quotes":          true,
		"invoices":        true,
		"time_entries":    true,
		"expenses":        true,
	}

	// FK-scoped child tables don't carry a user_id column themselves — they
	// inherit ownership through a parent. Without an explicit ownership
	// check, an attacker who knew a victim's parent ID could attach child
	// rows to it through this importer (the FK constraint passes because
	// the parent really exists). We defend by tracking which parent IDs
	// were inserted in *this* transaction and rejecting any child row whose
	// FK doesn't appear in that set.
	//
	// Map: parent table -> set of inserted IDs (after the import stamp).
	insertedParents := map[string]map[int64]bool{
		"clients": {},
		"quotes":  {},
	}
	// Child table -> (FK column, parent table)
	childFKs := map[string]struct {
		fkCol  string
		parent string
	}{
		"recipients":       {"client_id", "clients"},
		"payment_details":  {"client_id", "clients"},
		"quote_line_items": {"quote_id", "quotes"},
	}

	stats := map[string]int{}
	for _, t := range insertOrder {
		rows, ok := payload.Tables[t]
		if !ok || len(rows) == 0 {
			continue
		}
		if !tableExists(h.db, t) {
			continue
		}
		stamped := rows
		if directUserIDTables[t] {
			stamped = make([]map[string]any, len(rows))
			for i, r := range rows {
				cp := make(map[string]any, len(r))
				for k, v := range r {
					cp[k] = v
				}
				cp["user_id"] = userID
				stamped[i] = cp
			}
		}
		// Reject FK-scoped child rows whose parent isn't part of THIS import.
		// Without this check, importing with a hand-crafted client_id pointing
		// at another tenant's row would attach the recipient to that tenant.
		if fk, isChild := childFKs[t]; isChild {
			parentSet := insertedParents[fk.parent]
			for _, row := range stamped {
				raw, present := row[fk.fkCol]
				if !present || raw == nil {
					writeError(w, http.StatusBadRequest, fmt.Errorf(
						"insert %s: row missing %s", t, fk.fkCol))
					return
				}
				pid, ok := toInt64(raw)
				if !ok {
					writeError(w, http.StatusBadRequest, fmt.Errorf(
						"insert %s: %s is not an integer", t, fk.fkCol))
					return
				}
				if !parentSet[pid] {
					writeError(w, http.StatusBadRequest, fmt.Errorf(
						"%s row references %s %d which is not part of this import",
						t, fk.fkCol, pid))
					return
				}
			}
		}
		n, err := insertRows(tx, t, stamped)
		if err != nil {
			writeError(w, http.StatusBadRequest,
				fmt.Errorf("insert %s: %w", t, err))
			return
		}
		// After a successful parent insert, capture the inserted IDs so
		// children referenced later in the loop can be ownership-checked.
		// We trust the row's "id" field because directUserIDTables already
		// stamped user_id and SQLite preserves the explicit id on insert
		// (export sends the integer id; collisions vs. existing rows would
		// cause the INSERT itself to fail under the UNIQUE pk).
		if set, isParent := insertedParents[t]; isParent {
			for _, row := range stamped {
				if rawID, ok := row["id"]; ok && rawID != nil {
					if id, ok := toInt64(rawID); ok {
						set[id] = true
					}
				}
			}
		}
		stats[t] = n
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":       true,
		"imported": stats,
	})
}

func insertRows(tx *sql.Tx, table string, rows []map[string]any) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	// Look up the destination table's actual columns so we can ignore any
	// keys in the export that no longer exist (e.g. a legacy `id` field on
	// business_info from before that table was rebuilt to be keyed by
	// user_id). This makes import resilient across migrations.
	validCols, err := tableColumnSet(tx, table)
	if err != nil {
		return 0, fmt.Errorf("introspect %s: %w", table, err)
	}
	for _, row := range rows {
		cols := make([]string, 0, len(row))
		for k := range row {
			if !validCols[k] {
				continue
			}
			cols = append(cols, k)
		}
		if len(cols) == 0 {
			continue
		}
		placeholders := make([]string, len(cols))
		args := make([]any, len(cols))
		for i, c := range cols {
			placeholders[i] = "?"
			args[i] = row[c]
		}
		stmt := fmt.Sprintf(
			`INSERT INTO %q (%s) VALUES (%s)`,
			table, joinCols(cols), joinCols(placeholders),
		)
		if _, err := tx.Exec(stmt, args...); err != nil {
			return 0, fmt.Errorf("row insert: %w", err)
		}
	}
	return len(rows), nil
}

func tableColumnSet(tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var dflt *string
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// toInt64 coerces a JSON-decoded value into int64. encoding/json hands us
// float64 for numbers by default; SQLite drivers also occasionally return
// int64 directly when re-encoding via map[string]any. Returns (0, false) for
// anything else.
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

func joinCols(cols []string) string {
	out := ""
	for i, c := range cols {
		if i > 0 {
			out += ","
		}
		// Cols come from the JSON document — quote them so reserved words
		// and unusual identifiers don't blow up.
		if c != "?" {
			out += fmt.Sprintf("%q", c)
		} else {
			out += "?"
		}
	}
	return out
}
