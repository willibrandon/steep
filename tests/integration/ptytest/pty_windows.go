//go:build windows

package ptytest

import (
	"fmt"
	"os/exec"
	"syscall"

	"github.com/charmbracelet/x/conpty"
)

// windowsPTY implements PTY using Windows ConPTY via charmbracelet/x/conpty.
type windowsPTY struct {
	cpty   *conpty.ConPty
	handle uintptr
}

func startPTY(cmd *exec.Cmd) (PTY, error) {
	return startPTYWithSize(cmd, &Winsize{Rows: 30, Cols: 120})
}

func startPTYWithSize(cmd *exec.Cmd, size *Winsize) (PTY, error) {
	// Create ConPTY with specified size
	cpty, err := conpty.New(int(size.Cols), int(size.Rows), 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create ConPTY: %w", err)
	}

	// Build args for Spawn
	args := cmd.Args
	if len(args) == 0 {
		args = []string{cmd.Path}
	}

	// Spawn process attached to ConPTY
	procAttr := &syscall.ProcAttr{
		Dir: cmd.Dir,
		Env: cmd.Env,
	}
	_, handle, err := cpty.Spawn(cmd.Path, args, procAttr)
	if err != nil {
		cpty.Close()
		return nil, fmt.Errorf("failed to spawn process: %w", err)
	}

	return &windowsPTY{cpty: cpty, handle: handle}, nil
}

func (p *windowsPTY) Read(b []byte) (int, error) {
	return p.cpty.Read(b)
}

func (p *windowsPTY) Write(b []byte) (int, error) {
	return p.cpty.Write(b)
}

func (p *windowsPTY) Close() error {
	return p.cpty.Close()
}

func (p *windowsPTY) Setsize(size *Winsize) error {
	return p.cpty.Resize(int(size.Cols), int(size.Rows))
}
