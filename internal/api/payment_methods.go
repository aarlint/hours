package api

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/austin/hours-mcp/internal/models"
)

// ---------- Payment Methods (business-level) ----------

func (h *handlers) listPaymentMethods(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	rows, err := h.db.Query(`
		SELECT id, label, COALESCE(bank_name,''), COALESCE(account_number,''),
		       COALESCE(routing_number,''), COALESCE(swift_code,''),
		       COALESCE(payment_terms,''), COALESCE(notes,''),
		       COALESCE(is_default, 0), created_at, updated_at
		FROM payment_methods
		WHERE user_id = ?
		ORDER BY is_default DESC, label
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.PaymentMethod{}
	for rows.Next() {
		var pm models.PaymentMethod
		if err := rows.Scan(&pm.ID, &pm.Label, &pm.BankName, &pm.AccountNumber,
			&pm.RoutingNumber, &pm.SwiftCode, &pm.PaymentTerms, &pm.Notes,
			&pm.IsDefault, &pm.CreatedAt, &pm.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, pm)
	}
	return out, nil
}

type paymentMethodReq struct {
	Label         string `json:"label"`
	BankName      string `json:"bank_name,omitempty"`
	AccountNumber string `json:"account_number,omitempty"`
	RoutingNumber string `json:"routing_number,omitempty"`
	SwiftCode     string `json:"swift_code,omitempty"`
	PaymentTerms  string `json:"payment_terms,omitempty"`
	Notes         string `json:"notes,omitempty"`
	IsDefault     bool   `json:"is_default,omitempty"`
}

func (h *handlers) addPaymentMethod(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var req paymentMethodReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Label) == "" {
		return nil, newAPIError(http.StatusBadRequest, "label required")
	}

	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if req.IsDefault {
		if _, err := tx.Exec(`UPDATE payment_methods SET is_default = 0 WHERE user_id = ?`, userID); err != nil {
			return nil, err
		}
	}

	var id int64
	err = tx.QueryRow(`
		INSERT INTO payment_methods (user_id, label, bank_name, account_number, routing_number,
		                             swift_code, payment_terms, notes, is_default, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, userID, req.Label, req.BankName, req.AccountNumber, req.RoutingNumber, req.SwiftCode,
		req.PaymentTerms, req.Notes, req.IsDefault, time.Now()).Scan(&id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	BroadcastUserEvent(userID, "payment_method.created", map[string]any{"id": id, "label": req.Label})
	return map[string]interface{}{"id": id, "label": req.Label}, nil
}

func (h *handlers) updatePaymentMethod(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}
	var req paymentMethodReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Label) == "" {
		return nil, newAPIError(http.StatusBadRequest, "label required")
	}

	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if req.IsDefault {
		if _, err := tx.Exec(`UPDATE payment_methods SET is_default = 0 WHERE user_id = ? AND id <> ?`, userID, id); err != nil {
			return nil, err
		}
	}

	res, err := tx.Exec(`
		UPDATE payment_methods
		SET label = ?, bank_name = ?, account_number = ?, routing_number = ?,
		    swift_code = ?, payment_terms = ?, notes = ?, is_default = ?,
		    updated_at = ?
		WHERE id = ? AND user_id = ?
	`, req.Label, req.BankName, req.AccountNumber, req.RoutingNumber, req.SwiftCode,
		req.PaymentTerms, req.Notes, req.IsDefault, time.Now(), id, userID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, newAPIError(http.StatusNotFound, "payment method not found")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	BroadcastUserEvent(userID, "payment_method.updated", map[string]any{"id": id, "label": req.Label})
	return map[string]interface{}{"id": id}, nil
}

func (h *handlers) deletePaymentMethod(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}

	// Make sure the method we're about to delete belongs to this user.
	var label string
	err = h.db.QueryRow(`SELECT label FROM payment_methods WHERE id = ? AND user_id = ?`, id, userID).Scan(&label)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "payment method not found")
	}
	if err != nil {
		return nil, err
	}

	// Count how many contracts currently point at this method so the
	// caller can decide what to do. We clear the pointer on those rows
	// rather than blocking the delete — contracts are long-lived and we
	// don't want stale FKs to prevent cleaning up bad records.
	var attachedContracts, attachedInvoices int
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM contracts WHERE payment_method_id = ? AND user_id = ?`, id, userID).Scan(&attachedContracts)
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM invoices  WHERE payment_method_id = ? AND user_id = ?`, id, userID).Scan(&attachedInvoices)

	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE contracts SET payment_method_id = NULL WHERE payment_method_id = ? AND user_id = ?`, id, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE invoices  SET payment_method_id = NULL WHERE payment_method_id = ? AND user_id = ?`, id, userID); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`DELETE FROM payment_methods WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	BroadcastUserEvent(userID, "payment_method.deleted", map[string]any{
		"id":                 id,
		"label":              label,
		"detached_contracts": attachedContracts,
		"detached_invoices":  attachedInvoices,
	})
	return map[string]interface{}{
		"deleted":            id,
		"label":              label,
		"detached_contracts": attachedContracts,
		"detached_invoices":  attachedInvoices,
	}, nil
}

// ResolveInvoicePaymentDetails returns payment info for PDF generation. It
// prefers the invoice's snapshotted payment_method_id -> payment_methods, and
// falls back to the legacy per-client payment_details table when the invoice
// has no attached method. Returns an empty struct when nothing is configured
// (the PDF layer renders an empty payment block in that case rather than
// refusing to produce the invoice). Exported so the MCP surface in
// internal/server can use the same resolution logic.
//
// userID is required for tenant isolation: every lookup also re-checks that
// the invoice / payment method / client row belongs to the caller. This is
// defense-in-depth — most callers have already authorised the
// (invoiceID, clientID) pair, but a future bug shouldn't be able to leak
// another tenant's banking info via a stale identifier.
func ResolveInvoicePaymentDetails(db *sql.DB, userID int64, invoiceID int64, clientID int) models.PaymentDetails {
	var pd models.PaymentDetails

	// Try snapshotted method first. We re-check user_id at every step so an
	// attacker-supplied invoice ID can't dereference another tenant's row.
	var methodID sql.NullInt64
	if invoiceID > 0 {
		_ = db.QueryRow(
			`SELECT payment_method_id FROM invoices WHERE id = ? AND user_id = ?`,
			invoiceID, userID,
		).Scan(&methodID)
	}
	if methodID.Valid {
		err := db.QueryRow(`
			SELECT COALESCE(bank_name,''), COALESCE(account_number,''),
			       COALESCE(routing_number,''), COALESCE(swift_code,''),
			       COALESCE(payment_terms,''), COALESCE(notes,'')
			FROM payment_methods WHERE id = ? AND user_id = ?
		`, methodID.Int64, userID).Scan(&pd.BankName, &pd.AccountNumber, &pd.RoutingNumber,
			&pd.SwiftCode, &pd.PaymentTerms, &pd.Notes)
		if err == nil {
			return pd
		}
	}

	// Fallback to legacy per-client record. payment_details has no direct
	// user_id column, so scope through the client.
	_ = db.QueryRow(`
		SELECT COALESCE(bank_name,''), COALESCE(account_number,''),
		       COALESCE(routing_number,''), COALESCE(swift_code,''),
		       COALESCE(payment_terms,''), COALESCE(notes,'')
		FROM payment_details
		WHERE client_id = ?
		  AND client_id IN (SELECT id FROM clients WHERE user_id = ?)
	`, clientID, userID).Scan(&pd.BankName, &pd.AccountNumber, &pd.RoutingNumber,
		&pd.SwiftCode, &pd.PaymentTerms, &pd.Notes)
	return pd
}

// resolveContractPaymentMethodID returns the payment_method_id snapshot a new
// invoice should record for the given client. It picks the payment method from
// the client's first matching contract, preferring active contracts with the
// method set. Returns NULL (sql.NullInt64{}) when no contract has a method
// attached — the invoice will then fall back to per-client legacy data.
func resolveContractPaymentMethodID(db *sql.DB, userID int64, clientID int) sql.NullInt64 {
	var methodID sql.NullInt64
	_ = db.QueryRow(`
		SELECT payment_method_id FROM contracts
		WHERE user_id = ? AND client_id = ? AND payment_method_id IS NOT NULL
		ORDER BY (status = 'active') DESC, id
		LIMIT 1
	`, userID, clientID).Scan(&methodID)
	return methodID
}
