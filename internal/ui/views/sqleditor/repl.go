package sqleditor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/willibrandon/steep/internal/logger"
)

// ReplExitedMsg is sent when the external REPL exits.
type ReplExitedMsg struct {
	Tool string // "pgcli" or "psql"
	Err  error
}

// ReplType represents the type of REPL to use.
type ReplType string

const (
	ReplPgcli       ReplType = "pgcli"
	ReplPsql        ReplType = "psql"
	ReplAuto        ReplType = "auto"   // Auto-detect best available
	ReplDocker      ReplType = "docker" // Force Docker (auto-detect pgcli or psql)
	ReplDockerPgcli ReplType = "docker-pgcli"
	ReplDockerPsql  ReplType = "docker-psql"
)

// clearScreenExecCommand wraps an exec.Cmd to clear the screen before running.
// This is needed because bubbletea's ExecProcess exits altscreen first, returning
// to the normal buffer which may have old terminal content.
type clearScreenExecCommand struct {
	cmd *exec.Cmd
}

func (c *clearScreenExecCommand) Run() error {
	// Clear screen AFTER bubbletea has released the terminal (exited altscreen)
	fmt.Print("\033[2J\033[H")
	return c.cmd.Run()
}

func (c *clearScreenExecCommand) SetStdin(r io.Reader) {
	if c.cmd.Stdin == nil {
		c.cmd.Stdin = r
	}
}

func (c *clearScreenExecCommand) SetStdout(w io.Writer) {
	if c.cmd.Stdout == nil {
		c.cmd.Stdout = w
	}
}

func (c *clearScreenExecCommand) SetStderr(w io.Writer) {
	if c.cmd.Stderr == nil {
		c.cmd.Stderr = w
	}
}

// replCmd launches an external REPL (pgcli or psql) with the current connection.
// This command uses tea.ExecProcess to suspend the TUI and hand control to the REPL.
func (v *SQLEditorView) replCmd(args []string) tea.Cmd {
	if v.executor == nil || v.executor.pool == nil {
		v.showToast("Not connected to database", true)
		return nil
	}

	// Get connection string from pool config
	connString := v.executor.pool.Config().ConnConfig.ConnString()
	if connString == "" {
		v.showToast("Could not get connection string", true)
		return nil
	}

	// Determine which REPL to use
	// Supports: :repl, :repl pgcli, :repl psql, :repl docker, :repl docker pgcli, :repl docker psql
	replType := ReplAuto
	if len(args) > 0 {
		switch args[0] {
		case "pgcli":
			replType = ReplPgcli
		case "psql":
			replType = ReplPsql
		case "docker":
			// :repl docker [pgcli|psql]
			if len(args) > 1 {
				switch args[1] {
				case "pgcli":
					replType = ReplDockerPgcli
				case "psql":
					replType = ReplDockerPsql
				default:
					replType = ReplDocker
				}
			} else {
				replType = ReplDocker
			}
		}
	}

	// Find available REPL (tries local, then Docker fallback)
	logger.Info("REPL: searching for REPL", "type", string(replType))
	result := findRepl(replType, connString)
	if result.cmd == nil {
		logger.Error("REPL: no REPL found", "type", string(replType), "error", result.err)
		v.showToast(result.err, true)
		return nil
	}

	// Log the command we're about to run
	logger.Info("REPL: launching", "tool", result.tool, "path", result.cmd.Path, "args", result.cmd.Args)

	// Set up terminal for the subprocess
	result.cmd.Stdin = os.Stdin
	result.cmd.Stdout = os.Stdout
	result.cmd.Stderr = os.Stderr

	// Use tea.Exec with our custom wrapper that clears screen after exiting altscreen
	return tea.Exec(&clearScreenExecCommand{cmd: result.cmd}, func(err error) tea.Msg {
		return ReplExitedMsg{Tool: result.tool, Err: err}
	})
}

// replResult contains the result of finding a REPL.
type replResult struct {
	tool string
	cmd  *exec.Cmd
	err  string // User-friendly error message if no REPL found
}

// findRepl finds and configures the appropriate REPL command.
// Tries local installations first, then falls back to Docker.
func findRepl(replType ReplType, connString string) replResult {
	switch replType {
	case ReplPgcli:
		// Try local pgcli
		if path := findExecutable("pgcli"); path != "" {
			return replResult{tool: "pgcli", cmd: exec.Command(path, connString)}
		}
		// Try Docker pgcli
		if cmd := tryDockerPgcli(connString); cmd != nil {
			return replResult{tool: "pgcli (docker)", cmd: cmd}
		}
		return replResult{err: "pgcli not found. Install with: pip install pgcli\nOr install Docker to use containerized pgcli"}

	case ReplPsql:
		// Try local psql
		if path := findExecutable("psql"); path != "" {
			return replResult{tool: "psql", cmd: exec.Command(path, connString)}
		}
		// Try Docker psql
		if cmd := tryDockerPsql(connString); cmd != nil {
			return replResult{tool: "psql (docker)", cmd: cmd}
		}
		return replResult{err: "psql not found. Install PostgreSQL client tools\nOr install Docker to use containerized psql"}

	case ReplAuto:
		// Try local pgcli first (preferred)
		if path := findExecutable("pgcli"); path != "" {
			return replResult{tool: "pgcli", cmd: exec.Command(path, connString)}
		}
		// Try local psql
		if path := findExecutable("psql"); path != "" {
			return replResult{tool: "psql", cmd: exec.Command(path, connString)}
		}
		// Try Docker pgcli
		if cmd := tryDockerPgcli(connString); cmd != nil {
			return replResult{tool: "pgcli (docker)", cmd: cmd}
		}
		// Try Docker psql
		if cmd := tryDockerPsql(connString); cmd != nil {
			return replResult{tool: "psql (docker)", cmd: cmd}
		}
		// Nothing available
		return replResult{err: noReplAvailableError()}

	case ReplDocker:
		// Force Docker - prefer pgcli, fall back to psql
		if cmd := tryDockerPgcli(connString); cmd != nil {
			return replResult{tool: "pgcli (docker)", cmd: cmd}
		}
		if cmd := tryDockerPsql(connString); cmd != nil {
			return replResult{tool: "psql (docker)", cmd: cmd}
		}
		return replResult{err: dockerNotAvailableError()}

	case ReplDockerPgcli:
		if cmd := tryDockerPgcli(connString); cmd != nil {
			return replResult{tool: "pgcli (docker)", cmd: cmd}
		}
		return replResult{err: dockerNotAvailableError()}

	case ReplDockerPsql:
		if cmd := tryDockerPsql(connString); cmd != nil {
			return replResult{tool: "psql (docker)", cmd: cmd}
		}
		return replResult{err: dockerNotAvailableError()}
	}

	return replResult{err: "Unknown REPL type"}
}

// dockerNotAvailableError returns an error when Docker is requested but not available.
func dockerNotAvailableError() string {
	if findExecutable("docker") == "" {
		return "Docker not found.\n\nInstall Docker to use containerized REPLs."
	}
	return "Docker is available but failed to create command.\n\nTry pulling images:\n  docker pull willibrandon/pgcli\n  docker pull postgres:alpine"
}

// noReplAvailableError returns a helpful error message when no REPL is available.
func noReplAvailableError() string {
	var msg string
	msg = "No PostgreSQL REPL available.\n\n"
	msg += "Install one of the following:\n"
	msg += "  pgcli (recommended): pip install pgcli\n"
	msg += "  psql: Install PostgreSQL client tools\n\n"
	if findExecutable("docker") == "" {
		msg += "Docker not found. Install Docker for automatic fallback."
	} else {
		msg += "Docker is available but images may need to be pulled.\n"
		msg += "Try: docker pull willibrandon/pgcli\n"
		msg += " or: docker pull postgres:alpine"
	}
	return msg
}

// adjustConnStringForDocker adjusts a connection string for Docker networking.
// On Windows, Docker runs in a VM where localhost doesn't reach the host.
// We replace localhost/127.0.0.1 with host.docker.internal.
func adjustConnStringForDocker(connString string) string {
	if runtime.GOOS != "windows" {
		return connString
	}

	// Replace localhost and 127.0.0.1 with host.docker.internal
	// Handle both @localhost and @127.0.0.1 in connection strings
	result := connString
	result = strings.Replace(result, "@localhost", "@host.docker.internal", 1)
	result = strings.Replace(result, "@127.0.0.1", "@host.docker.internal", 1)

	// Also handle host= parameter format (key=value style)
	result = strings.Replace(result, "host=localhost", "host=host.docker.internal", 1)
	result = strings.Replace(result, "host=127.0.0.1", "host=host.docker.internal", 1)

	return result
}

// tryDockerPgcli attempts to create a Docker command for pgcli.
// Returns nil if Docker is not available.
func tryDockerPgcli(connString string) *exec.Cmd {
	dockerPath := findExecutable("docker")
	if dockerPath == "" {
		return nil
	}

	// Adjust connection string for Docker networking on Windows
	dockerConnString := adjustConnStringForDocker(connString)

	// Use willibrandon/pgcli image (multi-arch: amd64 + arm64)
	// --net host allows connecting to localhost databases
	return exec.Command(dockerPath,
		"run", "--rm", "-it", "--net", "host",
		"willibrandon/pgcli",
		dockerConnString,
	)
}

// tryDockerPsql attempts to create a Docker command for psql.
// Returns nil if Docker is not available.
func tryDockerPsql(connString string) *exec.Cmd {
	dockerPath := findExecutable("docker")
	if dockerPath == "" {
		return nil
	}

	// Adjust connection string for Docker networking on Windows
	dockerConnString := adjustConnStringForDocker(connString)

	// Use official postgres:alpine image (includes psql, lightweight)
	// --net host allows connecting to localhost databases
	return exec.Command(dockerPath,
		"run", "--rm", "-it", "--net", "host",
		"postgres:alpine",
		"psql", dockerConnString,
	)
}

// findExecutable checks if a program is available in PATH.
func findExecutable(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}
