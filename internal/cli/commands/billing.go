package commands

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/austin/hours-mcp/internal/billing"
	"github.com/austin/hours-mcp/internal/cli/output"
	"github.com/austin/hours-mcp/internal/models"
)

// BillingCommands handles billing-related CLI commands
type BillingCommands struct {
	db     *sql.DB
	output *output.Formatter
}

// NewBillingCommands creates a new BillingCommands instance
func NewBillingCommands(db *sql.DB, out *output.Formatter) *BillingCommands {
	return &BillingCommands{
		db:     db,
		output: out,
	}
}

// ListReadyToBill lists all contracts that are ready to be billed
func (b *BillingCommands) ListReadyToBill(args []string) error {
	parsedArgs := parseFlags(args)

	query := `
		SELECT c.id, c.contract_number, c.name, c.hourly_rate, c.currency, c.contract_type,
		       c.start_date, c.end_date, c.status, c.payment_terms, cl.name as client_name,
		       c.billing_cycle_day, c.billing_cycle_type, c.next_billing_date, c.auto_bill_enabled
		FROM contracts c
		JOIN clients cl ON c.client_id = cl.id
		WHERE c.status = 'active'
		  AND c.billing_cycle_day IS NOT NULL
		  AND c.next_billing_date IS NOT NULL
		  AND c.next_billing_date <= date('now')
	`
	queryArgs := []interface{}{}

	// Filter by client if specified
	if clientName := parsedArgs["client"]; clientName != "" {
		query += " AND cl.name LIKE ?"
		queryArgs = append(queryArgs, "%"+clientName+"%")
	}

	// Filter by auto-bill enabled if specified
	if parsedArgs["auto-only"] == "true" {
		query += " AND c.auto_bill_enabled = 1"
	}

	query += " ORDER BY c.next_billing_date ASC, cl.name"

	rows, err := b.db.Query(query, queryArgs...)
	if err != nil {
		return fmt.Errorf("failed to query ready-to-bill contracts: %w", err)
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

	if len(contracts) == 0 {
		b.output.Info("No contracts are ready to be billed.")
		return nil
	}

	b.output.Header(fmt.Sprintf("Contracts Ready to Bill (%d)", len(contracts)))

	for _, contract := range contracts {
		daysOverdue := int(time.Since(*contract.NextBillingDate).Hours() / 24)

		fmt.Printf("• %s: %s (%s)\n",
			contract.ContractNumber,
			contract.Name,
			contract.Client.Name)

		fmt.Printf("  Rate: %s%.2f/%s\n", contract.Currency, contract.HourlyRate, contract.Currency)

		billingInfo := fmt.Sprintf("  Billing: %s on day %d", contract.BillingCycleType, *contract.BillingCycleDay)
		if contract.AutoBillEnabled {
			billingInfo += " (auto-billing enabled)"
		}
		fmt.Println(billingInfo)

		overdueText := ""
		if daysOverdue > 0 {
			overdueText = fmt.Sprintf(" (%d days overdue)", daysOverdue)
		}
		fmt.Printf("  Due date: %s%s\n", contract.NextBillingDate.Format("2006-01-02"), overdueText)

		// Calculate billing period for this contract
		period, err := billing.CalculateBillingPeriod(contract, *contract.NextBillingDate)
		if err == nil {
			fmt.Printf("  Period: %s to %s\n",
				period.StartDate.Format("2006-01-02"),
				period.EndDate.Format("2006-01-02"))
		}

		fmt.Println()
	}

	// Show summary
	autoContracts := 0
	overdueContracts := 0
	for _, contract := range contracts {
		if contract.AutoBillEnabled {
			autoContracts++
		}
		if contract.NextBillingDate.Before(time.Now()) {
			overdueContracts++
		}
	}

	b.output.Info(fmt.Sprintf("Summary: %d total, %d with auto-billing, %d overdue",
		len(contracts), autoContracts, overdueContracts))

	if autoContracts > 0 {
		b.output.Info("Use 'hours-mcp create-invoice <client> auto' to generate invoices using billing cycles")
	}

	return nil
}