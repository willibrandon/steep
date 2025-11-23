package queries

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EnableLoggingCollector enables logging_collector in postgresql.conf and restarts PostgreSQL.
// This modifies the config file directly and uses pg_ctl to restart.
func EnableLoggingCollector(ctx context.Context, pool *pgxpool.Pool) error {
	// Get config file path
	var configFile string
	err := pool.QueryRow(ctx, "SHOW config_file").Scan(&configFile)
	if err != nil {
		return fmt.Errorf("get config_file: %w", err)
	}

	// Get data directory for pg_ctl
	var dataDir string
	err = pool.QueryRow(ctx, "SHOW data_directory").Scan(&dataDir)
	if err != nil {
		return fmt.Errorf("get data_directory: %w", err)
	}

	// Read and modify postgresql.conf
	// Configure logging with proper rotation to prevent log file accumulation:
	// - log_filename with %a (day of week) enables weekly rotation
	// - log_truncate_on_rotation overwrites old logs instead of appending
	// - log_rotation_age of 1440 (1 day) rotates daily
	// - log_rotation_size of 0 disables size-based rotation for predictable names
	// This keeps exactly 7 days of logs, one file per day
	err = modifyPostgresConfig(configFile, map[string]string{
		"logging_collector":        "on",
		"log_lock_waits":           "on",
		"log_directory":            "log",
		"log_filename":             "'postgresql-%a.log'",
		"log_truncate_on_rotation": "on",
		"log_rotation_age":         "1440",
		"log_rotation_size":        "0",
	})
	if err != nil {
		return fmt.Errorf("modify config: %w", err)
	}

	// Create log directory if it doesn't exist
	logDir := filepath.Join(dataDir, "log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	// Restart PostgreSQL using pg_ctl
	// First try to find pg_ctl in common locations
	pgCtl := findPgCtl()
	if pgCtl == "" {
		return fmt.Errorf("pg_ctl not found - please restart PostgreSQL manually")
	}

	// Restart PostgreSQL
	cmd := exec.CommandContext(ctx, pgCtl, "restart", "-D", dataDir, "-m", "fast", "-w")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restart PostgreSQL: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// modifyPostgresConfig updates settings in postgresql.conf
func modifyPostgresConfig(configFile string, settings map[string]string) error {
	// Read existing config
	file, err := os.Open(configFile)
	if err != nil {
		return err
	}

	var lines []string
	scanner := bufio.NewScanner(file)
	found := make(map[string]bool)

	for scanner.Scan() {
		line := scanner.Text()
		modified := false

		for key, value := range settings {
			// Check if this line sets this key (commented or not)
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, key) || strings.HasPrefix(trimmed, "#"+key) {
				lines = append(lines, fmt.Sprintf("%s = %s", key, value))
				found[key] = true
				modified = true
				break
			}
		}

		if !modified {
			lines = append(lines, line)
		}
	}
	file.Close()

	if err := scanner.Err(); err != nil {
		return err
	}

	// Add any settings that weren't found
	for key, value := range settings {
		if !found[key] {
			lines = append(lines, fmt.Sprintf("%s = %s", key, value))
		}
	}

	// Write back
	return os.WriteFile(configFile, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// findPgCtl looks for pg_ctl using pg_config or common locations
func findPgCtl() string {
	// Try pg_config --bindir first (cross-platform)
	if pgConfig, err := exec.LookPath("pg_config"); err == nil {
		cmd := exec.Command(pgConfig, "--bindir")
		if output, err := cmd.Output(); err == nil {
			bindir := strings.TrimSpace(string(output))
			pgCtl := filepath.Join(bindir, "pg_ctl")
			if _, err := os.Stat(pgCtl); err == nil {
				return pgCtl
			}
		}
	}

	// Try PATH
	if path, err := exec.LookPath("pg_ctl"); err == nil {
		return path
	}

	// Platform-specific fallback locations
	var locations []string

	switch runtime.GOOS {
	case "darwin":
		locations = []string{
			"/Applications/Postgres.app/Contents/Versions/latest/bin/pg_ctl",
			"/opt/homebrew/bin/pg_ctl",
			"/usr/local/bin/pg_ctl",
		}
		// Versioned Postgres.app paths
		for v := 18; v >= 11; v-- {
			locations = append(locations, fmt.Sprintf("/Applications/Postgres.app/Contents/Versions/%d/bin/pg_ctl", v))
		}
	case "linux":
		locations = []string{
			"/usr/bin/pg_ctl",
			"/usr/lib/postgresql/16/bin/pg_ctl",
			"/usr/lib/postgresql/15/bin/pg_ctl",
			"/usr/lib/postgresql/14/bin/pg_ctl",
		}
	case "windows":
		locations = []string{
			"C:\\Program Files\\PostgreSQL\\16\\bin\\pg_ctl.exe",
			"C:\\Program Files\\PostgreSQL\\15\\bin\\pg_ctl.exe",
			"C:\\Program Files\\PostgreSQL\\14\\bin\\pg_ctl.exe",
		}
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	return ""
}

// CheckLoggingCollectorStatus returns whether logging_collector is enabled
func CheckLoggingCollectorStatus(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var status string
	err := pool.QueryRow(ctx, "SHOW logging_collector").Scan(&status)
	if err != nil {
		return false, err
	}
	return status == "on", nil
}
