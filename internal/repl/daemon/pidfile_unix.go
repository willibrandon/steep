//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

// isProcessRunning checks if a process with the given PID is running.
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Send signal 0 to check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds, so we need to signal
	err = process.Signal(syscall.Signal(0))
	return err == nil
}
