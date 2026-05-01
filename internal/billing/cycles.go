package billing

import (
	"fmt"
	"time"

	"github.com/austin/hours-mcp/internal/models"
)

// CycleType represents the type of billing cycle
type CycleType string

const (
	Monthly    CycleType = "monthly"
	Weekly     CycleType = "weekly"
	Quarterly  CycleType = "quarterly"
	Annually   CycleType = "annually"
)

// ValidCycleTypes returns all valid cycle types
func ValidCycleTypes() []string {
	return []string{string(Monthly), string(Weekly), string(Quarterly), string(Annually)}
}

// IsValidCycleType checks if a cycle type is valid
func IsValidCycleType(cycleType string) bool {
	for _, validType := range ValidCycleTypes() {
		if cycleType == validType {
			return true
		}
	}
	return false
}

// PeriodInfo contains information about a billing period
type PeriodInfo struct {
	StartDate time.Time
	EndDate   time.Time
	CycleType string
	CycleDay  int
}

// CalculateNextBillingDate calculates when a contract should next be billed
func CalculateNextBillingDate(contract models.Contract) (*time.Time, error) {
	if contract.BillingCycleDay == nil || contract.BillingCycleType == "" {
		return nil, nil // No billing cycle configured
	}

	now := time.Now()
	cycleDay := *contract.BillingCycleDay

	switch CycleType(contract.BillingCycleType) {
	case Monthly:
		return calculateNextMonthlyBilling(now, cycleDay), nil
	case Weekly:
		return calculateNextWeeklyBilling(now, cycleDay), nil
	case Quarterly:
		return calculateNextQuarterlyBilling(now, cycleDay), nil
	case Annually:
		return calculateNextAnnualBilling(now, cycleDay, contract.StartDate), nil
	default:
		return nil, fmt.Errorf("unsupported billing cycle type: %s", contract.BillingCycleType)
	}
}

// CalculateBillingPeriod calculates the billing period for a contract based on a reference date
func CalculateBillingPeriod(contract models.Contract, referenceDate time.Time) (*PeriodInfo, error) {
	if contract.BillingCycleDay == nil || contract.BillingCycleType == "" {
		return nil, fmt.Errorf("contract has no billing cycle configured")
	}

	cycleDay := *contract.BillingCycleDay

	switch CycleType(contract.BillingCycleType) {
	case Monthly:
		start, end := calculateMonthlyPeriod(referenceDate, cycleDay)
		return &PeriodInfo{
			StartDate: start,
			EndDate:   end,
			CycleType: contract.BillingCycleType,
			CycleDay:  cycleDay,
		}, nil
	case Weekly:
		start, end := calculateWeeklyPeriod(referenceDate, cycleDay)
		return &PeriodInfo{
			StartDate: start,
			EndDate:   end,
			CycleType: contract.BillingCycleType,
			CycleDay:  cycleDay,
		}, nil
	case Quarterly:
		start, end := calculateQuarterlyPeriod(referenceDate, cycleDay)
		return &PeriodInfo{
			StartDate: start,
			EndDate:   end,
			CycleType: contract.BillingCycleType,
			CycleDay:  cycleDay,
		}, nil
	case Annually:
		start, end := calculateAnnualPeriod(referenceDate, cycleDay, contract.StartDate)
		return &PeriodInfo{
			StartDate: start,
			EndDate:   end,
			CycleType: contract.BillingCycleType,
			CycleDay:  cycleDay,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported billing cycle type: %s", contract.BillingCycleType)
	}
}

// calculateNextMonthlyBilling calculates the next monthly billing date
func calculateNextMonthlyBilling(now time.Time, cycleDay int) *time.Time {
	year, month, day := now.Date()
	location := now.Location()

	// Get the billing day for current month
	currentMonthBillingDay := getValidDayForMonth(year, month, cycleDay)
	currentMonthBilling := time.Date(year, month, currentMonthBillingDay, 0, 0, 0, 0, location)

	// If today is before or on the current month's billing day, use current month
	if day <= currentMonthBillingDay {
		return &currentMonthBilling
	}

	// Otherwise, move to next month
	nextMonth := month + 1
	nextYear := year
	if nextMonth > 12 {
		nextMonth = 1
		nextYear++
	}

	nextBillingDay := getValidDayForMonth(nextYear, nextMonth, cycleDay)
	nextBilling := time.Date(nextYear, nextMonth, nextBillingDay, 0, 0, 0, 0, location)
	return &nextBilling
}

// calculateNextWeeklyBilling calculates the next weekly billing date
func calculateNextWeeklyBilling(now time.Time, dayOfWeek int) *time.Time {
	// dayOfWeek: 0=Sunday, 1=Monday, ..., 6=Saturday
	if dayOfWeek < 0 || dayOfWeek > 6 {
		dayOfWeek = 1 // Default to Monday
	}

	daysUntilTarget := (dayOfWeek - int(now.Weekday()) + 7) % 7
	if daysUntilTarget == 0 && now.Hour() >= 12 { // If it's already past noon on the target day
		daysUntilTarget = 7
	}

	nextBilling := now.AddDate(0, 0, daysUntilTarget)
	nextBilling = time.Date(nextBilling.Year(), nextBilling.Month(), nextBilling.Day(), 0, 0, 0, 0, nextBilling.Location())
	return &nextBilling
}

// calculateNextQuarterlyBilling calculates the next quarterly billing date
func calculateNextQuarterlyBilling(now time.Time, cycleDay int) *time.Time {
	year, month, _ := now.Date()
	location := now.Location()

	// Find the current quarter's billing month
	quarterStartMonths := []time.Month{time.January, time.April, time.July, time.October}
	var currentQuarterStart time.Month

	for i := len(quarterStartMonths) - 1; i >= 0; i-- {
		if month >= quarterStartMonths[i] {
			currentQuarterStart = quarterStartMonths[i]
			break
		}
	}

	// Calculate billing date for current quarter (third month of quarter)
	billingMonth := currentQuarterStart + 2
	billingDay := getValidDayForMonth(year, billingMonth, cycleDay)
	currentQuarterBilling := time.Date(year, billingMonth, billingDay, 0, 0, 0, 0, location)

	if now.Before(currentQuarterBilling) || now.Equal(currentQuarterBilling) {
		return &currentQuarterBilling
	}

	// Move to next quarter
	nextQuarterStart := currentQuarterStart + 3
	nextYear := year
	if nextQuarterStart > 12 {
		nextQuarterStart -= 12
		nextYear++
	}

	nextBillingMonth := nextQuarterStart + 2
	if nextBillingMonth > 12 {
		nextBillingMonth -= 12
		nextYear++
	}

	nextBillingDay := getValidDayForMonth(nextYear, nextBillingMonth, cycleDay)
	nextBilling := time.Date(nextYear, nextBillingMonth, nextBillingDay, 0, 0, 0, 0, location)
	return &nextBilling
}

// calculateNextAnnualBilling calculates the next annual billing date
func calculateNextAnnualBilling(now time.Time, cycleDay int, contractStart time.Time) *time.Time {
	startMonth := contractStart.Month()
	currentYear := now.Year()
	location := now.Location()

	billingDay := getValidDayForMonth(currentYear, startMonth, cycleDay)
	currentYearBilling := time.Date(currentYear, startMonth, billingDay, 0, 0, 0, 0, location)

	if now.Before(currentYearBilling) || now.Equal(currentYearBilling) {
		return &currentYearBilling
	}

	// Move to next year
	nextYear := currentYear + 1
	nextBillingDay := getValidDayForMonth(nextYear, startMonth, cycleDay)
	nextBilling := time.Date(nextYear, startMonth, nextBillingDay, 0, 0, 0, 0, location)
	return &nextBilling
}

// calculateMonthlyPeriod calculates the start and end dates for a monthly billing period
func calculateMonthlyPeriod(referenceDate time.Time, cycleDay int) (time.Time, time.Time) {
	year, month, day := referenceDate.Date()
	location := referenceDate.Location()

	// Get the billing day for the current month
	currentMonthBillingDay := getValidDayForMonth(year, month, cycleDay)

	var periodEnd time.Time
	var periodStart time.Time

	// Determine which billing period we're in
	if day <= currentMonthBillingDay {
		// We're in the current month's billing period (previous billing day to current billing day)
		periodEnd = time.Date(year, month, currentMonthBillingDay, 23, 59, 59, 0, location)

		// Calculate previous month
		prevMonth := month - 1
		prevYear := year
		if prevMonth < 1 {
			prevMonth = 12
			prevYear--
		}

		prevBillingDay := getValidDayForMonth(prevYear, prevMonth, cycleDay)
		periodStart = time.Date(prevYear, prevMonth, prevBillingDay+1, 0, 0, 0, 0, location)
	} else {
		// We're past this month's billing day, so we're in next month's billing period
		// Calculate next month
		nextMonth := month + 1
		nextYear := year
		if nextMonth > 12 {
			nextMonth = 1
			nextYear++
		}

		nextBillingDay := getValidDayForMonth(nextYear, nextMonth, cycleDay)
		periodEnd = time.Date(nextYear, nextMonth, nextBillingDay, 23, 59, 59, 0, location)
		periodStart = time.Date(year, month, currentMonthBillingDay+1, 0, 0, 0, 0, location)
	}

	return periodStart, periodEnd
}

// calculateWeeklyPeriod calculates the start and end dates for a weekly billing period
func calculateWeeklyPeriod(referenceDate time.Time, dayOfWeek int) (time.Time, time.Time) {
	if dayOfWeek < 0 || dayOfWeek > 6 {
		dayOfWeek = 1 // Default to Monday
	}

	location := referenceDate.Location()

	// Find the most recent billing day (or today if it's the billing day)
	daysBack := (int(referenceDate.Weekday()) - dayOfWeek + 7) % 7
	periodEnd := referenceDate.AddDate(0, 0, -daysBack)
	periodEnd = time.Date(periodEnd.Year(), periodEnd.Month(), periodEnd.Day(), 23, 59, 59, 0, location)

	periodStart := periodEnd.AddDate(0, 0, -6)
	periodStart = time.Date(periodStart.Year(), periodStart.Month(), periodStart.Day(), 0, 0, 0, 0, location)

	return periodStart, periodEnd
}

// calculateQuarterlyPeriod calculates the start and end dates for a quarterly billing period
func calculateQuarterlyPeriod(referenceDate time.Time, cycleDay int) (time.Time, time.Time) {
	year, month, day := referenceDate.Date()
	location := referenceDate.Location()

	// Find current quarter
	quarterStartMonths := []time.Month{time.January, time.April, time.July, time.October}
	var quarterStart time.Month

	for i := len(quarterStartMonths) - 1; i >= 0; i-- {
		if month >= quarterStartMonths[i] {
			quarterStart = quarterStartMonths[i]
			break
		}
	}

	quarterEnd := quarterStart + 2
	billingDay := getValidDayForMonth(year, quarterEnd, cycleDay)

	var periodEnd time.Time
	var periodStart time.Time

	if month < quarterEnd || (month == quarterEnd && day <= billingDay) {
		// We're in the current quarter's period
		periodEnd = time.Date(year, quarterEnd, billingDay, 23, 59, 59, 0, location)

		// Start is the day after last quarter's billing day
		prevQuarterStart := quarterStart - 3
		prevYear := year
		if prevQuarterStart < 1 {
			prevQuarterStart += 12
			prevYear--
		}

		prevQuarterEnd := prevQuarterStart + 2
		if prevQuarterEnd > 12 {
			prevQuarterEnd -= 12
			prevYear++
		}

		prevBillingDay := getValidDayForMonth(prevYear, prevQuarterEnd, cycleDay)
		periodStart = time.Date(prevYear, prevQuarterEnd, prevBillingDay+1, 0, 0, 0, 0, location)
	} else {
		// We're past this quarter's billing day
		nextQuarterStart := quarterStart + 3
		nextYear := year
		if nextQuarterStart > 12 {
			nextQuarterStart -= 12
			nextYear++
		}

		nextQuarterEnd := nextQuarterStart + 2
		if nextQuarterEnd > 12 {
			nextQuarterEnd -= 12
			nextYear++
		}

		nextBillingDay := getValidDayForMonth(nextYear, nextQuarterEnd, cycleDay)
		periodEnd = time.Date(nextYear, nextQuarterEnd, nextBillingDay, 23, 59, 59, 0, location)
		periodStart = time.Date(year, quarterEnd, billingDay+1, 0, 0, 0, 0, location)
	}

	return periodStart, periodEnd
}

// calculateAnnualPeriod calculates the start and end dates for an annual billing period
func calculateAnnualPeriod(referenceDate time.Time, cycleDay int, contractStart time.Time) (time.Time, time.Time) {
	startMonth := contractStart.Month()
	currentYear := referenceDate.Year()
	location := referenceDate.Location()

	billingDay := getValidDayForMonth(currentYear, startMonth, cycleDay)
	currentYearBilling := time.Date(currentYear, startMonth, billingDay, 0, 0, 0, 0, location)

	var periodEnd time.Time
	var periodStart time.Time

	if referenceDate.Before(currentYearBilling) || referenceDate.Equal(currentYearBilling) {
		// We're in the current year's period
		periodEnd = time.Date(currentYear, startMonth, billingDay, 23, 59, 59, 0, location)

		// Start is the day after last year's billing day
		prevYear := currentYear - 1
		prevBillingDay := getValidDayForMonth(prevYear, startMonth, cycleDay)
		periodStart = time.Date(prevYear, startMonth, prevBillingDay+1, 0, 0, 0, 0, location)
	} else {
		// We're past this year's billing day
		nextYear := currentYear + 1
		nextBillingDay := getValidDayForMonth(nextYear, startMonth, cycleDay)
		periodEnd = time.Date(nextYear, startMonth, nextBillingDay, 23, 59, 59, 0, location)
		periodStart = time.Date(currentYear, startMonth, billingDay+1, 0, 0, 0, 0, location)
	}

	return periodStart, periodEnd
}

// getValidDayForMonth returns a valid day for the given month/year, handling month-end cases
func getValidDayForMonth(year int, month time.Month, desiredDay int) int {
	// Get the last day of the month
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()

	if desiredDay > lastDay {
		return lastDay
	}

	if desiredDay < 1 {
		return 1
	}

	return desiredDay
}

// IsContractReadyToBill checks if a contract is ready to be billed
func IsContractReadyToBill(contract models.Contract) bool {
	// Contract must be active
	if contract.Status != "active" {
		return false
	}

	// Contract must have auto-billing enabled
	if !contract.AutoBillEnabled {
		return false
	}

	// Contract must have a next billing date
	if contract.NextBillingDate == nil {
		return false
	}

	// Next billing date must be today or in the past
	now := time.Now()
	return contract.NextBillingDate.Before(now) || contract.NextBillingDate.Equal(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()))
}

// UpdateNextBillingDate updates the next billing date for a contract after billing
func UpdateNextBillingDate(contract *models.Contract) error {
	nextBilling, err := CalculateNextBillingDate(*contract)
	if err != nil {
		return err
	}

	contract.NextBillingDate = nextBilling
	return nil
}