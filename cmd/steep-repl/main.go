package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
	"github.com/willibrandon/steep/internal/repl/grpc/certs"
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
		Use:   "steep-repl",
		Short: "Steep bidirectional replication daemon",
		Long: `steep-repl is a background daemon that coordinates bidirectional replication
across PostgreSQL 18 instances. It manages node registration, coordinator election,
and provides status via IPC, gRPC, and HTTP endpoints.

Service Management:
  steep-repl install [--user]   Install as system/user service
  steep-repl uninstall          Remove the service
  steep-repl start              Start the installed service
  steep-repl stop               Stop the running service
  steep-repl restart            Restart the service
  steep-repl status [--json]    Show service status

Direct Run (for debugging):
  steep-repl run [--debug]      Run in foreground mode`,
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
		newHealthCmd(),
		newInitTLSCmd(),
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

// newStatusCmd creates the status subcommand
func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show service status and health",
		Long: `Show service status including:
  - Service state (running/stopped/not installed)
  - Node ID and name
  - Uptime
  - Component health (PostgreSQL, gRPC, IPC, HTTP)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := daemon.GetStatus()
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
				os.Exit(daemon.ExitServiceNotFound)
			case "stopped":
				os.Exit(daemon.ExitStopped)
			case "running":
				os.Exit(daemon.ExitSuccess)
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
func printHumanStatus(status *daemon.ServiceStatus) {
	fmt.Printf("steep-repl status: %s\n", status.State)

	if status.State == "not_installed" {
		fmt.Println("\nTo install the service:")
		fmt.Println("  steep-repl install")
		return
	}

	if status.State == "stopped" {
		fmt.Println("\nTo start the service:")
		fmt.Println("  steep-repl start")
		return
	}

	// Running - show details
	if status.NodeID != "" {
		fmt.Printf("  Node ID:    %s\n", status.NodeID)
	}
	if status.NodeName != "" {
		fmt.Printf("  Node Name:  %s\n", status.NodeName)
	}
	if status.PID > 0 {
		fmt.Printf("  PID:        %d\n", status.PID)
	}
	if status.Uptime != "" {
		fmt.Printf("  Uptime:     %s\n", status.Uptime)
	}
	if status.Version != "" {
		fmt.Printf("  Version:    %s\n", status.Version)
	}

	// Component status
	fmt.Println("\nComponents:")
	printComponentStatus("PostgreSQL", status.PostgreSQL)
	printComponentStatus("gRPC", status.GRPC)
	printComponentStatus("IPC", status.IPC)
	printComponentStatus("HTTP", status.HTTP)
}

// newHealthCmd creates the health subcommand for remote node health checks.
func newHealthCmd() *cobra.Command {
	var remoteAddr string
	var timeout time.Duration
	var certFile, keyFile, caFile string
	var insecure bool

	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check health of a remote node via gRPC",
		Long: `Check the health status of a remote steep-repl node via gRPC.

This command connects to a remote node and retrieves its health status,
including component health (PostgreSQL, gRPC, etc.) and uptime information.

Examples:
  # Without TLS (insecure)
  steep-repl health --remote localhost:5433 --insecure

  # With TLS (using config file)
  steep-repl health --remote localhost:5433 --config repl.config.yaml

  # With TLS (explicit certs)
  steep-repl health --remote localhost:5433 --ca certs/ca.crt`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if remoteAddr == "" {
				return fmt.Errorf("--remote flag is required")
			}

			// Load TLS config from config file if specified
			if configPath != "" && caFile == "" {
				cfg, err := config.LoadFromPath(configPath)
				if err == nil && cfg.GRPC.TLS.CAFile != "" {
					caFile = cfg.GRPC.TLS.CAFile
					// Use client certs if available, otherwise fall back to server certs
					if cfg.GRPC.TLS.ClientCertFile != "" {
						certFile = cfg.GRPC.TLS.ClientCertFile
						keyFile = cfg.GRPC.TLS.ClientKeyFile
					} else {
						certFile = cfg.GRPC.TLS.CertFile
						keyFile = cfg.GRPC.TLS.KeyFile
					}
				}
			}

			// Require either --insecure or TLS config
			if !insecure && caFile == "" {
				return fmt.Errorf("either --insecure or --ca (or --config with TLS) is required")
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			// Create gRPC client
			clientCfg := replgrpc.ClientConfig{
				Address: remoteAddr,
				Timeout: timeout,
			}
			if !insecure {
				clientCfg.CAFile = caFile
				clientCfg.CertFile = certFile
				clientCfg.KeyFile = keyFile
			}

			client, err := replgrpc.NewClient(ctx, clientCfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error connecting to %s: %v\n", remoteAddr, err)
				os.Exit(1)
			}
			defer client.Close()

			// Get health check result
			result, err := client.GetHealthCheckResult(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(result); err != nil {
					fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
					os.Exit(1)
				}
			} else {
				printHealthResult(result)
			}

			// Exit code based on status
			if result.Status != "healthy" {
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&remoteAddr, "remote", "", "remote node address (host:port)")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "connection timeout")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file")
	cmd.Flags().StringVar(&certFile, "cert", "", "client certificate file (for mTLS)")
	cmd.Flags().StringVar(&keyFile, "key", "", "client key file (for mTLS)")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("remote")

	return cmd
}

// printHealthResult prints the health check result in human-readable format.
func printHealthResult(result *replgrpc.HealthCheckResult) {
	fmt.Printf("Remote node health: %s\n", result.Status)
	if result.NodeID != "" {
		fmt.Printf("  Node ID:    %s\n", result.NodeID)
	}
	if result.NodeName != "" {
		fmt.Printf("  Node Name:  %s\n", result.NodeName)
	}
	if result.Version != "" {
		fmt.Printf("  Version:    %s\n", result.Version)
	}
	if !result.UptimeSince.IsZero() {
		uptime := time.Since(result.UptimeSince)
		fmt.Printf("  Uptime:     %s\n", formatDuration(uptime))
	}

	if len(result.Components) > 0 {
		fmt.Println("\nComponents:")
		for name, comp := range result.Components {
			status := comp.Status
			if comp.Message != "" {
				status = fmt.Sprintf("%s (%s)", status, comp.Message)
			}
			healthIcon := "✓"
			if !comp.Healthy {
				healthIcon = "✗"
			}
			fmt.Printf("  %-12s %s %s\n", name+":", healthIcon, status)
		}
	}
}

// formatDuration formats a duration in human-readable form.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}

// printComponentStatus prints a single component's status.
func printComponentStatus(name string, cs daemon.ComponentStatus) {
	status := cs.Status
	if status == "" {
		status = "not_initialized"
	}

	detail := ""
	if cs.Port > 0 {
		detail = fmt.Sprintf(" (port %d)", cs.Port)
	}
	if cs.Version != "" {
		detail = fmt.Sprintf(" (v%s)", cs.Version)
	}
	if cs.Error != "" {
		detail = fmt.Sprintf(" [%s]", cs.Error)
	}

	fmt.Printf("  %-12s %s%s\n", name+":", status, detail)
}

// newInitTLSCmd creates the init-tls subcommand for generating mTLS certificates.
func newInitTLSCmd() *cobra.Command {
	var (
		outputDir string
		nodeName  string
		hosts     []string
		validDays int
	)

	cmd := &cobra.Command{
		Use:   "init-tls",
		Short: "Generate mTLS certificates for secure node communication",
		Long: `Generate a CA and server/client certificates for mTLS.

This creates all certificates needed for secure gRPC communication:
  - ca.crt, ca.key       CA certificate and key
  - server.crt, server.key   Server certificate for this node
  - client.crt, client.key   Client certificate for connecting to other nodes

Example:
  steep-repl init-tls
  steep-repl init-tls --hosts localhost,192.168.1.10,node1.example.com
  steep-repl init-tls --output ~/.config/steep/certs --days 365`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Default output directory
			if outputDir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("get home dir: %w", err)
				}
				outputDir = filepath.Join(home, ".config", "steep", "certs")
			}

			// Default hosts
			if len(hosts) == 0 {
				hosts = []string{"localhost", "127.0.0.1"}
				// Try to add hostname
				if h, err := os.Hostname(); err == nil {
					hosts = append(hosts, h)
				}
			}

			cfg := certs.Config{
				OutputDir: outputDir,
				NodeName:  nodeName,
				Hosts:     hosts,
				ValidDays: validDays,
			}

			fmt.Printf("Generating mTLS certificates in %s...\n", outputDir)
			result, err := certs.Generate(cfg)
			if err != nil {
				return err
			}

			fmt.Println("\nGenerated files:")
			fmt.Printf("  CA:     %s, %s\n", result.CACert, result.CAKey)
			fmt.Printf("  Server: %s, %s\n", result.ServerCert, result.ServerKey)
			fmt.Printf("  Client: %s, %s\n", result.ClientCert, result.ClientKey)

			fmt.Println("\nAdd this to your config.yaml:")
			fmt.Println("─────────────────────────────────")
			fmt.Print(certs.ConfigSnippet(result))
			fmt.Println("─────────────────────────────────")

			fmt.Println("\nFor multi-node setup:")
			fmt.Println("  1. Copy ca.crt to all nodes")
			fmt.Println("  2. Run 'steep-repl init-tls' on each node with appropriate --hosts")
			fmt.Println("  3. Each node uses its own server.crt/key and the shared ca.crt")

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "output directory (default ~/.config/steep/certs)")
	cmd.Flags().StringVarP(&nodeName, "name", "n", "", "node name for certificate CN (default steep-repl)")
	cmd.Flags().StringSliceVar(&hosts, "hosts", nil, "hostnames and IPs for certificate SANs (default localhost,127.0.0.1)")
	cmd.Flags().IntVar(&validDays, "days", 365, "certificate validity in days")

	return cmd
}
