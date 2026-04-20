package pdf

import (
	"fmt"

	"github.com/austin/hours-mcp/internal/models"
	"github.com/johnfercher/maroto/v2"
	"github.com/johnfercher/maroto/v2/pkg/components/col"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/config"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/props"
)

type QuoteGenerator struct{}

func NewQuoteGenerator() *QuoteGenerator {
	return &QuoteGenerator{}
}

func (g *QuoteGenerator) Generate(quote models.Quote, recipients []models.Recipient, business models.BusinessInfo, outputPath string) error {
	m := maroto.New(config.NewBuilder().Build())

	// Header: business name + QUOTE
	m.AddRow(10,
		col.New(8).Add(
			text.New(business.BusinessName, props.Text{
				Size:  16,
				Style: fontstyle.Bold,
			}),
		),
		col.New(4).Add(
			text.New("QUOTE", props.Text{
				Size:  20,
				Style: fontstyle.BoldItalic,
				Align: align.Right,
			}),
		),
	)

	m.AddRow(6,
		col.New(8).Add(
			text.New(business.ContactName, props.Text{Size: 10}),
		),
		col.New(4).Add(
			text.New(fmt.Sprintf("Quote #: %s", quote.QuoteNumber), props.Text{
				Size:  10,
				Style: fontstyle.Bold,
				Align: align.Right,
			}),
		),
	)

	if business.Email != "" {
		m.AddRow(5,
			col.New(8).Add(text.New(business.Email, props.Text{Size: 9})),
			col.New(4).Add(
				text.New(fmt.Sprintf("Issued: %s", quote.IssueDate.Format("January 2, 2006")), props.Text{
					Size: 9, Align: align.Right,
				}),
			),
		)
	}

	if business.Phone != "" {
		m.AddRow(5,
			col.New(8).Add(text.New(business.Phone, props.Text{Size: 9})),
			col.New(4).Add(
				text.New(fmt.Sprintf("Valid Until: %s", quote.ValidUntil.Format("January 2, 2006")), props.Text{
					Size: 9, Align: align.Right,
				}),
			),
		)
	} else {
		m.AddRow(5,
			col.New(8),
			col.New(4).Add(
				text.New(fmt.Sprintf("Valid Until: %s", quote.ValidUntil.Format("January 2, 2006")), props.Text{
					Size: 9, Align: align.Right,
				}),
			),
		)
	}

	// Business address
	if business.Address != "" {
		addressText := business.Address
		if business.City != "" {
			addressText += ", " + business.City
		}
		if business.State != "" {
			addressText += ", " + business.State
		}
		if business.ZipCode != "" {
			addressText += " " + business.ZipCode
		}
		if business.Country != "" {
			addressText += ", " + business.Country
		}
		m.AddRow(5, col.New(8).Add(text.New(addressText, props.Text{Size: 9})))
	}

	if business.Website != "" {
		m.AddRow(5, col.New(8).Add(text.New(business.Website, props.Text{Size: 9})))
	}

	m.AddRow(10)

	// Bill-to block
	if quote.Client != nil {
		m.AddRow(8,
			col.New(12).Add(
				text.New(fmt.Sprintf("Prepared For: %s", quote.Client.Name), props.Text{
					Size:  11,
					Style: fontstyle.Bold,
				}),
			),
		)

		if quote.Client.Address != "" {
			m.AddRow(5, col.New(12).Add(text.New(quote.Client.Address, props.Text{Size: 9})))
		}

		if quote.Client.City != "" || quote.Client.State != "" || quote.Client.ZipCode != "" {
			cityStateZip := ""
			if quote.Client.City != "" {
				cityStateZip = quote.Client.City
			}
			if quote.Client.State != "" {
				if cityStateZip != "" {
					cityStateZip += ", "
				}
				cityStateZip += quote.Client.State
			}
			if quote.Client.ZipCode != "" {
				if cityStateZip != "" {
					cityStateZip += " "
				}
				cityStateZip += quote.Client.ZipCode
			}
			m.AddRow(5, col.New(12).Add(text.New(cityStateZip, props.Text{Size: 9})))
		}

		if quote.Client.Country != "" {
			m.AddRow(5, col.New(12).Add(text.New(quote.Client.Country, props.Text{Size: 9})))
		}
	}

	for _, r := range recipients {
		m.AddRow(5,
			col.New(12).Add(
				text.New(fmt.Sprintf("%s <%s>", r.Name, r.Email), props.Text{Size: 9}),
			),
		)
	}

	m.AddRow(10)

	// Title block
	m.AddRow(8,
		col.New(12).Add(
			text.New(quote.Title, props.Text{
				Size: 12, Style: fontstyle.Bold,
			}),
		),
	)
	m.AddRow(8)

	// Line items table
	m.AddRow(8,
		col.New(6).Add(text.New("Description", props.Text{Size: 9, Style: fontstyle.Bold})),
		col.New(1).Add(text.New("Qty", props.Text{Size: 9, Style: fontstyle.Bold, Align: align.Right})),
		col.New(2).Add(text.New("Unit", props.Text{Size: 9, Style: fontstyle.Bold})),
		col.New(3).Add(text.New("Amount", props.Text{Size: 9, Style: fontstyle.Bold, Align: align.Right})),
	)

	for _, li := range quote.LineItems {
		m.AddRow(6,
			col.New(6).Add(text.New(li.Description, props.Text{Size: 8})),
			col.New(1).Add(text.New(fmt.Sprintf("%.2f", li.Quantity), props.Text{Size: 8, Align: align.Right})),
			col.New(2).Add(text.New(li.Unit, props.Text{Size: 8})),
			col.New(3).Add(
				text.New(fmt.Sprintf("%s %.2f", quote.Currency, li.Amount), props.Text{Size: 8, Align: align.Right}),
			),
		)
	}

	m.AddRow(8)

	// Total row
	m.AddRow(8,
		col.New(9).Add(
			text.New("Total:", props.Text{Size: 10, Style: fontstyle.Bold, Align: align.Right}),
		),
		col.New(3).Add(
			text.New(fmt.Sprintf("%s %.2f", quote.Currency, quote.TotalAmount), props.Text{
				Size: 11, Style: fontstyle.Bold, Align: align.Right,
			}),
		),
	)

	// Notes
	if quote.Notes != "" {
		m.AddRow(10)
		m.AddRow(8,
			col.New(12).Add(text.New("Notes", props.Text{Size: 11, Style: fontstyle.Bold})),
		)
		m.AddRow(12,
			col.New(12).Add(text.New(quote.Notes, props.Text{Size: 9})),
		)
	}

	// Footer note
	m.AddRow(10)
	m.AddRow(6,
		col.New(12).Add(
			text.New(fmt.Sprintf("This quote is valid until %s. Accepted quotes will be converted into a formal contract.",
				quote.ValidUntil.Format("January 2, 2006")), props.Text{
				Size: 8, Style: fontstyle.Italic,
			}),
		),
	)

	document, err := m.Generate()
	if err != nil {
		return fmt.Errorf("failed to generate quote PDF: %w", err)
	}
	if err := document.Save(outputPath); err != nil {
		return fmt.Errorf("failed to save quote PDF: %w", err)
	}
	return nil
}
