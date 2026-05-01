package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// exportData walks every row of every business-level table and dumps it to
// the response as a single JSON document. The shape is intentionally
// table-keyed (not nested by client) so the importer can stream it back
// without rewriting foreign keys.
//
// Schema/migrations and sessions are excluded — the importer recreates the
// schema via runMigrations on startup, and sessions/users belong to the
// host instance, not the data being moved.
func (h *handlers) exportData(w http.ResponseWriter, r *http.Request) {
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
		rows, err := dumpTable(h.db, t)
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

// dumpTable returns []map[col]value for every row in the table, preserving
// SQLite's column order. Skips silently if the table doesn't exist (legacy
// payment_details after migration, etc.).
func dumpTable(db *sql.DB, table string) ([]map[string]any, error) {
	if !tableExists(db, table) {
		return []map[string]any{}, nil
	}
	rows, err := db.Query(fmt.Sprintf(`SELECT * FROM %q`, table))
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

// importData wipes the current data tables and replaces them from the
// uploaded JSON. This is destructive and irreversible — gated to the admin
// role when auth is enabled. We run inside a single transaction so a partial
// import doesn't leave the DB half-overwritten.
func (h *handlers) importData(w http.ResponseWriter, r *http.Request) {
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
		if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %q`, t)); err != nil {
			writeError(w, http.StatusInternalServerError,
				fmt.Errorf("wipe %s: %w", t, err))
			return
		}
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
		n, err := insertRows(tx, t, rows)
		if err != nil {
			writeError(w, http.StatusBadRequest,
				fmt.Errorf("insert %s: %w", t, err))
			return
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
	// Build column list from the union of keys across the first row. We
	// trust the export came from the same schema and stick with a single
	// stmt per row to keep the code simple.
	for _, row := range rows {
		cols := make([]string, 0, len(row))
		for k := range row {
			cols = append(cols, k)
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
