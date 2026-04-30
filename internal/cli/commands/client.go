package commands

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/austin/hours-mcp/internal/cli/output"
	"github.com/austin/hours-mcp/internal/models"
)

// ClientCommands handles client-related CLI commands
type ClientCommands struct {
	db     *sql.DB
	output *output.Formatter
}

// NewClientCommands creates a new ClientCommands instance
func NewClientCommands(db *sql.DB, out *output.Formatter) *ClientCommands {
	return &ClientCommands{
		db:     db,
		output: out,
	}
}

// AddClient adds a new client
func (c *ClientCommands) AddClient(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("client name is required\n\nUsage: hours-mcp add-client <name> [OPTIONS]")
	}

	name := args[0]
	parsedArgs := parseFlags(args[1:])

	address := parsedArgs["address"]
	city := parsedArgs["city"]
	state := parsedArgs["state"]
	zipCode := parsedArgs["zip"]
	country := parsedArgs["country"]

	result, err := c.db.Exec(`
		INSERT INTO clients (name, address, city, state, zip_code, country)
		VALUES (?, ?, ?, ?, ?, ?)
	`, name, address, city, state, zipCode, country)

	if err != nil {
		return fmt.Errorf("failed to add client: %w", err)
	}

	id, _ := result.LastInsertId()
	c.output.Success(fmt.Sprintf("Client '%s' added successfully (ID: %d)", name, id))
	return nil
}

// ListClients lists all clients
func (c *ClientCommands) ListClients(args []string) error {
	rows, err := c.db.Query(`
		SELECT id, name, address, city, state, zip_code, country, created_at, updated_at
		FROM clients
		ORDER BY name
	`)
	if err != nil {
		return fmt.Errorf("failed to list clients: %w", err)
	}
	defer rows.Close()

	var clients []models.Client
	for rows.Next() {
		var client models.Client
		if err := rows.Scan(&client.ID, &client.Name, &client.Address, &client.City,
			&client.State, &client.ZipCode, &client.Country, &client.CreatedAt, &client.UpdatedAt); err != nil {
			return fmt.Errorf("failed to scan client: %w", err)
		}
		clients = append(clients, client)
	}

	c.output.PrintClients(clients)
	return nil
}

// EditClient edits an existing client
func (c *ClientCommands) EditClient(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("client name is required\n\nUsage: hours-mcp edit-client <name> [OPTIONS]")
	}

	name := args[0]
	parsedArgs := parseFlags(args[1:])

	// Get current client ID
	clientID, err := c.getClientIDByName(name)
	if err != nil {
		return fmt.Errorf("failed to find client: %w", err)
	}

	// Build dynamic UPDATE query
	setParts := []string{}
	values := []interface{}{}

	if newName, exists := parsedArgs["name"]; exists {
		setParts = append(setParts, "name = ?")
		values = append(values, newName)
	}
	if address, exists := parsedArgs["address"]; exists {
		setParts = append(setParts, "address = ?")
		values = append(values, address)
	}
	if city, exists := parsedArgs["city"]; exists {
		setParts = append(setParts, "city = ?")
		values = append(values, city)
	}
	if state, exists := parsedArgs["state"]; exists {
		setParts = append(setParts, "state = ?")
		values = append(values, state)
	}
	if zipCode, exists := parsedArgs["zip"]; exists {
		setParts = append(setParts, "zip_code = ?")
		values = append(values, zipCode)
	}
	if country, exists := parsedArgs["country"]; exists {
		setParts = append(setParts, "country = ?")
		values = append(values, country)
	}

	if len(setParts) == 0 {
		return fmt.Errorf("no fields provided to update")
	}

	// Add updated_at and client ID
	setParts = append(setParts, "updated_at = CURRENT_TIMESTAMP")
	values = append(values, clientID)

	query := fmt.Sprintf("UPDATE clients SET %s WHERE id = ?", strings.Join(setParts, ", "))

	_, err = c.db.Exec(query, values...)
	if err != nil {
		return fmt.Errorf("failed to update client: %w", err)
	}

	// Use the new name if provided, otherwise use the original name
	displayName := name
	if newName, exists := parsedArgs["name"]; exists {
		displayName = newName
	}

	c.output.Success(fmt.Sprintf("Successfully updated client: %s", displayName))
	return nil
}

// AddRecipient adds a recipient for a client
func (c *ClientCommands) AddRecipient(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("client name, recipient name, and email are required\n\nUsage: hours-mcp add-recipient <client> <name> <email> [OPTIONS]")
	}

	clientName := args[0]
	recipientName := args[1]
	email := args[2]
	parsedArgs := parseFlags(args[3:])

	clientID, err := c.getClientIDByName(clientName)
	if err != nil {
		return fmt.Errorf("client not found: %w", err)
	}

	title := parsedArgs["title"]
	phone := parsedArgs["phone"]
	isPrimary := parsedArgs["primary"] == "true"

	if isPrimary {
		_, err = c.db.Exec(`
			UPDATE recipients SET is_primary = FALSE
			WHERE client_id = ?
		`, clientID)
		if err != nil {
			return fmt.Errorf("failed to update primary recipient: %w", err)
		}
	}

	result, err := c.db.Exec(`
		INSERT INTO recipients (client_id, name, email, title, phone, is_primary)
		VALUES (?, ?, ?, ?, ?, ?)
	`, clientID, recipientName, email, title, phone, isPrimary)

	if err != nil {
		return fmt.Errorf("failed to add recipient: %w", err)
	}

	id, _ := result.LastInsertId()
	c.output.Success(fmt.Sprintf("Recipient '%s' added for client '%s' (ID: %d)", recipientName, clientName, id))
	return nil
}

// ListRecipients lists recipients for a client
func (c *ClientCommands) ListRecipients(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("client name is required\n\nUsage: hours-mcp list-recipients <client>")
	}

	clientName := args[0]
	clientID, err := c.getClientIDByName(clientName)
	if err != nil {
		return fmt.Errorf("client not found: %w", err)
	}

	rows, err := c.db.Query(`
		SELECT id, name, email, title, phone, is_primary
		FROM recipients
		WHERE client_id = ?
		ORDER BY is_primary DESC, name
	`, clientID)
	if err != nil {
		return fmt.Errorf("failed to list recipients: %w", err)
	}
	defer rows.Close()

	c.output.Info(fmt.Sprintf("Recipients for %s:", clientName))
	count := 0
	for rows.Next() {
		var id int
		var name, email, title, phone string
		var isPrimary bool

		err := rows.Scan(&id, &name, &email, &title, &phone, &isPrimary)
		if err != nil {
			return fmt.Errorf("failed to scan recipient: %w", err)
		}
		count++

		primaryLabel := ""
		if isPrimary {
			primaryLabel = " (PRIMARY)"
		}
		fmt.Printf("• ID %d: %s <%s>%s\n", id, name, email, primaryLabel)
		if title != "" {
			fmt.Printf("  Title: %s\n", title)
		}
		if phone != "" {
			fmt.Printf("  Phone: %s\n", phone)
		}
		fmt.Println()
	}

	if count == 0 {
		c.output.Info("No recipients found.")
	}
	return nil
}

// SetPaymentDetails sets payment details for a client
func (c *ClientCommands) SetPaymentDetails(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("client name is required\n\nUsage: hours-mcp set-payment-details <client> [OPTIONS]")
	}

	clientName := args[0]
	parsedArgs := parseFlags(args[1:])

	clientID, err := c.getClientIDByName(clientName)
	if err != nil {
		return fmt.Errorf("client not found: %w", err)
	}

	bankName := parsedArgs["bank"]
	accountNumber := parsedArgs["account"]
	routingNumber := parsedArgs["routing"]
	swiftCode := parsedArgs["swift"]
	paymentTerms := parsedArgs["terms"]
	notes := parsedArgs["notes"]

	_, err = c.db.Exec(`
		INSERT INTO payment_details (client_id, bank_name, account_number, routing_number, swift_code, payment_terms, notes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(client_id) DO UPDATE SET
			bank_name = excluded.bank_name,
			account_number = excluded.account_number,
			routing_number = excluded.routing_number,
			swift_code = excluded.swift_code,
			payment_terms = excluded.payment_terms,
			notes = excluded.notes,
			updated_at = excluded.updated_at
	`, clientID, bankName, accountNumber, routingNumber, swiftCode, paymentTerms, notes)

	if err != nil {
		return fmt.Errorf("failed to set payment details: %w", err)
	}

	c.output.Success(fmt.Sprintf("Payment details updated for client '%s'", clientName))
	return nil
}

// getClientIDByName is a helper method to get client ID by name
func (c *ClientCommands) getClientIDByName(name string) (int, error) {
	var id int
	err := c.db.QueryRow("SELECT id FROM clients WHERE name = ?", name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("client '%s' not found", name)
	}
	return id, err
}

// parseFlags parses command-line flags in the format --key value
func parseFlags(args []string) map[string]string {
	flags := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			key := strings.TrimPrefix(args[i], "--")
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				flags[key] = args[i+1]
				i++ // Skip the value
			} else {
				flags[key] = "true" // Boolean flag
			}
		}
	}
	return flags
}