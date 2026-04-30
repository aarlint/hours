package main

import (
	"fmt"
	"time"

	"github.com/austin/hours-mcp/internal/models"
	"github.com/austin/hours-mcp/internal/pdf"
)

func main() {
	contract := &models.Contract{
		ID:             1,
		ContractNumber: "ACME-2026-001",
		Name:           "Platform Engineering Retainer",
		HourlyRate:     185,
		Currency:       "USD",
		PaymentTerms:   "Net 30",
	}
	now := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)

	entries := []models.TimeEntry{}
	descs := []string{
		"Reviewed migration plan for legacy auth middleware; drafted rollout doc.",
		"Pair session with platform team on observability gaps.",
		"Implemented contract billing cycle support and tests.",
		"Triaged failing CI pipeline; fixed flaky integration test.",
		"Refactored invoice generator to support expenses section.",
		"Customer call: roadmap alignment for Q3.",
	}
	for i, d := range descs {
		entries = append(entries, models.TimeEntry{
			ID:          fmt.Sprintf("entry-%d", i+1),
			ContractID:  1,
			Date:        now.AddDate(0, 0, -10+i),
			Hours:       2.5 + float64(i)*0.75,
			Description: d,
			Contract:    contract,
		})
	}

	expenses := []models.Expense{
		{ID: "exp-1", Date: now.AddDate(0, 0, -8), Description: "Round-trip flight to client site (DFW)", Amount: 612.40, Currency: "USD", Category: "travel"},
		{ID: "exp-2", Date: now.AddDate(0, 0, -7), Description: "Hotel (2 nights)", Amount: 480.00, Currency: "USD", Category: "lodging"},
		{ID: "exp-3", Date: now.AddDate(0, 0, -5), Description: "Datadog APM seat for engagement window", Amount: 89.00, Currency: "USD", Category: "software"},
	}

	inv := models.Invoice{
		ID:            42,
		ClientID:      1,
		InvoiceNumber: "INV-202604-A1B2C3D4",
		IssueDate:     now,
		DueDate:       now.AddDate(0, 0, 30),
		Status:        "sent",
		Client: &models.Client{
			Name:    "Acme Robotics, Inc.",
			Address: "1200 Industrial Way",
			City:    "Austin", State: "TX", ZipCode: "78704",
			Country: "USA",
		},
		TimeEntries: entries,
		Expenses:    expenses,
	}

	biz := models.BusinessInfo{
		BusinessName: "Arlint Engineering",
		ContactName:  "Austin Arlint",
		Email:        "austin@arlint.dev",
		Phone:        "+1 (512) 555-0100",
		Address:      "401 Congress Ave",
		City:         "Austin", State: "TX", ZipCode: "78701",
		Country: "USA",
	}

	payment := models.PaymentDetails{
		BankName:      "Chase Bank",
		AccountNumber: "****5432",
		RoutingNumber: "021000021",
		PaymentTerms:  "Net 30",
	}

	recipients := []models.Recipient{
		{Name: "Jane Doe", Email: "jane@acme.example", Title: "CFO"},
	}

	out := "/tmp/pdf_smoke/sample_invoice.pdf"
	if err := pdf.NewInvoiceGenerator().Generate(inv, payment, recipients, biz, out); err != nil {
		panic(err)
	}
	fmt.Println("wrote", out)
}
