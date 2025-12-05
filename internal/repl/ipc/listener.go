package ipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultSocketPath returns the default IPC socket/pipe path for the current platform.
func DefaultSocketPath() string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\steep-repl`
	}
	return "/tmp/steep-repl.sock"
}

// Listener wraps a net.Listener for cross-platform IPC.
type Listener struct {
	listener net.Listener
	path     string
}

// NewListener creates a new IPC listener at the given path.
// On Unix, this creates a Unix domain socket.
// On Windows, this creates a named pipe.
func NewListener(path string) (*Listener, error) {
	if path == "" {
		path = DefaultSocketPath()
	}

	// Clean up stale socket/pipe if it exists
	if err := cleanupStaleEndpoint(path); err != nil {
		return nil, fmt.Errorf("failed to cleanup stale endpoint: %w", err)
	}

	listener, err := createListener(path)
	if err != nil {
		return nil, err
	}

	return &Listener{
		listener: listener,
		path:     path,
	}, nil
}

// Accept waits for and returns the next connection to the listener.
func (l *Listener) Accept() (net.Conn, error) {
	return l.listener.Accept()
}

// Close closes the listener.
func (l *Listener) Close() error {
	err := l.listener.Close()

	// On Unix, remove the socket file
	if runtime.GOOS != "windows" {
		os.Remove(l.path)
	}

	return err
}

// Addr returns the listener's network address.
func (l *Listener) Addr() net.Addr {
	return l.listener.Addr()
}

// Path returns the socket/pipe path.
func (l *Listener) Path() string {
	return l.path
}

// cleanupStaleEndpoint removes a stale socket file or checks if a pipe is available.
func cleanupStaleEndpoint(path string) error {
	if runtime.GOOS == "windows" {
		// Windows named pipes are managed by the OS
		// No cleanup needed - if pipe exists and is active, creating a new one will fail
		return nil
	}

	// Unix: check if socket file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Socket doesn't exist, nothing to clean up
		return nil
	}

	// Try to connect to see if it's active
	conn, err := net.Dial("unix", path)
	if err == nil {
		// Socket is active - another instance is running
		conn.Close()
		return fmt.Errorf("IPC socket already in use by another process: %s", path)
	}

	// Socket file exists but no one is listening - it's stale
	// Ensure the directory exists before removing
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil // Directory doesn't exist, nothing to remove
	}

	// Remove stale socket
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove stale socket: %w", err)
	}

	return nil
}
