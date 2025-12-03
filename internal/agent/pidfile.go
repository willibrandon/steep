package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ErrAgentRunning is returned when another agent instance is already running.
var ErrAgentRunning = errors.New("another steep-agent instance is already running")

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

	// Check if another agent is running
	existingPID, err := ReadPIDFile(path)
	if err == nil && existingPID > 0 {
		// Check if process is still running
		if isProcessRunning(existingPID) {
			return ErrAgentRunning
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

// CheckPIDFile checks if an agent is running based on the PID file.
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

// isProcessRunning checks if a process with the given PID is running.
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Send signal 0 to check if process exists
	// This works on Unix-like systems
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds, so we need to signal
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// DefaultPIDFilePath returns the default PID file path.
func DefaultPIDFilePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "steep-agent.pid"
	}
	return filepath.Join(homeDir, ".config", "steep", "steep-agent.pid")
}

// AgentRunning checks if the agent is currently running.
// This is a convenience function that combines CheckPIDFile with status check.
func AgentRunning() (bool, int, error) {
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
