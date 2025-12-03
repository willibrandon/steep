//go:build windows

package ptytest

import (
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/conpty"
)

func TestConPTYBasic(t *testing.T) {
	// Test with a simple echo command using charmbracelet/x/conpty
	cpty, err := conpty.New(80, 25, 0)
	if err != nil {
		t.Fatalf("Failed to create ConPTY: %v", err)
	}
	defer cpty.Close()

	// Spawn process
	_, _, err = cpty.Spawn("cmd.exe", []string{"/c", "echo", "hello_from_conpty"}, &syscall.ProcAttr{})
	if err != nil {
		t.Fatalf("Failed to spawn process: %v", err)
	}

	// Read output in a goroutine
	var output strings.Builder
	readDone := make(chan struct{})

	go func() {
		defer close(readDone)
		buf := make([]byte, 4096)
		for {
			n, err := cpty.Read(buf)
			if n > 0 {
				output.Write(buf[:n])
				t.Logf("Read %d bytes: %q", n, string(buf[:n]))
			}
			if err != nil {
				t.Logf("Read ended: %v", err)
				return
			}
		}
	}()

	// Wait for process to complete
	time.Sleep(2 * time.Second)

	// Close to unblock any remaining reads
	cpty.Close()

	// Wait for read goroutine
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Log("Read goroutine didn't finish")
	}

	t.Logf("Final output: %q", output.String())
	if !strings.Contains(output.String(), "hello_from_conpty") {
		t.Errorf("Expected 'hello_from_conpty' in output, got: %q", output.String())
	}
}
