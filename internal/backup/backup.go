package backup

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Service handles database backup and restore operations
type Service struct {
	db *sql.DB
}

// New creates a new backup service instance
func New(db *sql.DB) *Service {
	return &Service{db: db}
}

// Backup creates a backup of the database to the specified file path
func (s *Service) Backup(backupPath string) error {
	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Use SQLite's backup command to create a clean backup
	// This uses SQLite's backup API which is more reliable than copying the file
	backupQuery := fmt.Sprintf("VACUUM INTO '%s'", backupPath)

	_, err := s.db.Exec(backupQuery)
	if err != nil {
		return fmt.Errorf("failed to create database backup: %w", err)
	}

	// Verify the backup file was created
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file was not created at %s", backupPath)
	}

	return nil
}

// Restore restores the database from the specified backup file
func (s *Service) Restore(backupPath string) error {
	// Verify the backup file exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file does not exist: %s", backupPath)
	}

	// Get the database file path from the connection
	var dbPath string
	err := s.db.QueryRow("PRAGMA database_list").Scan(nil, nil, &dbPath)
	if err != nil {
		return fmt.Errorf("failed to get database path: %w", err)
	}

	// Close the current database connection
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	// Create a backup of the current database before restoring
	currentTime := time.Now().Format("20060102_150405")
	backupCurrentPath := fmt.Sprintf("%s.backup_%s", dbPath, currentTime)

	if err := copyFile(dbPath, backupCurrentPath); err != nil {
		return fmt.Errorf("failed to backup current database: %w", err)
	}

	// Copy the backup file to the database location
	if err := copyFile(backupPath, dbPath); err != nil {
		// If restore fails, try to restore the original
		if restoreErr := copyFile(backupCurrentPath, dbPath); restoreErr != nil {
			return fmt.Errorf("failed to restore from backup and failed to restore original: restore error: %w, original restore error: %w", err, restoreErr)
		}
		return fmt.Errorf("failed to restore database from backup: %w", err)
	}

	// Remove the temporary backup of current database on successful restore
	os.Remove(backupCurrentPath)

	return nil
}

// CreateAutoBackup creates an automatic backup with timestamp
func (s *Service) CreateAutoBackup(backupDir string) (string, error) {
	timestamp := time.Now().Format("20060102_150405")
	backupFileName := fmt.Sprintf("hours_backup_%s.db", timestamp)
	backupPath := filepath.Join(backupDir, backupFileName)

	if err := s.Backup(backupPath); err != nil {
		return "", err
	}

	return backupPath, nil
}

// ListBackups lists all backup files in the specified directory
func (s *Service) ListBackups(backupDir string) ([]BackupInfo, error) {
	files, err := os.ReadDir(backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupInfo{}, nil
		}
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []BackupInfo
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// Check if it's a database file
		if filepath.Ext(file.Name()) != ".db" {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		backupPath := filepath.Join(backupDir, file.Name())
		backups = append(backups, BackupInfo{
			Name:    file.Name(),
			Path:    backupPath,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	return backups, nil
}

// ValidateBackup validates that a backup file is a valid SQLite database
func (s *Service) ValidateBackup(backupPath string) error {
	// Open the backup file to verify it's a valid SQLite database
	testDB, err := sql.Open("sqlite3", backupPath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer testDB.Close()

	// Try to query the database to verify it's valid
	var count int
	err = testDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&count)
	if err != nil {
		return fmt.Errorf("backup file is not a valid SQLite database: %w", err)
	}

	// Check if it has the expected tables for hours-mcp
	expectedTables := []string{"clients", "contracts", "time_entries", "invoices", "business_info"}
	for _, table := range expectedTables {
		var tableExists int
		err = testDB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&tableExists)
		if err != nil {
			return fmt.Errorf("failed to check table %s in backup: %w", table, err)
		}
		if tableExists == 0 {
			return fmt.Errorf("backup file is missing required table: %s", table)
		}
	}

	return nil
}

// BackupInfo contains information about a backup file
type BackupInfo struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	// Sync to ensure data is written to disk
	return destFile.Sync()
}