package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/austin/hours-mcp/internal/models"
	"github.com/austin/hours-mcp/internal/pdf"
	"github.com/austin/hours-mcp/internal/timeparse"
	"github.com/google/uuid"
)

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

func (h *handlers) clientIDByName(name string) (int, error) {
	var id int
	err := h.db.QueryRow("SELECT id FROM clients WHERE name = ?", name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, newAPIError(http.StatusNotFound, "client '%s' not found", name)
	}
	return id, err
}

// ---------- Stats ----------

func (h *handlers) getStats(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	s := statsDTO{}

	_ = h.db.QueryRow(`SELECT COUNT(*) FROM clients`).Scan(&s.TotalClients)
	_ = h.db.QueryRow(`SELECT COUNT(*) FROM contracts WHERE status = 'active'`).Scan(&s.ActiveContracts)

	_ = h.db.QueryRow(`
		SELECT COALESCE(SUM(te.hours), 0), COALESCE(SUM(te.hours * ct.hourly_rate), 0)
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		WHERE te.invoice_id IS NULL
	`).Scan(&s.UnbilledHours, &s.UnbilledAmount)

	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	lastMonthStart := monthStart.AddDate(0, -1, 0)

	_ = h.db.QueryRow(`
		SELECT COALESCE(SUM(hours), 0) FROM time_entries WHERE date >= ?
	`, monthStart.Format("2006-01-02")).Scan(&s.HoursThisMonth)

	_ = h.db.QueryRow(`
		SELECT COALESCE(SUM(hours), 0) FROM time_entries WHERE date >= ? AND date < ?
	`, lastMonthStart.Format("2006-01-02"), monthStart.Format("2006-01-02")).Scan(&s.HoursLastMonth)

	_ = h.db.QueryRow(`
		SELECT COALESCE(SUM(total_amount), 0), COUNT(*) FROM invoices WHERE status IN ('pending','sent','overdue')
	`).Scan(&s.OutstandingAmount, &s.InvoicesPending)

	_ = h.db.QueryRow(`
		SELECT COALESCE(SUM(total_amount), 0), COUNT(*) FROM invoices WHERE status = 'paid'
	`).Scan(&s.PaidAmount, &s.InvoicesPaid)

	entries, err := h.queryTimeEntries(`
		SELECT te.id, te.contract_id, te.date, te.hours, te.description, te.invoice_id, te.created_at,
		       cl.id, cl.name, ct.contract_number, ct.name, ct.hourly_rate, ct.currency, i.invoice_number
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		JOIN clients cl ON ct.client_id = cl.id
		LEFT JOIN invoices i ON te.invoice_id = i.id
		ORDER BY te.date DESC, te.created_at DESC
		LIMIT 10
	`)
	if err == nil {
		s.RecentEntries = entries
	} else {
		s.RecentEntries = []timeEntryDTO{}
	}

	return s, nil
}

// ---------- Clients ----------

func (h *handlers) listClients(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	rows, err := h.db.Query(`
		SELECT c.id, c.name, COALESCE(c.address,''), COALESCE(c.city,''), COALESCE(c.state,''),
		       COALESCE(c.zip_code,''), COALESCE(c.country,''), c.created_at, c.updated_at,
		       COALESCE((SELECT COUNT(*) FROM contracts WHERE client_id = c.id AND status = 'active'), 0)
		FROM clients c
		ORDER BY c.name
	`)
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
	var req addClientReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, newAPIError(http.StatusBadRequest, "name is required")
	}
	res, err := h.db.Exec(`
		INSERT INTO clients (name, address, city, state, zip_code, country)
		VALUES (?, ?, ?, ?, ?, ?)
	`, req.Name, req.Address, req.City, req.State, req.ZipCode, req.Country)
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
	args = append(args, id)
	q := fmt.Sprintf("UPDATE clients SET %s WHERE id = ?", strings.Join(sets, ", "))
	if _, err := h.db.Exec(q, args...); err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": id}, nil
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
	id, err := pathInt(r, "id")
	if err != nil {
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
	clientID, err := pathInt(r, "id")
	if err != nil {
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
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}
	res, err := h.db.Exec(`DELETE FROM recipients WHERE id = ?`, id)
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
	id, err := pathInt(r, "id")
	if err != nil {
		return nil, err
	}
	var pd models.PaymentDetails
	err = h.db.QueryRow(`
		SELECT id, client_id, COALESCE(bank_name,''), COALESCE(account_number,''), COALESCE(routing_number,''),
		       COALESCE(swift_code,''), COALESCE(payment_terms,''), COALESCE(notes,''), updated_at
		FROM payment_details WHERE client_id = ?
	`, id).Scan(&pd.ID, &pd.ClientID, &pd.BankName, &pd.AccountNumber, &pd.RoutingNumber,
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
	id, err := pathInt(r, "id")
	if err != nil {
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
	q := `
		SELECT c.id, c.client_id, c.contract_number, c.name, c.hourly_rate, c.currency, c.contract_type,
		       c.start_date, c.end_date, c.status, COALESCE(c.payment_terms,''), COALESCE(c.notes,''),
		       c.created_at, c.updated_at, cl.name
		FROM contracts c
		JOIN clients cl ON c.client_id = cl.id
		WHERE 1=1
	`
	args := []interface{}{}
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
		if err := rows.Scan(&c.ID, &c.ClientID, &c.ContractNumber, &c.Name, &c.HourlyRate, &c.Currency,
			&c.ContractType, &c.StartDate, &endDate, &c.Status, &c.PaymentTerms, &c.Notes,
			&c.CreatedAt, &c.UpdatedAt, &c.ClientName); err != nil {
			return nil, err
		}
		if endDate.Valid {
			t, _ := time.Parse("2006-01-02", endDate.String)
			c.EndDate = &t
		}
		out = append(out, c)
	}
	return out, nil
}

type addContractReq struct {
	ClientID       int     `json:"client_id"`
	ContractNumber string  `json:"contract_number"`
	Name           string  `json:"name"`
	HourlyRate     float64 `json:"hourly_rate"`
	Currency       string  `json:"currency,omitempty"`
	ContractType   string  `json:"contract_type,omitempty"`
	StartDate      string  `json:"start_date"`
	EndDate        string  `json:"end_date,omitempty"`
	PaymentTerms   string  `json:"payment_terms,omitempty"`
	Notes          string  `json:"notes,omitempty"`
	Status         string  `json:"status,omitempty"`
}

func (h *handlers) addContract(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	var req addContractReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if req.ClientID == 0 || req.ContractNumber == "" || req.Name == "" || req.StartDate == "" {
		return nil, newAPIError(http.StatusBadRequest, "client_id, contract_number, name, start_date required")
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
	var id int64
	err = h.db.QueryRow(`
		INSERT INTO contracts (client_id, contract_number, name, hourly_rate, currency, contract_type,
		                      start_date, end_date, status, payment_terms, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, req.ClientID, req.ContractNumber, req.Name, req.HourlyRate, req.Currency, req.ContractType,
		start.Format("2006-01-02"), endPtr, req.Status, req.PaymentTerms, req.Notes).Scan(&id)
	if err != nil {
		return nil, err
	}
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
	q := `
		SELECT te.id, te.contract_id, te.date, te.hours, te.description, te.invoice_id, te.created_at,
		       cl.id, cl.name, ct.contract_number, ct.name, ct.hourly_rate, ct.currency, i.invoice_number
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		JOIN clients cl ON ct.client_id = cl.id
		LEFT JOIN invoices i ON te.invoice_id = i.id
		WHERE 1=1
	`
	args := []interface{}{}

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
		q += " LIMIT " + v
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
	var req addTimeEntryReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	contractID, clientID, err := h.resolveContract(req.ContractID, req.ContractNumber)
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
		INSERT INTO time_entries (id, client_id, contract_id, date, hours, description)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, clientID, contractID, date.Format("2006-01-02"), req.Hours, req.Description)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": id}, nil
}

func (h *handlers) resolveContract(id int, number string) (int, int, error) {
	if id != 0 {
		var clientID int
		err := h.db.QueryRow(`SELECT client_id FROM contracts WHERE id = ? AND status = 'active'`, id).Scan(&clientID)
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
		err := h.db.QueryRow(`SELECT id, client_id FROM contracts WHERE contract_number = ? AND status = 'active'`, number).Scan(&contractID, &clientID)
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
		contractID, clientID, err := h.resolveContract(e.ContractID, e.ContractNumber)
		if err != nil {
			return nil, err
		}
		date, err := parseDate(e.Date)
		if err != nil {
			return nil, err
		}
		id := uuid.New().String()
		if _, err := tx.Exec(`
			INSERT INTO time_entries (id, client_id, contract_id, date, hours, description)
			VALUES (?, ?, ?, ?, ?, ?)
		`, id, clientID, contractID, date.Format("2006-01-02"), e.Hours, e.Description); err != nil {
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
	id := r.PathValue("id")
	var existing struct {
		InvoiceID *int
	}
	err := h.db.QueryRow(`SELECT invoice_id FROM time_entries WHERE id = ?`, id).Scan(&existing.InvoiceID)
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
	args = append(args, id)
	if _, err := h.db.Exec(fmt.Sprintf("UPDATE time_entries SET %s WHERE id = ?", strings.Join(sets, ", ")), args...); err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": id}, nil
}

func (h *handlers) deleteTimeEntry(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	id := r.PathValue("id")
	res, err := h.db.Exec(`DELETE FROM time_entries WHERE id = ?`, id)
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
		res, err := tx.Exec(`DELETE FROM time_entries WHERE id = ?`, id)
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
	var req markInvoicedReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	var invoiceID int
	err := h.db.QueryRow(`SELECT id FROM invoices WHERE invoice_number = ?`, req.InvoiceNumber).Scan(&invoiceID)
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
		if err := tx.QueryRow(`SELECT invoice_id FROM time_entries WHERE id = ?`, id).Scan(&existing); err != nil {
			continue
		}
		if existing != nil {
			return nil, newAPIError(http.StatusConflict, "entry %s is already invoiced", id)
		}
		res, err := tx.Exec(`UPDATE time_entries SET invoice_id = ? WHERE id = ?`, invoiceID, id)
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
		res, err := tx.Exec(`UPDATE time_entries SET invoice_id = NULL WHERE id = ?`, id)
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
	q := `
		SELECT i.id, i.invoice_number, i.client_id, c.name, i.issue_date, i.due_date,
		       i.total_amount, i.status, COALESCE(i.pdf_path,''), i.created_at
		FROM invoices i
		JOIN clients c ON i.client_id = c.id
		WHERE 1=1
	`
	args := []interface{}{}
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
	Invoice     invoiceDTO     `json:"invoice"`
	TimeEntries []timeEntryDTO `json:"time_entries"`
	TotalHours  float64        `json:"total_hours"`
}

func (h *handlers) getInvoiceDetails(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	number := r.PathValue("number")
	var inv invoiceDTO
	err := h.db.QueryRow(`
		SELECT i.id, i.invoice_number, i.client_id, c.name, i.issue_date, i.due_date,
		       i.total_amount, i.status, COALESCE(i.pdf_path,''), i.created_at
		FROM invoices i
		JOIN clients c ON i.client_id = c.id
		WHERE i.invoice_number = ?
	`, number).Scan(&inv.ID, &inv.InvoiceNumber, &inv.ClientID, &inv.ClientName,
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
		WHERE te.invoice_id = ?
		ORDER BY te.date
	`, inv.ID)
	if err != nil {
		return nil, err
	}
	total := 0.0
	for _, e := range entries {
		total += e.Hours
	}
	return invoiceDetailsResponse{Invoice: inv, TimeEntries: entries, TotalHours: total}, nil
}

type createInvoiceReq struct {
	ClientID  int    `json:"client_id"`
	Period    string `json:"period"`
	DueDays   int    `json:"due_days,omitempty"`
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
}

func (h *handlers) createInvoice(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	var req createInvoiceReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if req.ClientID == 0 {
		return nil, newAPIError(http.StatusBadRequest, "client_id required")
	}
	if req.DueDays == 0 {
		req.DueDays = 30
	}

	// Business info validation
	var businessName string
	err := h.db.QueryRow(`SELECT business_name FROM business_info WHERE id = 1`).Scan(&businessName)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusPreconditionFailed, "business info not configured")
	}
	if err != nil {
		return nil, err
	}

	// Payment details validation
	var paymentBank string
	err = h.db.QueryRow(`SELECT COALESCE(bank_name,'') FROM payment_details WHERE client_id = ?`, req.ClientID).Scan(&paymentBank)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusPreconditionFailed, "payment details not configured for client")
	}
	if err != nil {
		return nil, err
	}

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
		FROM clients WHERE id = ?
	`, req.ClientID).Scan(&client.ID, &client.Name, &client.Address, &client.City, &client.State, &client.ZipCode, &client.Country)
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
		WHERE ct.client_id = ? AND te.date >= ? AND te.date <= ? AND te.invoice_id IS NULL
		ORDER BY te.date
	`, req.ClientID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
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
	if len(entries) == 0 {
		return nil, newAPIError(http.StatusPreconditionFailed, "no unbilled hours in period")
	}

	invoiceNumber := fmt.Sprintf("INV-%s-%s", time.Now().Format("200601"), uuid.New().String()[:8])
	issueDate := time.Now()
	dueDate := issueDate.AddDate(0, 0, req.DueDays)

	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO invoices (client_id, invoice_number, issue_date, due_date, total_amount, status)
		VALUES (?, ?, ?, ?, ?, 'pending')
	`, req.ClientID, invoiceNumber, issueDate.Format("2006-01-02"), dueDate.Format("2006-01-02"), totalAmount)
	if err != nil {
		return nil, err
	}
	invoiceID, _ := res.LastInsertId()

	for _, e := range entries {
		if _, err := tx.Exec(`UPDATE time_entries SET invoice_id = ? WHERE id = ?`, invoiceID, e.ID); err != nil {
			return nil, err
		}
	}

	// Gather info for PDF
	var paymentDetails models.PaymentDetails
	tx.QueryRow(`
		SELECT COALESCE(bank_name,''), COALESCE(account_number,''), COALESCE(routing_number,''),
		       COALESCE(swift_code,''), COALESCE(payment_terms,''), COALESCE(notes,'')
		FROM payment_details WHERE client_id = ?
	`, req.ClientID).Scan(&paymentDetails.BankName, &paymentDetails.AccountNumber,
		&paymentDetails.RoutingNumber, &paymentDetails.SwiftCode,
		&paymentDetails.PaymentTerms, &paymentDetails.Notes)

	var recipients []models.Recipient
	recRows, _ := tx.Query(`
		SELECT name, email, COALESCE(title,''), COALESCE(phone,'')
		FROM recipients WHERE client_id = ? ORDER BY is_primary DESC
	`, req.ClientID)
	if recRows != nil {
		for recRows.Next() {
			var rcp models.Recipient
			recRows.Scan(&rcp.Name, &rcp.Email, &rcp.Title, &rcp.Phone)
			recipients = append(recipients, rcp)
		}
		recRows.Close()
	}

	var business models.BusinessInfo
	tx.QueryRow(`
		SELECT id, business_name, contact_name, email, COALESCE(phone,''), COALESCE(address,''),
		       COALESCE(city,''), COALESCE(state,''), COALESCE(zip_code,''), COALESCE(country,''),
		       COALESCE(tax_id,''), COALESCE(website,''), COALESCE(logo_path,''), COALESCE(invoice_prefix,'INV'),
		       COALESCE(export_path,''), updated_at
		FROM business_info WHERE id = 1
	`).Scan(&business.ID, &business.BusinessName, &business.ContactName, &business.Email,
		&business.Phone, &business.Address, &business.City, &business.State,
		&business.ZipCode, &business.Country, &business.TaxID, &business.Website,
		&business.LogoPath, &business.InvoicePrefix, &business.ExportPath, &business.UpdatedAt)

	exportDir := resolveExportDir(business.ExportPath)
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
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
	}

	generator := pdf.NewInvoiceGenerator()
	if err := generator.Generate(invoice, paymentDetails, recipients, business, pdfPath); err != nil {
		return nil, newAPIError(http.StatusInternalServerError, "failed to generate PDF: %s", err)
	}

	if _, err := tx.Exec(`UPDATE invoices SET pdf_path = ? WHERE id = ?`, pdfPath, invoiceID); err != nil {
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
		"pdf_path":       pdfPath,
	}, nil
}

type updateInvoiceStatusReq struct {
	Status string `json:"status"`
}

func (h *handlers) updateInvoiceStatus(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	number := r.PathValue("number")
	var req updateInvoiceStatusReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	valid := map[string]bool{"draft": true, "pending": true, "sent": true, "paid": true, "overdue": true, "cancelled": true}
	if !valid[req.Status] {
		return nil, newAPIError(http.StatusBadRequest, "invalid status")
	}
	res, err := h.db.Exec(`UPDATE invoices SET status = ? WHERE invoice_number = ?`, req.Status, number)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, newAPIError(http.StatusNotFound, "invoice not found")
	}
	BroadcastEvent("invoice.updated", map[string]any{
		"source":         "api",
		"invoice_number": number,
		"status":         req.Status,
	})
	return map[string]interface{}{"status": req.Status}, nil
}

func (h *handlers) deleteInvoice(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	number := r.PathValue("number")

	var id int64
	var status, pdfPath string
	err := h.db.QueryRow(
		`SELECT id, status, COALESCE(pdf_path,'') FROM invoices WHERE invoice_number = ?`,
		number,
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
		`UPDATE time_entries SET invoice_id = NULL WHERE invoice_id = ?`, id,
	); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM invoices WHERE id = ?`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if pdfPath != "" {
		_ = os.Remove(pdfPath)
	}

	BroadcastEvent("invoice.deleted", map[string]any{
		"source":         "api",
		"invoice_number": number,
	})
	return map[string]interface{}{"deleted": number}, nil
}

// ---------- Business Info ----------

func (h *handlers) getBusinessInfo(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	var b models.BusinessInfo
	err := h.db.QueryRow(`
		SELECT id, business_name, contact_name, email, COALESCE(phone,''), COALESCE(address,''),
		       COALESCE(city,''), COALESCE(state,''), COALESCE(zip_code,''), COALESCE(country,''),
		       COALESCE(tax_id,''), COALESCE(website,''), COALESCE(logo_path,''), COALESCE(invoice_prefix,'INV'),
		       COALESCE(export_path,''), updated_at
		FROM business_info WHERE id = 1
	`).Scan(&b.ID, &b.BusinessName, &b.ContactName, &b.Email, &b.Phone, &b.Address, &b.City, &b.State,
		&b.ZipCode, &b.Country, &b.TaxID, &b.Website, &b.LogoPath, &b.InvoicePrefix, &b.ExportPath, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
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
	LogoPath      string `json:"logo_path,omitempty"`
	InvoicePrefix string `json:"invoice_prefix,omitempty"`
	ExportPath    string `json:"export_path,omitempty"`
}

func (h *handlers) setBusinessInfo(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	var req businessInfoReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if req.InvoicePrefix == "" {
		req.InvoicePrefix = "INV"
	}
	_, err := h.db.Exec(`
		INSERT INTO business_info (id, business_name, contact_name, email, phone, address, city, state,
		                         zip_code, country, tax_id, website, logo_path, invoice_prefix, export_path, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
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
	`, req.BusinessName, req.ContactName, req.Email, req.Phone, req.Address, req.City, req.State,
		req.ZipCode, req.Country, req.TaxID, req.Website, req.LogoPath, req.InvoicePrefix, req.ExportPath, time.Now())
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

func resolveExportDir(configured string) string {
	home, _ := os.UserHomeDir()
	p := strings.TrimSpace(configured)
	if p == "" {
		return filepath.Join(home, "Downloads")
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, p[2:])
	} else if p == "~" {
		p = home
	}
	return p
}

// ---------- Invoice download ----------

func (h *handlers) downloadInvoice(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	number := r.PathValue("number")

	var inv struct {
		ID          int64
		ClientID    int
		IssueDate   time.Time
		DueDate     time.Time
		TotalAmount float64
		Status      string
		PDFPath     string
	}
	err := h.db.QueryRow(`
		SELECT id, client_id, issue_date, due_date, total_amount, status, COALESCE(pdf_path,'')
		FROM invoices WHERE invoice_number = ?
	`, number).Scan(&inv.ID, &inv.ClientID, &inv.IssueDate, &inv.DueDate, &inv.TotalAmount, &inv.Status, &inv.PDFPath)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "invoice not found")
	}
	if err != nil {
		return nil, err
	}

	var client models.Client
	err = h.db.QueryRow(`
		SELECT id, name, COALESCE(address,''), COALESCE(city,''), COALESCE(state,''),
		       COALESCE(zip_code,''), COALESCE(country,'')
		FROM clients WHERE id = ?
	`, inv.ClientID).Scan(&client.ID, &client.Name, &client.Address, &client.City, &client.State, &client.ZipCode, &client.Country)
	if err != nil {
		return nil, err
	}

	rows, err := h.db.Query(`
		SELECT te.id, te.date, te.hours, te.description, ct.hourly_rate, ct.currency,
		       ct.id, ct.contract_number, ct.name, COALESCE(ct.payment_terms,'')
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		WHERE te.invoice_id = ?
		ORDER BY te.date
	`, inv.ID)
	if err != nil {
		return nil, err
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
			return nil, err
		}
		contract.HourlyRate = rate
		contract.Currency = currency
		e.Contract = &contract
		e.ContractID = contract.ID
		entries = append(entries, e)
	}

	var paymentDetails models.PaymentDetails
	h.db.QueryRow(`
		SELECT COALESCE(bank_name,''), COALESCE(account_number,''), COALESCE(routing_number,''),
		       COALESCE(swift_code,''), COALESCE(payment_terms,''), COALESCE(notes,'')
		FROM payment_details WHERE client_id = ?
	`, inv.ClientID).Scan(&paymentDetails.BankName, &paymentDetails.AccountNumber,
		&paymentDetails.RoutingNumber, &paymentDetails.SwiftCode,
		&paymentDetails.PaymentTerms, &paymentDetails.Notes)

	var recipients []models.Recipient
	recRows, _ := h.db.Query(`
		SELECT name, email, COALESCE(title,''), COALESCE(phone,'')
		FROM recipients WHERE client_id = ? ORDER BY is_primary DESC
	`, inv.ClientID)
	if recRows != nil {
		for recRows.Next() {
			var rcp models.Recipient
			recRows.Scan(&rcp.Name, &rcp.Email, &rcp.Title, &rcp.Phone)
			recipients = append(recipients, rcp)
		}
		recRows.Close()
	}

	var business models.BusinessInfo
	h.db.QueryRow(`
		SELECT id, business_name, contact_name, email, COALESCE(phone,''), COALESCE(address,''),
		       COALESCE(city,''), COALESCE(state,''), COALESCE(zip_code,''), COALESCE(country,''),
		       COALESCE(tax_id,''), COALESCE(website,''), COALESCE(logo_path,''), COALESCE(invoice_prefix,'INV'),
		       COALESCE(export_path,''), updated_at
		FROM business_info WHERE id = 1
	`).Scan(&business.ID, &business.BusinessName, &business.ContactName, &business.Email,
		&business.Phone, &business.Address, &business.City, &business.State,
		&business.ZipCode, &business.Country, &business.TaxID, &business.Website,
		&business.LogoPath, &business.InvoicePrefix, &business.ExportPath, &business.UpdatedAt)

	exportDir := resolveExportDir(business.ExportPath)
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		return nil, newAPIError(http.StatusInternalServerError, "failed to create export dir: %s", err)
	}
	pdfPath := filepath.Join(exportDir, fmt.Sprintf("invoice_%s_%s.pdf", number, inv.IssueDate.Format("2006-01-02")))

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
	if err := generator.Generate(invoice, paymentDetails, recipients, business, pdfPath); err != nil {
		return nil, newAPIError(http.StatusInternalServerError, "failed to generate PDF: %s", err)
	}

	if _, err := h.db.Exec(`UPDATE invoices SET pdf_path = ? WHERE id = ?`, pdfPath, inv.ID); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"invoice_number": number,
		"pdf_path":       pdfPath,
		"export_dir":     exportDir,
	}, nil
}
