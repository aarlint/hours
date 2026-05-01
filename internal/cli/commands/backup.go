package commands

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/austin/hours-mcp/internal/backup"
	"github.com/austin/hours-mcp/internal/cli/output"
)

// BackupCommands handles backup/restore CLI commands
type BackupCommands struct {
	db     *sql.DB
	output *output.Formatter
	backup *backup.Service
}

// NewBackupCommands creates a new BackupCommands instance
func NewBackupCommands(db *sql.DB, out *output.Formatter) *BackupCommands {
	return &BackupCommands{
		db:     db,
		output: out,
		backup: backup.New(db),
	}
}

// CreateBackup creates a backup of the database
func (b *BackupCommands) CreateBackup(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("backup file path is required\n\nUsage: hours-mcp backup <path> [OPTIONS]")
	}

	backupPath := args[0]
	parsedArgs := parseFlags(args[1:])

	// Auto-generate filename if only directory is provided
	if strings.HasSuffix(backupPath, "/") || isDirectory(backupPath) {
		autoBackupPath, err := b.backup.CreateAutoBackup(backupPath)
		if err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
		b.output.Success(fmt.Sprintf("Database backup created: %s", autoBackupPath))
		return nil
	}

	// Manual backup path
	if err := b.backup.Backup(backupPath); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	if parsedArgs["validate"] == "true" {
		if err := b.backup.ValidateBackup(backupPath); err != nil {
			b.output.Warning(fmt.Sprintf("Backup created but validation failed: %v", err))
		} else {
			b.output.Info("Backup validation successful")
		}
	}

	b.output.Success(fmt.Sprintf("Database backup created: %s", backupPath))
	return nil
}

// RestoreBackup restores the database from a backup
func (b *BackupCommands) RestoreBackup(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("backup file path is required\n\nUsage: hours-mcp restore <path> [OPTIONS]")
	}

	backupPath := args[0]
	parsedArgs := parseFlags(args[1:])

	// Validate backup before restoring unless explicitly skipped
	if parsedArgs["skip-validation"] != "true" {
		b.output.Info("Validating backup file...")
		if err := b.backup.ValidateBackup(backupPath); err != nil {
			return fmt.Errorf("backup validation failed: %w", err)
		}
		b.output.Info("Backup validation successful")
	}

	// Confirm restoration unless force flag is used
	if parsedArgs["force"] != "true" {
		b.output.Warning("This will replace your current database with the backup.")
		b.output.Warning("A backup of your current database will be created automatically.")
		b.output.Info("Use --force to skip this confirmation in scripts.")
		return fmt.Errorf("restore cancelled - use --force flag to proceed")
	}

	b.output.Info("Restoring database from backup...")
	if err := b.backup.Restore(backupPath); err != nil {
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	b.output.Success(fmt.Sprintf("Database restored from: %s", backupPath))
	b.output.Info("Application restart may be required for changes to take effect")
	return nil
}

// ListBackups lists available backup files
func (b *BackupCommands) ListBackups(args []string) error {
	var backupDir string
	if len(args) > 0 {
		backupDir = args[0]
	} else {
		// Default to user's home directory + /hours-backups
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		backupDir = filepath.Join(homeDir, "hours-backups")
	}

	backups, err := b.backup.ListBackups(backupDir)
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	if len(backups) == 0 {
		b.output.Info(fmt.Sprintf("No backup files found in: %s", backupDir))
		return nil
	}

	b.output.Header(fmt.Sprintf("Backup Files in %s (%d files)", backupDir, len(backups)))
	for _, backup := range backups {
		fmt.Printf("• %s\n", backup.Name)
		fmt.Printf("  Size: %s\n", formatBytes(backup.Size))
		fmt.Printf("  Modified: %s\n", backup.ModTime.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Path: %s\n", backup.Path)
		fmt.Println()
	}

	return nil
}

// ValidateBackup validates a backup file
func (b *BackupCommands) ValidateBackup(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("backup file path is required\n\nUsage: hours-mcp validate-backup <path>")
	}

	backupPath := args[0]

	b.output.Info(fmt.Sprintf("Validating backup: %s", backupPath))
	if err := b.backup.ValidateBackup(backupPath); err != nil {
		b.output.Error(fmt.Sprintf("Backup validation failed: %v", err))
		return err
	}

	b.output.Success("Backup file is valid")
	return nil
}

// isDirectory checks if a path is a directory
func isDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// formatBytes formats byte size in a human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}