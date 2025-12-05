//go:build windows

package ipc

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

// createListener creates a Windows named pipe listener.
func createListener(path string) (net.Listener, error) {
	// Configure pipe security to allow only the current user
	config := &winio.PipeConfig{
		SecurityDescriptor: "", // Default: creator/owner only
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	}

	listener, err := winio.ListenPipe(path, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create named pipe: %w", err)
	}

	return listener, nil
}

// Dial connects to an IPC endpoint.
func Dial(path string) (net.Conn, error) {
	if path == "" {
		path = DefaultSocketPath()
	}
	return winio.DialPipe(path, nil)
}
