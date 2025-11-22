package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ClipboardWriter provides cross-platform clipboard access with graceful degradation.
type ClipboardWriter struct {
	available bool
	errMsg    string
}

// NewClipboardWriter creates a new ClipboardWriter and checks availability.
func NewClipboardWriter() *ClipboardWriter {
	cw := &ClipboardWriter{}
	cw.checkAvailability()
	return cw
}

// checkAvailability determines if clipboard is accessible.
func (cw *ClipboardWriter) checkAvailability() {
	switch runtime.GOOS {
	case "darwin":
		// macOS uses pbcopy
		if _, err := exec.LookPath("pbcopy"); err != nil {
			cw.available = false
			cw.errMsg = "pbcopy not found"
			return
		}
		cw.available = true

	case "linux":
		// Linux uses xclip or xsel
		if _, err := exec.LookPath("xclip"); err == nil {
			cw.available = true
			return
		}
		if _, err := exec.LookPath("xsel"); err == nil {
			cw.available = true
			return
		}
		// Check for wayland
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cw.available = true
			return
		}
		cw.available = false
		cw.errMsg = "clipboard tool not found (install xclip, xsel, or wl-copy)"

	case "windows":
		// Windows uses clip.exe
		if _, err := exec.LookPath("clip"); err != nil {
			cw.available = false
			cw.errMsg = "clip.exe not found"
			return
		}
		cw.available = true

	default:
		cw.available = false
		cw.errMsg = fmt.Sprintf("unsupported platform: %s", runtime.GOOS)
	}
}

// IsAvailable returns whether clipboard operations are supported.
func (cw *ClipboardWriter) IsAvailable() bool {
	return cw.available
}

// Error returns the reason clipboard is unavailable.
func (cw *ClipboardWriter) Error() string {
	return cw.errMsg
}

// Write copies text to the system clipboard.
func (cw *ClipboardWriter) Write(text string) error {
	if !cw.available {
		return fmt.Errorf("clipboard unavailable: %s", cw.errMsg)
	}

	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")

	case "linux":
		// Try xclip first, then xsel, then wl-copy
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else {
			cmd = exec.Command("wl-copy")
		}

	case "windows":
		cmd = exec.Command("clip")

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
