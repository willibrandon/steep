//go:build !windows

package ptytest

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// unixPTY wraps creack/pty for Unix systems.
type unixPTY struct {
	ptmx *os.File
}

func startPTY(cmd *exec.Cmd) (PTY, error) {
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &unixPTY{ptmx: ptmx}, nil
}

func (p *unixPTY) Read(b []byte) (int, error) {
	return p.ptmx.Read(b)
}

func (p *unixPTY) Write(b []byte) (int, error) {
	return p.ptmx.Write(b)
}

func (p *unixPTY) Close() error {
	return p.ptmx.Close()
}

func (p *unixPTY) Setsize(size *Winsize) error {
	return pty.Setsize(p.ptmx, &pty.Winsize{
		Rows: size.Rows,
		Cols: size.Cols,
	})
}
