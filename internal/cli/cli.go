package cli

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/austin/hours-mcp/internal/cli/commands"
	"github.com/austin/hours-mcp/internal/cli/output"
)

// CLI represents the command line interface
type CLI struct {
	db      *sql.DB
	output  *output.Formatter
	version string
}

// New creates a new CLI instance
func New(db *sql.DB) *CLI {
	return &CLI{
		db:     db,
		output: output.New(),
	}
}

// NewWithVersion creates a new CLI instance with version
func NewWithVersion(db *sql.DB, version string) *CLI {
	return &CLI{
		db:      db,
		output:  output.New(),
		version: version,
	}
}

// Run executes the CLI command based on provided arguments
func (c *CLI) Run(args []string) error {
	if len(args) < 2 {
		return c.showHelp()
	}

	command := args[1]
	commandArgs := args[2:]

	// Check for help flags on any command
	for _, arg := range commandArgs {
		if arg == "--help" || arg == "-h" || arg == "help" {
			return c.showCommandHelp(command)
		}
	}

	switch command {
	case "help", "--help", "-h":
		if len(commandArgs) > 0 {
			return c.showCommandHelp(commandArgs[0])
		}
		return c.showHelp()
	case "version", "--version", "-v":
		return c.showVersion()
	case "add-client":
		return commands.NewClientCommands(c.db, c.output).AddClient(commandArgs)
	case "list-clients":
		return commands.NewClientCommands(c.db, c.output).ListClients(commandArgs)
	case "edit-client":
		return commands.NewClientCommands(c.db, c.output).EditClient(commandArgs)
	case "add-contract":
		return commands.NewContractCommands(c.db, c.output).AddContract(commandArgs)
	case "list-contracts":
		return commands.NewContractCommands(c.db, c.output).ListContracts(commandArgs)
	case "delete-contract":
		return commands.NewContractCommands(c.db, c.output).DeleteContract(commandArgs)
	case "edit-contract":
		return commands.NewContractCommands(c.db, c.output).EditContract(commandArgs)
	case "add-time":
		return commands.NewTimeCommands(c.db, c.output).AddTime(commandArgs)
	case "list-time":
		return commands.NewTimeCommands(c.db, c.output).ListTime(commandArgs)
	case "create-invoice":
		return commands.NewInvoiceCommands(c.db, c.output).CreateInvoice(commandArgs)
	case "list-invoices":
		return commands.NewInvoiceCommands(c.db, c.output).ListInvoices(commandArgs)
	case "generate-pdf":
		return commands.NewInvoiceCommands(c.db, c.output).GeneratePDF(commandArgs)
	case "setup-business":
		return commands.NewBusinessCommands(c.db, c.output).SetupBusiness(commandArgs)
	case "add-recipient":
		return commands.NewClientCommands(c.db, c.output).AddRecipient(commandArgs)
	case "list-recipients":
		return commands.NewClientCommands(c.db, c.output).ListRecipients(commandArgs)
	case "set-payment-details":
		return commands.NewClientCommands(c.db, c.output).SetPaymentDetails(commandArgs)
	case "backup":
		return commands.NewBackupCommands(c.db, c.output).CreateBackup(commandArgs)
	case "restore":
		return commands.NewBackupCommands(c.db, c.output).RestoreBackup(commandArgs)
	case "list-backups":
		return commands.NewBackupCommands(c.db, c.output).ListBackups(commandArgs)
	case "validate-backup":
		return commands.NewBackupCommands(c.db, c.output).ValidateBackup(commandArgs)
	case "list-ready-to-bill":
		return commands.NewBillingCommands(c.db, c.output).ListReadyToBill(commandArgs)
	default:
		return fmt.Errorf("unknown command: %s\n\nRun 'hours-mcp help' for available commands", command)
	}
}

// showHelp displays the main help message
func (c *CLI) showHelp() error {
	help := `hours-mcp - Professional Time Tracking & Invoice Generation

USAGE:
    hours-mcp [COMMAND] [OPTIONS]

COMMANDS:
    Client Management:
        add-client <name>           Add a new client
        list-clients               List all clients
        edit-client <name>         Edit client information
        add-recipient <client>     Add recipient for client
        list-recipients <client>   List recipients for client
        set-payment-details        Set payment details for client

    Contract Management:
        add-contract <number> <client> <rate>  Add new contract
        list-contracts [--client <name>]      List contracts
        delete-contract <number>               Delete contract
        edit-contract <number>                 Edit contract

    Time Tracking:
        add-time <contract> <hours>    Add time entry
        list-time [OPTIONS]            List time entries

    Invoice Management:
        create-invoice <client> <period>  Create invoice
        list-invoices [OPTIONS]           List invoices
        generate-pdf <invoice-number>     Generate PDF for existing invoice

    Setup:
        setup-business                Setup business information

    Billing Cycles:
        list-ready-to-bill            List contracts ready to be billed

    Backup & Restore:
        backup <path>                 Create database backup
        restore <path>                Restore database from backup
        list-backups [dir]            List available backup files
        validate-backup <path>        Validate backup file

    General:
        help [COMMAND]                Show help for command
        version                       Show version information

EXAMPLES:
    hours-mcp add-client "Acme Corp" --address "123 Main St" --city "SF"
    hours-mcp add-contract AC-001 "Acme Corp" 150.00 --desc "Backend Development" --billing-day 15 --billing-type monthly
    hours-mcp add-time AC-001 2.5 --date today --desc "API development"
    hours-mcp create-invoice "Acme Corp" "this month"
    hours-mcp list-ready-to-bill
    hours-mcp backup ~/backups/hours_backup.db
    hours-mcp restore ~/backups/hours_backup.db --force

For detailed help on a specific command, use:
    hours-mcp help <command>
`
	c.output.Info(help)
	return nil
}

// showCommandHelp displays help for a specific command
func (c *CLI) showCommandHelp(command string) error {
	helps := map[string]string{
		"add-client": `Add a new client

USAGE:
    hours-mcp add-client <name> [OPTIONS]

OPTIONS:
    --address <address>    Street address
    --city <city>         City
    --state <state>       State or province
    --zip <code>          ZIP or postal code
    --country <country>   Country

EXAMPLES:
    hours-mcp add-client "Acme Corp"
    hours-mcp add-client "Acme Corp" --address "123 Main St" --city "San Francisco" --state "CA" --zip "94102"
`,
		"add-contract": `Add a new contract for a client

USAGE:
    hours-mcp add-contract <number> <client> <rate> [OPTIONS]

OPTIONS:
    --desc <description>     Contract description/name
    --currency <code>        Currency code (default: USD)
    --type <type>           Contract type (default: hourly)
    --start <date>          Start date (YYYY-MM-DD, default: today)
    --end <date>            End date (YYYY-MM-DD, optional)
    --terms <terms>         Payment terms
    --billing-day <day>     Day of month for billing cycle (1-31)
    --billing-type <type>   Billing cycle type (monthly, weekly, quarterly, annually)
    --auto-bill             Enable automatic billing

EXAMPLES:
    hours-mcp add-contract AC-001 "Acme Corp" 150.00
    hours-mcp add-contract AC-001 "Acme Corp" 150.00 --desc "Backend Development" --start 2024-01-01
    hours-mcp add-contract AC-001 "Acme Corp" 150.00 --billing-day 15 --billing-type monthly --auto-bill
`,
		"add-time": `Add a time entry for a contract

USAGE:
    hours-mcp add-time <contract> <hours> [OPTIONS]

OPTIONS:
    --date <date>         Date (YYYY-MM-DD, today, yesterday, etc.)
    --desc <description>  Description of work performed

EXAMPLES:
    hours-mcp add-time AC-001 2.5 --desc "API development"
    hours-mcp add-time AC-001 8.0 --date yesterday --desc "Database optimization"
`,
		"backup": `Create a backup of the database

USAGE:
    hours-mcp backup <path> [OPTIONS]

OPTIONS:
    --validate            Validate backup after creation (default: true)

EXAMPLES:
    hours-mcp backup ~/backups/hours_backup.db
    hours-mcp backup ~/backups/                    # Auto-generate filename
    hours-mcp backup /path/to/backup.db --validate
`,
		"restore": `Restore database from a backup file

USAGE:
    hours-mcp restore <path> [OPTIONS]

OPTIONS:
    --force               Skip confirmation prompt (required)
    --skip-validation     Skip backup validation before restore

EXAMPLES:
    hours-mcp restore ~/backups/hours_backup.db --force
    hours-mcp restore /path/to/backup.db --force --skip-validation

WARNING: This will replace your current database. A backup of the current
database will be created automatically before restoration.
`,
		"list-backups": `List available backup files

USAGE:
    hours-mcp list-backups [directory]

EXAMPLES:
    hours-mcp list-backups                         # Default directory
    hours-mcp list-backups ~/my-backups/           # Custom directory
`,
		"validate-backup": `Validate that a backup file is valid

USAGE:
    hours-mcp validate-backup <path>

EXAMPLES:
    hours-mcp validate-backup ~/backups/hours_backup.db
`,
		"list-ready-to-bill": `List contracts that are ready to be billed

USAGE:
    hours-mcp list-ready-to-bill [OPTIONS]

OPTIONS:
    --client <name>         Filter by client name
    --auto-only             Show only contracts with auto-billing enabled

EXAMPLES:
    hours-mcp list-ready-to-bill
    hours-mcp list-ready-to-bill --client "Acme Corp"
    hours-mcp list-ready-to-bill --auto-only
`,
		"delete-contract": `Delete a contract

USAGE:
    hours-mcp delete-contract <number>

EXAMPLES:
    hours-mcp delete-contract AC-001

NOTE: Cannot delete contracts that have associated time entries.
Delete time entries first before deleting the contract.
`,
		"edit-contract": `Edit an existing contract

USAGE:
    hours-mcp edit-contract <number> [OPTIONS]

OPTIONS:
    --billing-day <day>         Day of month for billing cycle (1-31)
    --billing-type <type>       Billing cycle type (monthly, weekly, quarterly, annually)
    --auto-bill                 Enable automatic billing (use true/false)

EXAMPLES:
    hours-mcp edit-contract CA0925-02 --billing-day 20 --billing-type monthly --auto-bill true
    hours-mcp edit-contract CA0925-02 --billing-day 15
`,
		"generate-pdf": `Generate PDF for an existing invoice

USAGE:
    hours-mcp generate-pdf <invoice-number>

EXAMPLES:
    hours-mcp generate-pdf INV-202510-f886fcf8

This command regenerates the PDF for an existing invoice, useful when:
- You want to regenerate a PDF with updated formatting
- The original PDF was lost or corrupted
- You need a fresh copy of an existing invoice
`,
	}

	if help, exists := helps[command]; exists {
		c.output.Info(help)
		return nil
	}

	return fmt.Errorf("no help available for command: %s", command)
}

// showVersion displays version information
func (c *CLI) showVersion() error {
	version := c.version
	if version == "" {
		version = "dev"
	}
	c.output.Info(fmt.Sprintf("hours-mcp version %s", version))
	return nil
}

// IsCliMode checks if the provided arguments indicate CLI mode
func IsCliMode(args []string) bool {
	if len(args) < 2 {
		return false
	}

	// Check for CLI commands
	cliCommands := []string{
		"help", "--help", "-h",
		"version", "--version", "-v",
		"add-client", "list-clients", "edit-client",
		"add-contract", "list-contracts", "delete-contract", "edit-contract",
		"add-time", "list-time",
		"create-invoice", "list-invoices", "generate-pdf",
		"setup-business",
		"add-recipient", "list-recipients",
		"set-payment-details",
		"backup", "restore", "list-backups", "validate-backup",
		"list-ready-to-bill",
	}

	command := strings.ToLower(args[1])
	for _, cmd := range cliCommands {
		if command == cmd {
			return true
		}
	}

	return false
}