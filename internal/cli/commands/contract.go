package commands

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/austin/hours-mcp/internal/billing"
	"github.com/austin/hours-mcp/internal/cli/output"
	"github.com/austin/hours-mcp/internal/models"
)

// ContractCommands handles contract-related CLI commands
type ContractCommands struct {
	db     *sql.DB
	output *output.Formatter
}

// NewContractCommands creates a new ContractCommands instance
func NewContractCommands(db *sql.DB, out *output.Formatter) *ContractCommands {
	return &ContractCommands{
		db:     db,
		output: out,
	}
}

// AddContract adds a new contract for a client
func (c *ContractCommands) AddContract(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("contract number, client name, and hourly rate are required\n\nUsage: hours-mcp add-contract <number> <client> <rate> [OPTIONS]")
	}

	contractNumber := args[0]
	clientName := args[1]
	rateStr := args[2]
	parsedArgs := parseFlags(args[3:])

	// Parse hourly rate
	hourlyRate, err := strconv.ParseFloat(rateStr, 64)
	if err != nil {
		return fmt.Errorf("invalid hourly rate: %s", rateStr)
	}

	// Get client ID
	clientID, err := c.getClientIDByName(clientName)
	if err != nil {
		return fmt.Errorf("failed to find client: %w", err)
	}

	// Set defaults
	name := parsedArgs["desc"]
	if name == "" {
		name = contractNumber
	}
	currency := parsedArgs["currency"]
	if currency == "" {
		currency = "USD"
	}
	contractType := parsedArgs["type"]
	if contractType == "" {
		contractType = "hourly"
	}

	// Parse dates
	startDateStr := parsedArgs["start"]
	if startDateStr == "" {
		startDateStr = time.Now().Format("2006-01-02")
	}
	startDate, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		return fmt.Errorf("invalid start date format (use YYYY-MM-DD): %w", err)
	}

	var endDate *time.Time
	if endDateStr := parsedArgs["end"]; endDateStr != "" {
		ed, err := time.Parse("2006-01-02", endDateStr)
		if err != nil {
			return fmt.Errorf("invalid end date format (use YYYY-MM-DD): %w", err)
		}
		endDate = &ed
	}

	paymentTerms := parsedArgs["terms"]

	// Parse billing cycle options
	var billingCycleDay *int
	var billingCycleType string = "monthly" // default
	var autoBillEnabled bool

	if billingDayStr := parsedArgs["billing-day"]; billingDayStr != "" {
		day, err := strconv.Atoi(billingDayStr)
		if err != nil {
			return fmt.Errorf("invalid billing day: %s", billingDayStr)
		}
		if day < 1 || day > 31 {
			return fmt.Errorf("billing day must be between 1 and 31")
		}
		billingCycleDay = &day
	}

	if billingTypeArg := parsedArgs["billing-type"]; billingTypeArg != "" {
		if !billing.IsValidCycleType(billingTypeArg) {
			return fmt.Errorf("invalid billing type: %s. Valid types: %v", billingTypeArg, billing.ValidCycleTypes())
		}
		billingCycleType = billingTypeArg
	}

	if parsedArgs["auto-bill"] == "true" {
		autoBillEnabled = true
	}

	// Insert contract
	var contractID int64
	err = c.db.QueryRow(`
		INSERT INTO contracts (client_id, contract_number, name, hourly_rate, currency, contract_type, start_date, end_date, payment_terms, billing_cycle_day, billing_cycle_type, auto_bill_enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, clientID, contractNumber, name, hourlyRate, currency, contractType, startDate.Format("2006-01-02"),
		func() interface{} {
			if endDate != nil {
				return endDate.Format("2006-01-02")
			}
			return nil
		}(), paymentTerms, billingCycleDay, billingCycleType, autoBillEnabled).Scan(&contractID)

	if err != nil {
		return fmt.Errorf("failed to add contract: %w", err)
	}

	// Calculate and set next billing date if billing cycle is configured
	if billingCycleDay != nil {
		contract := models.Contract{
			ID:               int(contractID),
			ClientID:         clientID,
			ContractNumber:   contractNumber,
			Name:             name,
			HourlyRate:       hourlyRate,
			Currency:         currency,
			ContractType:     contractType,
			StartDate:        startDate,
			EndDate:          endDate,
			PaymentTerms:     paymentTerms,
			BillingCycleDay:  billingCycleDay,
			BillingCycleType: billingCycleType,
			AutoBillEnabled:  autoBillEnabled,
		}

		nextBilling, err := billing.CalculateNextBillingDate(contract)
		if err != nil {
			c.output.Warning(fmt.Sprintf("Contract created but failed to calculate next billing date: %v", err))
		} else if nextBilling != nil {
			_, err = c.db.Exec(`
				UPDATE contracts SET next_billing_date = ? WHERE id = ?
			`, nextBilling.Format("2006-01-02"), contractID)
			if err != nil {
				c.output.Warning(fmt.Sprintf("Contract created but failed to set next billing date: %v", err))
			}
		}
	}

	message := fmt.Sprintf("Successfully added contract %s for %s (ID: %d)", contractNumber, clientName, contractID)
	if billingCycleDay != nil {
		message += fmt.Sprintf("\nBilling cycle: %s on day %d", billingCycleType, *billingCycleDay)
		if autoBillEnabled {
			message += " (auto-billing enabled)"
		}
	}

	c.output.Success(message)
	return nil
}

// ListContracts lists contracts with optional filtering
func (c *ContractCommands) ListContracts(args []string) error {
	parsedArgs := parseFlags(args)

	query := `
		SELECT c.id, c.contract_number, c.name, c.hourly_rate, c.currency, c.contract_type,
		       c.start_date, c.end_date, c.status, c.payment_terms, cl.name as client_name,
		       c.billing_cycle_day, c.billing_cycle_type, c.next_billing_date, c.auto_bill_enabled
		FROM contracts c
		JOIN clients cl ON c.client_id = cl.id
		WHERE 1=1
	`
	queryArgs := []interface{}{}

	if clientName := parsedArgs["client"]; clientName != "" {
		query += " AND cl.name LIKE ?"
		queryArgs = append(queryArgs, "%"+clientName+"%")
	}

	if status := parsedArgs["status"]; status != "" {
		query += " AND c.status = ?"
		queryArgs = append(queryArgs, status)
	}

	query += " ORDER BY c.start_date DESC, c.contract_number"

	rows, err := c.db.Query(query, queryArgs...)
	if err != nil {
		return fmt.Errorf("failed to list contracts: %w", err)
	}
	defer rows.Close()

	var contracts []models.Contract
	for rows.Next() {
		var contract models.Contract
		var clientName string
		var endDate *string
		var billingCycleDay *int
		var billingCycleType string
		var nextBillingDate *string
		var autoBillEnabled bool

		err := rows.Scan(&contract.ID, &contract.ContractNumber, &contract.Name, &contract.HourlyRate, &contract.Currency, &contract.ContractType,
			&contract.StartDate, &endDate, &contract.Status, &contract.PaymentTerms, &clientName,
			&billingCycleDay, &billingCycleType, &nextBillingDate, &autoBillEnabled)
		if err != nil {
			return fmt.Errorf("failed to scan contract: %w", err)
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
		contract.Client = &models.Client{Name: clientName}
		contracts = append(contracts, contract)
	}

	c.output.PrintContracts(contracts)
	return nil
}

// DeleteContract deletes a contract by contract number
func (c *ContractCommands) DeleteContract(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("contract number is required\n\nUsage: hours-mcp delete-contract <number>")
	}

	contractNumber := args[0]

	// First check if contract exists and get details
	var contractID int
	var clientName string
	var contractName string
	err := c.db.QueryRow(`
		SELECT c.id, c.name, cl.name as client_name
		FROM contracts c
		JOIN clients cl ON c.client_id = cl.id
		WHERE c.contract_number = ?
	`, contractNumber).Scan(&contractID, &contractName, &clientName)

	if err == sql.ErrNoRows {
		return fmt.Errorf("contract '%s' not found", contractNumber)
	}
	if err != nil {
		return fmt.Errorf("failed to find contract: %w", err)
	}

	// Check if contract has associated time entries
	var timeEntryCount int
	err = c.db.QueryRow("SELECT COUNT(*) FROM time_entries WHERE contract_id = ?", contractID).Scan(&timeEntryCount)
	if err != nil {
		return fmt.Errorf("failed to check time entries: %w", err)
	}

	if timeEntryCount > 0 {
		return fmt.Errorf("cannot delete contract '%s' - it has %d associated time entries. Delete the time entries first", contractNumber, timeEntryCount)
	}

	// Delete the contract
	result, err := c.db.Exec("DELETE FROM contracts WHERE id = ?", contractID)
	if err != nil {
		return fmt.Errorf("failed to delete contract: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to verify deletion: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("contract '%s' not found", contractNumber)
	}

	c.output.Success(fmt.Sprintf("Successfully deleted contract %s (%s) for %s", contractNumber, contractName, clientName))
	return nil
}

// EditContract edits an existing contract
func (c *ContractCommands) EditContract(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("contract number is required\n\nUsage: hours-mcp edit-contract <number> [OPTIONS]")
	}

	contractNumber := args[0]
	parsedArgs := parseFlags(args[1:])

	// First check if contract exists
	var contract models.Contract
	var endDate *string
	var billingCycleDay *int
	var billingCycleType string
	var nextBillingDate *string
	var autoBillEnabled bool

	err := c.db.QueryRow(`
		SELECT id, contract_number, name, hourly_rate, currency, contract_type,
		       start_date, end_date, status, payment_terms,
		       billing_cycle_day, billing_cycle_type, next_billing_date, auto_bill_enabled
		FROM contracts WHERE contract_number = ?
	`, contractNumber).Scan(
		&contract.ID, &contract.ContractNumber, &contract.Name, &contract.HourlyRate,
		&contract.Currency, &contract.ContractType, &contract.StartDate, &endDate,
		&contract.Status, &contract.PaymentTerms, &billingCycleDay, &billingCycleType,
		&nextBillingDate, &autoBillEnabled)

	if err == sql.ErrNoRows {
		return fmt.Errorf("contract '%s' not found", contractNumber)
	}
	if err != nil {
		return fmt.Errorf("failed to find contract: %w", err)
	}

	// Set current values
	contract.BillingCycleDay = billingCycleDay
	contract.BillingCycleType = billingCycleType
	contract.AutoBillEnabled = autoBillEnabled

	if endDate != nil {
		ed, _ := time.Parse("2006-01-02", *endDate)
		contract.EndDate = &ed
	}

	if nextBillingDate != nil {
		formats := []string{"2006-01-02T15:04:05Z", "2006-01-02"}
		for _, format := range formats {
			if nbd, err := time.Parse(format, *nextBillingDate); err == nil {
				contract.NextBillingDate = &nbd
				break
			}
		}
	}

	// Apply updates from arguments
	updateFields := []string{}
	updateValues := []interface{}{}

	if billingDayStr := parsedArgs["billing-day"]; billingDayStr != "" {
		day, err := strconv.Atoi(billingDayStr)
		if err != nil {
			return fmt.Errorf("invalid billing day: %s", billingDayStr)
		}
		if day < 1 || day > 31 {
			return fmt.Errorf("billing day must be between 1 and 31")
		}
		updateFields = append(updateFields, "billing_cycle_day = ?")
		updateValues = append(updateValues, day)
		contract.BillingCycleDay = &day
	}

	if billingTypeArg := parsedArgs["billing-type"]; billingTypeArg != "" {
		if !billing.IsValidCycleType(billingTypeArg) {
			return fmt.Errorf("invalid billing type: %s. Valid types: %v", billingTypeArg, billing.ValidCycleTypes())
		}
		updateFields = append(updateFields, "billing_cycle_type = ?")
		updateValues = append(updateValues, billingTypeArg)
		contract.BillingCycleType = billingTypeArg
	}

	if parsedArgs["auto-bill"] == "true" {
		updateFields = append(updateFields, "auto_bill_enabled = ?")
		updateValues = append(updateValues, true)
		contract.AutoBillEnabled = true
	} else if parsedArgs["auto-bill"] == "false" {
		updateFields = append(updateFields, "auto_bill_enabled = ?")
		updateValues = append(updateValues, false)
		contract.AutoBillEnabled = false
	}

	if len(updateFields) == 0 {
		return fmt.Errorf("no changes specified. Use --billing-day, --billing-type, or --auto-bill")
	}

	// Update contract
	query := fmt.Sprintf("UPDATE contracts SET %s WHERE id = ?", strings.Join(updateFields, ", "))
	updateValues = append(updateValues, contract.ID)

	_, err = c.db.Exec(query, updateValues...)
	if err != nil {
		return fmt.Errorf("failed to update contract: %w", err)
	}

	// Recalculate next billing date if billing cycle was updated
	if contract.BillingCycleDay != nil {
		nextBilling, err := billing.CalculateNextBillingDate(contract)
		if err != nil {
			c.output.Warning(fmt.Sprintf("Contract updated but failed to calculate next billing date: %v", err))
		} else if nextBilling != nil {
			_, err = c.db.Exec(`
				UPDATE contracts SET next_billing_date = ? WHERE id = ?
			`, nextBilling.Format("2006-01-02"), contract.ID)
			if err != nil {
				c.output.Warning(fmt.Sprintf("Contract updated but failed to set next billing date: %v", err))
			}
		}
	}

	message := fmt.Sprintf("Successfully updated contract %s", contractNumber)
	if contract.BillingCycleDay != nil {
		message += fmt.Sprintf("\nBilling cycle: %s on day %d", contract.BillingCycleType, *contract.BillingCycleDay)
		if contract.AutoBillEnabled {
			message += " (auto-billing enabled)"
		}
	}

	c.output.Success(message)
	return nil
}

// getClientIDByName is a helper method to get client ID by name
func (c *ContractCommands) getClientIDByName(name string) (int, error) {
	var id int
	err := c.db.QueryRow("SELECT id FROM clients WHERE name = ?", name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("client '%s' not found", name)
	}
	return id, err
}