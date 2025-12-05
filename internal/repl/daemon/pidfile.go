package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrDaemonRunning is returned when another daemon instance is already running.
var ErrDaemonRunning = errors.New("another steep-repl instance is already running")

// ErrNoPIDFile is returned when no PID file exists.
var ErrNoPIDFile = errors.New("no PID file found")

// ErrStalePIDFile is returned when the PID file exists but the process is not running.
var ErrStalePIDFile = errors.New("stale PID file (process not running)")

// WritePIDFile writes the current process ID to the PID file.
func WritePIDFile(path string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create PID file directory: %w", err)
	}

	// Check if another daemon is running
	existingPID, err := ReadPIDFile(path)
	if err == nil && existingPID > 0 {
		// Check if process is still running
		if isProcessRunning(existingPID) {
			return ErrDaemonRunning
		}
		// Stale PID file, remove it
	}

	// Write our PID
	pid := os.Getpid()
	content := fmt.Sprintf("%d\n", pid)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	return nil
}

// ReadPIDFile reads the PID from the PID file.
func ReadPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, ErrNoPIDFile
		}
		return 0, fmt.Errorf("failed to read PID file: %w", err)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID file content: %w", err)
	}

	return pid, nil
}

// RemovePIDFile removes the PID file.
func RemovePIDFile(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}
	return nil
}

// CheckPIDFile checks if a daemon is running based on the PID file.
// Returns the PID if running, 0 if not running, and an error if the file cannot be read.
func CheckPIDFile(path string) (int, error) {
	pid, err := ReadPIDFile(path)
	if err != nil {
		if errors.Is(err, ErrNoPIDFile) {
			return 0, nil
		}
		return 0, err
	}

	if !isProcessRunning(pid) {
		return 0, ErrStalePIDFile
	}

	return pid, nil
}

// DefaultPIDFilePath returns the default PID file path.
func DefaultPIDFilePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "steep-repl.pid"
	}
	return filepath.Join(homeDir, ".config", "steep", "steep-repl.pid")
}

// DaemonRunning checks if the daemon is currently running.
// This is a convenience function that combines CheckPIDFile with status check.
func DaemonRunning() (bool, int, error) {
	path := DefaultPIDFilePath()
	pid, err := CheckPIDFile(path)
	if err != nil {
		if errors.Is(err, ErrStalePIDFile) {
			// Remove stale PID file
			_ = RemovePIDFile(path)
			return false, 0, nil
		}
		return false, 0, err
	}
	return pid > 0, pid, nil
}
