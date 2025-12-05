//go:build windows

package daemon

import (
	"golang.org/x/sys/windows"
)

// isProcessRunning checks if a process with the given PID is running.
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	// On Windows, we need to open the process to check if it exists
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	windows.CloseHandle(handle)
	return true
}
