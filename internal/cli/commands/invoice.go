package commands

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/austin/hours-mcp/internal/billing"
	"github.com/austin/hours-mcp/internal/cli/output"
	"github.com/austin/hours-mcp/internal/database"
	"github.com/austin/hours-mcp/internal/models"
	"github.com/austin/hours-mcp/internal/pdf"
	"github.com/austin/hours-mcp/internal/timeparse"
	"github.com/google/uuid"
)

// InvoiceCommands handles invoice-related CLI commands
type InvoiceCommands struct {
	db     *sql.DB
	output *output.Formatter
}

// NewInvoiceCommands creates a new InvoiceCommands instance
func NewInvoiceCommands(db *sql.DB, out *output.Formatter) *InvoiceCommands {
	return &InvoiceCommands{
		db:     db,
		output: out,
	}
}

// CreateInvoice creates an invoice for a client
func (i *InvoiceCommands) CreateInvoice(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("client name and period are required\n\nUsage: hours-mcp create-invoice <client> <period> [OPTIONS]")
	}

	clientName := args[0]
	period := args[1]
	parsedArgs := parseFlags(args[2:])

	dueDays := 30
	if dueDaysStr := parsedArgs["due"]; dueDaysStr != "" {
		dd, err := strconv.Atoi(dueDaysStr)
		if err != nil {
			return fmt.Errorf("invalid due days: %s", dueDaysStr)
		}
		dueDays = dd
	}

	clientID, err := i.getClientIDByName(clientName)
	if err != nil {
		return fmt.Errorf("client not found: %w", err)
	}

	// Validate business information is configured
	var businessName string
	err = i.db.QueryRow("SELECT business_name FROM business_info WHERE user_id = ?", database.DefaultUserID).Scan(&businessName)
	if err == sql.ErrNoRows {
		return fmt.Errorf("business information not configured. Please use 'hours-mcp setup-business' to configure your business details before creating invoices")
	} else if err != nil {
		return fmt.Errorf("failed to check business info: %w", err)
	}

	// Validate payment details exist for client
	var paymentBankName string
	err = i.db.QueryRow("SELECT bank_name FROM payment_details WHERE client_id = ?", clientID).Scan(&paymentBankName)
	if err == sql.ErrNoRows {
		return fmt.Errorf("payment details not configured for client '%s'. Please use 'hours-mcp set-payment-details' to configure payment information before creating invoices", clientName)
	} else if err != nil {
		return fmt.Errorf("failed to check payment details: %w", err)
	}

	var startDate, endDate time.Time

	// Handle auto period detection based on billing cycles
	if period == "auto" {
		contract, billingPeriod, err := i.getContractWithBillingCycle(clientID)
		if err != nil {
			return fmt.Errorf("failed to auto-detect billing period: %w", err)
		}

		if contract == nil {
			return fmt.Errorf("no contract with billing cycle found for client '%s'. Use a specific period or add billing cycle to contract", clientName)
		}

		startDate = billingPeriod.StartDate
		endDate = billingPeriod.EndDate

		i.output.Info(fmt.Sprintf("Auto-detected billing period: %s to %s (based on contract %s)",
			startDate.Format("2006-01-02"),
			endDate.Format("2006-01-02"),
			contract.ContractNumber))
	} else {
		startDate, endDate, err = timeparse.ParsePeriod(period)
		if err != nil {
			return fmt.Errorf("invalid period: %w", err)
		}
	}

	var client models.Client
	err = i.db.QueryRow(`
		SELECT id, name, address, city, state, zip_code, country
		FROM clients WHERE id = ?
	`, clientID).Scan(&client.ID, &client.Name, &client.Address, &client.City, &client.State, &client.ZipCode, &client.Country)
	if err != nil {
		return fmt.Errorf("failed to get client details: %w", err)
	}

	rows, err := i.db.Query(`
		SELECT te.id, te.date, te.hours, te.description, ct.hourly_rate, ct.currency
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		WHERE ct.client_id = ? AND te.date >= ? AND te.date <= ? AND te.invoice_id IS NULL
		ORDER BY te.date
	`, clientID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return fmt.Errorf("failed to get time entries: %w", err)
	}
	defer rows.Close()

	var entries []models.TimeEntry
	var totalHours float64
	var totalAmount float64
	for rows.Next() {
		var e models.TimeEntry
		var hourlyRate float64
		var currency string
		if err := rows.Scan(&e.ID, &e.Date, &e.Hours, &e.Description, &hourlyRate, &currency); err != nil {
			return fmt.Errorf("failed to scan entry: %w", err)
		}
		entries = append(entries, e)
		totalHours += e.Hours
		totalAmount += e.Hours * hourlyRate
	}

	if len(entries) == 0 {
		return fmt.Errorf("no unbilled hours found for %s in %s", clientName, period)
	}

	invoiceNumber := fmt.Sprintf("INV-%s-%s", time.Now().Format("200601"), uuid.New().String()[:8])
	issueDate := time.Now()
	dueDate := issueDate.AddDate(0, 0, dueDays)

	tx, err := i.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		INSERT INTO invoices (user_id, client_id, invoice_number, issue_date, due_date, total_amount, status)
		VALUES (?, ?, ?, ?, ?, ?, 'pending')
	`, database.DefaultUserID, clientID, invoiceNumber, issueDate.Format("2006-01-02"), dueDate.Format("2006-01-02"), totalAmount)
	if err != nil {
		return fmt.Errorf("failed to create invoice: %w", err)
	}

	invoiceID, _ := result.LastInsertId()

	invoice := models.Invoice{
		ID:            int(invoiceID),
		ClientID:      clientID,
		InvoiceNumber: invoiceNumber,
		IssueDate:     issueDate,
		DueDate:       dueDate,
		TotalAmount:   totalAmount,
		Status:        "pending",
		Client:        &client,
		TimeEntries:   entries,
	}

	var paymentDetails models.PaymentDetails
	i.db.QueryRow(`
		SELECT bank_name, account_number, routing_number, swift_code, payment_terms, notes
		FROM payment_details WHERE client_id = ?
	`, clientID).Scan(&paymentDetails.BankName, &paymentDetails.AccountNumber,
		&paymentDetails.RoutingNumber, &paymentDetails.SwiftCode,
		&paymentDetails.PaymentTerms, &paymentDetails.Notes)

	var recipients []models.Recipient
	recipientRows, err := i.db.Query(`
		SELECT name, email, title, phone FROM recipients
		WHERE client_id = ? ORDER BY is_primary DESC
	`, clientID)
	if err == nil {
		defer recipientRows.Close()
		for recipientRows.Next() {
			var r models.Recipient
			recipientRows.Scan(&r.Name, &r.Email, &r.Title, &r.Phone)
			recipients = append(recipients, r)
		}
	}

	var business models.BusinessInfo
	i.db.QueryRow(`
		SELECT user_id, business_name, contact_name, email, phone, address, city, state, zip_code, country, tax_id, website, logo_path, invoice_prefix, updated_at
		FROM business_info WHERE user_id = ?
	`, database.DefaultUserID).Scan(&business.ID, &business.BusinessName, &business.ContactName, &business.Email,
		&business.Phone, &business.Address, &business.City, &business.State,
		&business.ZipCode, &business.Country, &business.TaxID, &business.Website,
		&business.LogoPath, &business.InvoicePrefix, &business.UpdatedAt)

	homeDir, _ := os.UserHomeDir()
	downloadsPath := filepath.Join(homeDir, "Downloads")
	pdfPath := filepath.Join(downloadsPath, fmt.Sprintf("invoice_%s.pdf", issueDate.Format("2006-01-02")))

	// Link time entries to the invoice
	for _, entry := range entries {
		_, err = tx.Exec(`UPDATE time_entries SET invoice_id = ? WHERE id = ?`, invoiceID, entry.ID)
		if err != nil {
			return fmt.Errorf("failed to link time entry to invoice: %w", err)
		}
	}

	// Update invoice entries with contract info for PDF generation
	for j := range entries {
		var contract models.Contract
		err = tx.QueryRow(`
			SELECT c.id, c.contract_number, c.name, c.hourly_rate, c.currency, c.payment_terms
			FROM contracts c
			JOIN time_entries te ON te.contract_id = c.id
			WHERE te.id = ?
		`, entries[j].ID).Scan(&contract.ID, &contract.ContractNumber, &contract.Name,
			&contract.HourlyRate, &contract.Currency, &contract.PaymentTerms)
		if err == nil {
			entries[j].Contract = &contract
		}
	}
	invoice.TimeEntries = entries

	generator := pdf.NewInvoiceGenerator()
	if err := generator.Generate(invoice, paymentDetails, recipients, business, pdfPath); err != nil {
		return fmt.Errorf("failed to generate PDF: %w", err)
	}

	tx.Exec(`UPDATE invoices SET pdf_path = ? WHERE id = ?`, pdfPath, invoiceID)

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	i.output.Success(fmt.Sprintf("Invoice %s created successfully", invoiceNumber))
	i.output.Info(fmt.Sprintf("Total: $%.2f (%.2f hours)", totalAmount, totalHours))
	i.output.Info(fmt.Sprintf("PDF saved to: %s", pdfPath))
	return nil
}

// ListInvoices lists invoices with optional filtering
func (i *InvoiceCommands) ListInvoices(args []string) error {
	parsedArgs := parseFlags(args)

	query := `
		SELECT i.id, i.invoice_number, i.issue_date, i.due_date, i.total_amount, i.status, c.name
		FROM invoices i
		JOIN clients c ON i.client_id = c.id
		WHERE 1=1
	`
	queryArgs := []interface{}{}

	if clientName := parsedArgs["client"]; clientName != "" {
		clientID, err := i.getClientIDByName(clientName)
		if err != nil {
			return fmt.Errorf("client not found: %w", err)
		}
		query += " AND i.client_id = ?"
		queryArgs = append(queryArgs, clientID)
	}

	if status := parsedArgs["status"]; status != "" {
		query += " AND i.status = ?"
		queryArgs = append(queryArgs, status)
	}

	if startDate := parsedArgs["start"]; startDate != "" {
		sd, err := timeparse.ParseDate(startDate)
		if err != nil {
			return fmt.Errorf("invalid start date: %w", err)
		}
		query += " AND i.issue_date >= ?"
		queryArgs = append(queryArgs, sd.Format("2006-01-02"))
	}

	if endDate := parsedArgs["end"]; endDate != "" {
		ed, err := timeparse.ParseDate(endDate)
		if err != nil {
			return fmt.Errorf("invalid end date: %w", err)
		}
		query += " AND i.issue_date <= ?"
		queryArgs = append(queryArgs, ed.Format("2006-01-02"))
	}

	query += " ORDER BY i.issue_date DESC"

	rows, err := i.db.Query(query, queryArgs...)
	if err != nil {
		return fmt.Errorf("failed to list invoices: %w", err)
	}
	defer rows.Close()

	var invoices []models.Invoice
	var totalAmount float64

	for rows.Next() {
		var inv models.Invoice
		var clientName string
		if err := rows.Scan(&inv.ID, &inv.InvoiceNumber, &inv.IssueDate, &inv.DueDate,
			&inv.TotalAmount, &inv.Status, &clientName); err != nil {
			return fmt.Errorf("failed to scan invoice: %w", err)
		}
		inv.Client = &models.Client{Name: clientName}
		invoices = append(invoices, inv)
		totalAmount += inv.TotalAmount
	}

	i.output.PrintInvoices(invoices, totalAmount)
	return nil
}

// getClientIDByName is a helper method to get client ID by name (scoped to
// the local DefaultUserID — the CLI is single-user only).
func (i *InvoiceCommands) getClientIDByName(name string) (int, error) {
	var id int
	err := i.db.QueryRow("SELECT id FROM clients WHERE user_id = ? AND name = ?",
		database.DefaultUserID, name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("client '%s' not found", name)
	}
	return id, err
}

// getContractWithBillingCycle gets a contract with billing cycle for auto period detection
func (i *InvoiceCommands) getContractWithBillingCycle(clientID int) (*models.Contract, *billing.PeriodInfo, error) {
	query := `
		SELECT id, contract_number, name, hourly_rate, currency, contract_type,
		       start_date, end_date, status, payment_terms,
		       billing_cycle_day, billing_cycle_type, next_billing_date, auto_bill_enabled
		FROM contracts
		WHERE client_id = ? AND status = 'active' AND billing_cycle_day IS NOT NULL
		ORDER BY next_billing_date ASC
		LIMIT 1
	`

	var contract models.Contract
	var endDate *string
	var billingCycleDay *int
	var billingCycleType string
	var nextBillingDate *string
	var autoBillEnabled bool

	err := i.db.QueryRow(query, clientID).Scan(
		&contract.ID, &contract.ContractNumber, &contract.Name, &contract.HourlyRate,
		&contract.Currency, &contract.ContractType, &contract.StartDate, &endDate,
		&contract.Status, &contract.PaymentTerms, &billingCycleDay, &billingCycleType,
		&nextBillingDate, &autoBillEnabled)

	if err == sql.ErrNoRows {
		return nil, nil, nil // No contract with billing cycle found
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query contract: %w", err)
	}

	if endDate != nil {
		ed, _ := time.Parse("2006-01-02", *endDate)
		contract.EndDate = &ed
	}

	if nextBillingDate != nil {
		// Try multiple date formats
		formats := []string{"2006-01-02T15:04:05Z", "2006-01-02"}
		for _, format := range formats {
			if nbd, err := time.Parse(format, *nextBillingDate); err == nil {
				contract.NextBillingDate = &nbd
				break
			}
		}
	}

	contract.BillingCycleDay = billingCycleDay
	contract.BillingCycleType = billingCycleType
	contract.AutoBillEnabled = autoBillEnabled

	// Calculate the billing period
	referenceDate := time.Now()
	if contract.NextBillingDate != nil {
		referenceDate = *contract.NextBillingDate
	}

	billingPeriod, err := billing.CalculateBillingPeriod(contract, referenceDate)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to calculate billing period: %w", err)
	}

	return &contract, billingPeriod, nil
}

// GeneratePDF generates a PDF for an existing invoice
func (i *InvoiceCommands) GeneratePDF(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("invoice number is required\n\nUsage: hours-mcp generate-pdf <invoice-number>")
	}

	invoiceNumber := args[0]

	// Get invoice details
	var invoice models.Invoice
	var clientID int
	var clientName string
	err := i.db.QueryRow(`
		SELECT i.id, i.invoice_number, i.issue_date, i.due_date, i.total_amount, i.status,
		       c.id, c.name
		FROM invoices i
		JOIN clients c ON i.client_id = c.id
		WHERE i.invoice_number = ?
	`, invoiceNumber).Scan(&invoice.ID, &invoice.InvoiceNumber, &invoice.IssueDate,
		&invoice.DueDate, &invoice.TotalAmount, &invoice.Status, &clientID, &clientName)

	if err == sql.ErrNoRows {
		return fmt.Errorf("invoice '%s' not found", invoiceNumber)
	}
	if err != nil {
		return fmt.Errorf("failed to find invoice: %w", err)
	}

	invoice.Client = &models.Client{ID: clientID, Name: clientName}

	// Get time entries for this invoice
	timeRows, err := i.db.Query(`
		SELECT te.id, te.contract_id, te.date, te.hours, te.description, te.invoice_id, te.created_at,
		       ct.contract_number, ct.name, ct.hourly_rate, ct.currency
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		WHERE te.invoice_id = ?
		ORDER BY te.date, te.created_at
	`, invoice.ID)
	if err != nil {
		return fmt.Errorf("failed to query time entries: %w", err)
	}
	defer timeRows.Close()

	for timeRows.Next() {
		var entry models.TimeEntry
		var contractNumber, contractName, currency string
		var hourlyRate float64

		err := timeRows.Scan(&entry.ID, &entry.ContractID, &entry.Date, &entry.Hours,
			&entry.Description, &entry.InvoiceID, &entry.CreatedAt,
			&contractNumber, &contractName, &hourlyRate, &currency)
		if err != nil {
			return fmt.Errorf("failed to scan time entry: %w", err)
		}

		entry.Contract = &models.Contract{
			ID:             entry.ContractID,
			ContractNumber: contractNumber,
			Name:           contractName,
			HourlyRate:     hourlyRate,
			Currency:       currency,
		}

		invoice.TimeEntries = append(invoice.TimeEntries, entry)
	}

	// Get payment details
	var payment models.PaymentDetails
	err = i.db.QueryRow(`
		SELECT bank_name, routing_number, account_number
		FROM payment_details WHERE client_id = ?
	`, clientID).Scan(&payment.BankName, &payment.RoutingNumber, &payment.AccountNumber)

	if err == sql.ErrNoRows {
		return fmt.Errorf("payment details not found for client '%s'", clientName)
	}
	if err != nil {
		return fmt.Errorf("failed to get payment details: %w", err)
	}

	// Get recipients
	var recipients []models.Recipient
	recipientRows, err := i.db.Query(`
		SELECT name, email FROM recipients WHERE client_id = ?
	`, clientID)
	if err != nil {
		return fmt.Errorf("failed to query recipients: %w", err)
	}
	defer recipientRows.Close()

	for recipientRows.Next() {
		var recipient models.Recipient
		err := recipientRows.Scan(&recipient.Name, &recipient.Email)
		if err != nil {
			return fmt.Errorf("failed to scan recipient: %w", err)
		}
		recipients = append(recipients, recipient)
	}

	// Get business info
	var business models.BusinessInfo
	err = i.db.QueryRow(`
		SELECT business_name, contact_name, email, phone, address, city, state, zip_code, country
		FROM business_info WHERE user_id = ?
	`, database.DefaultUserID).Scan(&business.BusinessName, &business.ContactName, &business.Email, &business.Phone,
		&business.Address, &business.City, &business.State, &business.ZipCode, &business.Country)

	if err == sql.ErrNoRows {
		return fmt.Errorf("business information not configured. Please run 'hours-mcp setup-business' first")
	}
	if err != nil {
		return fmt.Errorf("failed to get business info: %w", err)
	}

	// Generate PDF
	homeDir, _ := os.UserHomeDir()
	downloadsPath := filepath.Join(homeDir, "Downloads")
	outputPath := filepath.Join(downloadsPath, fmt.Sprintf("invoice_%s.pdf", invoice.IssueDate.Format("2006-01-02")))
	generator := pdf.NewInvoiceGenerator()
	err = generator.Generate(invoice, payment, recipients, business, outputPath)
	if err != nil {
		return fmt.Errorf("failed to generate PDF: %w", err)
	}

	// Calculate totals for display
	totalHours := 0.0
	for _, entry := range invoice.TimeEntries {
		totalHours += entry.Hours
	}

	i.output.Success(fmt.Sprintf("PDF generated for invoice %s", invoiceNumber))
	i.output.Info(fmt.Sprintf("Total: $%.2f (%.2f hours)", invoice.TotalAmount, totalHours))
	i.output.Info(fmt.Sprintf("PDF saved to: %s", outputPath))

	return nil
}