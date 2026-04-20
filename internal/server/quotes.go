package server

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/austin/hours-mcp/internal/models"
	"github.com/austin/hours-mcp/internal/pdf"
	"github.com/austin/hours-mcp/internal/timeparse"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterQuoteTools registers MCP tools for the quoting system.
// Called from RegisterTools.
func RegisterQuoteTools(server *mcp.Server, db *sql.DB, h *Handler) {
	// ---------- add_quote ----------

	type quoteLineItemInput struct {
		Description string  `json:"description" jsonschema:"Line item description"`
		Quantity    float64 `json:"quantity" jsonschema:"Quantity (e.g. hours)"`
		Unit        string  `json:"unit,omitempty" jsonschema:"Unit label (default 'hours')"`
		UnitPrice   float64 `json:"unit_price" jsonschema:"Price per unit"`
	}

	type addQuoteArgs struct {
		ClientName string               `json:"client_name" jsonschema:"Client name"`
		Title      string               `json:"title" jsonschema:"Short title, e.g. 'Q2 backend refactor'"`
		LineItems  []quoteLineItemInput `json:"line_items" jsonschema:"Line items"`
		ValidDays  int                  `json:"valid_days,omitempty" jsonschema:"Days the quote is valid (default 30)"`
		ValidUntil string               `json:"valid_until,omitempty" jsonschema:"Explicit valid-until date (YYYY-MM-DD); overrides valid_days"`
		IssueDate  string               `json:"issue_date,omitempty" jsonschema:"Issue date (YYYY-MM-DD, default today)"`
		Currency   string               `json:"currency,omitempty" jsonschema:"Currency code (default USD)"`
		Notes      string               `json:"notes,omitempty" jsonschema:"Optional notes"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_quote",
		Description: "Create a new draft quote for a client with one or more line items",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args addQuoteArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.Title) == "" {
			return nil, nil, fmt.Errorf("title required")
		}
		if len(args.LineItems) == 0 {
			return nil, nil, fmt.Errorf("at least one line item required")
		}

		clientID, err := h.getClientIDByName(args.ClientName)
		if err != nil {
			return nil, nil, err
		}

		currency := args.Currency
		if currency == "" {
			currency = "USD"
		}

		issue := time.Now()
		if args.IssueDate != "" {
			t, err := timeparse.ParseDate(args.IssueDate)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid issue_date: %w", err)
			}
			issue = t
		}

		validDays := args.ValidDays
		if validDays <= 0 {
			validDays = 30
		}
		var validUntil time.Time
		if args.ValidUntil != "" {
			t, err := timeparse.ParseDate(args.ValidUntil)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid valid_until: %w", err)
			}
			validUntil = t
		} else {
			validUntil = issue.AddDate(0, 0, validDays)
		}

		subtotal := 0.0
		for _, li := range args.LineItems {
			if strings.TrimSpace(li.Description) == "" {
				return nil, nil, fmt.Errorf("line item description required")
			}
			if li.Quantity <= 0 {
				return nil, nil, fmt.Errorf("line item quantity must be > 0")
			}
			subtotal += li.Quantity * li.UnitPrice
		}

		quoteNumber := fmt.Sprintf("QT-%s-%s", time.Now().Format("200601"), uuid.New().String()[:8])

		tx, err := db.Begin()
		if err != nil {
			return nil, nil, err
		}
		defer tx.Rollback()

		res, err := tx.Exec(`
			INSERT INTO quotes (client_id, quote_number, title, issue_date, valid_until,
			                   subtotal, total_amount, currency, status, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'draft', ?)
		`, clientID, quoteNumber, args.Title,
			issue.Format("2006-01-02"), validUntil.Format("2006-01-02"),
			subtotal, subtotal, currency, args.Notes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create quote: %w", err)
		}
		quoteID, _ := res.LastInsertId()

		for idx, li := range args.LineItems {
			unit := li.Unit
			if unit == "" {
				unit = "hours"
			}
			amount := li.Quantity * li.UnitPrice
			if _, err := tx.Exec(`
				INSERT INTO quote_line_items (quote_id, description, quantity, unit, unit_price, amount, sort_order)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, quoteID, li.Description, li.Quantity, unit, li.UnitPrice, amount, idx); err != nil {
				return nil, nil, fmt.Errorf("failed to insert line item: %w", err)
			}
		}

		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}

		text := fmt.Sprintf("Quote %s created for %s — %s %.2f (valid until %s)",
			quoteNumber, args.ClientName, currency, subtotal, validUntil.Format("2006-01-02"))
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, map[string]interface{}{
			"quote_number": quoteNumber,
			"client_name":  args.ClientName,
			"total_amount": subtotal,
			"valid_until":  validUntil.Format("2006-01-02"),
		}, nil
	})

	// ---------- list_quotes ----------

	type listQuotesArgs struct {
		ClientName string `json:"client_name,omitempty" jsonschema:"Filter by client"`
		Status     string `json:"status,omitempty" jsonschema:"Filter by status"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_quotes",
		Description: "List quotes with optional filters",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listQuotesArgs) (*mcp.CallToolResult, any, error) {
		q := `
			SELECT q.quote_number, q.title, c.name, q.issue_date, q.valid_until,
			       q.total_amount, q.currency, q.status
			FROM quotes q
			JOIN clients c ON q.client_id = c.id
			WHERE 1=1
		`
		queryArgs := []interface{}{}
		if args.ClientName != "" {
			clientID, err := h.getClientIDByName(args.ClientName)
			if err != nil {
				return nil, nil, err
			}
			q += " AND q.client_id = ?"
			queryArgs = append(queryArgs, clientID)
		}
		if args.Status != "" {
			q += " AND q.status = ?"
			queryArgs = append(queryArgs, args.Status)
		}
		q += " ORDER BY q.issue_date DESC"

		rows, err := db.Query(q, queryArgs...)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()

		type row struct {
			QuoteNumber string    `json:"quote_number"`
			Title       string    `json:"title"`
			ClientName  string    `json:"client_name"`
			IssueDate   time.Time `json:"issue_date"`
			ValidUntil  time.Time `json:"valid_until"`
			TotalAmount float64   `json:"total_amount"`
			Currency    string    `json:"currency"`
			Status      string    `json:"status"`
		}
		out := []row{}
		total := 0.0
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.QuoteNumber, &r.Title, &r.ClientName, &r.IssueDate,
				&r.ValidUntil, &r.TotalAmount, &r.Currency, &r.Status); err != nil {
				return nil, nil, err
			}
			out = append(out, r)
			total += r.TotalAmount
		}

		text := fmt.Sprintf("Found %d quotes (total: $%.2f):\n", len(out), total)
		for _, r := range out {
			text += fmt.Sprintf("- %s · %s · %s — %s %.2f (%s; valid until %s)\n",
				r.QuoteNumber, r.ClientName, r.Title,
				r.Currency, r.TotalAmount, r.Status, r.ValidUntil.Format("2006-01-02"))
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, map[string]interface{}{
			"quotes": out,
			"count":  len(out),
			"total":  total,
		}, nil
	})

	// ---------- get_quote ----------

	type getQuoteArgs struct {
		QuoteNumber string `json:"quote_number" jsonschema:"Quote number, e.g. QT-202604-abc12345"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_quote",
		Description: "Get full details of a quote including line items",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getQuoteArgs) (*mcp.CallToolResult, any, error) {
		var (
			id         int
			clientName string
			title      string
			status     string
			currency   string
			notes      sql.NullString
			issue      time.Time
			validUntil time.Time
			total      float64
		)
		err := db.QueryRow(`
			SELECT q.id, c.name, q.title, q.status, q.currency, q.notes,
			       q.issue_date, q.valid_until, q.total_amount
			FROM quotes q JOIN clients c ON q.client_id = c.id
			WHERE q.quote_number = ?
		`, args.QuoteNumber).Scan(&id, &clientName, &title, &status, &currency, &notes,
			&issue, &validUntil, &total)
		if err == sql.ErrNoRows {
			return nil, nil, fmt.Errorf("quote %s not found", args.QuoteNumber)
		}
		if err != nil {
			return nil, nil, err
		}
		liRows, err := db.Query(`
			SELECT description, quantity, unit, unit_price, amount
			FROM quote_line_items WHERE quote_id = ? ORDER BY sort_order, id
		`, id)
		if err != nil {
			return nil, nil, err
		}
		defer liRows.Close()
		type liOut struct {
			Description string  `json:"description"`
			Quantity    float64 `json:"quantity"`
			Unit        string  `json:"unit"`
			UnitPrice   float64 `json:"unit_price"`
			Amount      float64 `json:"amount"`
		}
		items := []liOut{}
		for liRows.Next() {
			var li liOut
			if err := liRows.Scan(&li.Description, &li.Quantity, &li.Unit, &li.UnitPrice, &li.Amount); err != nil {
				return nil, nil, err
			}
			items = append(items, li)
		}

		text := fmt.Sprintf("%s — %s\nClient: %s\nStatus: %s\nIssued: %s\nValid until: %s\nTotal: %s %.2f\n\nLine items:\n",
			args.QuoteNumber, title, clientName, status,
			issue.Format("2006-01-02"), validUntil.Format("2006-01-02"), currency, total)
		for _, li := range items {
			text += fmt.Sprintf("- %s · %.2f %s @ %s %.2f = %s %.2f\n",
				li.Description, li.Quantity, li.Unit, currency, li.UnitPrice, currency, li.Amount)
		}
		if notes.Valid && notes.String != "" {
			text += "\nNotes: " + notes.String
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, map[string]interface{}{
			"quote_number": args.QuoteNumber,
			"title":        title,
			"client_name":  clientName,
			"status":       status,
			"currency":     currency,
			"total_amount": total,
			"issue_date":   issue.Format("2006-01-02"),
			"valid_until":  validUntil.Format("2006-01-02"),
			"line_items":   items,
		}, nil
	})

	// ---------- update_quote_status ----------

	type updateQuoteStatusArgs struct {
		QuoteNumber string `json:"quote_number" jsonschema:"Quote number"`
		Status      string `json:"status" jsonschema:"New status: sent, accepted, rejected, expired"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_quote_status",
		Description: "Update a quote's status (sent, accepted, rejected, expired)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args updateQuoteStatusArgs) (*mcp.CallToolResult, any, error) {
		valid := map[string]bool{"draft": true, "sent": true, "accepted": true, "rejected": true, "expired": true}
		if !valid[args.Status] {
			return nil, nil, fmt.Errorf("invalid status (allowed: draft, sent, accepted, rejected, expired)")
		}
		res, err := db.Exec(`UPDATE quotes SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE quote_number = ?`, args.Status, args.QuoteNumber)
		if err != nil {
			return nil, nil, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return nil, nil, fmt.Errorf("quote %s not found", args.QuoteNumber)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("Quote %s → %s", args.QuoteNumber, args.Status),
			}},
		}, map[string]interface{}{"quote_number": args.QuoteNumber, "status": args.Status}, nil
	})

	// ---------- convert_quote_to_contract ----------

	type convertQuoteArgs struct {
		QuoteNumber    string `json:"quote_number" jsonschema:"Accepted quote number"`
		ContractNumber string `json:"contract_number" jsonschema:"Unique contract number, e.g. CA-2026-001"`
		ContractName   string `json:"contract_name,omitempty" jsonschema:"Contract name (default: quote title)"`
		StartDate      string `json:"start_date,omitempty" jsonschema:"Contract start date (YYYY-MM-DD, default today)"`
		EndDate        string `json:"end_date,omitempty" jsonschema:"Optional end date"`
		PaymentTerms   string `json:"payment_terms,omitempty" jsonschema:"Payment terms (e.g. Net 30)"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "convert_quote_to_contract",
		Description: "Convert an accepted quote into an active contract. The contract's hourly rate is derived from the quote's line items (weighted average of hour-based items, or the first line's unit price).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args convertQuoteArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.ContractNumber) == "" {
			return nil, nil, fmt.Errorf("contract_number required")
		}
		var (
			qID      int64
			clientID int
			status   string
			currency string
			title    string
			total    float64
		)
		err := db.QueryRow(`
			SELECT id, client_id, status, currency, title, total_amount
			FROM quotes WHERE quote_number = ?
		`, args.QuoteNumber).Scan(&qID, &clientID, &status, &currency, &title, &total)
		if err == sql.ErrNoRows {
			return nil, nil, fmt.Errorf("quote %s not found", args.QuoteNumber)
		}
		if err != nil {
			return nil, nil, err
		}
		if status != "accepted" {
			return nil, nil, fmt.Errorf("only accepted quotes can be converted (current: %s)", status)
		}

		// Derive hourly rate
		liRows, err := db.Query(`
			SELECT quantity, unit, unit_price, amount FROM quote_line_items
			WHERE quote_id = ? ORDER BY sort_order, id
		`, qID)
		if err != nil {
			return nil, nil, err
		}
		var hourQty, hourAmt, firstPrice float64
		firstSet := false
		for liRows.Next() {
			var qty, up, amt float64
			var unit string
			if err := liRows.Scan(&qty, &unit, &up, &amt); err != nil {
				liRows.Close()
				return nil, nil, err
			}
			if !firstSet {
				firstPrice = up
				firstSet = true
			}
			u := strings.ToLower(unit)
			if u == "hours" || u == "hour" || u == "hr" {
				hourQty += qty
				hourAmt += amt
			}
		}
		liRows.Close()
		rate := firstPrice
		if hourQty > 0 {
			rate = hourAmt / hourQty
		}

		name := strings.TrimSpace(args.ContractName)
		if name == "" {
			name = title
		}
		start := time.Now()
		if args.StartDate != "" {
			t, err := timeparse.ParseDate(args.StartDate)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid start_date: %w", err)
			}
			start = t
		}
		var endPtr interface{}
		if args.EndDate != "" {
			t, err := timeparse.ParseDate(args.EndDate)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid end_date: %w", err)
			}
			endPtr = t.Format("2006-01-02")
		}

		tx, err := db.Begin()
		if err != nil {
			return nil, nil, err
		}
		defer tx.Rollback()

		var contractID int64
		err = tx.QueryRow(`
			INSERT INTO contracts (client_id, contract_number, name, hourly_rate, currency, contract_type,
			                      start_date, end_date, status, payment_terms, notes)
			VALUES (?, ?, ?, ?, ?, 'hourly', ?, ?, 'active', ?, ?)
			RETURNING id
		`, clientID, args.ContractNumber, name, rate, currency,
			start.Format("2006-01-02"), endPtr, args.PaymentTerms,
			fmt.Sprintf("Derived from quote %s", args.QuoteNumber)).Scan(&contractID)
		if err != nil {
			return nil, nil, err
		}
		if _, err := tx.Exec(`
			UPDATE quotes SET status = 'converted', converted_contract_id = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, contractID, qID); err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("Quote %s converted → contract %s at %s %.2f/hr",
					args.QuoteNumber, args.ContractNumber, currency, rate),
			}},
		}, map[string]interface{}{
			"quote_number":    args.QuoteNumber,
			"contract_number": args.ContractNumber,
			"contract_id":     contractID,
			"hourly_rate":     rate,
		}, nil
	})

	// ---------- download_quote ----------

	type downloadQuoteArgs struct {
		QuoteNumber string `json:"quote_number" jsonschema:"Quote number"`
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "download_quote",
		Description: "Regenerate and save the PDF for a quote into the configured export directory",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args downloadQuoteArgs) (*mcp.CallToolResult, any, error) {
		var (
			id          int
			clientID    int
			title       string
			status      string
			currency    string
			notes       sql.NullString
			issue       time.Time
			validUntil  time.Time
			totalAmount float64
			subtotal    float64
		)
		err := db.QueryRow(`
			SELECT id, client_id, title, status, currency, notes, issue_date, valid_until, total_amount, subtotal
			FROM quotes WHERE quote_number = ?
		`, args.QuoteNumber).Scan(&id, &clientID, &title, &status, &currency, &notes,
			&issue, &validUntil, &totalAmount, &subtotal)
		if err == sql.ErrNoRows {
			return nil, nil, fmt.Errorf("quote %s not found", args.QuoteNumber)
		}
		if err != nil {
			return nil, nil, err
		}

		var client models.Client
		err = db.QueryRow(`
			SELECT id, name, COALESCE(address,''), COALESCE(city,''), COALESCE(state,''),
			       COALESCE(zip_code,''), COALESCE(country,'')
			FROM clients WHERE id = ?
		`, clientID).Scan(&client.ID, &client.Name, &client.Address, &client.City, &client.State, &client.ZipCode, &client.Country)
		if err != nil {
			return nil, nil, err
		}

		liRows, err := db.Query(`
			SELECT id, quote_id, description, quantity, unit, unit_price, amount, sort_order
			FROM quote_line_items WHERE quote_id = ? ORDER BY sort_order, id
		`, id)
		if err != nil {
			return nil, nil, err
		}
		defer liRows.Close()
		var items []models.QuoteLineItem
		for liRows.Next() {
			var li models.QuoteLineItem
			if err := liRows.Scan(&li.ID, &li.QuoteID, &li.Description, &li.Quantity, &li.Unit,
				&li.UnitPrice, &li.Amount, &li.SortOrder); err != nil {
				return nil, nil, err
			}
			items = append(items, li)
		}

		var recipients []models.Recipient
		recRows, _ := db.Query(`
			SELECT name, email, COALESCE(title,''), COALESCE(phone,'')
			FROM recipients WHERE client_id = ? ORDER BY is_primary DESC
		`, clientID)
		if recRows != nil {
			for recRows.Next() {
				var rcp models.Recipient
				recRows.Scan(&rcp.Name, &rcp.Email, &rcp.Title, &rcp.Phone)
				recipients = append(recipients, rcp)
			}
			recRows.Close()
		}

		var business models.BusinessInfo
		db.QueryRow(`
			SELECT id, business_name, contact_name, email, COALESCE(phone,''), COALESCE(address,''),
			       COALESCE(city,''), COALESCE(state,''), COALESCE(zip_code,''), COALESCE(country,''),
			       COALESCE(tax_id,''), COALESCE(website,''), COALESCE(logo_path,''), COALESCE(invoice_prefix,'INV'),
			       COALESCE(export_path,''), updated_at
			FROM business_info WHERE id = 1
		`).Scan(&business.ID, &business.BusinessName, &business.ContactName, &business.Email,
			&business.Phone, &business.Address, &business.City, &business.State,
			&business.ZipCode, &business.Country, &business.TaxID, &business.Website,
			&business.LogoPath, &business.InvoicePrefix, &business.ExportPath, &business.UpdatedAt)

		exportDir := resolveMCPExportDir(business.ExportPath)
		if err := os.MkdirAll(exportDir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("failed to create export dir: %w", err)
		}
		pdfPath := filepath.Join(exportDir, fmt.Sprintf("quote_%s_%s.pdf", args.QuoteNumber, issue.Format("2006-01-02")))

		quote := models.Quote{
			ID:          id,
			ClientID:    clientID,
			QuoteNumber: args.QuoteNumber,
			Title:       title,
			IssueDate:   issue,
			ValidUntil:  validUntil,
			Subtotal:    subtotal,
			TotalAmount: totalAmount,
			Currency:    currency,
			Status:      status,
			Client:      &client,
			LineItems:   items,
		}
		if notes.Valid {
			quote.Notes = notes.String
		}

		if err := pdf.NewQuoteGenerator().Generate(quote, recipients, business, pdfPath); err != nil {
			return nil, nil, fmt.Errorf("failed to generate PDF: %w", err)
		}
		if _, err := db.Exec(`UPDATE quotes SET pdf_path = ? WHERE id = ?`, pdfPath, id); err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf("Quote PDF saved: %s", pdfPath),
			}},
		}, map[string]interface{}{"pdf_path": pdfPath}, nil
	})
}

// resolveMCPExportDir mirrors api.resolveExportDir so we don't leak internal
// packages across the api<->server boundary.
func resolveMCPExportDir(configured string) string {
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
