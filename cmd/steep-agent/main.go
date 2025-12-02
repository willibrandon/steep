package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/willibrandon/steep/internal/agent"
	"github.com/willibrandon/steep/internal/config"
)

var (
	// Version info (set by ldflags)
	version = "dev"

	// Flags
	configPath string
	debug      bool
	userMode   bool
	jsonOutput bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "steep-agent",
		Short: "Steep background monitoring agent",
		Long: `steep-agent is a background daemon that continuously collects PostgreSQL
monitoring data and persists it to SQLite for the Steep TUI to read.

Service Management:
  steep-agent install [--user]   Install as system/user service
  steep-agent uninstall          Remove the service
  steep-agent start              Start the installed service
  steep-agent stop               Stop the running service
  steep-agent restart            Restart the service
  steep-agent status [--json]    Show service status

Direct Run (for debugging):
  steep-agent run [--debug]      Run in foreground mode`,
		Version: version,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "config file path (default ~/.config/steep/config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging")

	// Add subcommands
	rootCmd.AddCommand(
		newRunCmd(),
		newInstallCmd(),
		newUninstallCmd(),
		newStartCmd(),
		newStopCmd(),
		newRestartCmd(),
		newStatusCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newRunCmd creates the run subcommand for foreground execution
func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run agent in foreground (for debugging)",
		Long:  `Run the agent in foreground mode. Useful for debugging and testing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runForeground()
		},
	}
}

// runForeground runs the agent in foreground mode.
func runForeground() error {
	// Load configuration
	var cfg *config.Config
	var err error

	if configPath != "" {
		cfg, err = config.LoadConfigFromPath(configPath)
	} else {
		cfg, err = config.LoadConfig()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return err
	}

	// Create agent
	a, err := agent.New(cfg, debug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating agent: %v\n", err)
		return err
	}

	// Set version
	agent.Version = version

	// Start agent
	if err := a.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting agent: %v\n", err)
		return err
	}

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for signal
	sig := <-sigChan
	fmt.Printf("\nReceived signal %v, shutting down...\n", sig)

	// Stop agent gracefully
	if err := a.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping agent: %v\n", err)
		return err
	}

	return nil
}

// newInstallCmd creates the install subcommand
func newInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install steep-agent as a system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Implement in US2
			fmt.Println("steep-agent install: not yet implemented (US2)")
			return nil
		},
	}
	cmd.Flags().BoolVar(&userMode, "user", false, "install as user service instead of system")
	return cmd
}

// newUninstallCmd creates the uninstall subcommand
func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove steep-agent service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Implement in US2
			fmt.Println("steep-agent uninstall: not yet implemented (US2)")
			return nil
		},
	}
}

// newStartCmd creates the start subcommand
func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the installed service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Implement in US2
			fmt.Println("steep-agent start: not yet implemented (US2)")
			return nil
		},
	}
}

// newStopCmd creates the stop subcommand
func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Implement in US2
			fmt.Println("steep-agent stop: not yet implemented (US2)")
			return nil
		},
	}
}

// newRestartCmd creates the restart subcommand
func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Implement in US2
			fmt.Println("steep-agent restart: not yet implemented (US2)")
			return nil
		},
	}
}

// newStatusCmd creates the status subcommand
func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show service status and health",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Implement in US2
			fmt.Println("steep-agent status: not yet implemented (US2)")
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	return cmd
}
