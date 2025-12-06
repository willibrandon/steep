package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
)

// newRunCmd creates the run subcommand for foreground execution
func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run daemon in foreground (for debugging)",
		Long:  `Run the daemon in foreground mode. Useful for debugging and testing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runForeground()
		},
	}
}

// runForeground runs the daemon in foreground mode.
func runForeground() error {
	// Load configuration
	var cfg *config.Config
	var err error

	if configPath != "" {
		cfg, err = config.LoadFromPath(configPath)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(daemon.ExitConfigError)
	}

	// Initialize logger for debug logging in repl packages
	logLevel := logger.LevelInfo
	if debug {
		logLevel = logger.LevelDebug
	}
	// Use ~/.config/steep/ for consistency with TUI
	logFile := ""
	if homeDir, err := os.UserHomeDir(); err == nil {
		logFile = filepath.Join(homeDir, ".config", "steep", "steep-repl.log")
	}
	logger.InitLogger(logLevel, logFile)
	defer logger.Close()
	if debug && logFile != "" {
		fmt.Fprintf(os.Stderr, "Debug mode: Logs written to %s\n", logFile)
		logger.Debug("steep-repl daemon starting", "version", version, "config", configPath)
	}

	// Create daemon
	d, err := daemon.New(cfg, debug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating daemon: %v\n", err)
		os.Exit(daemon.ExitConfigError)
	}

	// Set version
	daemon.Version = version

	// Start daemon
	if err := d.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting daemon: %v\n", err)
		os.Exit(daemon.ExitStartFailed)
	}

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for signal
	sig := <-sigChan
	fmt.Printf("\nReceived signal %v, shutting down...\n", sig)

	// Stop daemon gracefully
	if err := d.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping daemon: %v\n", err)
		os.Exit(1)
	}

	return nil
}
