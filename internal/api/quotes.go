package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/austin/hours-mcp/internal/models"
	"github.com/austin/hours-mcp/internal/pdf"
	"github.com/google/uuid"
)

// ---------- DTOs ----------

type quoteDTO struct {
	ID                  int       `json:"id"`
	QuoteNumber         string    `json:"quote_number"`
	ClientID            int       `json:"client_id"`
	ClientName          string    `json:"client_name"`
	Title               string    `json:"title"`
	IssueDate           time.Time `json:"issue_date"`
	ValidUntil          time.Time `json:"valid_until"`
	Subtotal            float64   `json:"subtotal"`
	TotalAmount         float64   `json:"total_amount"`
	Currency            string    `json:"currency"`
	Status              string    `json:"status"`
	Notes               string    `json:"notes,omitempty"`
	PDFPath             string    `json:"pdf_path,omitempty"`
	ConvertedContractID *int      `json:"converted_contract_id,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type quoteLineItemDTO struct {
	ID          int     `json:"id"`
	QuoteID     int     `json:"quote_id"`
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	Unit        string  `json:"unit"`
	UnitPrice   float64 `json:"unit_price"`
	Amount      float64 `json:"amount"`
	SortOrder   int     `json:"sort_order"`
}

type quoteDetailsResponse struct {
	Quote     quoteDTO           `json:"quote"`
	LineItems []quoteLineItemDTO `json:"line_items"`
}

// ---------- Queries ----------

func scanQuote(row interface {
	Scan(dest ...any) error
}) (quoteDTO, error) {
	var q quoteDTO
	var convID sql.NullInt64
	var notes sql.NullString
	var pdfPath sql.NullString
	err := row.Scan(
		&q.ID, &q.QuoteNumber, &q.ClientID, &q.ClientName,
		&q.Title, &q.IssueDate, &q.ValidUntil,
		&q.Subtotal, &q.TotalAmount, &q.Currency, &q.Status,
		&notes, &pdfPath, &convID, &q.CreatedAt, &q.UpdatedAt,
	)
	if err != nil {
		return q, err
	}
	if notes.Valid {
		q.Notes = notes.String
	}
	if pdfPath.Valid {
		q.PDFPath = pdfPath.String
	}
	if convID.Valid {
		v := int(convID.Int64)
		q.ConvertedContractID = &v
	}
	return q, nil
}

const quoteSelect = `
	SELECT q.id, q.quote_number, q.client_id, c.name,
	       q.title, q.issue_date, q.valid_until,
	       q.subtotal, q.total_amount, q.currency, q.status,
	       q.notes, q.pdf_path, q.converted_contract_id,
	       q.created_at, q.updated_at
	FROM quotes q
	JOIN clients c ON q.client_id = c.id
`

// ---------- List ----------

func (h *handlers) listQuotes(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	q := quoteSelect + " WHERE q.user_id = ?"
	args := []interface{}{userID}
	qv := r.URL.Query()
	if v := qv.Get("client_id"); v != "" {
		q += " AND q.client_id = ?"
		args = append(args, v)
	}
	if v := qv.Get("status"); v != "" {
		q += " AND q.status = ?"
		args = append(args, v)
	}
	q += " ORDER BY q.issue_date DESC, q.created_at DESC"

	rows, err := h.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []quoteDTO{}
	for rows.Next() {
		dto, err := scanQuote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}

// ---------- Get ----------

func (h *handlers) getQuoteDetails(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	number := r.PathValue("number")
	row := h.db.QueryRow(quoteSelect+" WHERE q.quote_number = ? AND q.user_id = ?", number, userID)
	q, err := scanQuote(row)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "quote not found")
	}
	if err != nil {
		return nil, err
	}

	liRows, err := h.db.Query(`
		SELECT id, quote_id, description, quantity, unit, unit_price, amount, sort_order
		FROM quote_line_items
		WHERE quote_id = ?
		  AND quote_id IN (SELECT id FROM quotes WHERE user_id = ?)
		ORDER BY sort_order, id
	`, q.ID, userID)
	if err != nil {
		return nil, err
	}
	defer liRows.Close()
	items := []quoteLineItemDTO{}
	for liRows.Next() {
		var li quoteLineItemDTO
		if err := liRows.Scan(&li.ID, &li.QuoteID, &li.Description, &li.Quantity,
			&li.Unit, &li.UnitPrice, &li.Amount, &li.SortOrder); err != nil {
			return nil, err
		}
		items = append(items, li)
	}
	return quoteDetailsResponse{Quote: q, LineItems: items}, nil
}

// ---------- Create ----------

type quoteLineItemReq struct {
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	Unit        string  `json:"unit,omitempty"`
	UnitPrice   float64 `json:"unit_price"`
}

type createQuoteReq struct {
	ClientID   int                `json:"client_id"`
	Title      string             `json:"title"`
	IssueDate  string             `json:"issue_date,omitempty"`
	ValidUntil string             `json:"valid_until,omitempty"`
	ValidDays  int                `json:"valid_days,omitempty"`
	Currency   string             `json:"currency,omitempty"`
	Notes      string             `json:"notes,omitempty"`
	LineItems  []quoteLineItemReq `json:"line_items"`
}

func (h *handlers) createQuote(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	var req createQuoteReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if req.ClientID == 0 {
		return nil, newAPIError(http.StatusBadRequest, "client_id required")
	}
	if _, err := h.requireClient(userID, req.ClientID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Title) == "" {
		return nil, newAPIError(http.StatusBadRequest, "title required")
	}
	if len(req.LineItems) == 0 {
		return nil, newAPIError(http.StatusBadRequest, "at least one line item required")
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	if req.ValidDays <= 0 {
		req.ValidDays = 30
	}

	issueDate := time.Now()
	if req.IssueDate != "" {
		t, err := parseDate(req.IssueDate)
		if err != nil {
			return nil, err
		}
		issueDate = t
	}

	var validUntil time.Time
	if req.ValidUntil != "" {
		t, err := parseDate(req.ValidUntil)
		if err != nil {
			return nil, err
		}
		validUntil = t
	} else {
		validUntil = issueDate.AddDate(0, 0, req.ValidDays)
	}

	// Compute totals
	subtotal := 0.0
	normalized := make([]quoteLineItemReq, 0, len(req.LineItems))
	for _, li := range req.LineItems {
		if strings.TrimSpace(li.Description) == "" {
			return nil, newAPIError(http.StatusBadRequest, "line item description required")
		}
		if li.Quantity <= 0 {
			return nil, newAPIError(http.StatusBadRequest, "line item quantity must be > 0")
		}
		if li.UnitPrice < 0 {
			return nil, newAPIError(http.StatusBadRequest, "line item unit_price must be >= 0")
		}
		if li.Unit == "" {
			li.Unit = "hours"
		}
		subtotal += li.Quantity * li.UnitPrice
		normalized = append(normalized, li)
	}

	// Validate client exists (already user-scoped above)
	var clientName string
	err = h.db.QueryRow(`SELECT name FROM clients WHERE id = ? AND user_id = ?`, req.ClientID, userID).Scan(&clientName)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "client not found")
	}
	if err != nil {
		return nil, err
	}

	quoteNumber := fmt.Sprintf("QT-%s-%s", time.Now().Format("200601"), uuid.New().String()[:8])

	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO quotes (user_id, client_id, quote_number, title, issue_date, valid_until,
		                   subtotal, total_amount, currency, status, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'draft', ?)
	`, userID, req.ClientID, quoteNumber, req.Title,
		issueDate.Format("2006-01-02"), validUntil.Format("2006-01-02"),
		subtotal, subtotal, req.Currency, req.Notes)
	if err != nil {
		return nil, err
	}
	quoteID, _ := res.LastInsertId()

	for idx, li := range normalized {
		amount := li.Quantity * li.UnitPrice
		if _, err := tx.Exec(`
			INSERT INTO quote_line_items (quote_id, description, quantity, unit, unit_price, amount, sort_order)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, quoteID, li.Description, li.Quantity, li.Unit, li.UnitPrice, amount, idx); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	BroadcastUserEvent(userID, "quote.created", map[string]any{
		"source":       "api",
		"quote_number": quoteNumber,
		"client_id":    req.ClientID,
		"client_name":  clientName,
		"total_amount": subtotal,
	})

	return map[string]interface{}{
		"id":           quoteID,
		"quote_number": quoteNumber,
		"total_amount": subtotal,
	}, nil
}

// ---------- Update (title / notes / valid_until / line items) ----------

type updateQuoteReq struct {
	Title      *string             `json:"title,omitempty"`
	Notes      *string             `json:"notes,omitempty"`
	ValidUntil *string             `json:"valid_until,omitempty"`
	Currency   *string             `json:"currency,omitempty"`
	LineItems  *[]quoteLineItemReq `json:"line_items,omitempty"`
}

func (h *handlers) updateQuote(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	number := r.PathValue("number")
	var existing struct {
		ID     int
		Status string
	}
	err = h.db.QueryRow(`SELECT id, status FROM quotes WHERE quote_number = ? AND user_id = ?`, number, userID).Scan(&existing.ID, &existing.Status)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "quote not found")
	}
	if err != nil {
		return nil, err
	}
	if existing.Status != "draft" {
		return nil, newAPIError(http.StatusConflict, "only draft quotes can be edited")
	}

	var req updateQuoteReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}

	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	sets := []string{}
	args := []interface{}{}
	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			return nil, newAPIError(http.StatusBadRequest, "title cannot be empty")
		}
		sets = append(sets, "title = ?")
		args = append(args, *req.Title)
	}
	if req.Notes != nil {
		sets = append(sets, "notes = ?")
		args = append(args, *req.Notes)
	}
	if req.ValidUntil != nil {
		t, err := parseDate(*req.ValidUntil)
		if err != nil {
			return nil, err
		}
		sets = append(sets, "valid_until = ?")
		args = append(args, t.Format("2006-01-02"))
	}
	if req.Currency != nil {
		sets = append(sets, "currency = ?")
		args = append(args, *req.Currency)
	}

	// Recompute totals if line items given
	if req.LineItems != nil {
		if len(*req.LineItems) == 0 {
			return nil, newAPIError(http.StatusBadRequest, "at least one line item required")
		}
		if _, err := tx.Exec(`DELETE FROM quote_line_items WHERE quote_id = ? AND quote_id IN (SELECT id FROM quotes WHERE user_id = ?)`, existing.ID, userID); err != nil {
			return nil, err
		}
		subtotal := 0.0
		for idx, li := range *req.LineItems {
			if strings.TrimSpace(li.Description) == "" {
				return nil, newAPIError(http.StatusBadRequest, "line item description required")
			}
			if li.Quantity <= 0 {
				return nil, newAPIError(http.StatusBadRequest, "line item quantity must be > 0")
			}
			if li.Unit == "" {
				li.Unit = "hours"
			}
			amount := li.Quantity * li.UnitPrice
			subtotal += amount
			if _, err := tx.Exec(`
				INSERT INTO quote_line_items (quote_id, description, quantity, unit, unit_price, amount, sort_order)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, existing.ID, li.Description, li.Quantity, li.Unit, li.UnitPrice, amount, idx); err != nil {
				return nil, err
			}
		}
		sets = append(sets, "subtotal = ?", "total_amount = ?")
		args = append(args, subtotal, subtotal)
	}

	if len(sets) == 0 {
		return nil, newAPIError(http.StatusBadRequest, "no fields provided")
	}
	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, existing.ID, userID)
	q := fmt.Sprintf("UPDATE quotes SET %s WHERE id = ? AND user_id = ?", strings.Join(sets, ", "))
	if _, err := tx.Exec(q, args...); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]interface{}{"quote_number": number}, nil
}

// ---------- Status ----------

type updateQuoteStatusReq struct {
	Status string `json:"status"`
}

func (h *handlers) updateQuoteStatus(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	number := r.PathValue("number")
	var req updateQuoteStatusReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	valid := map[string]bool{
		"draft": true, "sent": true, "accepted": true,
		"rejected": true, "expired": true, "converted": true,
	}
	if !valid[req.Status] {
		return nil, newAPIError(http.StatusBadRequest, "invalid status")
	}
	res, err := h.db.Exec(`UPDATE quotes SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE quote_number = ? AND user_id = ?`, req.Status, number, userID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, newAPIError(http.StatusNotFound, "quote not found")
	}
	BroadcastUserEvent(userID, "quote.updated", map[string]any{
		"source":       "api",
		"quote_number": number,
		"status":       req.Status,
	})
	return map[string]interface{}{"status": req.Status}, nil
}

// ---------- Delete ----------

func (h *handlers) deleteQuote(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	number := r.PathValue("number")
	var id int64
	var pdfPath sql.NullString
	var status string
	err = h.db.QueryRow(`SELECT id, status, pdf_path FROM quotes WHERE quote_number = ? AND user_id = ?`, number, userID).Scan(&id, &status, &pdfPath)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "quote not found")
	}
	if err != nil {
		return nil, err
	}
	if status == "converted" {
		return nil, newAPIError(http.StatusConflict, "converted quotes cannot be deleted")
	}

	if _, err := h.db.Exec(`DELETE FROM quotes WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return nil, err
	}
	if pdfPath.Valid && pdfPath.String != "" {
		_ = os.Remove(pdfPath.String)
	}
	BroadcastUserEvent(userID, "quote.deleted", map[string]any{
		"source":       "api",
		"quote_number": number,
	})
	return map[string]interface{}{"deleted": number}, nil
}

// ---------- Download PDF ----------

// downloadQuote streams the rendered quote PDF as the HTTP response body with
// a Content-Disposition: attachment header. Mirrors downloadInvoice — see that
// handler's comment for the rationale on why we no longer write the PDF to
// the server's filesystem.
func (h *handlers) downloadQuote(w http.ResponseWriter, r *http.Request) {
	userID, err := currentUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	number := r.PathValue("number")

	row := h.db.QueryRow(quoteSelect+" WHERE q.quote_number = ? AND q.user_id = ?", number, userID)
	dto, err := scanQuote(row)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, fmt.Errorf("quote not found"))
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
	`, dto.ClientID, userID).Scan(&client.ID, &client.Name, &client.Address, &client.City, &client.State, &client.ZipCode, &client.Country)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	itemRows, err := h.db.Query(`
		SELECT id, quote_id, description, quantity, unit, unit_price, amount, sort_order
		FROM quote_line_items
		WHERE quote_id = ?
		  AND quote_id IN (SELECT id FROM quotes WHERE user_id = ?)
		ORDER BY sort_order, id
	`, dto.ID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer itemRows.Close()
	var items []models.QuoteLineItem
	for itemRows.Next() {
		var li models.QuoteLineItem
		if err := itemRows.Scan(&li.ID, &li.QuoteID, &li.Description, &li.Quantity,
			&li.Unit, &li.UnitPrice, &li.Amount, &li.SortOrder); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		items = append(items, li)
	}

	// Defense-in-depth ownership re-check: see comment in invoicePreview.
	var recipients []models.Recipient
	recRows, _ := h.db.Query(`
		SELECT name, email, COALESCE(title,''), COALESCE(phone,'')
		FROM recipients
		WHERE client_id = ?
		  AND client_id IN (SELECT id FROM clients WHERE user_id = ?)
		ORDER BY is_primary DESC
	`, dto.ClientID, userID)
	if recRows != nil {
		for recRows.Next() {
			var rcp models.Recipient
			recRows.Scan(&rcp.Name, &rcp.Email, &rcp.Title, &rcp.Phone)
			recipients = append(recipients, rcp)
		}
		recRows.Close()
	}

	business := loadBusinessInfo(h.db, userID)

	quote := models.Quote{
		ID:          dto.ID,
		ClientID:    dto.ClientID,
		QuoteNumber: number,
		Title:       dto.Title,
		IssueDate:   dto.IssueDate,
		ValidUntil:  dto.ValidUntil,
		Subtotal:    dto.Subtotal,
		TotalAmount: dto.TotalAmount,
		Currency:    dto.Currency,
		Status:      dto.Status,
		Notes:       dto.Notes,
		Client:      &client,
		LineItems:   items,
	}

	generator := pdf.NewQuoteGenerator()
	bytes, err := generator.GenerateBytes(quote, recipients, business)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("generate PDF: %w", err))
		return
	}

	filename := fmt.Sprintf("quote_%s_%s.pdf", number, dto.IssueDate.Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(bytes)
}

// ---------- Convert to contract ----------

type convertQuoteReq struct {
	ContractNumber string `json:"contract_number"`
	ContractName   string `json:"contract_name,omitempty"`
	StartDate      string `json:"start_date,omitempty"`
	EndDate        string `json:"end_date,omitempty"`
	PaymentTerms   string `json:"payment_terms,omitempty"`
}

func (h *handlers) convertQuote(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	userID, err := currentUserID(r)
	if err != nil {
		return nil, err
	}
	number := r.PathValue("number")
	var req convertQuoteReq
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ContractNumber) == "" {
		return nil, newAPIError(http.StatusBadRequest, "contract_number required")
	}

	var qID int64
	var clientID int
	var status, currency, title string
	var totalAmount float64
	err = h.db.QueryRow(`
		SELECT id, client_id, status, currency, title, total_amount FROM quotes WHERE quote_number = ? AND user_id = ?
	`, number, userID).Scan(&qID, &clientID, &status, &currency, &title, &totalAmount)
	if err == sql.ErrNoRows {
		return nil, newAPIError(http.StatusNotFound, "quote not found")
	}
	if err != nil {
		return nil, err
	}
	if status != "accepted" {
		return nil, newAPIError(http.StatusConflict, "only accepted quotes can be converted (current: %s)", status)
	}

	// Derive the contract's hourly rate from the quote.
	// Prefer the first line item's unit_price when its unit is "hours";
	// fall back to total / total_quantity_in_hours, else the first line's unit_price.
	var rate float64
	var hourQty, hourAmt float64
	liRows, err := h.db.Query(`
		SELECT quantity, unit, unit_price, amount FROM quote_line_items
		WHERE quote_id = ?
		  AND quote_id IN (SELECT id FROM quotes WHERE user_id = ?)
		ORDER BY sort_order, id
	`, qID, userID)
	if err != nil {
		return nil, err
	}
	firstPrice := 0.0
	firstPriceSet := false
	for liRows.Next() {
		var qty, unitPrice, amount float64
		var unit string
		if err := liRows.Scan(&qty, &unit, &unitPrice, &amount); err != nil {
			liRows.Close()
			return nil, err
		}
		if !firstPriceSet {
			firstPrice = unitPrice
			firstPriceSet = true
		}
		if strings.EqualFold(unit, "hours") || strings.EqualFold(unit, "hour") || strings.EqualFold(unit, "hr") {
			hourQty += qty
			hourAmt += amount
		}
	}
	liRows.Close()
	if hourQty > 0 {
		rate = hourAmt / hourQty
	} else {
		rate = firstPrice
	}

	name := strings.TrimSpace(req.ContractName)
	if name == "" {
		name = title
	}
	start := time.Now()
	if req.StartDate != "" {
		t, err := parseDate(req.StartDate)
		if err != nil {
			return nil, err
		}
		start = t
	}
	var endPtr interface{}
	if req.EndDate != "" {
		t, err := parseDate(req.EndDate)
		if err != nil {
			return nil, err
		}
		endPtr = t.Format("2006-01-02")
	}

	tx, err := h.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var contractID int64
	err = tx.QueryRow(`
		INSERT INTO contracts (user_id, client_id, contract_number, name, hourly_rate, currency, contract_type,
		                      start_date, end_date, status, payment_terms, notes)
		VALUES (?, ?, ?, ?, ?, ?, 'hourly', ?, ?, 'active', ?, ?)
		RETURNING id
	`, userID, clientID, req.ContractNumber, name, rate, currency,
		start.Format("2006-01-02"), endPtr, req.PaymentTerms,
		fmt.Sprintf("Derived from quote %s", number)).Scan(&contractID)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`
		UPDATE quotes SET status = 'converted', converted_contract_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND user_id = ?
	`, contractID, qID, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	BroadcastUserEvent(userID, "quote.converted", map[string]any{
		"source":          "api",
		"quote_number":    number,
		"contract_id":     contractID,
		"contract_number": req.ContractNumber,
	})

	return map[string]interface{}{
		"quote_number":    number,
		"contract_id":     contractID,
		"contract_number": req.ContractNumber,
		"hourly_rate":     rate,
	}, nil
}
