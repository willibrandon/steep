//go:build !windows

package ipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// createListener creates a Unix domain socket listener.
func createListener(path string) (net.Listener, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %w", err)
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("failed to create Unix socket: %w", err)
	}

	// Set socket permissions to allow only owner access
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		os.Remove(path)
		return nil, fmt.Errorf("failed to set socket permissions: %w", err)
	}

	return listener, nil
}

// Dial connects to an IPC endpoint.
func Dial(path string) (net.Conn, error) {
	if path == "" {
		path = DefaultSocketPath()
	}
	return net.Dial("unix", path)
}
