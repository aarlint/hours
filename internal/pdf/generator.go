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
	"github.com/johnfercher/maroto/v2/pkg/consts/linestyle"
	"github.com/johnfercher/maroto/v2/pkg/consts/orientation"
	"github.com/johnfercher/maroto/v2/pkg/consts/pagesize"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/props"
)

type InvoiceGenerator struct{}

func NewInvoiceGenerator() *InvoiceGenerator {
	return &InvoiceGenerator{}
}

// Ledger palette: high-contrast, accountant feel.
var (
	colorInk    = &props.Color{Red: 28, Green: 27, Blue: 25}    // #1C1B19
	colorBody   = &props.Color{Red: 85, Green: 83, Blue: 78}    // #55534E
	colorMuted  = &props.Color{Red: 138, Green: 135, Blue: 127} // #8A877F
	colorRule   = &props.Color{Red: 28, Green: 27, Blue: 25}
	colorPaper  = &props.Color{Red: 251, Green: 250, Blue: 247} // #FBFAF7
	colorHairline = &props.Color{Red: 200, Green: 197, Blue: 190}
)

const (
	fontMono = fontfamily.Courier
	fontSans = fontfamily.Helvetica
)

func wrapText(s string, maxWidth int) []string {
	if s == "" {
		return []string{""}
	}
	lines := strings.Split(s, "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if len(l) <= maxWidth {
			out = append(out, l)
			continue
		}
		words := strings.Fields(l)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		cur := words[0]
		for _, w := range words[1:] {
			if len(cur)+1+len(w) <= maxWidth {
				cur += " " + w
			} else {
				out = append(out, cur)
				cur = w
			}
		}
		if cur != "" {
			out = append(out, cur)
		}
	}
	return out
}

func (g *InvoiceGenerator) Generate(invoice models.Invoice, payment models.PaymentDetails, recipients []models.Recipient, business models.BusinessInfo, outputPath string) error {
	cfg := config.NewBuilder().
		WithPageSize(pagesize.Letter).
		WithLeftMargin(15).
		WithRightMargin(15).
		WithTopMargin(15).
		WithBottomMargin(15).
		WithDefaultFont(&props.Font{Family: fontMono, Size: 10, Color: colorInk}).
		Build()

	m := maroto.New(cfg)

	// totals
	var totalHours, hoursAmount, expensesAmount float64
	currency := "USD"
	if len(invoice.TimeEntries) > 0 && invoice.TimeEntries[0].Contract != nil {
		currency = invoice.TimeEntries[0].Contract.Currency
	}
	for _, e := range invoice.TimeEntries {
		if e.Contract == nil {
			continue
		}
		totalHours += e.Hours
		hoursAmount += e.Hours * e.Contract.HourlyRate
	}
	for _, ex := range invoice.Expenses {
		expensesAmount += ex.Amount
		if currency == "USD" && ex.Currency != "" {
			currency = ex.Currency
		}
	}
	grandTotal := hoursAmount + expensesAmount

	addMastBlock(m, invoice)
	addRemitBlock(m, invoice, business, recipients)
	if len(invoice.TimeEntries) > 0 && invoice.TimeEntries[0].Contract != nil {
		addContractBar(m, *invoice.TimeEntries[0].Contract)
	}
	if len(invoice.TimeEntries) > 0 {
		addEntriesTable(m, invoice.TimeEntries, currency)
	}
	if len(invoice.Expenses) > 0 {
		addExpensesTable(m, invoice.Expenses, currency)
	}
	addTotals(m, totalHours, hoursAmount, expensesAmount, grandTotal, currency)
	addPaymentLedger(m, payment, invoice)

	doc, err := m.Generate()
	if err != nil {
		return fmt.Errorf("failed to generate PDF: %w", err)
	}
	if err := doc.Save(outputPath); err != nil {
		return fmt.Errorf("failed to save PDF: %w", err)
	}
	return nil
}

// ─── sections ────────────────────────────────────────────────────────────────

func addMastBlock(m core.Maroto, inv models.Invoice) {
	heavyRule(m, 0.7)
	m.AddRow(3)
	m.AddRow(4,
		labelMono(3, "DOCUMENT"),
		labelMono(3, "NUMBER"),
		labelMono(3, "ISSUED"),
		labelMono(3, "DUE"),
	)
	m.AddRow(6,
		valueMono(3, "INVOICE"),
		valueMono(3, inv.InvoiceNumber),
		valueMono(3, strings.ToUpper(inv.IssueDate.Format("02 Jan 2006"))),
		valueMono(3, strings.ToUpper(inv.DueDate.Format("02 Jan 2006"))),
	)
	m.AddRow(3)
	heavyRule(m, 0.7)
}

func addRemitBlock(m core.Maroto, inv models.Invoice, biz models.BusinessInfo, recipients []models.Recipient) {
	m.AddRow(6)
	m.AddRow(4,
		labelMono(6, "REMIT FROM"),
		labelMono(6, "REMIT TO"),
	)
	m.AddRow(2)

	from := stripEmpty([]string{
		biz.BusinessName,
		fmt.Sprintf("%s <%s>", biz.ContactName, biz.Email),
		biz.Phone,
		biz.Address,
		joinCityStateZip(biz.City, biz.State, biz.ZipCode),
		biz.Country,
	})
	to := stripEmpty([]string{
		inv.Client.Name,
		inv.Client.Address,
		joinCityStateZip(inv.Client.City, inv.Client.State, inv.Client.ZipCode),
		inv.Client.Country,
	})
	for _, r := range recipients {
		to = append(to, fmt.Sprintf("%s <%s>", r.Name, r.Email))
	}

	max := len(from)
	if len(to) > max {
		max = len(to)
	}
	for i := 0; i < max; i++ {
		var l, r string
		if i < len(from) {
			l = from[i]
		}
		if i < len(to) {
			r = to[i]
		}
		m.AddRow(4.5,
			col.New(6).Add(text.New(l, props.Text{Family: fontMono, Size: 9, Color: colorInk})),
			col.New(6).Add(text.New(r, props.Text{Family: fontMono, Size: 9, Color: colorInk})),
		)
	}
}

func addContractBar(m core.Maroto, c models.Contract) {
	m.AddRow(5)

	contractText := fmt.Sprintf("%s · %s", c.ContractNumber, c.Name)
	contractLines := wrapText(contractText, 38)
	height := 8.0
	if len(contractLines) > 1 {
		height = 5.0 + float64(len(contractLines))*3.5
	}

	bar := row.New(height).WithStyle(&props.Cell{BackgroundColor: colorInk})
	bar.Add(
		col.New(2).Add(text.New("CONTRACT", props.Text{
			Family: fontMono, Size: 9, Color: colorPaper, Top: 2, Left: 2,
		})),
		col.New(5).Add(text.New(strings.Join(contractLines, "\n"), props.Text{
			Family: fontMono, Size: 9, Color: colorPaper, Top: 2,
		})),
		col.New(3).Add(text.New(strings.ToUpper(fmt.Sprintf("RATE %s %.2f/HR", c.Currency, c.HourlyRate)), props.Text{
			Family: fontMono, Size: 9, Color: colorPaper, Top: 2,
		})),
		col.New(2).Add(text.New(strings.ToUpper(orFallback(c.PaymentTerms, "Net 30")), props.Text{
			Family: fontMono, Size: 9, Color: colorPaper, Align: align.Right, Top: 2, Right: 2,
		})),
	)
	m.AddRows(bar)
}

func addEntriesTable(m core.Maroto, entries []models.TimeEntry, currency string) {
	m.AddRow(5)
	heavyRule(m, 0.4)
	m.AddRow(5,
		labelMono(2, "DATE"),
		labelMono(7, "DESCRIPTION"),
		labelMonoRight(1, "HRS"),
		labelMonoRight(2, "AMOUNT"),
	)
	hairlineRule(m)

	for _, e := range entries {
		if e.Contract == nil {
			continue
		}
		amt := e.Hours * e.Contract.HourlyRate
		descLines := wrapText(e.Description, 56)
		h := 5.0
		if len(descLines) > 1 {
			h = 5.0 + float64(len(descLines)-1)*3.5
		}
		m.AddRow(h,
			col.New(2).Add(text.New(e.Date.Format("2006-01-02"), props.Text{
				Family: fontMono, Size: 9, Color: colorBody, Top: 1,
			})),
			col.New(7).Add(text.New(strings.Join(descLines, "\n"), props.Text{
				Family: fontMono, Size: 9, Color: colorInk, Top: 1,
			})),
			col.New(1).Add(text.New(fmt.Sprintf("%.2f", e.Hours), props.Text{
				Family: fontMono, Size: 9, Color: colorInk, Align: align.Right, Top: 1,
			})),
			col.New(2).Add(text.New(formatMoney(currency, amt), props.Text{
				Family: fontMono, Size: 9, Color: colorInk, Align: align.Right, Top: 1,
			})),
		)
		dashedRule(m)
	}
}

func addExpensesTable(m core.Maroto, expenses []models.Expense, currency string) {
	m.AddRow(5)
	heavyRule(m, 0.4)
	m.AddRow(5,
		labelMono(2, "DATE"),
		labelMono(8, "EXPENSE"),
		labelMonoRight(2, "AMOUNT"),
	)
	hairlineRule(m)

	for _, e := range expenses {
		desc := e.Description
		if e.Category != "" {
			desc = fmt.Sprintf("[%s] %s", strings.ToUpper(e.Category), e.Description)
		}
		descLines := wrapText(desc, 64)
		h := 5.0
		if len(descLines) > 1 {
			h = 5.0 + float64(len(descLines)-1)*3.5
		}
		exCurrency := e.Currency
		if exCurrency == "" {
			exCurrency = currency
		}
		m.AddRow(h,
			col.New(2).Add(text.New(e.Date.Format("2006-01-02"), props.Text{
				Family: fontMono, Size: 9, Color: colorBody, Top: 1,
			})),
			col.New(8).Add(text.New(strings.Join(descLines, "\n"), props.Text{
				Family: fontMono, Size: 9, Color: colorInk, Top: 1,
			})),
			col.New(2).Add(text.New(formatMoney(exCurrency, e.Amount), props.Text{
				Family: fontMono, Size: 9, Color: colorInk, Align: align.Right, Top: 1,
			})),
		)
		dashedRule(m)
	}
}

func addTotals(m core.Maroto, totalHours, hoursAmount, expensesAmount, grandTotal float64, currency string) {
	m.AddRow(2)
	heavyRule(m, 0.7)
	m.AddRow(6,
		col.New(2),
		col.New(7).Add(text.New("HOURS SUBTOTAL", props.Text{
			Family: fontMono, Size: 9, Color: colorBody, Align: align.Right, Top: 1,
		})),
		col.New(1).Add(text.New(fmt.Sprintf("%.2f", totalHours), props.Text{
			Family: fontMono, Size: 9, Color: colorInk, Align: align.Right, Top: 1,
		})),
		col.New(2).Add(text.New(formatMoney(currency, hoursAmount), props.Text{
			Family: fontMono, Size: 9, Color: colorInk, Align: align.Right, Top: 1,
		})),
	)
	if expensesAmount > 0 {
		m.AddRow(6,
			col.New(10).Add(text.New("EXPENSES SUBTOTAL", props.Text{
				Family: fontMono, Size: 9, Color: colorBody, Align: align.Right, Top: 1,
			})),
			col.New(2).Add(text.New(formatMoney(currency, expensesAmount), props.Text{
				Family: fontMono, Size: 9, Color: colorInk, Align: align.Right, Top: 1,
			})),
		)
	}
	hairlineRule(m)
	m.AddRow(8,
		col.New(8).Add(text.New("TOTAL DUE", props.Text{
			Family: fontMono, Size: 11, Color: colorInk, Align: align.Right, Top: 2,
		})),
		col.New(2).Add(text.New(strings.ToUpper(currency), props.Text{
			Family: fontMono, Size: 9, Color: colorMuted, Align: align.Right, Top: 3,
		})),
		col.New(2).Add(text.New(formatMoney(currency, grandTotal), props.Text{
			Family: fontMono, Size: 14, Color: colorInk, Align: align.Right, Top: 1,
		})),
	)
	heavyRule(m, 0.7)
}

func addPaymentLedger(m core.Maroto, p models.PaymentDetails, inv models.Invoice) {
	m.AddRow(8)
	m.AddRow(4, labelMono(12, "PAYMENT"))
	m.AddRow(2)

	type line struct{ label, val string }
	lines := []line{}
	if p.BankName != "" {
		lines = append(lines, line{"BANK", p.BankName})
	}
	if p.AccountNumber != "" {
		lines = append(lines, line{"ACCOUNT", p.AccountNumber})
	}
	if p.RoutingNumber != "" {
		lines = append(lines, line{"ROUTING", p.RoutingNumber})
	}
	if p.SwiftCode != "" {
		lines = append(lines, line{"SWIFT", p.SwiftCode})
	}
	if p.PaymentTerms != "" {
		lines = append(lines, line{"TERMS", p.PaymentTerms})
	}
	lines = append(lines, line{"REFERENCE", "#" + inv.InvoiceNumber})

	for _, l := range lines {
		m.AddRow(4.5,
			col.New(12).Add(text.New(formatLeader(l.label, l.val, 70), props.Text{
				Family: fontMono, Size: 9, Color: colorInk,
			})),
		)
	}

	if p.Notes != "" {
		m.AddRow(6)
		m.AddRow(4, labelMono(12, "NOTES"))
		m.AddRow(2)
		for _, l := range wrapText(p.Notes, 90) {
			m.AddRow(4.5, col.New(12).Add(text.New(l, props.Text{
				Family: fontMono, Size: 9, Color: colorBody,
			})))
		}
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func heavyRule(m core.Maroto, thickness float64) {
	m.AddRow(0.5, col.New(12).Add(line.New(props.Line{
		Color: colorRule, Thickness: thickness,
		Orientation: orientation.Horizontal, SizePercent: 100, OffsetPercent: 50,
	})))
}

func hairlineRule(m core.Maroto) {
	m.AddRow(0.4, col.New(12).Add(line.New(props.Line{
		Color: colorHairline, Thickness: 0.2,
		Orientation: orientation.Horizontal, SizePercent: 100, OffsetPercent: 50,
	})))
}

func dashedRule(m core.Maroto) {
	m.AddRow(0.4, col.New(12).Add(line.New(props.Line{
		Color: colorHairline, Thickness: 0.2, Style: linestyle.Dashed,
		Orientation: orientation.Horizontal, SizePercent: 100, OffsetPercent: 50,
	})))
}

func labelMono(size int, t string) core.Col {
	return col.New(size).Add(text.New(strings.ToUpper(t), props.Text{
		Family: fontMono, Size: 7.5, Color: colorMuted,
	}))
}

func labelMonoRight(size int, t string) core.Col {
	return col.New(size).Add(text.New(strings.ToUpper(t), props.Text{
		Family: fontMono, Size: 7.5, Color: colorMuted, Align: align.Right,
	}))
}

func valueMono(size int, v string) core.Col {
	return col.New(size).Add(text.New(v, props.Text{
		Family: fontMono, Size: 10, Color: colorInk,
	}))
}

func stripEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func joinCityStateZip(city, state, zip string) string {
	parts := stripEmpty([]string{city, state})
	left := strings.Join(parts, ", ")
	if zip != "" {
		if left != "" {
			return left + " " + zip
		}
		return zip
	}
	return left
}

func formatMoney(currency string, amount float64) string {
	switch strings.ToUpper(currency) {
	case "USD":
		return "$" + formatThousands(amount)
	case "EUR":
		return "€" + formatThousands(amount)
	case "GBP":
		return "£" + formatThousands(amount)
	default:
		return fmt.Sprintf("%s %s", currency, formatThousands(amount))
	}
}

func formatThousands(amount float64) string {
	s := fmt.Sprintf("%.2f", amount)
	dot := strings.Index(s, ".")
	intPart, dec := s[:dot], s[dot:]
	negative := strings.HasPrefix(intPart, "-")
	if negative {
		intPart = intPart[1:]
	}
	n := len(intPart)
	if n <= 3 {
		if negative {
			return "-" + intPart + dec
		}
		return intPart + dec
	}
	first := n % 3
	var b strings.Builder
	if first > 0 {
		b.WriteString(intPart[:first])
		if n > first {
			b.WriteString(",")
		}
	}
	for i := first; i < n; i += 3 {
		b.WriteString(intPart[i : i+3])
		if i+3 < n {
			b.WriteString(",")
		}
	}
	out := b.String() + dec
	if negative {
		return "-" + out
	}
	return out
}

func formatLeader(label, value string, width int) string {
	label = strings.ToUpper(label)
	gap := width - len(label) - len(value)
	if gap < 3 {
		gap = 3
	}
	return label + " " + strings.Repeat(".", gap) + " " + value
}

func orFallback(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
