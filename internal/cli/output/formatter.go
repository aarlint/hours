package output

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/austin/hours-mcp/internal/models"
)

// Formatter handles CLI output formatting
type Formatter struct {
	useColor bool
}

// New creates a new formatter instance
func New() *Formatter {
	// Enable color if stdout is a terminal
	useColor := isTerminal()
	return &Formatter{
		useColor: useColor,
	}
}

// Colors for terminal output
const (
	Reset   = "\033[0m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	Bold    = "\033[1m"
)

// colorize applies color to text if color output is enabled
func (f *Formatter) colorize(color, text string) string {
	if !f.useColor {
		return text
	}
	return color + text + Reset
}

// Success prints a success message
func (f *Formatter) Success(message string) {
	fmt.Println(f.colorize(Green, "✓ "+message))
}

// Error prints an error message
func (f *Formatter) Error(message string) {
	fmt.Fprintf(os.Stderr, f.colorize(Red, "✗ "+message)+"\n")
}

// Warning prints a warning message
func (f *Formatter) Warning(message string) {
	fmt.Println(f.colorize(Yellow, "⚠ "+message))
}

// Info prints an informational message
func (f *Formatter) Info(message string) {
	fmt.Println(message)
}

// Header prints a section header
func (f *Formatter) Header(title string) {
	fmt.Println(f.colorize(Bold+Cyan, title))
	fmt.Println(strings.Repeat("=", len(title)))
}

// PrintClients formats and prints a list of clients
func (f *Formatter) PrintClients(clients []models.Client) {
	if len(clients) == 0 {
		f.Info("No clients found.")
		return
	}

	f.Header(fmt.Sprintf("Clients (%d)", len(clients)))
	for _, client := range clients {
		fmt.Printf("• %s (ID: %d)\n", f.colorize(Bold, client.Name), client.ID)
		if client.Address != "" {
			fmt.Printf("  %s", client.Address)
			if client.City != "" {
				fmt.Printf(", %s", client.City)
			}
			if client.State != "" {
				fmt.Printf(", %s", client.State)
			}
			if client.ZipCode != "" {
				fmt.Printf(" %s", client.ZipCode)
			}
			fmt.Println()
		}
		fmt.Printf("  Created: %s\n", client.CreatedAt.Format("2006-01-02"))
		fmt.Println()
	}
}

// PrintContracts formats and prints a list of contracts
func (f *Formatter) PrintContracts(contracts []models.Contract) {
	if len(contracts) == 0 {
		f.Info("No contracts found.")
		return
	}

	f.Header(fmt.Sprintf("Contracts (%d)", len(contracts)))
	for _, contract := range contracts {
		statusColor := Green
		if contract.Status != "active" {
			statusColor = Yellow
		}

		fmt.Printf("• %s: %s (%s)\n",
			f.colorize(Bold, contract.ContractNumber),
			contract.Name,
			f.colorize(statusColor, contract.Status))

		if contract.Client != nil {
			fmt.Printf("  Client: %s\n", contract.Client.Name)
		}

		fmt.Printf("  Rate: %s%.2f/%s\n", contract.Currency, contract.HourlyRate, contract.Currency)
		fmt.Printf("  Period: %s", contract.StartDate.Format("2006-01-02"))
		if contract.EndDate != nil {
			fmt.Printf(" to %s", contract.EndDate.Format("2006-01-02"))
		} else {
			fmt.Printf(" (ongoing)")
		}
		fmt.Println()

		if contract.PaymentTerms != "" {
			fmt.Printf("  Terms: %s\n", contract.PaymentTerms)
		}

		// Display billing cycle information
		if contract.BillingCycleDay != nil {
			billingInfo := fmt.Sprintf("  Billing: %s on day %d", contract.BillingCycleType, *contract.BillingCycleDay)
			if contract.AutoBillEnabled {
				billingInfo += " (auto-billing enabled)"
			}
			fmt.Println(billingInfo)

			if contract.NextBillingDate != nil {
				billingColor := Green
				if contract.NextBillingDate.Before(time.Now()) {
					billingColor = Yellow
				}
				fmt.Printf("  Next billing: %s\n", f.colorize(billingColor, contract.NextBillingDate.Format("2006-01-02")))
			}
		}

		fmt.Println()
	}
}

// PrintTimeEntries formats and prints time entries
func (f *Formatter) PrintTimeEntries(entries []models.TimeEntry, totalHours float64) {
	if len(entries) == 0 {
		f.Info("No time entries found.")
		return
	}

	f.Header(fmt.Sprintf("Time Entries (%d entries, %.2f total hours)", len(entries), totalHours))

	currentDate := ""
	for _, entry := range entries {
		entryDate := entry.Date.Format("2006-01-02")
		if entryDate != currentDate {
			if currentDate != "" {
				fmt.Println()
			}
			fmt.Println(f.colorize(Bold, entryDate))
			currentDate = entryDate
		}

		invoiceStatus := ""
		if entry.InvoiceID != nil {
			invoiceStatus = f.colorize(Green, " [INVOICED]")
		}

		fmt.Printf("  • %.2f hours%s\n", entry.Hours, invoiceStatus)
		if entry.Description != "" {
			fmt.Printf("    %s\n", entry.Description)
		}
		if entry.Contract != nil {
			fmt.Printf("    Contract: %s\n", entry.Contract.ContractNumber)
		}
		fmt.Printf("    ID: %s\n", f.colorize(Cyan, entry.ID))
	}
	fmt.Println()
}

// PrintInvoices formats and prints invoices
func (f *Formatter) PrintInvoices(invoices []models.Invoice, totalAmount float64) {
	if len(invoices) == 0 {
		f.Info("No invoices found.")
		return
	}

	f.Header(fmt.Sprintf("Invoices (%d invoices, $%.2f total)", len(invoices), totalAmount))

	for _, invoice := range invoices {
		statusColor := Yellow
		switch invoice.Status {
		case "paid":
			statusColor = Green
		case "overdue":
			statusColor = Red
		case "sent":
			statusColor = Blue
		}

		fmt.Printf("• %s (%s)\n",
			f.colorize(Bold, invoice.InvoiceNumber),
			f.colorize(statusColor, invoice.Status))

		if invoice.Client != nil {
			fmt.Printf("  Client: %s\n", invoice.Client.Name)
		}

		fmt.Printf("  Amount: $%.2f\n", invoice.TotalAmount)
		fmt.Printf("  Issued: %s, Due: %s\n",
			invoice.IssueDate.Format("2006-01-02"),
			invoice.DueDate.Format("2006-01-02"))

		if invoice.PDFPath != "" {
			fmt.Printf("  PDF: %s\n", invoice.PDFPath)
		}
		fmt.Println()
	}
}

// PrintBusinessInfo formats and prints business information
func (f *Formatter) PrintBusinessInfo(business models.BusinessInfo) {
	f.Header("Business Information")
	fmt.Printf("Name: %s\n", f.colorize(Bold, business.BusinessName))
	fmt.Printf("Contact: %s\n", business.ContactName)
	fmt.Printf("Email: %s\n", business.Email)

	if business.Phone != "" {
		fmt.Printf("Phone: %s\n", business.Phone)
	}

	if business.Address != "" {
		fmt.Printf("Address: %s", business.Address)
		if business.City != "" {
			fmt.Printf(", %s", business.City)
		}
		if business.State != "" {
			fmt.Printf(", %s", business.State)
		}
		if business.ZipCode != "" {
			fmt.Printf(" %s", business.ZipCode)
		}
		fmt.Println()
	}

	if business.TaxID != "" {
		fmt.Printf("Tax ID: %s\n", business.TaxID)
	}

	if business.Website != "" {
		fmt.Printf("Website: %s\n", business.Website)
	}

	fmt.Printf("Invoice Prefix: %s\n", business.InvoicePrefix)
	fmt.Printf("Last Updated: %s\n", business.UpdatedAt.Format("2006-01-02 15:04:05"))
}

// isTerminal checks if stdout is connected to a terminal
func isTerminal() bool {
	// Simple check - in a real implementation, you might want to use
	// a library like golang.org/x/term to properly detect terminal support
	if os.Getenv("TERM") == "" {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return true
}