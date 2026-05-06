package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

func Initialize() (*sql.DB, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	dbPath := filepath.Join(homeDir, ".hours", "db")
	dbDir := filepath.Dir(dbPath)

	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

// DefaultUserID is the user id assumed by single-user execution paths
// (the Wails desktop app, MCP stdio server, CLI commands). Multi-tenant
// HTTP serve mode never uses this constant — it derives the user from
// the authenticated session.
const DefaultUserID int64 = 1

func createTables(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS clients (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		name TEXT NOT NULL,
		address TEXT,
		city TEXT,
		state TEXT,
		zip_code TEXT,
		country TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, name)
	);

	CREATE TABLE IF NOT EXISTS recipients (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		client_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		email TEXT NOT NULL,
		title TEXT,
		phone TEXT,
		is_primary BOOLEAN DEFAULT FALSE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS payment_details (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		client_id INTEGER NOT NULL UNIQUE,
		bank_name TEXT,
		account_number TEXT,
		routing_number TEXT,
		swift_code TEXT,
		payment_terms TEXT,
		notes TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS time_entries (
		id TEXT PRIMARY KEY,
		user_id INTEGER,
		client_id INTEGER NOT NULL,
		date DATE NOT NULL,
		hours REAL NOT NULL,
		description TEXT,
		contract_ref TEXT,
		invoice_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
		FOREIGN KEY (invoice_id) REFERENCES invoices(id)
	);

	CREATE TABLE IF NOT EXISTS invoices (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		client_id INTEGER NOT NULL,
		invoice_number TEXT NOT NULL,
		issue_date DATE NOT NULL,
		due_date DATE NOT NULL,
		total_amount REAL NOT NULL,
		status TEXT DEFAULT 'pending',
		pdf_path TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
		UNIQUE(user_id, invoice_number)
	);

	CREATE TABLE IF NOT EXISTS contracts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		client_id INTEGER NOT NULL,
		contract_number TEXT NOT NULL,
		name TEXT NOT NULL,
		hourly_rate REAL NOT NULL,
		currency TEXT DEFAULT 'USD',
		contract_type TEXT DEFAULT 'hourly',
		start_date DATE NOT NULL,
		end_date DATE,
		status TEXT DEFAULT 'active',
		payment_terms TEXT,
		notes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
		UNIQUE(user_id, contract_number)
	);

	CREATE TABLE IF NOT EXISTS quotes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		client_id INTEGER NOT NULL,
		quote_number TEXT NOT NULL,
		title TEXT NOT NULL,
		issue_date DATE NOT NULL,
		valid_until DATE NOT NULL,
		subtotal REAL NOT NULL DEFAULT 0,
		total_amount REAL NOT NULL DEFAULT 0,
		currency TEXT DEFAULT 'USD',
		status TEXT DEFAULT 'draft',
		notes TEXT,
		pdf_path TEXT,
		converted_contract_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
		FOREIGN KEY (converted_contract_id) REFERENCES contracts(id),
		UNIQUE(user_id, quote_number)
	);

	CREATE TABLE IF NOT EXISTS quote_line_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		quote_id INTEGER NOT NULL,
		description TEXT NOT NULL,
		quantity REAL NOT NULL DEFAULT 1,
		unit TEXT DEFAULT 'hours',
		unit_price REAL NOT NULL DEFAULT 0,
		amount REAL NOT NULL DEFAULT 0,
		sort_order INTEGER DEFAULT 0,
		FOREIGN KEY (quote_id) REFERENCES quotes(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS payment_methods (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		label TEXT NOT NULL,
		bank_name TEXT,
		account_number TEXT,
		routing_number TEXT,
		swift_code TEXT,
		payment_terms TEXT,
		notes TEXT,
		is_default BOOLEAN DEFAULT FALSE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS business_info (
		user_id INTEGER PRIMARY KEY,
		business_name TEXT NOT NULL,
		contact_name TEXT NOT NULL,
		email TEXT NOT NULL,
		phone TEXT,
		address TEXT,
		city TEXT,
		state TEXT,
		zip_code TEXT,
		country TEXT,
		tax_id TEXT,
		website TEXT,
		logo_path TEXT,
		invoice_prefix TEXT DEFAULT 'INV',
		export_path TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS expenses (
		id TEXT PRIMARY KEY,
		user_id INTEGER,
		client_id INTEGER NOT NULL,
		contract_id INTEGER,
		date DATE NOT NULL,
		description TEXT NOT NULL,
		amount REAL NOT NULL,
		currency TEXT DEFAULT 'USD',
		category TEXT,
		receipt_path TEXT,
		invoice_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
		FOREIGN KEY (contract_id) REFERENCES contracts(id),
		FOREIGN KEY (invoice_id) REFERENCES invoices(id)
	);

	CREATE INDEX IF NOT EXISTS idx_time_entries_date ON time_entries(date);
	CREATE INDEX IF NOT EXISTS idx_time_entries_client ON time_entries(client_id);
	CREATE INDEX IF NOT EXISTS idx_time_entries_user ON time_entries(user_id);
	CREATE INDEX IF NOT EXISTS idx_invoices_client ON invoices(client_id);
	CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status);
	CREATE INDEX IF NOT EXISTS idx_invoices_user ON invoices(user_id);
	CREATE INDEX IF NOT EXISTS idx_contracts_client ON contracts(client_id);
	CREATE INDEX IF NOT EXISTS idx_contracts_status ON contracts(status);
	CREATE INDEX IF NOT EXISTS idx_contracts_dates ON contracts(start_date, end_date);
	CREATE INDEX IF NOT EXISTS idx_contracts_user ON contracts(user_id);
	CREATE INDEX IF NOT EXISTS idx_quotes_client ON quotes(client_id);
	CREATE INDEX IF NOT EXISTS idx_quotes_status ON quotes(status);
	CREATE INDEX IF NOT EXISTS idx_quotes_user ON quotes(user_id);
	CREATE INDEX IF NOT EXISTS idx_quote_line_items_quote ON quote_line_items(quote_id);
	CREATE INDEX IF NOT EXISTS idx_expenses_date ON expenses(date);
	CREATE INDEX IF NOT EXISTS idx_expenses_client ON expenses(client_id);
	CREATE INDEX IF NOT EXISTS idx_expenses_invoice ON expenses(invoice_id);
	CREATE INDEX IF NOT EXISTS idx_expenses_user ON expenses(user_id);
	CREATE INDEX IF NOT EXISTS idx_clients_user ON clients(user_id);
	CREATE INDEX IF NOT EXISTS idx_payment_methods_user ON payment_methods(user_id);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

func runMigrations(db *sql.DB) error {
	// Create migrations table if it doesn't exist
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			id INTEGER PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	migrations := []migration{
		{
			name: "add_contract_ref_to_time_entries",
			apply: func(db *sql.DB) error {
				return addColumnIfNotExists(db, "time_entries", "contract_ref", "TEXT")
			},
		},
		{
			name: "add_title_to_recipients",
			apply: func(db *sql.DB) error {
				return addColumnIfNotExists(db, "recipients", "title", "TEXT")
			},
		},
		{
			name: "add_phone_to_recipients",
			apply: func(db *sql.DB) error {
				return addColumnIfNotExists(db, "recipients", "phone", "TEXT")
			},
		},
		{
			name: "add_address_to_clients",
			apply: func(db *sql.DB) error {
				if err := addColumnIfNotExists(db, "clients", "address", "TEXT"); err != nil {
					return err
				}
				if err := addColumnIfNotExists(db, "clients", "city", "TEXT"); err != nil {
					return err
				}
				if err := addColumnIfNotExists(db, "clients", "state", "TEXT"); err != nil {
					return err
				}
				if err := addColumnIfNotExists(db, "clients", "zip_code", "TEXT"); err != nil {
					return err
				}
				return addColumnIfNotExists(db, "clients", "country", "TEXT")
			},
		},
		{
			name: "restructure_for_contracts",
			apply: func(db *sql.DB) error {
				return restructureForContracts(db)
			},
		},
		{
			name: "remove_rate_constraints_from_clients",
			apply: func(db *sql.DB) error {
				return removeRateConstraintsFromClients(db)
			},
		},
		{
			name: "add_export_path_to_business_info",
			apply: func(db *sql.DB) error {
				return addColumnIfNotExists(db, "business_info", "export_path", "TEXT")
			},
		},
		{
			name: "add_payment_method_to_contracts",
			apply: func(db *sql.DB) error {
				return addColumnIfNotExists(db, "contracts", "payment_method_id", "INTEGER")
			},
		},
		{
			name: "add_payment_method_to_invoices",
			apply: func(db *sql.DB) error {
				return addColumnIfNotExists(db, "invoices", "payment_method_id", "INTEGER")
			},
		},
		{
			name: "migrate_payment_details_to_payment_methods",
			apply: func(db *sql.DB) error {
				return migratePaymentDetailsToMethods(db)
			},
		},
		{
			name: "add_billing_cycles_to_contracts",
			apply: func(db *sql.DB) error {
				return addBillingCyclesToContracts(db)
			},
		},
		{
			name: "add_users_table",
			apply: func(db *sql.DB) error {
				return addUsersTable(db)
			},
		},
		{
			name: "add_user_scoping",
			apply: func(db *sql.DB) error {
				return addUserScoping(db)
			},
		},
		{
			name: "add_api_tokens",
			apply: func(db *sql.DB) error {
				return addAPITokens(db)
			},
		},
		{
			name: "add_api_token_usage",
			apply: func(db *sql.DB) error {
				return addAPITokenUsage(db)
			},
		},
	}

	for _, migration := range migrations {
		// Check if migration has already been applied
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM migrations WHERE name = ?", migration.name).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration %s: %w", migration.name, err)
		}

		if count > 0 {
			// Migration already applied
			continue
		}

		// Apply migration
		err = migration.apply(db)
		if err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", migration.name, err)
		}

		// Record migration as applied
		_, err = db.Exec("INSERT INTO migrations (name) VALUES (?)", migration.name)
		if err != nil {
			return fmt.Errorf("failed to record migration %s: %w", migration.name, err)
		}

		fmt.Printf("Applied migration: %s\n", migration.name)
	}

	return nil
}

type migration struct {
	name  string
	apply func(*sql.DB) error
}

func addColumnIfNotExists(db *sql.DB, tableName, columnName, columnType string) error {
	// Check if column already exists
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return fmt.Errorf("failed to get table info for %s: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue *string
		err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey)
		if err != nil {
			return fmt.Errorf("failed to scan column info: %w", err)
		}

		if name == columnName {
			// Column already exists
			fmt.Printf("Column %s.%s already exists, skipping\n", tableName, columnName)
			return nil
		}
	}

	// Column doesn't exist, add it
	sql := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, columnName, columnType)
	_, err = db.Exec(sql)
	if err != nil {
		return fmt.Errorf("failed to add column %s to %s: %w", columnName, tableName, err)
	}

	fmt.Printf("Added column %s.%s\n", tableName, columnName)
	return nil
}

func columnExists(db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return false, fmt.Errorf("failed to get table info for %s: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue *string
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, fmt.Errorf("failed to scan column info: %w", err)
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, nil
}

func restructureForContracts(db *sql.DB) error {
	fmt.Println("Restructuring database for contract-based billing...")

	// Step 1: Create contracts table if it doesn't exist (will be created by main schema)
	// The contracts table is already in the main schema above

	// Step 2: Add contract_id column to time_entries
	if err := addColumnIfNotExists(db, "time_entries", "contract_id", "INTEGER"); err != nil {
		return fmt.Errorf("failed to add contract_id to time_entries: %w", err)
	}

	// Fresh databases don't have the legacy hourly_rate column on clients;
	// skip legacy data migration entirely in that case.
	hasRate, err := columnExists(db, "clients", "hourly_rate")
	if err != nil {
		return fmt.Errorf("failed to check clients schema: %w", err)
	}
	if !hasRate {
		fmt.Println("No legacy hourly_rate column on clients — skipping contract backfill.")
		return nil
	}

	// Step 3: Check if we have any existing clients with rates that need migration
	var clientCount int
	err = db.QueryRow("SELECT COUNT(*) FROM clients WHERE hourly_rate IS NOT NULL AND hourly_rate > 0").Scan(&clientCount)
	if err != nil {
		return fmt.Errorf("failed to check existing clients: %w", err)
	}

	if clientCount > 0 {
		fmt.Printf("Migrating %d clients to contract-based structure...\n", clientCount)

		// Step 4: Create default contracts for existing clients
		rows, err := db.Query(`
			SELECT id, name, hourly_rate, currency, created_at
			FROM clients
			WHERE hourly_rate IS NOT NULL AND hourly_rate > 0
		`)
		if err != nil {
			return fmt.Errorf("failed to query existing clients: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var clientID int
			var clientName string
			var hourlyRate float64
			var currency string
			var createdAt string

			err := rows.Scan(&clientID, &clientName, &hourlyRate, &currency, &createdAt)
			if err != nil {
				return fmt.Errorf("failed to scan client: %w", err)
			}

			// Create a default contract for this client
			contractNumber := fmt.Sprintf("LEGACY-%d", clientID)
			contractName := fmt.Sprintf("Legacy Contract - %s", clientName)

			var contractID int64
			err = db.QueryRow(`
				INSERT INTO contracts (client_id, contract_number, name, hourly_rate, currency, start_date, status)
				VALUES (?, ?, ?, ?, ?, ?, 'active')
				RETURNING id
			`, clientID, contractNumber, contractName, hourlyRate, currency, createdAt[:10]).Scan(&contractID)

			if err != nil {
				return fmt.Errorf("failed to create legacy contract for client %s: %w", clientName, err)
			}

			// Step 5: Update existing time entries to reference the new contract
			_, err = db.Exec(`
				UPDATE time_entries
				SET contract_id = ?
				WHERE client_id = ? AND contract_id IS NULL
			`, contractID, clientID)

			if err != nil {
				return fmt.Errorf("failed to update time entries for client %s: %w", clientName, err)
			}

			fmt.Printf("Created legacy contract %s for client %s\n", contractNumber, clientName)
		}
	}

	// Step 6: Make contract_id required and add foreign key constraint for new time entries
	// We'll handle this in business logic rather than database constraints for easier migration

	fmt.Println("Contract restructuring completed successfully!")
	return nil
}

func removeRateConstraintsFromClients(db *sql.DB) error {
	fmt.Println("Removing rate constraints from clients table...")

	// SQLite doesn't support ALTER TABLE DROP COLUMN or modifying constraints directly
	// We need to recreate the table without the rate fields

	// Step 1: Create new clients table without rate fields
	_, err := db.Exec(`
		CREATE TABLE clients_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			address TEXT,
			city TEXT,
			state TEXT,
			zip_code TEXT,
			country TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create new clients table: %w", err)
	}

	// Step 2: Copy data from old table (excluding rate fields)
	_, err = db.Exec(`
		INSERT INTO clients_new (id, name, address, city, state, zip_code, country, created_at, updated_at)
		SELECT id, name, address, city, state, zip_code, country, created_at, updated_at
		FROM clients
	`)
	if err != nil {
		return fmt.Errorf("failed to copy client data: %w", err)
	}

	// Step 3: Drop old table and rename new table
	_, err = db.Exec(`DROP TABLE clients`)
	if err != nil {
		return fmt.Errorf("failed to drop old clients table: %w", err)
	}

	_, err = db.Exec(`ALTER TABLE clients_new RENAME TO clients`)
	if err != nil {
		return fmt.Errorf("failed to rename new clients table: %w", err)
	}

	fmt.Println("Successfully removed rate constraints from clients table")
	return nil
}

// migratePaymentDetailsToMethods lifts every legacy per-client payment_details
// row into a business-level payment_methods entry and points each contract
// belonging to that client at the new method. Existing invoices keep their
// data flow (they read through contracts now) so nothing on disk changes.
//
// Labels use the client name so the user can immediately recognise methods
// in the Settings UI and rename/consolidate them there.
func migratePaymentDetailsToMethods(db *sql.DB) error {
	// Nothing to do unless the legacy table exists.
	var tblName string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='payment_details'`,
	).Scan(&tblName)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check legacy payment_details: %w", err)
	}

	rows, err := db.Query(`
		SELECT pd.client_id, c.name,
		       COALESCE(pd.bank_name,''), COALESCE(pd.account_number,''),
		       COALESCE(pd.routing_number,''), COALESCE(pd.swift_code,''),
		       COALESCE(pd.payment_terms,''), COALESCE(pd.notes,'')
		FROM payment_details pd
		JOIN clients c ON c.id = pd.client_id
	`)
	if err != nil {
		return fmt.Errorf("scan legacy payment_details: %w", err)
	}
	defer rows.Close()

	type legacy struct {
		clientID                                        int
		clientName, bank, acct, routing, swift, terms, notes string
	}
	var batch []legacy
	for rows.Next() {
		var l legacy
		if err := rows.Scan(&l.clientID, &l.clientName, &l.bank, &l.acct, &l.routing,
			&l.swift, &l.terms, &l.notes); err != nil {
			return err
		}
		batch = append(batch, l)
	}
	if len(batch) == 0 {
		return nil
	}

	fmt.Printf("Migrating %d legacy per-client payment_details into business-level payment_methods...\n", len(batch))

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i, l := range batch {
		label := fmt.Sprintf("%s (migrated)", l.clientName)
		isDefault := i == 0 // first one becomes the default method
		var methodID int64
		if err := tx.QueryRow(`
			INSERT INTO payment_methods (label, bank_name, account_number, routing_number,
			                             swift_code, payment_terms, notes, is_default)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			RETURNING id
		`, label, l.bank, l.acct, l.routing, l.swift, l.terms, l.notes, isDefault).Scan(&methodID); err != nil {
			return fmt.Errorf("insert payment_method for %s: %w", l.clientName, err)
		}
		if _, err := tx.Exec(`
			UPDATE contracts SET payment_method_id = ?
			WHERE client_id = ? AND payment_method_id IS NULL
		`, methodID, l.clientID); err != nil {
			return fmt.Errorf("attach payment_method to contracts for %s: %w", l.clientName, err)
		}
		// Snapshot the same method onto any past invoices so the PDF
		// regenerate path keeps working identically.
		if _, err := tx.Exec(`
			UPDATE invoices SET payment_method_id = ?
			WHERE client_id = ? AND payment_method_id IS NULL
		`, methodID, l.clientID); err != nil {
			return fmt.Errorf("attach payment_method to invoices for %s: %w", l.clientName, err)
		}
	}

	return tx.Commit()
}

// addAPITokens creates the api_tokens table used by the bearer-token auth
// path. Each row is owned by a single user (FK CASCADE on user delete);
// the raw token is never persisted — only its SHA-256 hex digest plus a short
// human-recognisable prefix.
func addAPITokens(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS api_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			token_prefix TEXT NOT NULL,
			scopes TEXT NOT NULL,
			expires_at DATETIME,
			last_used_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			revoked_at DATETIME,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("create api_tokens: %w", err)
		}
	}
	return nil
}

// addAPITokenUsage creates the per-request usage log written by the
// UsageRecorder middleware (HTTP API) and by the recordToolCall defer
// (MCP tools). Rows are written best-effort from a goroutine and read
// only by the session-authenticated /api/tokens/{id}/usage endpoints.
//
// Both FKs CASCADE so usage rows disappear when the owning user or token
// is deleted/revoked-and-purged.
func addAPITokenUsage(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS api_token_usage (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			status INTEGER NOT NULL,
			duration_ms INTEGER NOT NULL,
			error TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (token_id) REFERENCES api_tokens(id) ON DELETE CASCADE,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_token_usage_token ON api_token_usage(token_id)`,
		`CREATE INDEX IF NOT EXISTS idx_api_token_usage_user_created ON api_token_usage(user_id, created_at)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("create api_token_usage: %w", err)
		}
	}
	return nil
}

// addUsersTable creates the users + sessions tables used by the OIDC auth
// layer. The serve mode requires auth when OIDC env vars are set; users are
// auto-provisioned on first sign-in. Wails GUI bypasses both tables — they
// only matter on the network-exposed HTTP path.
func addUsersTable(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			oidc_subject TEXT NOT NULL UNIQUE,
			email TEXT NOT NULL,
			name TEXT,
			role TEXT NOT NULL DEFAULT 'user',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_login_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("create users/sessions: %w", err)
		}
	}
	return nil
}

func addBillingCyclesToContracts(db *sql.DB) error {
	fmt.Println("Adding billing cycle support to contracts table...")

	// Add billing cycle columns
	if err := addColumnIfNotExists(db, "contracts", "billing_cycle_day", "INTEGER"); err != nil {
		return fmt.Errorf("failed to add billing_cycle_day column: %w", err)
	}

	if err := addColumnIfNotExists(db, "contracts", "billing_cycle_type", "TEXT DEFAULT 'monthly'"); err != nil {
		return fmt.Errorf("failed to add billing_cycle_type column: %w", err)
	}

	if err := addColumnIfNotExists(db, "contracts", "next_billing_date", "DATE"); err != nil {
		return fmt.Errorf("failed to add next_billing_date column: %w", err)
	}

	if err := addColumnIfNotExists(db, "contracts", "auto_bill_enabled", "BOOLEAN DEFAULT FALSE"); err != nil {
		return fmt.Errorf("failed to add auto_bill_enabled column: %w", err)
	}

	// Set default billing cycle type for existing contracts
	_, err := db.Exec(`
		UPDATE contracts
		SET billing_cycle_type = 'monthly'
		WHERE billing_cycle_type IS NULL OR billing_cycle_type = ''
	`)
	if err != nil {
		return fmt.Errorf("failed to set default billing cycle type: %w", err)
	}

	// Add index for billing date queries
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_contracts_billing_date ON contracts(next_billing_date, status)
	`)
	if err != nil {
		return fmt.Errorf("failed to create billing date index: %w", err)
	}

	fmt.Println("Successfully added billing cycle support to contracts")
	return nil
}

// addUserScoping denormalizes user_id onto every business-level table so each
// row is owned by exactly one user, then rebuilds the affected tables with
// per-user UNIQUE constraints in place of the old global ones. Existing rows
// are backfilled to user_id = 1 (the bootstrap admin / Wails single-user
// installation).
//
// SQLite can't ALTER a UNIQUE constraint, so for clients/contracts/invoices/
// quotes/business_info we use the standard rebuild dance: CREATE NEW, INSERT
// ... SELECT, DROP OLD, RENAME. payment_methods just gets the user_id column
// (no UNIQUE because labels are non-canonical). time_entries/expenses likewise
// only need the column + index.
func addUserScoping(db *sql.DB) error {
	fmt.Println("Adding user scoping to business tables...")

	// 1) Add user_id columns where they don't yet exist + backfill to user 1.
	plainCols := []string{
		"clients", "time_entries", "invoices", "expenses",
		"payment_methods", "contracts", "quotes",
	}
	for _, tbl := range plainCols {
		if err := addColumnIfNotExists(db, tbl, "user_id", "INTEGER"); err != nil {
			return fmt.Errorf("add user_id to %s: %w", tbl, err)
		}
		if _, err := db.Exec(fmt.Sprintf(
			`UPDATE %s SET user_id = 1 WHERE user_id IS NULL`, tbl)); err != nil {
			return fmt.Errorf("backfill user_id on %s: %w", tbl, err)
		}
	}

	// 2) Rebuild clients to swap UNIQUE(name) for UNIQUE(user_id, name).
	if err := rebuildClientsForUserScoping(db); err != nil {
		return fmt.Errorf("rebuild clients: %w", err)
	}

	// 3) Rebuild contracts to swap UNIQUE(contract_number) for UNIQUE(user_id,
	//    contract_number). Carry every column that may have been added by
	//    earlier migrations (payment_method_id, billing_cycle_*, etc.).
	if err := rebuildContractsForUserScoping(db); err != nil {
		return fmt.Errorf("rebuild contracts: %w", err)
	}

	// 4) Rebuild invoices to swap UNIQUE(invoice_number) for UNIQUE(user_id,
	//    invoice_number).
	if err := rebuildInvoicesForUserScoping(db); err != nil {
		return fmt.Errorf("rebuild invoices: %w", err)
	}

	// 5) Rebuild quotes to swap UNIQUE(quote_number) for UNIQUE(user_id,
	//    quote_number).
	if err := rebuildQuotesForUserScoping(db); err != nil {
		return fmt.Errorf("rebuild quotes: %w", err)
	}

	// 6) Rebuild business_info: drop the singleton id PK, key by user_id.
	if err := rebuildBusinessInfoForUserScoping(db); err != nil {
		return fmt.Errorf("rebuild business_info: %w", err)
	}

	// 7) Recreate indexes (table rebuilds drop them).
	idx := []string{
		`CREATE INDEX IF NOT EXISTS idx_time_entries_date ON time_entries(date)`,
		`CREATE INDEX IF NOT EXISTS idx_time_entries_client ON time_entries(client_id)`,
		`CREATE INDEX IF NOT EXISTS idx_time_entries_user ON time_entries(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_invoices_client ON invoices(client_id)`,
		`CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status)`,
		`CREATE INDEX IF NOT EXISTS idx_invoices_user ON invoices(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_contracts_client ON contracts(client_id)`,
		`CREATE INDEX IF NOT EXISTS idx_contracts_status ON contracts(status)`,
		`CREATE INDEX IF NOT EXISTS idx_contracts_dates ON contracts(start_date, end_date)`,
		`CREATE INDEX IF NOT EXISTS idx_contracts_user ON contracts(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_quotes_client ON quotes(client_id)`,
		`CREATE INDEX IF NOT EXISTS idx_quotes_status ON quotes(status)`,
		`CREATE INDEX IF NOT EXISTS idx_quotes_user ON quotes(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_expenses_date ON expenses(date)`,
		`CREATE INDEX IF NOT EXISTS idx_expenses_client ON expenses(client_id)`,
		`CREATE INDEX IF NOT EXISTS idx_expenses_invoice ON expenses(invoice_id)`,
		`CREATE INDEX IF NOT EXISTS idx_expenses_user ON expenses(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_clients_user ON clients(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_methods_user ON payment_methods(user_id)`,
	}
	for _, q := range idx {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	fmt.Println("Successfully added user scoping to business tables")
	return nil
}

// columnList returns the ordered set of column names on a table — used to drive
// the INSERT ... SELECT step of the rebuild dance.
func columnList(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue *string
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, nil
}

func rebuildClientsForUserScoping(db *sql.DB) error {
	cols, err := columnList(db, "clients")
	if err != nil {
		return err
	}
	colCSV := strings.Join(cols, ", ")

	stmts := []string{
		`CREATE TABLE clients_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			name TEXT NOT NULL,
			address TEXT,
			city TEXT,
			state TEXT,
			zip_code TEXT,
			country TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, name)
		)`,
		fmt.Sprintf(`INSERT INTO clients_new (%s) SELECT %s FROM clients`, colCSV, colCSV),
		`DROP TABLE clients`,
		`ALTER TABLE clients_new RENAME TO clients`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("clients rebuild step (%s): %w", trunc(s), err)
		}
	}
	return nil
}

func rebuildContractsForUserScoping(db *sql.DB) error {
	cols, err := columnList(db, "contracts")
	if err != nil {
		return err
	}
	colCSV := strings.Join(cols, ", ")

	// Build the new table with every column we know about plus the per-user
	// UNIQUE constraint. Optional columns use IF NOT EXISTS-style add later
	// via ALTER (all addColumnIfNotExists migrations re-run idempotently).
	createNew := `CREATE TABLE contracts_new (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		client_id INTEGER NOT NULL,
		contract_number TEXT NOT NULL,
		name TEXT NOT NULL,
		hourly_rate REAL NOT NULL,
		currency TEXT DEFAULT 'USD',
		contract_type TEXT DEFAULT 'hourly',
		start_date DATE NOT NULL,
		end_date DATE,
		status TEXT DEFAULT 'active',
		payment_terms TEXT,
		notes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		payment_method_id INTEGER,
		billing_cycle_day INTEGER,
		billing_cycle_type TEXT DEFAULT 'monthly',
		next_billing_date DATE,
		auto_bill_enabled BOOLEAN DEFAULT FALSE,
		FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
		UNIQUE(user_id, contract_number)
	)`

	// Restrict the INSERT column list to columns that exist on both old and
	// new — defensive in case a forward migration adds a column we haven't
	// codified here yet.
	newCols, err := columnListPragma(createNew)
	if err != nil {
		return fmt.Errorf("parse new contracts cols: %w", err)
	}
	carry := intersect(cols, newCols)
	carryCSV := strings.Join(carry, ", ")
	_ = colCSV

	stmts := []string{
		createNew,
		fmt.Sprintf(`INSERT INTO contracts_new (%s) SELECT %s FROM contracts`, carryCSV, carryCSV),
		`DROP TABLE contracts`,
		`ALTER TABLE contracts_new RENAME TO contracts`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("contracts rebuild step (%s): %w", trunc(s), err)
		}
	}
	return nil
}

func rebuildInvoicesForUserScoping(db *sql.DB) error {
	cols, err := columnList(db, "invoices")
	if err != nil {
		return err
	}

	createNew := `CREATE TABLE invoices_new (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		client_id INTEGER NOT NULL,
		invoice_number TEXT NOT NULL,
		issue_date DATE NOT NULL,
		due_date DATE NOT NULL,
		total_amount REAL NOT NULL,
		status TEXT DEFAULT 'pending',
		pdf_path TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		payment_method_id INTEGER,
		FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
		UNIQUE(user_id, invoice_number)
	)`
	newCols, err := columnListPragma(createNew)
	if err != nil {
		return err
	}
	carry := intersect(cols, newCols)
	carryCSV := strings.Join(carry, ", ")

	stmts := []string{
		createNew,
		fmt.Sprintf(`INSERT INTO invoices_new (%s) SELECT %s FROM invoices`, carryCSV, carryCSV),
		`DROP TABLE invoices`,
		`ALTER TABLE invoices_new RENAME TO invoices`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("invoices rebuild step (%s): %w", trunc(s), err)
		}
	}
	return nil
}

func rebuildQuotesForUserScoping(db *sql.DB) error {
	cols, err := columnList(db, "quotes")
	if err != nil {
		return err
	}

	createNew := `CREATE TABLE quotes_new (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER,
		client_id INTEGER NOT NULL,
		quote_number TEXT NOT NULL,
		title TEXT NOT NULL,
		issue_date DATE NOT NULL,
		valid_until DATE NOT NULL,
		subtotal REAL NOT NULL DEFAULT 0,
		total_amount REAL NOT NULL DEFAULT 0,
		currency TEXT DEFAULT 'USD',
		status TEXT DEFAULT 'draft',
		notes TEXT,
		pdf_path TEXT,
		converted_contract_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE,
		FOREIGN KEY (converted_contract_id) REFERENCES contracts(id),
		UNIQUE(user_id, quote_number)
	)`
	newCols, err := columnListPragma(createNew)
	if err != nil {
		return err
	}
	carry := intersect(cols, newCols)
	carryCSV := strings.Join(carry, ", ")

	stmts := []string{
		createNew,
		fmt.Sprintf(`INSERT INTO quotes_new (%s) SELECT %s FROM quotes`, carryCSV, carryCSV),
		`DROP TABLE quotes`,
		`ALTER TABLE quotes_new RENAME TO quotes`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("quotes rebuild step (%s): %w", trunc(s), err)
		}
	}
	return nil
}

// rebuildBusinessInfoForUserScoping turns the singleton (id INTEGER PRIMARY
// KEY) row into a per-user keyed table. The existing row (id=1) becomes
// user_id=1; subsequent users get their own row.
func rebuildBusinessInfoForUserScoping(db *sql.DB) error {
	cols, err := columnList(db, "business_info")
	if err != nil {
		return err
	}
	hasUser := false
	for _, c := range cols {
		if c == "user_id" {
			hasUser = true
		}
	}

	createNew := `CREATE TABLE business_info_new (
		user_id INTEGER PRIMARY KEY,
		business_name TEXT NOT NULL,
		contact_name TEXT NOT NULL,
		email TEXT NOT NULL,
		phone TEXT,
		address TEXT,
		city TEXT,
		state TEXT,
		zip_code TEXT,
		country TEXT,
		tax_id TEXT,
		website TEXT,
		logo_path TEXT,
		invoice_prefix TEXT DEFAULT 'INV',
		export_path TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
	newCols, err := columnListPragma(createNew)
	if err != nil {
		return err
	}

	// Carry every shared column except id (which is being replaced by user_id
	// as the primary key). user_id may not exist yet — synthesize it from id
	// during the SELECT in that case.
	carry := []string{}
	for _, c := range cols {
		if c == "id" {
			continue
		}
		for _, nc := range newCols {
			if nc == c {
				carry = append(carry, c)
				break
			}
		}
	}

	carryCSV := strings.Join(carry, ", ")
	var selectExpr string
	if hasUser {
		selectExpr = fmt.Sprintf(`SELECT user_id, %s FROM business_info`, carryCSV)
	} else {
		// Map the singleton id=1 row to user_id=1.
		selectExpr = fmt.Sprintf(`SELECT 1 AS user_id, %s FROM business_info`, carryCSV)
	}

	stmts := []string{
		createNew,
		fmt.Sprintf(`INSERT INTO business_info_new (user_id, %s) %s`, carryCSV, selectExpr),
		`DROP TABLE business_info`,
		`ALTER TABLE business_info_new RENAME TO business_info`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("business_info rebuild step (%s): %w", trunc(s), err)
		}
	}
	return nil
}

// columnListPragma extracts column names from a CREATE TABLE statement by
// asking SQLite — we run it against a temp DB-less parse via a throwaway
// transaction approach. Simpler: re-derive via regex on the body. To keep
// the implementation small we fall back to a simple parser.
func columnListPragma(createSQL string) ([]string, error) {
	// Crude but adequate: pull out the body between the first "(" and the
	// final ")", split on top-level commas, take the leading identifier of
	// each segment that doesn't start with a constraint keyword.
	open := strings.Index(createSQL, "(")
	close := strings.LastIndex(createSQL, ")")
	if open < 0 || close < 0 || close <= open {
		return nil, fmt.Errorf("malformed CREATE TABLE")
	}
	body := createSQL[open+1 : close]
	parts := splitTopLevel(body)
	var cols []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		upper := strings.ToUpper(p)
		switch {
		case strings.HasPrefix(upper, "PRIMARY KEY"),
			strings.HasPrefix(upper, "FOREIGN KEY"),
			strings.HasPrefix(upper, "UNIQUE"),
			strings.HasPrefix(upper, "CHECK"),
			strings.HasPrefix(upper, "CONSTRAINT"):
			continue
		}
		// First whitespace-delimited token is the column name.
		fields := strings.Fields(p)
		if len(fields) == 0 {
			continue
		}
		name := strings.Trim(fields[0], "`\"[]")
		cols = append(cols, name)
	}
	return cols, nil
}

func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	last := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[last:i])
				last = i + 1
			}
		}
	}
	out = append(out, s[last:])
	return out
}

func intersect(a, b []string) []string {
	bset := map[string]struct{}{}
	for _, x := range b {
		bset[x] = struct{}{}
	}
	var out []string
	for _, x := range a {
		if _, ok := bset[x]; ok {
			out = append(out, x)
		}
	}
	return out
}

func trunc(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}
