package pdf

import (
	"fmt"
	"strings"

	"github.com/austin/hours-mcp/internal/models"
	"github.com/johnfercher/maroto/v2"
	"github.com/johnfercher/maroto/v2/pkg/components/col"
	"github.com/johnfercher/maroto/v2/pkg/components/line"
	"github.com/johnfercher/maroto/v2/pkg/components/row"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/config"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontfamily"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/consts/linestyle"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/props"
)

// InvoiceGenerator renders invoice PDFs.
//
// Design direction (Nothing-inspired, senior-designer craft):
//   - Confident typography over ornament. Space-maximalist not ink-maximalist.
//   - Hero moves: enormous invoice number up top, hero TOTAL DUE near the bottom.
//   - Hairline rules (solid, 0.3pt) separate sections; no boxes, no zebra stripes.
//   - Monospace (Courier) does the heavy lifting on values; Helvetica carries prose.
//   - Asymmetric: primary content anchored left, meta pinned right.
type InvoiceGenerator struct{}

func NewInvoiceGenerator() *InvoiceGenerator {
	return &InvoiceGenerator{}
}

// palette
var (
	inkPrimary   = &props.Color{Red: 17, Green: 17, Blue: 17}     // near-black
	inkSecondary = &props.Color{Red: 102, Green: 102, Blue: 102}  // ~60%
	inkDisabled  = &props.Color{Red: 160, Green: 160, Blue: 160}  // ~40%
	inkRule      = &props.Color{Red: 0, Green: 0, Blue: 0}        // hairlines
	inkAccent    = &props.Color{Red: 215, Green: 25, Blue: 33}    // #D71921 — single red accent
)

// Type tokens
const (
	serifFam = fontfamily.Helvetica // stands in for Space Grotesk
	monoFam  = fontfamily.Courier   // stands in for Space Mono

	sizeHero    = 32.0 // the invoice number
	sizeTitle   = 18.0
	sizeBody    = 9.5
	sizeSmall   = 8.5
	sizeLabel   = 7.5
	sizeCaption = 7.0

	ruleThick = 0.3
)

func (g *InvoiceGenerator) Generate(
	invoice models.Invoice,
	payment models.PaymentDetails,
	recipients []models.Recipient,
	business models.BusinessInfo,
	outputPath string,
) error {
	m := maroto.New(
		config.NewBuilder().
			WithLeftMargin(18).
			WithRightMargin(18).
			WithTopMargin(18).
			WithBottomMargin(18).
			WithDefaultFont(&props.Font{Family: serifFam, Size: sizeBody, Color: inkPrimary}).
			Build(),
	)

	// ── Header strip ─────────────────────────────────────────────
	// Business name (small, top-left) / huge "INVOICE" wordmark right
	m.AddRow(12,
		col.New(7).Add(
			text.New(strings.ToUpper(business.BusinessName), props.Text{
				Family: monoFam, Size: sizeLabel, Color: inkPrimary,
				Top: 2, VerticalPadding: 0.5,
			}),
		),
		col.New(5).Add(
			text.New("INVOICE", props.Text{
				Family: serifFam, Size: sizeTitle,
				Style: fontstyle.Bold, Align: align.Right, Top: 0,
			}),
		),
	)

	// Single red dot anchoring the sheet — the one moment of expression.
	m.AddRow(2,
		col.New(12).Add(
			text.New("·", props.Text{
				Family: serifFam, Size: 18, Color: inkAccent,
				Align: align.Right, Top: -6,
			}),
		),
	)

	addHairline(m)

	// ── Hero invoice number + right-side meta ────────────────────
	// Left: label + huge invoice number. Right: stacked ISSUED/DUE.
	m.AddRow(6,
		col.New(7).Add(
			text.New("INVOICE NO.", props.Text{
				Family: monoFam, Size: sizeLabel, Color: inkDisabled, Top: 3,
			}),
		),
		col.New(5).Add(
			text.New("ISSUED", props.Text{
				Family: monoFam, Size: sizeLabel, Color: inkDisabled,
				Align: align.Right, Top: 3,
			}),
		),
	)
	m.AddRow(14,
		col.New(7).Add(
			text.New(invoice.InvoiceNumber, props.Text{
				Family: monoFam, Size: sizeHero, Color: inkPrimary,
				Style: fontstyle.Normal, Top: 0,
			}),
		),
		col.New(5).Add(
			text.New(invoice.IssueDate.Format("Jan 2, 2006"), props.Text{
				Family: serifFam, Size: sizeBody, Color: inkPrimary,
				Align: align.Right, Top: 2,
			}),
		),
	)
	m.AddRow(4,
		col.New(7),
		col.New(5).Add(
			text.New("DUE", props.Text{
				Family: monoFam, Size: sizeLabel, Color: inkDisabled,
				Align: align.Right,
			}),
		),
	)
	m.AddRow(5,
		col.New(7),
		col.New(5).Add(
			text.New(invoice.DueDate.Format("Jan 2, 2006"), props.Text{
				Family: serifFam, Size: sizeBody, Color: inkPrimary,
				Align: align.Right,
			}),
		),
	)

	m.AddRow(4)
	addHairline(m)

	// ── FROM  ·  BILL TO ────────────────────────────────────────
	m.AddRow(6,
		col.New(6).Add(
			text.New("FROM", props.Text{
				Family: monoFam, Size: sizeLabel, Color: inkDisabled, Top: 3,
			}),
		),
		col.New(6).Add(
			text.New("BILL TO", props.Text{
				Family: monoFam, Size: sizeLabel, Color: inkDisabled, Top: 3,
			}),
		),
	)

	fromLines := partyLines(
		business.BusinessName,
		business.ContactName,
		business.Email,
		business.Phone,
		joinAddress(business.Address, business.City, business.State, business.ZipCode, business.Country),
		business.Website,
	)
	billToLines := []string{invoice.Client.Name}
	billToLines = append(billToLines, nonEmpty(
		invoice.Client.Address,
		cityStateZip(invoice.Client.City, invoice.Client.State, invoice.Client.ZipCode),
		invoice.Client.Country,
	)...)
	for _, r := range recipients {
		if r.Email != "" {
			billToLines = append(billToLines, fmt.Sprintf("%s <%s>", r.Name, r.Email))
		} else if r.Name != "" {
			billToLines = append(billToLines, r.Name)
		}
	}

	addressRows(m, fromLines, billToLines)

	m.AddRow(3)
	addHairline(m)

	// ── Contract strip ──────────────────────────────────────────
	if len(invoice.TimeEntries) > 0 && invoice.TimeEntries[0].Contract != nil {
		c := invoice.TimeEntries[0].Contract
		m.AddRow(5,
			col.New(12).Add(
				text.New("CONTRACT", props.Text{
					Family: monoFam, Size: sizeLabel, Color: inkDisabled, Top: 2,
				}),
			),
		)
		rateTerms := fmt.Sprintf("%s %.2f / hour", c.Currency, c.HourlyRate)
		if c.PaymentTerms != "" {
			rateTerms += "   ·   " + c.PaymentTerms
		}
		m.AddRow(5,
			col.New(7).Add(
				text.New(fmt.Sprintf("%s  ·  %s", c.ContractNumber, c.Name), props.Text{
					Family: serifFam, Size: sizeBody, Color: inkPrimary,
				}),
			),
			col.New(5).Add(
				text.New(rateTerms, props.Text{
					Family: monoFam, Size: sizeSmall, Color: inkSecondary,
					Align: align.Right,
				}),
			),
		)
		m.AddRow(3)
		addHairline(m)
	}

	// ── Line items header ───────────────────────────────────────
	m.AddRow(7,
		col.New(12).Add(
			text.New(fmt.Sprintf("LINE ITEMS   ·   %d %s", len(invoice.TimeEntries),
				plural(len(invoice.TimeEntries), "ENTRY", "ENTRIES")),
				props.Text{
					Family: monoFam, Size: sizeLabel, Color: inkDisabled, Top: 3,
				}),
		),
	)

	m.AddRow(6,
		col.New(2).Add(
			text.New("DATE", props.Text{
				Family: monoFam, Size: sizeCaption, Color: inkDisabled, Top: 2,
			}),
		),
		col.New(6).Add(
			text.New("DESCRIPTION", props.Text{
				Family: monoFam, Size: sizeCaption, Color: inkDisabled, Top: 2,
			}),
		),
		col.New(1).Add(
			text.New("HRS", props.Text{
				Family: monoFam, Size: sizeCaption, Color: inkDisabled,
				Align: align.Right, Top: 2,
			}),
		),
		col.New(3).Add(
			text.New("AMOUNT", props.Text{
				Family: monoFam, Size: sizeCaption, Color: inkDisabled,
				Align: align.Right, Top: 2,
			}),
		),
	)
	addHairline(m)

	var totalHours, totalAmount float64
	currency := "USD"
	for _, e := range invoice.TimeEntries {
		if e.Contract == nil {
			continue
		}
		if e.Contract.Currency != "" {
			currency = e.Contract.Currency
		}
		amount := e.Hours * e.Contract.HourlyRate
		totalHours += e.Hours
		totalAmount += amount

		m.AddRow(6,
			col.New(2).Add(
				text.New(e.Date.Format("2006-01-02"), props.Text{
					Family: monoFam, Size: sizeSmall, Color: inkDisabled, Top: 1.5,
				}),
			),
			col.New(6).Add(
				text.New(truncate(e.Description, 78), props.Text{
					Family: serifFam, Size: sizeSmall, Color: inkPrimary, Top: 1.5,
				}),
			),
			col.New(1).Add(
				text.New(fmt.Sprintf("%.2f", e.Hours), props.Text{
					Family: monoFam, Size: sizeSmall, Color: inkSecondary,
					Align: align.Right, Top: 1.5,
				}),
			),
			col.New(3).Add(
				text.New(fmt.Sprintf("%s %s", e.Contract.Currency, money(amount)), props.Text{
					Family: monoFam, Size: sizeSmall, Color: inkPrimary,
					Align: align.Right, Top: 1.5,
				}),
			),
		)
	}

	addHairline(m)

	// Subtotal row (compact)
	m.AddRow(6,
		col.New(8).Add(
			text.New("SUBTOTAL", props.Text{
				Family: monoFam, Size: sizeLabel, Color: inkDisabled,
				Align: align.Right, Top: 2,
			}),
		),
		col.New(1).Add(
			text.New(fmt.Sprintf("%.2f", totalHours), props.Text{
				Family: monoFam, Size: sizeSmall, Color: inkSecondary,
				Align: align.Right, Top: 2,
			}),
		),
		col.New(3).Add(
			text.New(fmt.Sprintf("%s %s", currency, money(totalAmount)), props.Text{
				Family: monoFam, Size: sizeSmall, Color: inkSecondary,
				Align: align.Right, Top: 2,
			}),
		),
	)

	m.AddRow(6)

	// ── Hero TOTAL DUE ──────────────────────────────────────────
	m.AddRow(5,
		col.New(12).Add(
			text.New("TOTAL DUE", props.Text{
				Family: monoFam, Size: sizeLabel, Color: inkDisabled,
				Align: align.Right, Top: 2,
			}),
		),
	)
	m.AddRow(14,
		col.New(12).Add(
			text.New(fmt.Sprintf("%s %s", currency, money(totalAmount)), props.Text{
				Family: monoFam, Size: sizeHero - 4, Color: inkPrimary,
				Style: fontstyle.Normal, Align: align.Right,
			}),
		),
	)
	m.AddRow(5,
		col.New(12).Add(
			text.New(fmt.Sprintf("%.2f HOURS  ·  %d %s",
				totalHours,
				len(invoice.TimeEntries),
				plural(len(invoice.TimeEntries), "ENTRY", "ENTRIES")),
				props.Text{
					Family: monoFam, Size: sizeCaption, Color: inkDisabled,
					Align: align.Right,
				}),
		),
	)

	m.AddRow(5)
	addHairline(m)

	// ── Payment information ────────────────────────────────────
	if paymentHasAny(payment) {
		m.AddRow(6,
			col.New(12).Add(
				text.New("PAYMENT", props.Text{
					Family: monoFam, Size: sizeLabel, Color: inkDisabled, Top: 3,
				}),
			),
		)
		addPaymentRow(m, "BANK", payment.BankName)
		addPaymentRow(m, "ACCOUNT", maskAccount(payment.AccountNumber))
		addPaymentRow(m, "ROUTING", payment.RoutingNumber)
		addPaymentRow(m, "SWIFT / BIC", payment.SwiftCode)
		addPaymentRow(m, "TERMS", payment.PaymentTerms)
		if payment.Notes != "" {
			m.AddRow(4)
			m.AddRow(10,
				col.New(12).Add(
					text.New(payment.Notes, props.Text{
						Family: serifFam, Size: sizeSmall, Color: inkSecondary,
					}),
				),
			)
		}
		m.AddRow(4)
		addHairline(m)
	}

	// ── Footer ──────────────────────────────────────────────────
	m.AddRow(5,
		col.New(6).Add(
			text.New("THANK YOU FOR YOUR BUSINESS", props.Text{
				Family: monoFam, Size: sizeCaption, Color: inkDisabled, Top: 2,
			}),
		),
		col.New(6).Add(
			text.New(fmt.Sprintf("PAGE 1  ·  INV %s", invoice.InvoiceNumber), props.Text{
				Family: monoFam, Size: sizeCaption, Color: inkDisabled,
				Align: align.Right, Top: 2,
			}),
		),
	)

	document, err := m.Generate()
	if err != nil {
		return fmt.Errorf("failed to generate PDF document: %w", err)
	}
	if err := document.Save(outputPath); err != nil {
		return fmt.Errorf("failed to save PDF: %w", err)
	}
	return nil
}

// ── helpers ────────────────────────────────────────────────────

// addHairline draws a full-width 0.3pt rule. Used as a section break.
func addHairline(m core.Maroto) {
	m.AddRow(1.2,
		col.New(12).Add(
			line.New(props.Line{
				Color:         inkRule,
				Style:         linestyle.Solid,
				Thickness:     ruleThick,
				SizePercent:   100,
				OffsetPercent: 50,
			}),
		),
	)
}

// addressRows emits two parallel columns of address lines. The shorter side is
// padded with empty rows so the total height stays aligned.
func addressRows(m core.Maroto, left, right []string) {
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	for i := 0; i < n; i++ {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		color := inkPrimary
		if i > 0 {
			color = inkSecondary
		}
		m.AddRow(4.6,
			col.New(6).Add(
				text.New(l, props.Text{
					Family: serifFam, Size: sizeSmall, Color: color, Top: 1,
				}),
			),
			col.New(6).Add(
				text.New(r, props.Text{
					Family: serifFam, Size: sizeSmall, Color: color, Top: 1,
				}),
			),
		)
	}
}

func addPaymentRow(m core.Maroto, label, value string) {
	if value == "" {
		return
	}
	m.AddRow(5,
		col.New(3).Add(
			text.New(label, props.Text{
				Family: monoFam, Size: sizeCaption, Color: inkDisabled, Top: 1.5,
			}),
		),
		col.New(9).Add(
			text.New(value, props.Text{
				Family: monoFam, Size: sizeSmall, Color: inkPrimary, Top: 1.5,
			}),
		),
	)
}

// row is imported only to satisfy its indirect use path on some maroto paths;
// the explicit import keeps future row.New() upgrades trivial.
var _ = row.New

func partyLines(parts ...string) []string { return nonEmpty(parts...) }

func nonEmpty(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinAddress(street, city, state, zip, country string) string {
	var parts []string
	if street != "" {
		parts = append(parts, street)
	}
	csz := cityStateZip(city, state, zip)
	if csz != "" {
		parts = append(parts, csz)
	}
	if country != "" {
		parts = append(parts, country)
	}
	return strings.Join(parts, " · ")
}

func cityStateZip(city, state, zip string) string {
	s := ""
	if city != "" {
		s = city
	}
	if state != "" {
		if s != "" {
			s += ", "
		}
		s += state
	}
	if zip != "" {
		if s != "" {
			s += " "
		}
		s += zip
	}
	return s
}

func paymentHasAny(p models.PaymentDetails) bool {
	return p.BankName != "" || p.AccountNumber != "" || p.RoutingNumber != "" ||
		p.SwiftCode != "" || p.PaymentTerms != "" || p.Notes != ""
}

// maskAccount shows only the last 4 digits. Readers who need the full number
// already have it; the sheet should be safe to leave on a desk.
func maskAccount(acct string) string {
	if acct == "" {
		return ""
	}
	if len(acct) <= 4 {
		return acct
	}
	return "•••• " + acct[len(acct)-4:]
}

func money(v float64) string {
	// thousands-separated with two decimals, no locale grief.
	neg := ""
	if v < 0 {
		neg = "-"
		v = -v
	}
	whole := int64(v)
	frac := int64((v-float64(whole))*100 + 0.5)
	// manual grouping
	s := fmt.Sprintf("%d", whole)
	if len(s) > 3 {
		var b strings.Builder
		pre := len(s) % 3
		if pre > 0 {
			b.WriteString(s[:pre])
			if len(s) > pre {
				b.WriteByte(',')
			}
		}
		for i := pre; i < len(s); i += 3 {
			b.WriteString(s[i : i+3])
			if i+3 < len(s) {
				b.WriteByte(',')
			}
		}
		s = b.String()
	}
	return fmt.Sprintf("%s%s.%02d", neg, s, frac)
}

func plural(n int, singular, pluralWord string) string {
	if n == 1 {
		return singular
	}
	return pluralWord
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
