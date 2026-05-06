package api

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/austin/hours-mcp/internal/timeparse"
	"github.com/google/uuid"
)

// expenseRow is the API shape — adds derived fields the UI needs.
type expenseRow struct {
	ID            string  `json:"id"`
	ClientID      int     `json:"client_id"`
	ClientName    string  `json:"client_name"`
	ContractID    *int    `json:"contract_id,omitempty"`
	ContractNumber string `json:"contract_number,omitempty"`
	Date          string  `json:"date"`
	Description   string  `json:"description"`
	Amount        float64 `json:"amount"`
	Currency      string  `json:"currency"`
	Category      string  `json:"category,omitempty"`
	ReceiptPath   string  `json:"receipt_path,omitempty"`
	InvoiceID     *int    `json:"invoice_id,omitempty"`
	InvoiceNumber string  `json:"invoice_number,omitempty"`
	CreatedAt     string  `json:"created_at"`
}

func (h *handlers) listExpenses(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	clauses := []string{"e.user_id = ?"}
	args := []interface{}{userID}

	if v := q.Get("client_id"); v != "" {
		clauses = append(clauses, "e.client_id = ?")
		args = append(args, v)
	}
	if v := q.Get("invoiced"); v == "true" {
		clauses = append(clauses, "e.invoice_id IS NOT NULL")
	} else if v == "false" {
		clauses = append(clauses, "e.invoice_id IS NULL")
	}
	if v := q.Get("start_date"); v != "" {
		clauses = append(clauses, "e.date >= ?")
		args = append(args, v)
	}
	if v := q.Get("end_date"); v != "" {
		clauses = append(clauses, "e.date <= ?")
		args = append(args, v)
	}
	if v := q.Get("category"); v != "" {
		clauses = append(clauses, "LOWER(e.category) = LOWER(?)")
		args = append(args, v)
	}

	rows, err := h.db.Query(`
		SELECT e.id, e.client_id, c.name, e.contract_id, COALESCE(ct.contract_number,''),
		       e.date, e.description, e.amount, e.currency,
		       COALESCE(e.category,''), COALESCE(e.receipt_path,''),
		       e.invoice_id, COALESCE(i.invoice_number,''), e.created_at
		FROM expenses e
		JOIN clients c ON c.id = e.client_id
		LEFT JOIN contracts ct ON ct.id = e.contract_id
		LEFT JOIN invoices i ON i.id = e.invoice_id
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY e.date DESC, e.created_at DESC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []expenseRow{}
	for rows.Next() {
		var e expenseRow
		var date, created time.Time
		if err := rows.Scan(&e.ID, &e.ClientID, &e.ClientName, &e.ContractID, &e.ContractNumber,
			&date, &e.Description, &e.Amount, &e.Currency, &e.Category, &e.ReceiptPath,
			&e.InvoiceID, &e.InvoiceNumber, &created); err != nil {
			return nil, err
		}
		e.Date = date.Format("2006-01-02")
		e.CreatedAt = created.Format(time.RFC3339)
		out = append(out, e)
	}
	return out, nil
}

type expenseReq struct {
	ClientID       int     `json:"client_id"`
	ContractNumber string  `json:"contract_number,omitempty"`
	ContractID     *int    `json:"contract_id,omitempty"`
	Date           string  `json:"date"`
	Description    string  `json:"description"`
	Amount         float64 `json:"amount"`
	Currency       string  `json:"currency,omitempty"`
	Category       string  `json:"category,omitempty"`
	ReceiptPath    string  `json:"receipt_path,omitempty"`
}

func (h *handlers) addExpense(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var req expenseReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if req.ClientID == 0 {
		return nil, newAPIError(http.StatusBadRequest, "client_id required")
	}
	if _, err := h.requireClient(userID, req.ClientID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Description) == "" {
		return nil, newAPIError(http.StatusBadRequest, "description required")
	}
	if req.Amount <= 0 {
		return nil, newAPIError(http.StatusBadRequest, "amount must be greater than 0")
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}

	date, err := timeparse.ParseDate(req.Date)
	if err != nil {
		return nil, newAPIError(http.StatusBadRequest, "invalid date: %v", err)
	}

	contractID := req.ContractID
	if contractID == nil && req.ContractNumber != "" {
		var cid int
		if err := h.db.QueryRow(`SELECT id FROM contracts WHERE contract_number = ? AND client_id = ? AND user_id = ?`,
			req.ContractNumber, req.ClientID, userID).Scan(&cid); err == sql.ErrNoRows {
			return nil, newAPIError(http.StatusBadRequest, "contract %s not found for client", req.ContractNumber)
		} else if err != nil {
			return nil, err
		}
		contractID = &cid
	}

	id := uuid.New().String()
	_, err = h.db.Exec(`
		INSERT INTO expenses (id, user_id, client_id, contract_id, date, description, amount, currency, category, receipt_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, userID, req.ClientID, contractID, date.Format("2006-01-02"), req.Description, req.Amount,
		req.Currency, nullStr(req.Category), nullStr(req.ReceiptPath))
	if err != nil {
		return nil, err
	}

	BroadcastUserEvent(userID, "expense.created", map[string]any{"id": id, "client_id": req.ClientID})
	return map[string]interface{}{"id": id}, nil
}

func (h *handlers) updateExpense(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id := r.PathValue("id")
	if id == "" {
		return nil, newAPIError(http.StatusBadRequest, "id required")
	}

	var invoiceID sql.NullInt64
	if err := h.db.QueryRow(`SELECT invoice_id FROM expenses WHERE id = ? AND user_id = ?`, id, userID).Scan(&invoiceID); err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "expense not found")
	} else if err != nil {
		return nil, err
	}
	if invoiceID.Valid {
		return nil, newAPIError(http.StatusConflict, "cannot edit invoiced expense")
	}

	var req expenseReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}

	sets := []string{}
	args := []interface{}{}
	if req.Date != "" {
		date, err := timeparse.ParseDate(req.Date)
		if err != nil {
			return nil, newAPIError(http.StatusBadRequest, "invalid date: %v", err)
		}
		sets = append(sets, "date = ?")
		args = append(args, date.Format("2006-01-02"))
	}
	if req.Description != "" {
		sets = append(sets, "description = ?")
		args = append(args, req.Description)
	}
	if req.Amount > 0 {
		sets = append(sets, "amount = ?")
		args = append(args, req.Amount)
	}
	if req.Currency != "" {
		sets = append(sets, "currency = ?")
		args = append(args, req.Currency)
	}
	if req.Category != "" {
		sets = append(sets, "category = ?")
		args = append(args, req.Category)
	}
	if req.ReceiptPath != "" {
		sets = append(sets, "receipt_path = ?")
		args = append(args, req.ReceiptPath)
	}
	if len(sets) == 0 {
		return nil, newAPIError(http.StatusBadRequest, "no fields to update")
	}
	args = append(args, id, userID)
	if _, err := h.db.Exec(`UPDATE expenses SET `+strings.Join(sets, ", ")+` WHERE id = ? AND user_id = ?`, args...); err != nil {
		return nil, err
	}

	BroadcastUserEvent(userID, "expense.updated", map[string]any{"id": id})
	return map[string]interface{}{"id": id}, nil
}

func (h *handlers) deleteExpense(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id := r.PathValue("id")
	if id == "" {
		return nil, newAPIError(http.StatusBadRequest, "id required")
	}

	var invoiceID sql.NullInt64
	if err := h.db.QueryRow(`SELECT invoice_id FROM expenses WHERE id = ? AND user_id = ?`, id, userID).Scan(&invoiceID); err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "expense not found")
	} else if err != nil {
		return nil, err
	}
	if invoiceID.Valid {
		return nil, newAPIError(http.StatusConflict, "cannot delete invoiced expense")
	}

	if _, err := h.db.Exec(`DELETE FROM expenses WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return nil, err
	}

	BroadcastUserEvent(userID, "expense.deleted", map[string]any{"id": id})
	return map[string]interface{}{"deleted": id}, nil
}

func nullStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
