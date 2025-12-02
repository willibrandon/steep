package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/willibrandon/steep/internal/agent"
	"github.com/willibrandon/steep/internal/config"
	"github.com/willibrandon/steep/internal/logger"
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
		// Error already printed by cobra
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
		os.Exit(agent.ExitConfigError)
	}

	// Initialize logger
	logLevel := logger.LevelInfo
	if debug {
		logLevel = logger.LevelDebug
	}
	logger.InitLogger(logLevel, cfg.LogFile)
	defer logger.Close()

	// Create agent
	a, err := agent.New(cfg, debug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating agent: %v\n", err)
		os.Exit(agent.ExitConfigError)
	}

	// Set version
	agent.Version = version

	// Start agent
	if err := a.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting agent: %v\n", err)
		os.Exit(agent.ExitStartFailed)
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
		os.Exit(1)
	}

	return nil
}

// newInstallCmd creates the install subcommand
func newInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install steep-agent as a system service",
		Long: `Install steep-agent as a system service that starts on boot.

Use --user to install as a user service (no elevated privileges required).
System service installation requires administrator/root privileges.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svcConfig := agent.ServiceConfig{
				ConfigPath: configPath,
				UserMode:   userMode,
				Debug:      debug,
			}

			if err := agent.Install(svcConfig); err != nil {
				var permErr *agent.PermissionError
				if errors.As(err, &permErr) {
					fmt.Fprintf(os.Stderr, "Error: %v\n", permErr)
					os.Exit(agent.ExitPermissionDenied)
				}
				if err.Error() == "service already installed" {
					fmt.Fprintf(os.Stderr, "Error: service already installed\n")
					fmt.Fprintf(os.Stderr, "Use 'steep-agent uninstall' first to reinstall\n")
					os.Exit(agent.ExitServiceExists)
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(agent.ExitConfigError)
			}

			fmt.Println("steep-agent installed successfully")
			if userMode {
				fmt.Println("Installed as user service")
			} else {
				fmt.Println("Installed as system service")
			}
			fmt.Println("\nTo start the service:")
			fmt.Println("  steep-agent start")
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
		Long:  `Remove the steep-agent service. The service will be stopped if running.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if agent.RequiresSudo() {
				fmt.Fprintf(os.Stderr, "Error: system service installed, requires sudo\n")
				fmt.Fprintf(os.Stderr, "Run: sudo steep-agent uninstall\n")
				os.Exit(agent.ExitPermissionDenied)
			}
			if err := agent.Uninstall(); err != nil {
				var permErr *agent.PermissionError
				if errors.As(err, &permErr) {
					fmt.Fprintf(os.Stderr, "Error: %v\n", permErr)
					os.Exit(agent.ExitPermissionDenied)
				}
				if err.Error() == "service not installed" {
					fmt.Fprintf(os.Stderr, "Error: service not installed\n")
					os.Exit(agent.ExitServiceNotFound)
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			fmt.Println("steep-agent uninstalled successfully")
			return nil
		},
	}
}

// newStartCmd creates the start subcommand
func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the installed service",
		Long:  `Start the steep-agent service. The service must be installed first.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if agent.RequiresSudo() {
				fmt.Fprintf(os.Stderr, "Error: system service installed, requires sudo\n")
				fmt.Fprintf(os.Stderr, "Run: sudo steep-agent start\n")
				os.Exit(agent.ExitPermissionDenied)
			}
			if err := agent.Start(); err != nil {
				errMsg := err.Error()
				if errMsg == "service not installed" {
					fmt.Fprintf(os.Stderr, "Error: service not installed\n")
					fmt.Fprintf(os.Stderr, "Use 'steep-agent install' first\n")
					os.Exit(agent.ExitServiceNotFound)
				}
				if errMsg == "service already running" {
					fmt.Fprintf(os.Stderr, "Error: service already running\n")
					os.Exit(agent.ExitAlreadyRunning)
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(agent.ExitStartFailed)
			}

			fmt.Println("steep-agent started")
			return nil
		},
	}
}

// newStopCmd creates the stop subcommand
func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running service",
		Long:  `Stop the steep-agent service.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if agent.RequiresSudo() {
				fmt.Fprintf(os.Stderr, "Error: system service installed, requires sudo\n")
				fmt.Fprintf(os.Stderr, "Run: sudo steep-agent stop\n")
				os.Exit(agent.ExitPermissionDenied)
			}
			if err := agent.Stop(); err != nil {
				errMsg := err.Error()
				if errMsg == "service not installed" {
					fmt.Fprintf(os.Stderr, "Error: service not installed\n")
					os.Exit(agent.ExitServiceNotFound)
				}
				if errMsg == "service not running" {
					fmt.Fprintf(os.Stderr, "Error: service not running\n")
					os.Exit(agent.ExitNotRunning)
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(agent.ExitStopFailed)
			}

			fmt.Println("steep-agent stopped")
			return nil
		},
	}
}

// newRestartCmd creates the restart subcommand
func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the service",
		Long:  `Restart the steep-agent service (stop + start).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if agent.RequiresSudo() {
				fmt.Fprintf(os.Stderr, "Error: system service installed, requires sudo\n")
				fmt.Fprintf(os.Stderr, "Run: sudo steep-agent restart\n")
				os.Exit(agent.ExitPermissionDenied)
			}
			if err := agent.Restart(); err != nil {
				errMsg := err.Error()
				if errMsg == "service not installed" {
					fmt.Fprintf(os.Stderr, "Error: service not installed\n")
					fmt.Fprintf(os.Stderr, "Use 'steep-agent install' first\n")
					os.Exit(agent.ExitServiceNotFound)
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(agent.ExitRestartFailed)
			}

			fmt.Println("steep-agent restarted")
			return nil
		},
	}
}

// newStatusCmd creates the status subcommand
func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show service status and health",
		Long: `Show service status including:
  - Service state (running/stopped/not installed)
  - Process ID and uptime
  - Last collection timestamp
  - Connected instances
  - Recent errors`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config for status query
			var cfg *config.Config
			if configPath != "" {
				cfg, _ = config.LoadConfigFromPath(configPath)
			} else {
				cfg, _ = config.LoadConfig()
			}

			status, err := agent.GetStatus(cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(status); err != nil {
					fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
					os.Exit(1)
				}
			} else {
				printHumanStatus(status)
			}

			// Exit code based on status
			switch status.State {
			case "not_installed":
				os.Exit(agent.ExitServiceNotFound)
			case "stopped":
				os.Exit(agent.ExitStopped)
			case "running":
				if len(status.Errors) > 0 {
					os.Exit(agent.ExitUnhealthy)
				}
				os.Exit(agent.ExitSuccess)
			default:
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	return cmd
}

// printHumanStatus prints the status in human-readable format.
func printHumanStatus(status *agent.Status) {
	fmt.Printf("steep-agent status: %s\n", status.State)

	if status.State == "not_installed" {
		fmt.Println("\nTo install the service:")
		fmt.Println("  steep-agent install")
		return
	}

	if status.State == "stopped" {
		fmt.Println("\nTo start the service:")
		fmt.Println("  steep-agent start")
		return
	}

	// Running - show details
	if status.PID > 0 {
		fmt.Printf("  PID:          %d\n", status.PID)
	}
	if status.Uptime != "" {
		fmt.Printf("  Uptime:       %s\n", status.Uptime)
	}
	if status.LastCollect != "" {
		fmt.Printf("  Last Collect: %s\n", status.LastCollect)
	}
	if status.Version != "" {
		fmt.Printf("  Version:      %s\n", status.Version)
	}

	// Instances
	if len(status.Instances) > 0 {
		fmt.Println("\nInstances:")
		for _, inst := range status.Instances {
			lastSeen := ""
			if inst.LastSeen != "" {
				lastSeen = fmt.Sprintf(" (last seen: %s)", inst.LastSeen)
			}
			if inst.Error != "" {
				fmt.Printf("  %-12s %-12s %s [error: %s]\n", inst.Name, inst.Status, lastSeen, inst.Error)
			} else {
				fmt.Printf("  %-12s %-12s%s\n", inst.Name, inst.Status, lastSeen)
			}
		}
	}

	// Errors
	if len(status.Errors) > 0 {
		fmt.Println("\nErrors:")
		for _, e := range status.Errors {
			fmt.Printf("  - %s\n", e)
		}
	} else {
		fmt.Println("\nErrors: none")
	}
}
