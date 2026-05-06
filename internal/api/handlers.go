package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/austin/hours-mcp/internal/auth"
	"github.com/austin/hours-mcp/internal/models"
	"github.com/austin/hours-mcp/internal/pdf"
	"github.com/austin/hours-mcp/internal/timeparse"
	"github.com/google/uuid"
)

// currentUserID returns the authenticated user's id from the request context.
// Defense in depth: the auth middleware should already have rejected the
// request if there's no session, but every handler also calls this so that
// running unauthenticated (e.g. wrong wiring) cannot leak data.
func currentUserID(r *http.Request) (int64, error) {
	u, ok := auth.UserFromContext(r.Context())
	if !ok || u == nil {
		return 0, newAPIError(http.StatusUnauthorized, "authentication required")
	}
	return u.ID, nil
}

type handlers struct {
	db *sql.DB
}

// ---------- DTOs ----------

type clientDTO struct {
	models.Client
	ActiveContracts int `json:"active_contracts"`
}

type contractDTO struct {
	models.Contract
	ClientName string `json:"client_name"`
}

type timeEntryDTO struct {
	ID             string    `json:"id"`
	ContractID     int       `json:"contract_id"`
	ClientID       int       `json:"client_id"`
	ClientName     string    `json:"client_name"`
	ContractNumber string    `json:"contract_number"`
	ContractName   string    `json:"contract_name"`
	Date           time.Time `json:"date"`
	Hours          float64   `json:"hours"`
	Description    string    `json:"description"`
	InvoiceID      *int      `json:"invoice_id,omitempty"`
	InvoiceNumber  *string   `json:"invoice_number,omitempty"`
	HourlyRate     float64   `json:"hourly_rate"`
	Currency       string    `json:"currency"`
	Amount         float64   `json:"amount"`
	CreatedAt      time.Time `json:"created_at"`
}

type invoiceDTO struct {
	ID            int       `json:"id"`
	InvoiceNumber string    `json:"invoice_number"`
	ClientID      int       `json:"client_id"`
	ClientName    string    `json:"client_name"`
	IssueDate     time.Time `json:"issue_date"`
	DueDate       time.Time `json:"due_date"`
	TotalAmount   float64   `json:"total_amount"`
	Status        string    `json:"status"`
	PDFPath       string    `json:"pdf_path,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type statsDTO struct {
	TotalClients      int     `json:"total_clients"`
	ActiveContracts   int     `json:"active_contracts"`
	UnbilledHours     float64 `json:"unbilled_hours"`
	UnbilledAmount    float64 `json:"unbilled_amount"`
	HoursThisMonth    float64 `json:"hours_this_month"`
	HoursLastMonth    float64 `json:"hours_last_month"`
	OutstandingAmount float64 `json:"outstanding_amount"`
	PaidAmount        float64 `json:"paid_amount"`
	InvoicesPending   int     `json:"invoices_pending"`
	InvoicesPaid      int     `json:"invoices_paid"`
	RecentEntries     []timeEntryDTO `json:"recent_entries"`
}

// ---------- Helpers ----------

func (h *handlers) clientIDByName(userID int64, name string) (int, error) {
	var id int
	err := h.db.QueryRow("SELECT id FROM clients WHERE user_id = ? AND name = ?", userID, name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, newAPIError(http.StatusNotFound, "client '%s' not found", name)
	}
	return id, err
}

// requireClient confirms the client id belongs to the given user. Used by
// handlers that take :id path params for nested resources (recipients,
// payment-details) so we don't leak data via foreign-key reach-around.
func (h *handlers) requireClient(userID int64, clientID int) (string, error) {
	var name string
	err := h.db.QueryRow("SELECT name FROM clients WHERE id = ? AND user_id = ?", clientID, userID).Scan(&name)
	if err == sql.ErrNoRows {
		return "", newAPIError(http.StatusNotFound, "client not found")
	}
	if err != nil {
		return "", err
	}
	return name, nil
}

// ---------- Stats ----------

func (h *handlers) getStats(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	s := statsDTO{}

	_ = h.db.QueryRow(`SELECT COUNT(*) FROM clients WHERE user_id = ?`, userID).Scan(&s.TotalClients)
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM contracts WHERE user_id = ? AND status = 'active'`, userID).Scan(&s.ActiveContracts)

	_ = h.db.QueryRow(`
		SELECT COALESCE(SUM(te.hours), 0), COALESCE(SUM(te.hours * ct.hourly_rate), 0)
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		WHERE te.invoice_id IS NULL AND te.user_id = ?
	`, userID).Scan(&s.UnbilledHours, &s.UnbilledAmount)

	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	lastMonthStart := monthStart.AddDate(0, -1, 0)

	_ = h.db.QueryRow(`
		SELECT COALESCE(SUM(hours), 0) FROM time_entries WHERE user_id = ? AND date >= ?
	`, userID, monthStart.Format("2006-01-02")).Scan(&s.HoursThisMonth)

	_ = h.db.QueryRow(`
		SELECT COALESCE(SUM(hours), 0) FROM time_entries WHERE user_id = ? AND date >= ? AND date < ?
	`, userID, lastMonthStart.Format("2006-01-02"), monthStart.Format("2006-01-02")).Scan(&s.HoursLastMonth)

	_ = h.db.QueryRow(`
		SELECT COALESCE(SUM(total_amount), 0), COUNT(*) FROM invoices WHERE user_id = ? AND status IN ('pending','sent','overdue')
	`, userID).Scan(&s.OutstandingAmount, &s.InvoicesPending)

	_ = h.db.QueryRow(`
		SELECT COALESCE(SUM(total_amount), 0), COUNT(*) FROM invoices WHERE user_id = ? AND status = 'paid'
	`, userID).Scan(&s.PaidAmount, &s.InvoicesPaid)

	entries, err := h.queryTimeEntries(`
		SELECT te.id, te.contract_id, te.date, te.hours, te.description, te.invoice_id, te.created_at,
		       cl.id, cl.name, ct.contract_number, ct.name, ct.hourly_rate, ct.currency, i.invoice_number
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		JOIN clients cl ON ct.client_id = cl.id
		LEFT JOIN invoices i ON te.invoice_id = i.id
		WHERE te.user_id = ?
		ORDER BY te.date DESC, te.created_at DESC
		LIMIT 10
	`, userID)
	if err == nil {
		s.RecentEntries = entries
	} else {
		s.RecentEntries = []timeEntryDTO{}
	}

	return s, nil
}

// ---------- Clients ----------

func (h *handlers) listClients(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	rows, err := h.db.Query(`
		SELECT c.id, c.name, COALESCE(c.address,''), COALESCE(c.city,''), COALESCE(c.state,''),
		       COALESCE(c.zip_code,''), COALESCE(c.country,''), c.created_at, c.updated_at,
		       COALESCE((SELECT COUNT(*) FROM contracts WHERE client_id = c.id AND status = 'active'), 0)
		FROM clients c
		WHERE c.user_id = ?
		ORDER BY c.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []clientDTO{}
	for rows.Next() {
		var c clientDTO
		if err := rows.Scan(&c.ID, &c.Name, &c.Address, &c.City, &c.State, &c.ZipCode,
			&c.Country, &c.CreatedAt, &c.UpdatedAt, &c.ActiveContracts); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

type addClientReq struct {
	Name    string `json:"name"`
	Address string `json:"address,omitempty"`
	City    string `json:"city,omitempty"`
	State   string `json:"state,omitempty"`
	ZipCode string `json:"zip_code,omitempty"`
	Country string `json:"country,omitempty"`
}

func (h *handlers) addClient(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var req addClientReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, newAPIError(http.StatusBadRequest, "name is required")
	}
	res, err := h.db.Exec(`
		INSERT INTO clients (user_id, name, address, city, state, zip_code, country)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, userID, req.Name, req.Address, req.City, req.State, req.ZipCode, req.Country)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return map[string]interface{}{"id": id, "name": req.Name}, nil
}

type editClientReq struct {
	Name    *string `json:"name,omitempty"`
	Address *string `json:"address,omitempty"`
	City    *string `json:"city,omitempty"`
	State   *string `json:"state,omitempty"`
	ZipCode *string `json:"zip_code,omitempty"`
	Country *string `json:"country,omitempty"`
}

func (h *handlers) editClient(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}
	var req editClientReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	sets := []string{}
	args := []interface{}{}
	if req.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Address != nil {
		sets = append(sets, "address = ?")
		args = append(args, *req.Address)
	}
	if req.City != nil {
		sets = append(sets, "city = ?")
		args = append(args, *req.City)
	}
	if req.State != nil {
		sets = append(sets, "state = ?")
		args = append(args, *req.State)
	}
	if req.ZipCode != nil {
		sets = append(sets, "zip_code = ?")
		args = append(args, *req.ZipCode)
	}
	if req.Country != nil {
		sets = append(sets, "country = ?")
		args = append(args, *req.Country)
	}
	if len(sets) == 0 {
		return nil, newAPIError(http.StatusBadRequest, "no fields provided")
	}
	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id, userID)
	q := fmt.Sprintf("UPDATE clients SET %s WHERE id = ? AND user_id = ?", strings.Join(sets, ", "))
	res, err := h.db.Exec(q, args...)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, newAPIError(http.StatusNotFound, "client not found")
	}
	return map[string]interface{}{"id": id}, nil
}

func (h *handlers) deleteClient(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}

	var name string
	err = h.db.QueryRow("SELECT name FROM clients WHERE id = ? AND user_id = ?", id, userID).Scan(&name)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "client not found")
	}
	if err != nil {
		return nil, err
	}

	// Tally what will cascade so the caller and event listeners know.
	var contracts, timeEntries, invoices, quotes, recipients int
	_ = h.db.QueryRow("SELECT COUNT(*) FROM contracts WHERE client_id = ?", id).Scan(&contracts)
	_ = h.db.QueryRow("SELECT COUNT(*) FROM time_entries WHERE client_id = ?", id).Scan(&timeEntries)
	_ = h.db.QueryRow("SELECT COUNT(*) FROM invoices WHERE client_id = ?", id).Scan(&invoices)
	_ = h.db.QueryRow("SELECT COUNT(*) FROM quotes WHERE client_id = ?", id).Scan(&quotes)
	_ = h.db.QueryRow("SELECT COUNT(*) FROM recipients WHERE client_id = ?", id).Scan(&recipients)

	// ON DELETE CASCADE wipes recipients, payment_details, contracts,
	// time_entries, invoices, and quotes in one shot.
	if _, err := h.db.Exec("DELETE FROM clients WHERE id = ? AND user_id = ?", id, userID); err != nil {
		return nil, err
	}

	BroadcastUserEvent(userID, "client.deleted", map[string]any{
		"id":           id,
		"name":         name,
		"contracts":    contracts,
		"time_entries": timeEntries,
		"invoices":     invoices,
		"quotes":       quotes,
		"recipients":   recipients,
	})

	return map[string]interface{}{
		"deleted":      id,
		"name":         name,
		"contracts":    contracts,
		"time_entries": timeEntries,
		"invoices":     invoices,
		"quotes":       quotes,
		"recipients":   recipients,
	}, nil
}

// ---------- Recipients ----------

type recipientDTO struct {
	ID        int    `json:"id"`
	ClientID  int    `json:"client_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Title     string `json:"title,omitempty"`
	Phone     string `json:"phone,omitempty"`
	IsPrimary bool   `json:"is_primary"`
}

func (h *handlers) listRecipients(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}
	// Verify the client belongs to this user before exposing recipients.
	if _, err := h.requireClient(userID, id); err != nil {
		return nil, err
	}
	rows, err := h.db.Query(`
		SELECT id, client_id, name, email, COALESCE(title,''), COALESCE(phone,''), is_primary
		FROM recipients
		WHERE client_id = ?
		ORDER BY is_primary DESC, name
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []recipientDTO{}
	for rows.Next() {
		var rcp recipientDTO
		if err := rows.Scan(&rcp.ID, &rcp.ClientID, &rcp.Name, &rcp.Email, &rcp.Title, &rcp.Phone, &rcp.IsPrimary); err != nil {
			return nil, err
		}
		out = append(out, rcp)
	}
	return out, nil
}

type addRecipientReq struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	Title     string `json:"title,omitempty"`
	Phone     string `json:"phone,omitempty"`
	IsPrimary bool   `json:"is_primary,omitempty"`
}

func (h *handlers) addRecipient(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	clientID, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}
	if _, err := h.requireClient(userID, clientID); err != nil {
		return nil, err
	}
	var req addRecipientReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if req.Name == "" || req.Email == "" {
		return nil, newAPIError(http.StatusBadRequest, "name and email are required")
	}
	if req.IsPrimary {
		if _, err := h.db.Exec(`UPDATE recipients SET is_primary = 0 WHERE client_id = ?`, clientID); err != nil {
			return nil, err
		}
	}
	res, err := h.db.Exec(`
		INSERT INTO recipients (client_id, name, email, title, phone, is_primary)
		VALUES (?, ?, ?, ?, ?, ?)
	`, clientID, req.Name, req.Email, req.Title, req.Phone, req.IsPrimary)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return map[string]interface{}{"id": id}, nil
}

func (h *handlers) removeRecipient(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}
	// Scope through the client's user_id to prevent cross-tenant deletes.
	res, err := h.db.Exec(`
		DELETE FROM recipients
		WHERE id = ? AND client_id IN (SELECT id FROM clients WHERE user_id = ?)
	`, id, userID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, newAPIError(http.StatusNotFound, "recipient %d not found", id)
	}
	return map[string]interface{}{"deleted": id}, nil
}

// ---------- Payment details ----------

func (h *handlers) getPaymentDetails(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}
	if _, err := h.requireClient(userID, id); err != nil {
		return nil, err
	}
	// Defense-in-depth: requireClient above already verified ownership of the
	// client_id, but the payment_details query also re-checks user scope so
	// any future caller that skips requireClient still cannot leak rows.
	var pd models.PaymentDetails
	err = h.db.QueryRow(`
		SELECT id, client_id, COALESCE(bank_name,''), COALESCE(account_number,''), COALESCE(routing_number,''),
		       COALESCE(swift_code,''), COALESCE(payment_terms,''), COALESCE(notes,''), updated_at
		FROM payment_details
		WHERE client_id = ?
		  AND client_id IN (SELECT id FROM clients WHERE user_id = ?)
	`, id, userID).Scan(&pd.ID, &pd.ClientID, &pd.BankName, &pd.AccountNumber, &pd.RoutingNumber,
		&pd.SwiftCode, &pd.PaymentTerms, &pd.Notes, &pd.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return pd, nil
}

type paymentDetailsReq struct {
	BankName      string `json:"bank_name,omitempty"`
	AccountNumber string `json:"account_number,omitempty"`
	RoutingNumber string `json:"routing_number,omitempty"`
	SwiftCode     string `json:"swift_code,omitempty"`
	PaymentTerms  string `json:"payment_terms,omitempty"`
	Notes         string `json:"notes,omitempty"`
}

func (h *handlers) setPaymentDetails(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}
	if _, err := h.requireClient(userID, id); err != nil {
		return nil, err
	}
	var req paymentDetailsReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	_, err = h.db.Exec(`
		INSERT INTO payment_details (client_id, bank_name, account_number, routing_number, swift_code, payment_terms, notes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(client_id) DO UPDATE SET
			bank_name = excluded.bank_name,
			account_number = excluded.account_number,
			routing_number = excluded.routing_number,
			swift_code = excluded.swift_code,
			payment_terms = excluded.payment_terms,
			notes = excluded.notes,
			updated_at = excluded.updated_at
	`, id, req.BankName, req.AccountNumber, req.RoutingNumber, req.SwiftCode, req.PaymentTerms, req.Notes, time.Now())
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"client_id": id}, nil
}

// ---------- Contracts ----------

func (h *handlers) listContracts(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	q := `
		SELECT c.id, c.client_id, c.contract_number, c.name, c.hourly_rate, c.currency, c.contract_type,
		       c.start_date, c.end_date, c.status, COALESCE(c.payment_terms,''), c.payment_method_id,
		       COALESCE(c.notes,''),
		       c.created_at, c.updated_at, cl.name
		FROM contracts c
		JOIN clients cl ON c.client_id = cl.id
		WHERE c.user_id = ?
	`
	args := []interface{}{userID}
	if v := r.URL.Query().Get("client_id"); v != "" {
		q += " AND c.client_id = ?"
		args = append(args, v)
	}
	if v := r.URL.Query().Get("status"); v != "" {
		q += " AND c.status = ?"
		args = append(args, v)
	}
	q += " ORDER BY c.status = 'active' DESC, c.start_date DESC"
	rows, err := h.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contractDTO{}
	for rows.Next() {
		var c contractDTO
		var endDate sql.NullString
		var paymentMethodID sql.NullInt64
		if err := rows.Scan(&c.ID, &c.ClientID, &c.ContractNumber, &c.Name, &c.HourlyRate, &c.Currency,
			&c.ContractType, &c.StartDate, &endDate, &c.Status, &c.PaymentTerms, &paymentMethodID, &c.Notes,
			&c.CreatedAt, &c.UpdatedAt, &c.ClientName); err != nil {
			return nil, err
		}
		if endDate.Valid {
			t, _ := time.Parse("2006-01-02", endDate.String)
			c.EndDate = &t
		}
		if paymentMethodID.Valid {
			v := int(paymentMethodID.Int64)
			c.PaymentMethodID = &v
		}
		out = append(out, c)
	}
	return out, nil
}

type addContractReq struct {
	ClientID        int     `json:"client_id"`
	ContractNumber  string  `json:"contract_number"`
	Name            string  `json:"name"`
	HourlyRate      float64 `json:"hourly_rate"`
	Currency        string  `json:"currency,omitempty"`
	ContractType    string  `json:"contract_type,omitempty"`
	StartDate       string  `json:"start_date"`
	EndDate         string  `json:"end_date,omitempty"`
	PaymentTerms    string  `json:"payment_terms,omitempty"`
	PaymentMethodID *int    `json:"payment_method_id,omitempty"`
	Notes           string  `json:"notes,omitempty"`
	Status          string  `json:"status,omitempty"`
}

func (h *handlers) addContract(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var req addContractReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if req.ClientID == 0 || req.ContractNumber == "" || req.Name == "" || req.StartDate == "" {
		return nil, newAPIError(http.StatusBadRequest, "client_id, contract_number, name, start_date required")
	}
	if _, err := h.requireClient(userID, req.ClientID); err != nil {
		return nil, err
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	if req.ContractType == "" {
		req.ContractType = "hourly"
	}
	if req.Status == "" {
		req.Status = "active"
	}
	start, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		return nil, newAPIError(http.StatusBadRequest, "invalid start_date")
	}
	var endPtr interface{}
	if req.EndDate != "" {
		end, err := time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			return nil, newAPIError(http.StatusBadRequest, "invalid end_date")
		}
		endPtr = end.Format("2006-01-02")
	}
	var methodPtr interface{}
	if req.PaymentMethodID != nil {
		methodPtr = *req.PaymentMethodID
	}

	var id int64
	err = h.db.QueryRow(`
		INSERT INTO contracts (user_id, client_id, contract_number, name, hourly_rate, currency, contract_type,
		                      start_date, end_date, status, payment_terms, payment_method_id, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, userID, req.ClientID, req.ContractNumber, req.Name, req.HourlyRate, req.Currency, req.ContractType,
		start.Format("2006-01-02"), endPtr, req.Status, req.PaymentTerms, methodPtr, req.Notes).Scan(&id)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": id}, nil
}

type editContractReq struct {
	Name            *string  `json:"name,omitempty"`
	HourlyRate      *float64 `json:"hourly_rate,omitempty"`
	Currency        *string  `json:"currency,omitempty"`
	ContractType    *string  `json:"contract_type,omitempty"`
	EndDate         *string  `json:"end_date,omitempty"`
	Status          *string  `json:"status,omitempty"`
	PaymentTerms    *string  `json:"payment_terms,omitempty"`
	PaymentMethodID *int     `json:"payment_method_id,omitempty"`
	ClearPaymentMethod bool  `json:"clear_payment_method,omitempty"`
	Notes           *string  `json:"notes,omitempty"`
}

func (h *handlers) editContract(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}
	var req editContractReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	sets := []string{}
	args := []interface{}{}
	if req.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *req.Name)
	}
	if req.HourlyRate != nil {
		sets = append(sets, "hourly_rate = ?")
		args = append(args, *req.HourlyRate)
	}
	if req.Currency != nil {
		sets = append(sets, "currency = ?")
		args = append(args, *req.Currency)
	}
	if req.ContractType != nil {
		sets = append(sets, "contract_type = ?")
		args = append(args, *req.ContractType)
	}
	if req.EndDate != nil {
		if *req.EndDate == "" {
			sets = append(sets, "end_date = NULL")
		} else {
			if _, err := time.Parse("2006-01-02", *req.EndDate); err != nil {
				return nil, newAPIError(http.StatusBadRequest, "invalid end_date")
			}
			sets = append(sets, "end_date = ?")
			args = append(args, *req.EndDate)
		}
	}
	if req.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *req.Status)
	}
	if req.PaymentTerms != nil {
		sets = append(sets, "payment_terms = ?")
		args = append(args, *req.PaymentTerms)
	}
	if req.ClearPaymentMethod {
		sets = append(sets, "payment_method_id = NULL")
	} else if req.PaymentMethodID != nil {
		sets = append(sets, "payment_method_id = ?")
		args = append(args, *req.PaymentMethodID)
	}
	if req.Notes != nil {
		sets = append(sets, "notes = ?")
		args = append(args, *req.Notes)
	}
	if len(sets) == 0 {
		return nil, newAPIError(http.StatusBadRequest, "no fields provided")
	}
	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id, userID)
	q := fmt.Sprintf("UPDATE contracts SET %s WHERE id = ? AND user_id = ?", strings.Join(sets, ", "))
	res, err := h.db.Exec(q, args...)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, newAPIError(http.StatusNotFound, "contract not found")
	}
	BroadcastUserEvent(userID, "contract.updated", map[string]any{"id": id})
	return map[string]interface{}{"id": id}, nil
}

// ---------- Time entries ----------

func (h *handlers) queryTimeEntries(q string, args ...interface{}) ([]timeEntryDTO, error) {
	rows, err := h.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []timeEntryDTO{}
	for rows.Next() {
		var e timeEntryDTO
		var invoiceNumber sql.NullString
		if err := rows.Scan(&e.ID, &e.ContractID, &e.Date, &e.Hours, &e.Description, &e.InvoiceID, &e.CreatedAt,
			&e.ClientID, &e.ClientName, &e.ContractNumber, &e.ContractName, &e.HourlyRate, &e.Currency, &invoiceNumber); err != nil {
			return nil, err
		}
		if invoiceNumber.Valid {
			s := invoiceNumber.String
			e.InvoiceNumber = &s
		}
		e.Amount = e.Hours * e.HourlyRate
		out = append(out, e)
	}
	return out, nil
}

func (h *handlers) searchTimeEntries(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	q := `
		SELECT te.id, te.contract_id, te.date, te.hours, te.description, te.invoice_id, te.created_at,
		       cl.id, cl.name, ct.contract_number, ct.name, ct.hourly_rate, ct.currency, i.invoice_number
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		JOIN clients cl ON ct.client_id = cl.id
		LEFT JOIN invoices i ON te.invoice_id = i.id
		WHERE te.user_id = ?
	`
	args := []interface{}{userID}

	qv := r.URL.Query()
	if v := qv.Get("client_id"); v != "" {
		q += " AND cl.id = ?"
		args = append(args, v)
	}
	if v := qv.Get("contract_id"); v != "" {
		q += " AND ct.id = ?"
		args = append(args, v)
	}
	if v := qv.Get("description"); v != "" {
		q += " AND te.description LIKE ?"
		args = append(args, "%"+v+"%")
	}
	if v := qv.Get("start_date"); v != "" {
		t, err := timeparse.ParseDate(v)
		if err != nil {
			return nil, newAPIError(http.StatusBadRequest, "invalid start_date")
		}
		q += " AND te.date >= ?"
		args = append(args, t.Format("2006-01-02"))
	}
	if v := qv.Get("end_date"); v != "" {
		t, err := timeparse.ParseDate(v)
		if err != nil {
			return nil, newAPIError(http.StatusBadRequest, "invalid end_date")
		}
		q += " AND te.date <= ?"
		args = append(args, t.Format("2006-01-02"))
	}
	switch qv.Get("invoiced") {
	case "true":
		q += " AND te.invoice_id IS NOT NULL"
	case "false":
		q += " AND te.invoice_id IS NULL"
	}
	q += " ORDER BY te.date DESC, te.created_at DESC"
	if v := qv.Get("limit"); v != "" {
		// Parse and clamp the limit instead of inlining the raw query value
		// — it lands in the SQL string by value (no `?` placeholder) so any
		// non-integer would be a SQL injection foothold.
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, newAPIError(http.StatusBadRequest, "invalid limit")
		}
		if n > 5000 {
			n = 5000
		}
		q += fmt.Sprintf(" LIMIT %d", n)
	}
	return h.queryTimeEntries(q, args...)
}

type addTimeEntryReq struct {
	ContractID     int     `json:"contract_id"`
	ContractNumber string  `json:"contract_number,omitempty"`
	Hours          float64 `json:"hours"`
	Date           string  `json:"date"`
	Description    string  `json:"description,omitempty"`
}

func (h *handlers) addTimeEntry(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var req addTimeEntryReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	contractID, clientID, err := h.resolveContract(userID, req.ContractID, req.ContractNumber)
	if err != nil {
		return nil, err
	}
	date, err := parseDate(req.Date)
	if err != nil {
		return nil, err
	}
	if req.Hours <= 0 {
		return nil, newAPIError(http.StatusBadRequest, "hours must be > 0")
	}
	id := uuid.New().String()
	_, err = h.db.Exec(`
		INSERT INTO time_entries (id, user_id, client_id, contract_id, date, hours, description)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, userID, clientID, contractID, date.Format("2006-01-02"), req.Hours, req.Description)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": id}, nil
}

func (h *handlers) resolveContract(userID int64, id int, number string) (int, int, error) {
	if id != 0 {
		var clientID int
		err := h.db.QueryRow(`SELECT client_id FROM contracts WHERE id = ? AND user_id = ? AND status = 'active'`, id, userID).Scan(&clientID)
		if err == sql.ErrNoRows {
			return 0, 0, newAPIError(http.StatusNotFound, "contract %d not found or not active", id)
		}
		if err != nil {
			return 0, 0, err
		}
		return id, clientID, nil
	}
	if number != "" {
		var contractID, clientID int
		err := h.db.QueryRow(`SELECT id, client_id FROM contracts WHERE contract_number = ? AND user_id = ? AND status = 'active'`, number, userID).Scan(&contractID, &clientID)
		if err == sql.ErrNoRows {
			return 0, 0, newAPIError(http.StatusNotFound, "contract %s not found or not active", number)
		}
		if err != nil {
			return 0, 0, err
		}
		return contractID, clientID, nil
	}
	return 0, 0, newAPIError(http.StatusBadRequest, "contract_id or contract_number required")
}

type bulkAddReq struct {
	Entries []addTimeEntryReq `json:"entries"`
}

func (h *handlers) bulkAddTimeEntries(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var req bulkAddReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if len(req.Entries) == 0 {
		return nil, newAPIError(http.StatusBadRequest, "no entries")
	}
	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ids := []string{}
	for _, e := range req.Entries {
		contractID, clientID, err := h.resolveContract(userID, e.ContractID, e.ContractNumber)
		if err != nil {
			return nil, err
		}
		date, err := parseDate(e.Date)
		if err != nil {
			return nil, err
		}
		id := uuid.New().String()
		if _, err := tx.Exec(`
			INSERT INTO time_entries (id, user_id, client_id, contract_id, date, hours, description)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, id, userID, clientID, contractID, date.Format("2006-01-02"), e.Hours, e.Description); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]interface{}{"ids": ids, "count": len(ids)}, nil
}

type updateTimeEntryReq struct {
	Hours       *float64 `json:"hours,omitempty"`
	Date        *string  `json:"date,omitempty"`
	Description *string  `json:"description,omitempty"`
}

func (h *handlers) updateTimeEntry(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id := r.PathValue("id")
	var existing struct {
		InvoiceID *int
	}
	err = h.db.QueryRow(`SELECT invoice_id FROM time_entries WHERE id = ? AND user_id = ?`, id, userID).Scan(&existing.InvoiceID)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "entry not found")
	}
	if err != nil {
		return nil, err
	}
	if existing.InvoiceID != nil {
		return nil, newAPIError(http.StatusConflict, "cannot update invoiced entry")
	}
	var req updateTimeEntryReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	sets := []string{}
	args := []interface{}{}
	if req.Hours != nil {
		sets = append(sets, "hours = ?")
		args = append(args, *req.Hours)
	}
	if req.Date != nil {
		d, err := parseDate(*req.Date)
		if err != nil {
			return nil, err
		}
		sets = append(sets, "date = ?")
		args = append(args, d.Format("2006-01-02"))
	}
	if req.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *req.Description)
	}
	if len(sets) == 0 {
		return nil, newAPIError(http.StatusBadRequest, "no fields")
	}
	args = append(args, id, userID)
	if _, err := h.db.Exec(fmt.Sprintf("UPDATE time_entries SET %s WHERE id = ? AND user_id = ?", strings.Join(sets, ", ")), args...); err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": id}, nil
}

func (h *handlers) deleteTimeEntry(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	id := r.PathValue("id")
	res, err := h.db.Exec(`DELETE FROM time_entries WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, newAPIError(http.StatusNotFound, "entry not found")
	}
	return map[string]interface{}{"deleted": id}, nil
}

type bulkIDsReq struct {
	IDs []string `json:"ids"`
}

func (h *handlers) bulkDeleteTimeEntries(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var req bulkIDsReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if len(req.IDs) == 0 {
		return nil, newAPIError(http.StatusBadRequest, "no ids")
	}
	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	count := 0
	for _, id := range req.IDs {
		res, err := tx.Exec(`DELETE FROM time_entries WHERE id = ? AND user_id = ?`, id, userID)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		count += int(n)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]interface{}{"deleted": count}, nil
}

type markInvoicedReq struct {
	InvoiceNumber string   `json:"invoice_number"`
	IDs           []string `json:"ids"`
}

func (h *handlers) markTimeEntriesInvoiced(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var req markInvoicedReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	var invoiceID int
	err = h.db.QueryRow(`SELECT id FROM invoices WHERE invoice_number = ? AND user_id = ?`, req.InvoiceNumber, userID).Scan(&invoiceID)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "invoice not found")
	}
	if err != nil {
		return nil, err
	}
	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	count := 0
	for _, id := range req.IDs {
		var existing *int
		if err := tx.QueryRow(`SELECT invoice_id FROM time_entries WHERE id = ? AND user_id = ?`, id, userID).Scan(&existing); err != nil {
			continue
		}
		if existing != nil {
			return nil, newAPIError(http.StatusConflict, "entry %s is already invoiced", id)
		}
		res, err := tx.Exec(`UPDATE time_entries SET invoice_id = ? WHERE id = ? AND user_id = ?`, invoiceID, id, userID)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		count += int(n)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]interface{}{"marked": count}, nil
}

func (h *handlers) unmarkTimeEntries(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var req bulkIDsReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	count := 0
	for _, id := range req.IDs {
		res, err := tx.Exec(`UPDATE time_entries SET invoice_id = NULL WHERE id = ? AND user_id = ?`, id, userID)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		count += int(n)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]interface{}{"unmarked": count}, nil
}

// ---------- Invoices ----------

func (h *handlers) listInvoices(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	q := `
		SELECT i.id, i.invoice_number, i.client_id, c.name, i.issue_date, i.due_date,
		       i.total_amount, i.status, COALESCE(i.pdf_path,''), i.created_at
		FROM invoices i
		JOIN clients c ON i.client_id = c.id
		WHERE i.user_id = ?
	`
	args := []interface{}{userID}
	qv := r.URL.Query()
	if v := qv.Get("client_id"); v != "" {
		q += " AND i.client_id = ?"
		args = append(args, v)
	}
	if v := qv.Get("status"); v != "" {
		q += " AND i.status = ?"
		args = append(args, v)
	}
	q += " ORDER BY i.issue_date DESC"
	rows, err := h.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []invoiceDTO{}
	for rows.Next() {
		var inv invoiceDTO
		if err := rows.Scan(&inv.ID, &inv.InvoiceNumber, &inv.ClientID, &inv.ClientName,
			&inv.IssueDate, &inv.DueDate, &inv.TotalAmount, &inv.Status, &inv.PDFPath, &inv.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, nil
}

type invoiceDetailsResponse struct {
	Invoice       invoiceDTO     `json:"invoice"`
	TimeEntries   []timeEntryDTO `json:"time_entries"`
	Expenses      []expenseRow   `json:"expenses"`
	TotalHours    float64        `json:"total_hours"`
	TotalExpenses float64        `json:"total_expenses"`
}

func (h *handlers) getInvoiceDetails(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	number := r.PathValue("number")
	var inv invoiceDTO
	err = h.db.QueryRow(`
		SELECT i.id, i.invoice_number, i.client_id, c.name, i.issue_date, i.due_date,
		       i.total_amount, i.status, COALESCE(i.pdf_path,''), i.created_at
		FROM invoices i
		JOIN clients c ON i.client_id = c.id
		WHERE i.invoice_number = ? AND i.user_id = ?
	`, number, userID).Scan(&inv.ID, &inv.InvoiceNumber, &inv.ClientID, &inv.ClientName,
		&inv.IssueDate, &inv.DueDate, &inv.TotalAmount, &inv.Status, &inv.PDFPath, &inv.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "invoice not found")
	}
	if err != nil {
		return nil, err
	}
	entries, err := h.queryTimeEntries(`
		SELECT te.id, te.contract_id, te.date, te.hours, te.description, te.invoice_id, te.created_at,
		       cl.id, cl.name, ct.contract_number, ct.name, ct.hourly_rate, ct.currency, i.invoice_number
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		JOIN clients cl ON ct.client_id = cl.id
		LEFT JOIN invoices i ON te.invoice_id = i.id
		WHERE te.invoice_id = ? AND te.user_id = ?
		ORDER BY te.date
	`, inv.ID, userID)
	if err != nil {
		return nil, err
	}
	total := 0.0
	for _, e := range entries {
		total += e.Hours
	}

	expRows, err := h.db.Query(`
		SELECT e.id, e.client_id, c.name, e.contract_id, COALESCE(ct.contract_number,''),
		       e.date, e.description, e.amount, e.currency,
		       COALESCE(e.category,''), COALESCE(e.receipt_path,''),
		       e.invoice_id, COALESCE(i.invoice_number,''), e.created_at
		FROM expenses e
		JOIN clients c ON c.id = e.client_id
		LEFT JOIN contracts ct ON ct.id = e.contract_id
		LEFT JOIN invoices i ON i.id = e.invoice_id
		WHERE e.invoice_id = ? AND e.user_id = ?
		ORDER BY e.date
	`, inv.ID, userID)
	if err != nil {
		return nil, err
	}
	defer expRows.Close()
	expenses := []expenseRow{}
	totalExpenses := 0.0
	for expRows.Next() {
		var e expenseRow
		var date, created time.Time
		if err := expRows.Scan(&e.ID, &e.ClientID, &e.ClientName, &e.ContractID, &e.ContractNumber,
			&date, &e.Description, &e.Amount, &e.Currency, &e.Category, &e.ReceiptPath,
			&e.InvoiceID, &e.InvoiceNumber, &created); err != nil {
			return nil, err
		}
		e.Date = date.Format("2006-01-02")
		e.CreatedAt = created.Format(time.RFC3339)
		expenses = append(expenses, e)
		totalExpenses += e.Amount
	}

	return invoiceDetailsResponse{
		Invoice:       inv,
		TimeEntries:   entries,
		Expenses:      expenses,
		TotalHours:    total,
		TotalExpenses: totalExpenses,
	}, nil
}

// invoicePreviewResponse is the render-ready payload for the HTML preview.
// Everything the UI needs to draw the invoice without extra round-trips.
type invoicePreviewResponse struct {
	Invoice       invoiceDTO            `json:"invoice"`
	Client        models.Client         `json:"client"`
	Contracts     []models.Contract     `json:"contracts"`
	TimeEntries   []timeEntryDTO        `json:"time_entries"`
	Expenses      []expenseRow          `json:"expenses"`
	TotalHours    float64               `json:"total_hours"`
	TotalExpenses float64               `json:"total_expenses"`
	Payment       models.PaymentDetails `json:"payment"`
	Recipients    []models.Recipient    `json:"recipients"`
	Business      models.BusinessInfo   `json:"business"`
}

func (h *handlers) getInvoicePreview(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	number := r.PathValue("number")

	var inv invoiceDTO
	err = h.db.QueryRow(`
		SELECT i.id, i.invoice_number, i.client_id, c.name, i.issue_date, i.due_date,
		       i.total_amount, i.status, COALESCE(i.pdf_path,''), i.created_at
		FROM invoices i
		JOIN clients c ON i.client_id = c.id
		WHERE i.invoice_number = ? AND i.user_id = ?
	`, number, userID).Scan(&inv.ID, &inv.InvoiceNumber, &inv.ClientID, &inv.ClientName,
		&inv.IssueDate, &inv.DueDate, &inv.TotalAmount, &inv.Status, &inv.PDFPath, &inv.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "invoice not found")
	}
	if err != nil {
		return nil, err
	}

	var client models.Client
	if err := h.db.QueryRow(`
		SELECT id, name, COALESCE(address,''), COALESCE(city,''), COALESCE(state,''),
		       COALESCE(zip_code,''), COALESCE(country,'')
		FROM clients WHERE id = ? AND user_id = ?
	`, inv.ClientID, userID).Scan(&client.ID, &client.Name, &client.Address, &client.City,
		&client.State, &client.ZipCode, &client.Country); err != nil {
		return nil, err
	}

	entries, err := h.queryTimeEntries(`
		SELECT te.id, te.contract_id, te.date, te.hours, te.description, te.invoice_id, te.created_at,
		       cl.id, cl.name, ct.contract_number, ct.name, ct.hourly_rate, ct.currency, i.invoice_number
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		JOIN clients cl ON ct.client_id = cl.id
		LEFT JOIN invoices i ON te.invoice_id = i.id
		WHERE te.invoice_id = ? AND te.user_id = ?
		ORDER BY te.date
	`, inv.ID, userID)
	if err != nil {
		return nil, err
	}
	totalHours := 0.0
	for _, e := range entries {
		totalHours += e.Hours
	}

	// Contracts referenced by the entries (deduplicated, in first-seen order).
	seen := map[int]bool{}
	var contracts []models.Contract
	contractRows, _ := h.db.Query(`
		SELECT id, client_id, contract_number, name, hourly_rate, currency,
		       contract_type, start_date, end_date, status, COALESCE(payment_terms,''),
		       COALESCE(notes,''), created_at, updated_at
		FROM contracts WHERE client_id = ? AND user_id = ?
	`, inv.ClientID, userID)
	contractByID := map[int]models.Contract{}
	if contractRows != nil {
		for contractRows.Next() {
			var c models.Contract
			var endDate sql.NullTime
			if err := contractRows.Scan(&c.ID, &c.ClientID, &c.ContractNumber, &c.Name,
				&c.HourlyRate, &c.Currency, &c.ContractType, &c.StartDate, &endDate,
				&c.Status, &c.PaymentTerms, &c.Notes, &c.CreatedAt, &c.UpdatedAt); err != nil {
				continue
			}
			if endDate.Valid {
				t := endDate.Time
				c.EndDate = &t
			}
			contractByID[c.ID] = c
		}
		contractRows.Close()
	}
	for _, e := range entries {
		if seen[e.ContractID] {
			continue
		}
		seen[e.ContractID] = true
		if c, ok := contractByID[e.ContractID]; ok {
			contracts = append(contracts, c)
		}
	}

	payment := ResolveInvoicePaymentDetails(h.db, userID, int64(inv.ID), inv.ClientID)

	// Defense-in-depth: even though the invoice→client linkage was already
	// scoped by user_id above, every recipient/payment-details query also
	// re-checks ownership via a subquery so a future bug that propagates a
	// stale or attacker-supplied client_id can't leak rows.
	var recipients []models.Recipient
	recRows, _ := h.db.Query(`
		SELECT id, client_id, name, email, COALESCE(title,''), COALESCE(phone,''), is_primary, created_at
		FROM recipients
		WHERE client_id = ?
		  AND client_id IN (SELECT id FROM clients WHERE user_id = ?)
		ORDER BY is_primary DESC
	`, inv.ClientID, userID)
	if recRows != nil {
		for recRows.Next() {
			var rcp models.Recipient
			if err := recRows.Scan(&rcp.ID, &rcp.ClientID, &rcp.Name, &rcp.Email,
				&rcp.Title, &rcp.Phone, &rcp.IsPrimary, &rcp.CreatedAt); err != nil {
				continue
			}
			recipients = append(recipients, rcp)
		}
		recRows.Close()
	}

	business := loadBusinessInfo(h.db, userID)

	previewExpRows, err := h.db.Query(`
		SELECT e.id, e.client_id, c.name, e.contract_id, COALESCE(ct.contract_number,''),
		       e.date, e.description, e.amount, e.currency,
		       COALESCE(e.category,''), COALESCE(e.receipt_path,''),
		       e.invoice_id, COALESCE(i.invoice_number,''), e.created_at
		FROM expenses e
		JOIN clients c ON c.id = e.client_id
		LEFT JOIN contracts ct ON ct.id = e.contract_id
		LEFT JOIN invoices i ON i.id = e.invoice_id
		WHERE e.invoice_id = ? AND e.user_id = ?
		ORDER BY e.date
	`, inv.ID, userID)
	if err != nil {
		return nil, err
	}
	defer previewExpRows.Close()
	previewExpenses := []expenseRow{}
	totalPreviewExpenses := 0.0
	for previewExpRows.Next() {
		var ex expenseRow
		var date, created time.Time
		if err := previewExpRows.Scan(&ex.ID, &ex.ClientID, &ex.ClientName, &ex.ContractID, &ex.ContractNumber,
			&date, &ex.Description, &ex.Amount, &ex.Currency, &ex.Category, &ex.ReceiptPath,
			&ex.InvoiceID, &ex.InvoiceNumber, &created); err != nil {
			return nil, err
		}
		ex.Date = date.Format("2006-01-02")
		ex.CreatedAt = created.Format(time.RFC3339)
		previewExpenses = append(previewExpenses, ex)
		totalPreviewExpenses += ex.Amount
	}

	return invoicePreviewResponse{
		Invoice:       inv,
		Client:        client,
		Contracts:     contracts,
		TimeEntries:   entries,
		Expenses:      previewExpenses,
		TotalHours:    totalHours,
		TotalExpenses: totalPreviewExpenses,
		Payment:       payment,
		Recipients:    recipients,
		Business:      business,
	}, nil
}

type createInvoiceReq struct {
	ClientID  int    `json:"client_id"`
	Period    string `json:"period"`
	DueDays   int    `json:"due_days,omitempty"`
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
}

func (h *handlers) createInvoice(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var req createInvoiceReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if req.ClientID == 0 {
		return nil, newAPIError(http.StatusBadRequest, "client_id required")
	}
	if _, err := h.requireClient(userID, req.ClientID); err != nil {
		return nil, err
	}
	if req.DueDays == 0 {
		req.DueDays = 30
	}

	// Business info validation
	var businessName string
	err = h.db.QueryRow(`SELECT business_name FROM business_info WHERE user_id = ?`, userID).Scan(&businessName)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusPreconditionFailed, "business info not configured")
	}
	if err != nil {
		return nil, err
	}

	// Payment info is now optional at invoice-create time: it's snapshotted
	// from the client's contract below, and the PDF block is simply omitted
	// if nothing's configured. Users can always attach/adjust a payment
	// method post-hoc in Settings without re-issuing invoices.

	var startDate, endDate time.Time
	if req.StartDate != "" && req.EndDate != "" {
		startDate, err = time.Parse("2006-01-02", req.StartDate)
		if err != nil {
			return nil, newAPIError(http.StatusBadRequest, "invalid start_date")
		}
		endDate, err = time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			return nil, newAPIError(http.StatusBadRequest, "invalid end_date")
		}
	} else if req.Period != "" {
		startDate, endDate, err = timeparse.ParsePeriod(req.Period)
		if err != nil {
			return nil, newAPIError(http.StatusBadRequest, "invalid period: %s", err)
		}
	} else {
		return nil, newAPIError(http.StatusBadRequest, "period or start_date/end_date required")
	}

	// Load client
	var client models.Client
	err = h.db.QueryRow(`
		SELECT id, name, COALESCE(address,''), COALESCE(city,''), COALESCE(state,''),
		       COALESCE(zip_code,''), COALESCE(country,'')
		FROM clients WHERE id = ? AND user_id = ?
	`, req.ClientID, userID).Scan(&client.ID, &client.Name, &client.Address, &client.City, &client.State, &client.ZipCode, &client.Country)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "client not found")
	}
	if err != nil {
		return nil, err
	}

	// Fetch unbilled entries
	rows, err := h.db.Query(`
		SELECT te.id, te.date, te.hours, te.description, ct.hourly_rate, ct.currency,
		       ct.id, ct.contract_number, ct.name, COALESCE(ct.payment_terms,'')
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		WHERE ct.client_id = ? AND te.user_id = ? AND te.date >= ? AND te.date <= ? AND te.invoice_id IS NULL
		ORDER BY te.date
	`, req.ClientID, userID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []models.TimeEntry
	var totalHours, totalAmount float64
	for rows.Next() {
		var e models.TimeEntry
		var rate float64
		var currency string
		var contract models.Contract
		if err := rows.Scan(&e.ID, &e.Date, &e.Hours, &e.Description, &rate, &currency,
			&contract.ID, &contract.ContractNumber, &contract.Name, &contract.PaymentTerms); err != nil {
			return nil, err
		}
		contract.HourlyRate = rate
		contract.Currency = currency
		e.Contract = &contract
		e.ContractID = contract.ID
		entries = append(entries, e)
		totalHours += e.Hours
		totalAmount += e.Hours * rate
	}

	// Sweep up unbilled expenses in the same period — mirrors the MCP
	// path so the web invoice flow doesn't silently drop pass-through costs.
	expRows, err := h.db.Query(`
		SELECT id, client_id, contract_id, date, description, amount, currency,
		       COALESCE(category,''), COALESCE(receipt_path,'')
		FROM expenses
		WHERE client_id = ? AND user_id = ? AND date >= ? AND date <= ? AND invoice_id IS NULL
		ORDER BY date
	`, req.ClientID, userID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer expRows.Close()
	var expenses []models.Expense
	var totalExpenses float64
	for expRows.Next() {
		var ex models.Expense
		if err := expRows.Scan(&ex.ID, &ex.ClientID, &ex.ContractID, &ex.Date, &ex.Description,
			&ex.Amount, &ex.Currency, &ex.Category, &ex.ReceiptPath); err != nil {
			return nil, err
		}
		expenses = append(expenses, ex)
		totalExpenses += ex.Amount
	}
	totalAmount += totalExpenses

	if len(entries) == 0 && len(expenses) == 0 {
		return nil, newAPIError(http.StatusPreconditionFailed, "no unbilled hours or expenses in period")
	}

	invoiceNumber := fmt.Sprintf("INV-%s-%s", time.Now().Format("200601"), uuid.New().String()[:8])
	issueDate := time.Now()
	dueDate := issueDate.AddDate(0, 0, req.DueDays)

	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Snapshot the payment method from the client's contract so the invoice
	// stays stable even if the contract's method changes later.
	paymentMethodID := resolveContractPaymentMethodID(h.db, userID, req.ClientID)

	res, err := tx.Exec(`
		INSERT INTO invoices (user_id, client_id, invoice_number, issue_date, due_date, total_amount, status, payment_method_id)
		VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)
	`, userID, req.ClientID, invoiceNumber, issueDate.Format("2006-01-02"), dueDate.Format("2006-01-02"), totalAmount,
		paymentMethodID)
	if err != nil {
		return nil, err
	}
	invoiceID, _ := res.LastInsertId()

	for _, e := range entries {
		if _, err := tx.Exec(`UPDATE time_entries SET invoice_id = ? WHERE id = ? AND user_id = ?`, invoiceID, e.ID, userID); err != nil {
			return nil, err
		}
	}
	for _, ex := range expenses {
		if _, err := tx.Exec(`UPDATE expenses SET invoice_id = ? WHERE id = ? AND user_id = ?`, invoiceID, ex.ID, userID); err != nil {
			return nil, err
		}
	}

	// Gather info for PDF. We read payment through the helper so the
	// snapshot on this just-inserted invoice wins over legacy data.
	paymentDetails := ResolveInvoicePaymentDetails(h.db, userID, invoiceID, req.ClientID)

	var recipients []models.Recipient
	recRows, _ := tx.Query(`
		SELECT name, email, COALESCE(title,''), COALESCE(phone,'')
		FROM recipients
		WHERE client_id = ?
		  AND client_id IN (SELECT id FROM clients WHERE user_id = ?)
		ORDER BY is_primary DESC
	`, req.ClientID, userID)
	if recRows != nil {
		for recRows.Next() {
			var rcp models.Recipient
			recRows.Scan(&rcp.Name, &rcp.Email, &rcp.Title, &rcp.Phone)
			recipients = append(recipients, rcp)
		}
		recRows.Close()
	}

	business := loadBusinessInfo(h.db, userID)

	// All per-user file output is confined to a fixed per-user directory
	// under the server's home. The legacy business_info.export_path field
	// is intentionally ignored (it was a path-traversal footgun and has no
	// use case in the multi-tenant deployment — files written there were
	// invisible to the tenant anyway).
	exportDir, err := userExportDir(userID)
	if err != nil {
		return nil, newAPIError(http.StatusInternalServerError, "failed to create export dir: %s", err)
	}
	pdfPath := filepath.Join(exportDir, fmt.Sprintf("invoice_%s_%s.pdf", invoiceNumber, issueDate.Format("2006-01-02")))

	invoice := models.Invoice{
		ID:            int(invoiceID),
		ClientID:      req.ClientID,
		InvoiceNumber: invoiceNumber,
		IssueDate:     issueDate,
		DueDate:       dueDate,
		TotalAmount:   totalAmount,
		Status:        "pending",
		Client:        &client,
		TimeEntries:   entries,
		Expenses:      expenses,
	}

	generator := pdf.NewInvoiceGenerator()
	if err := generator.Generate(invoice, paymentDetails, recipients, business, pdfPath); err != nil {
		return nil, newAPIError(http.StatusInternalServerError, "failed to generate PDF: %s", err)
	}

	if _, err := tx.Exec(`UPDATE invoices SET pdf_path = ? WHERE id = ? AND user_id = ?`, pdfPath, invoiceID, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"id":             invoiceID,
		"invoice_number": invoiceNumber,
		"total_amount":   totalAmount,
		"total_hours":    totalHours,
		"total_expenses": totalExpenses,
		"expense_count":  len(expenses),
		"pdf_path":       pdfPath,
	}, nil
}

type updateInvoiceStatusReq struct {
	Status string `json:"status"`
}

func (h *handlers) updateInvoiceStatus(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	number := r.PathValue("number")
	var req updateInvoiceStatusReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	valid := map[string]bool{"draft": true, "pending": true, "sent": true, "paid": true, "overdue": true, "cancelled": true}
	if !valid[req.Status] {
		return nil, newAPIError(http.StatusBadRequest, "invalid status")
	}
	res, err := h.db.Exec(`UPDATE invoices SET status = ? WHERE invoice_number = ? AND user_id = ?`, req.Status, number, userID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, newAPIError(http.StatusNotFound, "invoice not found")
	}
	BroadcastUserEvent(userID, "invoice.updated", map[string]any{
		"source":         "api",
		"invoice_number": number,
		"status":         req.Status,
	})
	return map[string]interface{}{"status": req.Status}, nil
}

func (h *handlers) deleteInvoice(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	number := r.PathValue("number")

	var id int64
	var status, pdfPath string
	err = h.db.QueryRow(
		`SELECT id, status, COALESCE(pdf_path,'') FROM invoices WHERE invoice_number = ? AND user_id = ?`,
		number, userID,
	).Scan(&id, &status, &pdfPath)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "invoice not found")
	}
	if err != nil {
		return nil, err
	}
	if status != "cancelled" {
		return nil, newAPIError(http.StatusConflict,
			"only cancelled invoices can be deleted (mark as cancelled first)")
	}

	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`UPDATE time_entries SET invoice_id = NULL WHERE invoice_id = ? AND user_id = ?`, id, userID,
	); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM invoices WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if pdfPath != "" {
		_ = os.Remove(pdfPath)
	}

	BroadcastUserEvent(userID, "invoice.deleted", map[string]any{
		"source":         "api",
		"invoice_number": number,
	})
	return map[string]interface{}{"deleted": number}, nil
}

// ---------- Business Info ----------

func (h *handlers) getBusinessInfo(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var b models.BusinessInfo
	err = h.db.QueryRow(`
		SELECT user_id, business_name, contact_name, email, COALESCE(phone,''), COALESCE(address,''),
		       COALESCE(city,''), COALESCE(state,''), COALESCE(zip_code,''), COALESCE(country,''),
		       COALESCE(tax_id,''), COALESCE(website,''), COALESCE(logo_path,''), COALESCE(invoice_prefix,'INV'),
		       COALESCE(export_path,''), updated_at
		FROM business_info WHERE user_id = ?
	`, userID).Scan(&b.ID, &b.BusinessName, &b.ContactName, &b.Email, &b.Phone, &b.Address, &b.City, &b.State,
		&b.ZipCode, &b.Country, &b.TaxID, &b.Website, &b.LogoPath, &b.InvoicePrefix, &b.ExportPath, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// loadBusinessInfo is the package-internal lookup used by invoice/quote PDF
// flows that need the business block alongside other tenant-scoped data.
// Returns a zero value (the PDF layer is tolerant of empty fields) when the
// user hasn't configured their business details yet.
func loadBusinessInfo(db *sql.DB, userID int64) models.BusinessInfo {
	var b models.BusinessInfo
	_ = db.QueryRow(`
		SELECT user_id, business_name, contact_name, email, COALESCE(phone,''), COALESCE(address,''),
		       COALESCE(city,''), COALESCE(state,''), COALESCE(zip_code,''), COALESCE(country,''),
		       COALESCE(tax_id,''), COALESCE(website,''), COALESCE(logo_path,''), COALESCE(invoice_prefix,'INV'),
		       COALESCE(export_path,''), updated_at
		FROM business_info WHERE user_id = ?
	`, userID).Scan(&b.ID, &b.BusinessName, &b.ContactName, &b.Email, &b.Phone, &b.Address, &b.City, &b.State,
		&b.ZipCode, &b.Country, &b.TaxID, &b.Website, &b.LogoPath, &b.InvoicePrefix, &b.ExportPath, &b.UpdatedAt)
	return b
}

type businessInfoReq struct {
	BusinessName  string `json:"business_name"`
	ContactName   string `json:"contact_name"`
	Email         string `json:"email"`
	Phone         string `json:"phone,omitempty"`
	Address       string `json:"address,omitempty"`
	City          string `json:"city,omitempty"`
	State         string `json:"state,omitempty"`
	ZipCode       string `json:"zip_code,omitempty"`
	Country       string `json:"country,omitempty"`
	TaxID         string `json:"tax_id,omitempty"`
	Website       string `json:"website,omitempty"`
	// LogoPath and ExportPath are accepted for backcompat but never used:
	// LogoPath is no longer dereferenced by any code path (the PDF renderer
	// doesn't embed logos), and ExportPath was a path-traversal footgun
	// replaced by a per-user fixed export dir. Both fields are accepted
	// from the request so old clients don't error, but the values are
	// silently ignored at the SQL layer below.
	LogoPath      string `json:"logo_path,omitempty"`
	InvoicePrefix string `json:"invoice_prefix,omitempty"`
	ExportPath    string `json:"export_path,omitempty"`
}

func (h *handlers) setBusinessInfo(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var req businessInfoReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if req.InvoicePrefix == "" {
		req.InvoicePrefix = "INV"
	}
	// Force logo_path and export_path to empty regardless of what the client
	// sends — see businessInfoReq for rationale. We pass empty strings into
	// the SQL so that an existing stored value is also blanked on update,
	// closing out any leftover risky path that an earlier version persisted.
	_, err = h.db.Exec(`
		INSERT INTO business_info (user_id, business_name, contact_name, email, phone, address, city, state,
		                         zip_code, country, tax_id, website, logo_path, invoice_prefix, export_path, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			business_name = excluded.business_name,
			contact_name = excluded.contact_name,
			email = excluded.email,
			phone = excluded.phone,
			address = excluded.address,
			city = excluded.city,
			state = excluded.state,
			zip_code = excluded.zip_code,
			country = excluded.country,
			tax_id = excluded.tax_id,
			website = excluded.website,
			logo_path = excluded.logo_path,
			invoice_prefix = excluded.invoice_prefix,
			export_path = excluded.export_path,
			updated_at = excluded.updated_at
	`, userID, req.BusinessName, req.ContactName, req.Email, req.Phone, req.Address, req.City, req.State,
		req.ZipCode, req.Country, req.TaxID, req.Website, "", req.InvoicePrefix, "", time.Now())
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"ok": true}, nil
}

// ---------- Utility ----------

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Now(), nil
	}
	t, err := timeparse.ParseDate(s)
	if err != nil {
		return time.Time{}, newAPIError(http.StatusBadRequest, "invalid date: %s", s)
	}
	return t, nil
}

// userExportDir returns the absolute directory under which all server-side
// PDF output for the given user is written, creating it if it doesn't yet
// exist. The location is fixed (~/.hours/exports/<user_id>) — the legacy
// business_info.export_path setting is intentionally not honored here, since
// letting a tenant choose an arbitrary filesystem path is a traversal vector
// and has no meaningful use case behind a server deployment where the tenant
// can't see the host filesystem anyway.
func userExportDir(userID int64) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".hours", "exports", strconv.FormatInt(userID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// ---------- Invoice download ----------

// downloadInvoice streams the rendered invoice PDF as the HTTP response body
// with a Content-Disposition: attachment header. Both --serve mode (browser)
// and Wails mode (in-process httptest dispatch) consume the same bytes — the
// Wails frontend wraps the response in a native Save-As dialog rather than a
// browser download.
//
// The previous implementation wrote the PDF to ~/Downloads on the SERVER's
// filesystem, which is meaningless inside a Docker container and was the H3
// path-traversal vector flagged in the security review. This handler never
// touches the server filesystem.
func (h *handlers) downloadInvoice(w http.ResponseWriter, r *http.Request) {
	userID, err := currentUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	number := r.PathValue("number")

	var inv struct {
		ID          int64
		ClientID    int
		IssueDate   time.Time
		DueDate     time.Time
		TotalAmount float64
		Status      string
	}
	err = h.db.QueryRow(`
		SELECT id, client_id, issue_date, due_date, total_amount, status
		FROM invoices WHERE invoice_number = ? AND user_id = ?
	`, number, userID).Scan(&inv.ID, &inv.ClientID, &inv.IssueDate, &inv.DueDate, &inv.TotalAmount, &inv.Status)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, fmt.Errorf("invoice not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	var client models.Client
	err = h.db.QueryRow(`
		SELECT id, name, COALESCE(address,''), COALESCE(city,''), COALESCE(state,''),
		       COALESCE(zip_code,''), COALESCE(country,'')
		FROM clients WHERE id = ? AND user_id = ?
	`, inv.ClientID, userID).Scan(&client.ID, &client.Name, &client.Address, &client.City, &client.State, &client.ZipCode, &client.Country)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	rows, err := h.db.Query(`
		SELECT te.id, te.date, te.hours, te.description, ct.hourly_rate, ct.currency,
		       ct.id, ct.contract_number, ct.name, COALESCE(ct.payment_terms,'')
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		WHERE te.invoice_id = ? AND te.user_id = ?
		ORDER BY te.date
	`, inv.ID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var entries []models.TimeEntry
	for rows.Next() {
		var e models.TimeEntry
		var rate float64
		var currency string
		var contract models.Contract
		if err := rows.Scan(&e.ID, &e.Date, &e.Hours, &e.Description, &rate, &currency,
			&contract.ID, &contract.ContractNumber, &contract.Name, &contract.PaymentTerms); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		contract.HourlyRate = rate
		contract.Currency = currency
		e.Contract = &contract
		e.ContractID = contract.ID
		entries = append(entries, e)
	}

	paymentDetails := ResolveInvoicePaymentDetails(h.db, userID, inv.ID, inv.ClientID)

	var recipients []models.Recipient
	recRows, _ := h.db.Query(`
		SELECT name, email, COALESCE(title,''), COALESCE(phone,'')
		FROM recipients
		WHERE client_id = ?
		  AND client_id IN (SELECT id FROM clients WHERE user_id = ?)
		ORDER BY is_primary DESC
	`, inv.ClientID, userID)
	if recRows != nil {
		for recRows.Next() {
			var rcp models.Recipient
			recRows.Scan(&rcp.Name, &rcp.Email, &rcp.Title, &rcp.Phone)
			recipients = append(recipients, rcp)
		}
		recRows.Close()
	}

	business := loadBusinessInfo(h.db, userID)

	invoice := models.Invoice{
		ID:            int(inv.ID),
		ClientID:      inv.ClientID,
		InvoiceNumber: number,
		IssueDate:     inv.IssueDate,
		DueDate:       inv.DueDate,
		TotalAmount:   inv.TotalAmount,
		Status:        inv.Status,
		Client:        &client,
		TimeEntries:   entries,
	}

	generator := pdf.NewInvoiceGenerator()
	bytes, err := generator.GenerateBytes(invoice, paymentDetails, recipients, business)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("generate PDF: %w", err))
		return
	}

	filename := fmt.Sprintf("invoice_%s_%s.pdf", number, inv.IssueDate.Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(bytes)
}
