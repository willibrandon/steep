package agent

import (
	"database/sql"
	"fmt"
	"os"
)

// CurrentSchemaVersion is the current version of the agent database schema.
// Increment this when making breaking schema changes.
const CurrentSchemaVersion = 1

// SchemaManager handles database schema versioning and migrations.
type SchemaManager struct {
	db    *sql.DB
	debug bool
}

// NewSchemaManager creates a new SchemaManager.
func NewSchemaManager(db *sql.DB, debug bool) *SchemaManager {
	return &SchemaManager{
		db:    db,
		debug: debug,
	}
}

// CheckAndMigrate checks the schema version and runs migrations if needed.
// Returns an error if the schema version is incompatible and cannot be migrated.
func (m *SchemaManager) CheckAndMigrate() error {
	version, err := m.getSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}

	if version == CurrentSchemaVersion {
		return nil // Schema is current
	}

	if version > CurrentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d - please upgrade steep-agent", version, CurrentSchemaVersion)
	}

	// Run migrations from current version to target version
	for v := version; v < CurrentSchemaVersion; v++ {
		if err := m.runMigration(v, v+1); err != nil {
			return fmt.Errorf("migration from v%d to v%d failed: %w", v, v+1, err)
		}
	}

	// Update schema version
	if err := m.setSchemaVersion(CurrentSchemaVersion); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	return nil
}

// getSchemaVersion returns the current schema version from the database.
// Returns 0 if no version is set (fresh database).
func (m *SchemaManager) getSchemaVersion() (int, error) {
	// Use PRAGMA user_version for schema versioning
	var version int
	err := m.db.QueryRow("PRAGMA user_version").Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}

// setSchemaVersion sets the schema version in the database.
func (m *SchemaManager) setSchemaVersion(version int) error {
	_, err := m.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", version))
	return err
}

// runMigration runs a specific migration from one version to the next.
func (m *SchemaManager) runMigration(fromVersion, toVersion int) error {
	// Define migrations here as needed
	// Currently no migrations needed since this is the initial version
	switch {
	case fromVersion == 0 && toVersion == 1:
		// Migration from unversioned (0) to v1:
		// This is the initial schema setup - tables already use CREATE IF NOT EXISTS
		// so no action needed for existing databases
		return nil
	default:
		return fmt.Errorf("no migration path from v%d to v%d", fromVersion, toVersion)
	}
}

// CheckDatabaseIntegrity performs SQLite integrity checks.
// Returns an error describing corruption if found.
func CheckDatabaseIntegrity(dbPath string) error {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&mode=ro")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Quick integrity check (faster than full check)
	var result string
	err = db.QueryRow("PRAGMA quick_check").Scan(&result)
	if err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}

	if result != "ok" {
		return &CorruptionError{Details: result}
	}

	return nil
}

// CorruptionError indicates database corruption was detected.
type CorruptionError struct {
	Details string
}

func (e *CorruptionError) Error() string {
	return fmt.Sprintf("database corruption detected: %s", e.Details)
}

// RecreateDatabase removes and recreates a corrupted database.
// Returns the path to the backup (if any) and any error.
func RecreateDatabase(dbPath string) (string, error) {
	// Create backup of corrupted database
	backupPath := dbPath + ".corrupted"
	if _, err := os.Stat(dbPath); err == nil {
		// Backup exists, add timestamp
		backupPath = fmt.Sprintf("%s.corrupted.%d", dbPath, os.Getpid())
	}

	// Rename corrupted database to backup
	if err := os.Rename(dbPath, backupPath); err != nil {
		// Try to remove if rename fails
		if rmErr := os.Remove(dbPath); rmErr != nil {
			return "", fmt.Errorf("failed to backup or remove corrupted database: %w", err)
		}
		backupPath = "" // No backup created
	}

	// Remove WAL and SHM files
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")

	return backupPath, nil
}

// DiskSpaceInfo contains information about available disk space.
type DiskSpaceInfo struct {
	AvailableBytes uint64
	TotalBytes     uint64
	UsedBytes      uint64
	UsedPercent    float64
}

// DiskFullError indicates insufficient disk space.
type DiskFullError struct {
	AvailableBytes uint64
	RequiredBytes  uint64
}

func (e *DiskFullError) Error() string {
	return fmt.Sprintf("insufficient disk space: %d bytes available, %d bytes required",
		e.AvailableBytes, e.RequiredBytes)
}

// MinDiskSpaceBytes is the minimum required disk space (10 MB).
const MinDiskSpaceBytes = 10 * 1024 * 1024

// CheckMinDiskSpace checks if there is at least the minimum required disk space.
// Returns a DiskFullError if space is insufficient.
func CheckMinDiskSpace(path string) error {
	info, err := CheckDiskSpace(path)
	if err != nil {
		// If we can't check, don't fail - just warn
		return nil
	}

	if info.AvailableBytes < MinDiskSpaceBytes {
		return &DiskFullError{
			AvailableBytes: info.AvailableBytes,
			RequiredBytes:  MinDiskSpaceBytes,
		}
	}

	return nil
}

// IsDiskFullError checks if an error indicates disk full condition.
// This checks for common SQLite disk full error patterns.
func IsDiskFullError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// SQLite returns SQLITE_FULL (13) or "database or disk is full"
	return contains(errStr, "disk is full") ||
		contains(errStr, "database is full") ||
		contains(errStr, "no space left") ||
		contains(errStr, "SQLITE_FULL")
}

// contains checks if s contains substr (case-insensitive would be better but keeping simple).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
