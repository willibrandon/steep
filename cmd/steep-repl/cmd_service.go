package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/willibrandon/steep/internal/repl/daemon"
)

// newInstallCmd creates the install subcommand
func newInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install steep-repl as a system service",
		Long: `Install steep-repl as a system service that starts on boot.

Use --user to install as a user service (no elevated privileges required).
System service installation requires administrator/root privileges.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svcConfig := daemon.ServiceConfig{
				ConfigPath: configPath,
				UserMode:   userMode,
				Debug:      debug,
			}

			if err := daemon.Install(svcConfig); err != nil {
				var permErr *daemon.PermissionError
				if errors.As(err, &permErr) {
					fmt.Fprintf(os.Stderr, "Error: %v\n", permErr)
					os.Exit(daemon.ExitPermissionDenied)
				}
				if err.Error() == "service already installed" {
					fmt.Fprintf(os.Stderr, "Error: service already installed\n")
					fmt.Fprintf(os.Stderr, "Use 'steep-repl uninstall' first to reinstall\n")
					os.Exit(daemon.ExitServiceExists)
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(daemon.ExitConfigError)
			}

			fmt.Println("steep-repl installed successfully")
			if userMode {
				fmt.Println("Installed as user service")
			} else {
				fmt.Println("Installed as system service")
			}
			fmt.Println("\nTo start the service:")
			fmt.Println("  steep-repl start")
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
		Short: "Remove steep-repl service",
		Long:  `Remove the steep-repl service. The service will be stopped if running.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if daemon.RequiresSudo() {
				fmt.Fprintf(os.Stderr, "Error: system service installed, requires sudo\n")
				fmt.Fprintf(os.Stderr, "Run: sudo steep-repl uninstall\n")
				os.Exit(daemon.ExitPermissionDenied)
			}
			if err := daemon.Uninstall(); err != nil {
				var permErr *daemon.PermissionError
				if errors.As(err, &permErr) {
					fmt.Fprintf(os.Stderr, "Error: %v\n", permErr)
					os.Exit(daemon.ExitPermissionDenied)
				}
				if err.Error() == "service not installed" {
					fmt.Fprintf(os.Stderr, "Error: service not installed\n")
					os.Exit(daemon.ExitServiceNotFound)
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			fmt.Println("steep-repl uninstalled successfully")
			return nil
		},
	}
}

// newStartCmd creates the start subcommand
func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the installed service",
		Long:  `Start the steep-repl service. The service must be installed first.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if daemon.RequiresSudo() {
				fmt.Fprintf(os.Stderr, "Error: system service installed, requires sudo\n")
				fmt.Fprintf(os.Stderr, "Run: sudo steep-repl start\n")
				os.Exit(daemon.ExitPermissionDenied)
			}
			if err := daemon.StartService(); err != nil {
				errMsg := err.Error()
				if errMsg == "service not installed" {
					fmt.Fprintf(os.Stderr, "Error: service not installed\n")
					fmt.Fprintf(os.Stderr, "Use 'steep-repl install' first\n")
					os.Exit(daemon.ExitServiceNotFound)
				}
				if errMsg == "service already running" {
					fmt.Fprintf(os.Stderr, "Error: service already running\n")
					os.Exit(daemon.ExitAlreadyRunning)
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(daemon.ExitStartFailed)
			}

			fmt.Println("steep-repl started")
			return nil
		},
	}
}

// newStopCmd creates the stop subcommand
func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running service",
		Long:  `Stop the steep-repl service.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if daemon.RequiresSudo() {
				fmt.Fprintf(os.Stderr, "Error: system service installed, requires sudo\n")
				fmt.Fprintf(os.Stderr, "Run: sudo steep-repl stop\n")
				os.Exit(daemon.ExitPermissionDenied)
			}
			if err := daemon.StopService(); err != nil {
				errMsg := err.Error()
				if errMsg == "service not installed" {
					fmt.Fprintf(os.Stderr, "Error: service not installed\n")
					os.Exit(daemon.ExitServiceNotFound)
				}
				if errMsg == "service not running" {
					fmt.Fprintf(os.Stderr, "Error: service not running\n")
					os.Exit(daemon.ExitNotRunning)
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(daemon.ExitStopFailed)
			}

			fmt.Println("steep-repl stopped")
			return nil
		},
	}
}

// newRestartCmd creates the restart subcommand
func newRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the service",
		Long:  `Restart the steep-repl service (stop + start).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if daemon.RequiresSudo() {
				fmt.Fprintf(os.Stderr, "Error: system service installed, requires sudo\n")
				fmt.Fprintf(os.Stderr, "Run: sudo steep-repl restart\n")
				os.Exit(daemon.ExitPermissionDenied)
			}
			if err := daemon.Restart(); err != nil {
				errMsg := err.Error()
				if errMsg == "service not installed" {
					fmt.Fprintf(os.Stderr, "Error: service not installed\n")
					fmt.Fprintf(os.Stderr, "Use 'steep-repl install' first\n")
					os.Exit(daemon.ExitServiceNotFound)
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(daemon.ExitRestartFailed)
			}

			fmt.Println("steep-repl restarted")
			return nil
		},
	}
}
