package commands

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/austin/hours-mcp/internal/cli/output"
	"github.com/austin/hours-mcp/internal/models"
	"github.com/austin/hours-mcp/internal/timeparse"
	"github.com/google/uuid"
)

// TimeCommands handles time tracking CLI commands
type TimeCommands struct {
	db     *sql.DB
	output *output.Formatter
}

// NewTimeCommands creates a new TimeCommands instance
func NewTimeCommands(db *sql.DB, out *output.Formatter) *TimeCommands {
	return &TimeCommands{
		db:     db,
		output: out,
	}
}

// AddTime adds a time entry for a contract
func (t *TimeCommands) AddTime(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("contract number and hours are required\n\nUsage: hours-mcp add-time <contract> <hours> [OPTIONS]")
	}

	contractNumber := args[0]
	hoursStr := args[1]
	parsedArgs := parseFlags(args[2:])

	// Parse hours
	hours, err := strconv.ParseFloat(hoursStr, 64)
	if err != nil {
		return fmt.Errorf("invalid hours: %s", hoursStr)
	}

	// Get contract and verify it's active
	var contractID int
	var clientID int
	var clientName string
	var contractName string
	var status string
	err = t.db.QueryRow(`
		SELECT c.id, c.client_id, cl.name, c.name, c.status
		FROM contracts c
		JOIN clients cl ON c.client_id = cl.id
		WHERE c.contract_number = ?
	`, contractNumber).Scan(&contractID, &clientID, &clientName, &contractName, &status)

	if err == sql.ErrNoRows {
		return fmt.Errorf("contract %s not found", contractNumber)
	}
	if err != nil {
		return fmt.Errorf("failed to find contract: %w", err)
	}

	if status != "active" {
		return fmt.Errorf("contract %s is not active (status: %s)", contractNumber, status)
	}

	// Parse date
	entryDate := time.Now()
	if dateStr := parsedArgs["date"]; dateStr != "" {
		entryDate, err = timeparse.ParseDate(dateStr)
		if err != nil {
			return fmt.Errorf("invalid date: %w", err)
		}
	}

	description := parsedArgs["desc"]

	entryID := uuid.New().String()

	_, err = t.db.Exec(`
		INSERT INTO time_entries (id, client_id, contract_id, date, hours, description, contract_ref)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, entryID, clientID, contractID, entryDate.Format("2006-01-02"), hours, description, contractNumber)

	if err != nil {
		return fmt.Errorf("failed to add hours: %w", err)
	}

	t.output.Success(fmt.Sprintf("Added %.2f hours for %s (%s) on %s", hours, clientName, contractName, entryDate.Format("2006-01-02")))
	if description != "" {
		t.output.Info(fmt.Sprintf("Description: %s", description))
	}
	t.output.Info(fmt.Sprintf("Entry ID: %s", entryID))
	return nil
}

// ListTime lists time entries with optional filtering
func (t *TimeCommands) ListTime(args []string) error {
	parsedArgs := parseFlags(args)

	query := `
		SELECT te.id, te.contract_id, te.date, te.hours, te.description, te.invoice_id, te.created_at,
		       cl.name, ct.contract_number, ct.name, ct.hourly_rate, ct.currency
		FROM time_entries te
		JOIN contracts ct ON te.contract_id = ct.id
		JOIN clients cl ON ct.client_id = cl.id
		WHERE 1=1
	`
	queryArgs := []interface{}{}

	if clientName := parsedArgs["client"]; clientName != "" {
		clientID, err := t.getClientIDByName(clientName)
		if err != nil {
			return fmt.Errorf("client not found: %w", err)
		}
		query += " AND cl.id = ?"
		queryArgs = append(queryArgs, clientID)
	}

	if contract := parsedArgs["contract"]; contract != "" {
		query += " AND ct.contract_number = ?"
		queryArgs = append(queryArgs, contract)
	}

	if startDate := parsedArgs["start"]; startDate != "" {
		sd, err := timeparse.ParseDate(startDate)
		if err != nil {
			return fmt.Errorf("invalid start date: %w", err)
		}
		query += " AND te.date >= ?"
		queryArgs = append(queryArgs, sd.Format("2006-01-02"))
	}

	if endDate := parsedArgs["end"]; endDate != "" {
		ed, err := timeparse.ParseDate(endDate)
		if err != nil {
			return fmt.Errorf("invalid end date: %w", err)
		}
		query += " AND te.date <= ?"
		queryArgs = append(queryArgs, ed.Format("2006-01-02"))
	}

	if invoiced := parsedArgs["invoiced"]; invoiced != "" {
		if invoiced == "true" {
			query += " AND te.invoice_id IS NOT NULL"
		} else if invoiced == "false" {
			query += " AND te.invoice_id IS NULL"
		}
	}

	query += " ORDER BY te.date DESC, te.created_at DESC"

	rows, err := t.db.Query(query, queryArgs...)
	if err != nil {
		return fmt.Errorf("failed to list time entries: %w", err)
	}
	defer rows.Close()

	var entries []models.TimeEntry
	var totalHours float64

	for rows.Next() {
		var entry models.TimeEntry
		var clientName, contractNumber, contractName, currency string
		var hourlyRate float64

		if err := rows.Scan(&entry.ID, &entry.ContractID, &entry.Date, &entry.Hours, &entry.Description, &entry.InvoiceID, &entry.CreatedAt,
			&clientName, &contractNumber, &contractName, &hourlyRate, &currency); err != nil {
			return fmt.Errorf("failed to scan entry: %w", err)
		}

		// Create contract with relevant info for display
		entry.Contract = &models.Contract{
			ContractNumber: contractNumber,
			Name:           contractName,
			HourlyRate:     hourlyRate,
			Currency:       currency,
			Client:         &models.Client{Name: clientName},
		}

		entries = append(entries, entry)
		totalHours += entry.Hours
	}

	t.output.PrintTimeEntries(entries, totalHours)
	return nil
}

// getClientIDByName is a helper method to get client ID by name
func (t *TimeCommands) getClientIDByName(name string) (int, error) {
	var id int
	err := t.db.QueryRow("SELECT id FROM clients WHERE name = ?", name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("client '%s' not found", name)
	}
	return id, err
}