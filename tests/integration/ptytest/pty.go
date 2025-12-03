// Package ptytest provides a cross-platform PTY abstraction for integration tests.
package ptytest

import (
	"io"
	"os/exec"
)

// Winsize describes the terminal size.
type Winsize struct {
	Rows uint16
	Cols uint16
}

// PTY represents a pseudo-terminal that can be used to run commands.
type PTY interface {
	io.ReadWriteCloser

	// Setsize sets the terminal size.
	Setsize(size *Winsize) error
}

// Start starts a command in a new PTY and returns the PTY.
// The caller is responsible for closing the PTY and waiting for the command.
func Start(cmd *exec.Cmd) (PTY, error) {
	return startPTY(cmd)
}

// StartWithSize starts a command in a new PTY with the given size.
func StartWithSize(cmd *exec.Cmd, size *Winsize) (PTY, error) {
	p, err := startPTY(cmd)
	if err != nil {
		return nil, err
	}
	if size != nil {
		if err := p.Setsize(size); err != nil {
			p.Close()
			return nil, err
		}
	}
	return p, nil
}
