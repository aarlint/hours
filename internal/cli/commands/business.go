package commands

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/austin/hours-mcp/internal/cli/output"
	"github.com/austin/hours-mcp/internal/models"
)

// BusinessCommands handles business setup CLI commands
type BusinessCommands struct {
	db     *sql.DB
	output *output.Formatter
}

// NewBusinessCommands creates a new BusinessCommands instance
func NewBusinessCommands(db *sql.DB, out *output.Formatter) *BusinessCommands {
	return &BusinessCommands{
		db:     db,
		output: out,
	}
}

// SetupBusiness sets up or updates business information
func (b *BusinessCommands) SetupBusiness(args []string) error {
	parsedArgs := parseFlags(args)

	// Check if we have required fields
	if parsedArgs["name"] == "" {
		return fmt.Errorf("business name is required\n\nUsage: hours-mcp setup-business --name <name> --email <email> --contact <contact> [OPTIONS]")
	}

	if parsedArgs["email"] == "" {
		return fmt.Errorf("business email is required\n\nUsage: hours-mcp setup-business --name <name> --email <email> --contact <contact> [OPTIONS]")
	}

	if parsedArgs["contact"] == "" {
		return fmt.Errorf("contact name is required\n\nUsage: hours-mcp setup-business --name <name> --email <email> --contact <contact> [OPTIONS]")
	}

	businessName := parsedArgs["name"]
	contactName := parsedArgs["contact"]
	email := parsedArgs["email"]
	phone := parsedArgs["phone"]
	address := parsedArgs["address"]
	city := parsedArgs["city"]
	state := parsedArgs["state"]
	zipCode := parsedArgs["zip"]
	country := parsedArgs["country"]
	taxID := parsedArgs["tax-id"]
	website := parsedArgs["website"]
	logoPath := parsedArgs["logo"]
	invoicePrefix := parsedArgs["prefix"]

	if invoicePrefix == "" {
		invoicePrefix = "INV"
	}

	_, err := b.db.Exec(`
		INSERT INTO business_info (id, business_name, contact_name, email, phone, address, city, state, zip_code, country, tax_id, website, logo_path, invoice_prefix, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			business_name = excluded.business_name,
			contact_name = excluded.contact_name,
			email = excluded.email,
			phone = excluded.phone,
			address = excluded.address,
			city = excluded.city,
			state = excluded.state,
			zip_code = excluded.zip_code,
			country = excluded.country,
			tax_id = excluded.tax_id,
			website = excluded.website,
			logo_path = excluded.logo_path,
			invoice_prefix = excluded.invoice_prefix,
			updated_at = excluded.updated_at
	`, businessName, contactName, email, phone, address, city, state, zipCode, country, taxID, website, logoPath, invoicePrefix, time.Now())

	if err != nil {
		return fmt.Errorf("failed to set business info: %w", err)
	}

	b.output.Success(fmt.Sprintf("Business information updated successfully for '%s'", businessName))

	// Show the configured business info
	return b.ShowBusinessInfo()
}

// ShowBusinessInfo displays current business information
func (b *BusinessCommands) ShowBusinessInfo() error {
	var business models.BusinessInfo
	err := b.db.QueryRow(`
		SELECT id, business_name, contact_name, email, phone, address, city, state, zip_code, country, tax_id, website, logo_path, invoice_prefix, updated_at
		FROM business_info WHERE id = 1
	`).Scan(&business.ID, &business.BusinessName, &business.ContactName, &business.Email,
		&business.Phone, &business.Address, &business.City, &business.State,
		&business.ZipCode, &business.Country, &business.TaxID, &business.Website,
		&business.LogoPath, &business.InvoicePrefix, &business.UpdatedAt)

	if err == sql.ErrNoRows {
		b.output.Warning("No business information configured.")
		b.output.Info("Use 'hours-mcp setup-business --name <name> --email <email> --contact <contact>' to configure your business details.")
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get business info: %w", err)
	}

	b.output.PrintBusinessInfo(business)
	return nil
}