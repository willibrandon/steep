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
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
	"github.com/willibrandon/steep/internal/repl/grpc/certs"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
	replinit "github.com/willibrandon/steep/internal/repl/init"
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
		newInitCmd(),
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

// newInitCmd creates the init command group for node initialization.
func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Node initialization commands",
		Long: `Initialize nodes for bidirectional replication.

Available subcommands:
  steep-repl init <target> --from <source>    Start automatic snapshot initialization
  steep-repl init prepare --node <node>       Prepare for manual initialization
  steep-repl init complete --node <target>    Complete manual initialization
  steep-repl init cancel --node <node>        Cancel in-progress initialization

Examples:
  # Automatic snapshot initialization (recommended for <100GB)
  steep-repl init node-b --from node-a

  # Manual initialization for large databases
  steep-repl init prepare --node node-a --slot init_slot_001
  # ... run pg_basebackup and restore on node-b ...
  steep-repl init complete --node node-b --source node-a --lsn 0/1A234B00`,
	}

	// Add subcommands
	cmd.AddCommand(
		newInitStartCmd(),
		newInitPrepareCmd(),
		newInitCompleteCmd(),
		newInitCancelCmd(),
		newInitProgressCmd(),
		newInitReinitCmd(),
	)

	return cmd
}

// newInitStartCmd creates the init start subcommand for automatic snapshot initialization.
func newInitStartCmd() *cobra.Command {
	var (
		sourceNodeID    string
		method          string
		parallelWorkers int
		schemaSync      string
		remoteAddr      string
		caFile          string
		insecure        bool
		// Source node connection info for auto-registration
		sourceHost     string
		sourcePort     int
		sourceDatabase string
		sourceUser     string
	)

	cmd := &cobra.Command{
		Use:   "start <target-node-id>",
		Short: "Start automatic snapshot initialization",
		Long: `Start automatic snapshot initialization using PostgreSQL logical replication
with copy_data=true. Recommended for databases under 100GB.

The target node will be initialized from the source node's data. Progress
can be monitored via the TUI or 'steep-repl status' command.

The --source-host flag is required to specify how the target can connect to
the source PostgreSQL for replication.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetNodeID := args[0]

			if sourceNodeID == "" {
				return fmt.Errorf("--from flag is required")
			}
			if sourceHost == "" {
				return fmt.Errorf("--source-host flag is required")
			}

			fmt.Printf("Starting initialization of %s from %s...\n", targetNodeID, sourceNodeID)
			fmt.Printf("  Method: %s\n", method)
			fmt.Printf("  Parallel workers: %d\n", parallelWorkers)
			fmt.Printf("  Schema sync: %s\n", schemaSync)
			fmt.Printf("  Source: %s:%d\n", sourceHost, sourcePort)
			fmt.Println()

			sourceInfo := &pb.SourceNodeInfo{
				Host:     sourceHost,
				Port:     int32(sourcePort),
				Database: sourceDatabase,
				User:     sourceUser,
			}

			// If remote address specified, use gRPC
			if remoteAddr != "" {
				return runInitStartGRPC(targetNodeID, sourceNodeID, method, parallelWorkers, schemaSync, remoteAddr, caFile, insecure, sourceInfo)
			}

			// Otherwise try IPC
			return runInitStartIPC(targetNodeID, sourceNodeID, method, parallelWorkers, schemaSync, sourceInfo)
		},
	}

	// Source node flags (required)
	cmd.Flags().StringVar(&sourceNodeID, "from", "", "source node ID to initialize from (required)")
	cmd.Flags().StringVar(&sourceHost, "source-host", "", "source PostgreSQL host (required)")
	cmd.Flags().IntVar(&sourcePort, "source-port", 5432, "source PostgreSQL port")
	cmd.Flags().StringVar(&sourceDatabase, "source-database", "", "source PostgreSQL database")
	cmd.Flags().StringVar(&sourceUser, "source-user", "", "source PostgreSQL user")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("source-host")

	// Init options
	cmd.Flags().StringVar(&method, "method", "snapshot", "initialization method: snapshot, manual, two-phase, direct")
	cmd.Flags().IntVar(&parallelWorkers, "parallel", 4, "number of parallel workers (1-16)")
	cmd.Flags().StringVar(&schemaSync, "schema-sync", "strict", "schema sync mode: strict, auto, manual")

	// Connection flags
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "remote daemon address (host:port) for gRPC")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")

	return cmd
}

// runInitStartGRPC starts initialization via gRPC.
func runInitStartGRPC(targetNodeID, sourceNodeID, method string, parallelWorkers int, schemaSync, remoteAddr, caFile string, insecure bool, sourceInfo *pb.SourceNodeInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientCfg := replgrpc.ClientConfig{
		Address: remoteAddr,
		Timeout: 30 * time.Second,
	}
	if !insecure {
		clientCfg.CAFile = caFile
	}

	client, err := replgrpc.NewClient(ctx, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	// Convert method string to proto enum
	var pbMethod pb.InitMethod
	switch method {
	case "snapshot":
		pbMethod = pb.InitMethod_INIT_METHOD_SNAPSHOT
	case "manual":
		pbMethod = pb.InitMethod_INIT_METHOD_MANUAL
	case "two-phase":
		pbMethod = pb.InitMethod_INIT_METHOD_TWO_PHASE
	case "direct":
		pbMethod = pb.InitMethod_INIT_METHOD_DIRECT
	default:
		pbMethod = pb.InitMethod_INIT_METHOD_SNAPSHOT
	}

	// Convert schema sync to proto enum
	var pbSchemaSync pb.SchemaSyncMode
	switch schemaSync {
	case "strict":
		pbSchemaSync = pb.SchemaSyncMode_SCHEMA_SYNC_STRICT
	case "auto":
		pbSchemaSync = pb.SchemaSyncMode_SCHEMA_SYNC_AUTO
	case "manual":
		pbSchemaSync = pb.SchemaSyncMode_SCHEMA_SYNC_MANUAL
	default:
		pbSchemaSync = pb.SchemaSyncMode_SCHEMA_SYNC_STRICT
	}

	resp, err := client.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId:   targetNodeID,
		SourceNodeId:   sourceNodeID,
		Method:         pbMethod,
		SourceNodeInfo: sourceInfo,
		Options: &pb.InitOptions{
			ParallelWorkers: int32(parallelWorkers),
			SchemaSyncMode:  pbSchemaSync,
		},
	})
	if err != nil {
		return fmt.Errorf("gRPC call failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("initialization failed: %s", resp.Error)
	}

	fmt.Println("Initialization started successfully")
	fmt.Printf("Monitor progress with: steep-repl init progress --node %s --remote %s --insecure\n", targetNodeID, remoteAddr)
	return nil
}

// runInitStartIPC starts initialization via IPC (local daemon).
func runInitStartIPC(targetNodeID, sourceNodeID, method string, parallelWorkers int, schemaSync string, sourceInfo *pb.SourceNodeInfo) error {
	// For local daemon, we need to implement IPC call
	// For now, show error message
	fmt.Println("Error: IPC not implemented for init start")
	fmt.Println("Use --remote flag to connect to a running daemon via gRPC:")
	fmt.Printf("  steep-repl init start %s --from %s --source-host <host> --remote localhost:15461 --insecure\n", targetNodeID, sourceNodeID)
	return nil
}

// newInitPrepareCmd creates the init prepare subcommand for manual initialization.
func newInitPrepareCmd() *cobra.Command {
	var (
		nodeID     string
		slotName   string
		remoteAddr string
		caFile     string
		insecure   bool
	)

	cmd := &cobra.Command{
		Use:   "prepare",
		Short: "Prepare for manual initialization",
		Long: `Prepare for manual initialization by creating a replication slot and
recording the LSN. This is step 1 of the manual initialization workflow.

This command should be run on the SOURCE node.

After running this command:
1. Use pg_basebackup or pg_dump to create a backup from the source
2. Restore the backup on the target node
3. Run 'steep-repl init complete' on the TARGET node to finish initialization

Example:
  steep-repl init prepare --node source-node --slot init_slot_001 --remote localhost:15460 --insecure`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeID == "" {
				return fmt.Errorf("--node flag is required")
			}
			if slotName == "" {
				slotName = fmt.Sprintf("steep_init_%s", replinit.SanitizeSlotName(nodeID))
			}

			fmt.Printf("Preparing initialization for node %s...\n", nodeID)
			fmt.Printf("  Slot name: %s\n", slotName)
			fmt.Println()

			// If remote address specified, use gRPC
			if remoteAddr != "" {
				return runInitPrepareGRPC(nodeID, slotName, remoteAddr, caFile, insecure)
			}

			// Otherwise show error
			fmt.Println("Error: IPC not implemented for init prepare")
			fmt.Println("Use --remote flag to connect to a running daemon via gRPC:")
			fmt.Printf("  steep-repl init prepare --node %s --slot %s --remote localhost:5433 --insecure\n", nodeID, slotName)
			return nil
		},
	}

	cmd.Flags().StringVar(&nodeID, "node", "", "node ID to prepare (required)")
	cmd.Flags().StringVar(&slotName, "slot", "", "replication slot name (default: steep_init_<node>)")
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "remote daemon address (host:port) for gRPC")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("node")

	return cmd
}

// runInitPrepareGRPC prepares for initialization via gRPC.
func runInitPrepareGRPC(nodeID, slotName, remoteAddr, caFile string, insecure bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientCfg := replgrpc.ClientConfig{
		Address: remoteAddr,
		Timeout: 30 * time.Second,
	}
	if !insecure {
		clientCfg.CAFile = caFile
	}

	client, err := replgrpc.NewClient(ctx, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	resp, err := client.PrepareInit(ctx, &pb.PrepareInitRequest{
		NodeId:   nodeID,
		SlotName: slotName,
	})
	if err != nil {
		return fmt.Errorf("gRPC call failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("prepare failed: %s", resp.Error)
	}

	fmt.Println("Prepare completed successfully!")
	fmt.Printf("  Slot:       %s\n", resp.SlotName)
	fmt.Printf("  LSN:        %s\n", resp.Lsn)
	if resp.CreatedAt != nil {
		fmt.Printf("  Created at: %s\n", resp.CreatedAt.AsTime().Format(time.RFC3339))
	}
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Create a backup using the slot:")
	fmt.Printf("     pg_basebackup -h <source-host> -S %s -X stream -D /path/to/backup\n", resp.SlotName)
	fmt.Println("     # OR")
	fmt.Println("     pg_dump -h <source-host> -Fd -f /path/to/backup <database>")
	fmt.Println()
	fmt.Println("  2. Restore the backup on the target node")
	fmt.Println()
	fmt.Println("  3. Complete initialization on the target:")
	fmt.Printf("     steep-repl init complete --node <target> --source %s --lsn %s \\\n", nodeID, resp.Lsn)
	fmt.Println("       --source-host <source-host> --source-port 5432 --remote <target-daemon> --insecure")

	return nil
}

// newInitCompleteCmd creates the init complete subcommand for manual initialization.
func newInitCompleteCmd() *cobra.Command {
	var (
		targetNodeID    string
		sourceNodeID    string
		sourceLSN       string
		schemaSync      string
		skipSchemaCheck bool
		remoteAddr      string
		caFile          string
		insecure        bool
		// Source connection info for subscription
		sourceHost     string
		sourcePort     int
		sourceDatabase string
		sourceUser     string
		sourceRemote   string // gRPC address of source daemon for schema verification
	)

	cmd := &cobra.Command{
		Use:   "complete",
		Short: "Complete manual initialization",
		Long: `Complete manual initialization after restoring a backup.
This is step 2 of the manual initialization workflow.

This command should be run on the TARGET node.

Before running this command:
1. Run 'steep-repl init prepare' on the source node
2. Use pg_basebackup or pg_dump to create a backup
3. Restore the backup on the target node

This command will verify the schema matches and create the subscription
to start replication from the recorded LSN.

Example:
  steep-repl init complete --node target-node --source source-node --lsn 0/1A234B00 \
    --source-host pg-source --source-port 5432 --source-database testdb --source-user test \
    --remote localhost:15461 --insecure`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if targetNodeID == "" {
				return fmt.Errorf("--node flag is required")
			}
			if sourceNodeID == "" {
				return fmt.Errorf("--source flag is required")
			}
			if sourceHost == "" {
				return fmt.Errorf("--source-host flag is required")
			}

			fmt.Printf("Completing initialization of %s from %s...\n", targetNodeID, sourceNodeID)
			if sourceLSN != "" {
				fmt.Printf("  Source LSN: %s\n", sourceLSN)
			} else {
				fmt.Printf("  Source LSN: (auto-detect from init_slots)\n")
			}
			fmt.Printf("  Source: %s:%d\n", sourceHost, sourcePort)
			fmt.Printf("  Schema sync: %s\n", schemaSync)
			fmt.Printf("  Skip schema check: %v\n", skipSchemaCheck)
			fmt.Println()

			// If remote address specified, use gRPC
			if remoteAddr != "" {
				return runInitCompleteGRPC(targetNodeID, sourceNodeID, sourceLSN, schemaSync, skipSchemaCheck,
					sourceHost, sourcePort, sourceDatabase, sourceUser, sourceRemote,
					remoteAddr, caFile, insecure)
			}

			// Otherwise show error
			fmt.Println("Error: IPC not implemented for init complete")
			fmt.Println("Use --remote flag to connect to a running daemon via gRPC:")
			fmt.Printf("  steep-repl init complete --node %s --source %s --source-host <host> --remote localhost:5433 --insecure\n", targetNodeID, sourceNodeID)
			return nil
		},
	}

	cmd.Flags().StringVar(&targetNodeID, "node", "", "target node ID (required)")
	cmd.Flags().StringVar(&sourceNodeID, "source", "", "source node ID (required)")
	cmd.Flags().StringVar(&sourceLSN, "lsn", "", "source LSN from prepare step (auto-detected if not specified)")
	cmd.Flags().StringVar(&schemaSync, "schema-sync", "strict", "schema sync mode: strict, auto, manual")
	cmd.Flags().BoolVar(&skipSchemaCheck, "skip-schema-check", false, "skip schema verification (dangerous)")
	cmd.Flags().StringVar(&sourceHost, "source-host", "", "source PostgreSQL host (required)")
	cmd.Flags().IntVar(&sourcePort, "source-port", 5432, "source PostgreSQL port")
	cmd.Flags().StringVar(&sourceDatabase, "source-database", "", "source PostgreSQL database")
	cmd.Flags().StringVar(&sourceUser, "source-user", "", "source PostgreSQL user")
	cmd.Flags().StringVar(&sourceRemote, "source-remote", "", "source daemon gRPC address for schema verification (e.g., localhost:15461)")
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "remote daemon address (host:port) for gRPC")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("source-host")

	return cmd
}

// runInitCompleteGRPC completes initialization via gRPC.
func runInitCompleteGRPC(targetNodeID, sourceNodeID, sourceLSN, schemaSync string, skipSchemaCheck bool,
	sourceHost string, sourcePort int, sourceDatabase, sourceUser, sourceRemote string,
	remoteAddr, caFile string, insecure bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	clientCfg := replgrpc.ClientConfig{
		Address: remoteAddr,
		Timeout: 2 * time.Minute,
	}
	if !insecure {
		clientCfg.CAFile = caFile
	}

	client, err := replgrpc.NewClient(ctx, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	// Convert schema sync to proto enum
	var pbSchemaSync pb.SchemaSyncMode
	switch schemaSync {
	case "strict":
		pbSchemaSync = pb.SchemaSyncMode_SCHEMA_SYNC_STRICT
	case "auto":
		pbSchemaSync = pb.SchemaSyncMode_SCHEMA_SYNC_AUTO
	case "manual":
		pbSchemaSync = pb.SchemaSyncMode_SCHEMA_SYNC_MANUAL
	default:
		pbSchemaSync = pb.SchemaSyncMode_SCHEMA_SYNC_STRICT
	}

	resp, err := client.CompleteInit(ctx, &pb.CompleteInitRequest{
		TargetNodeId:   targetNodeID,
		SourceNodeId:   sourceNodeID,
		SourceLsn:      sourceLSN,
		SchemaSyncMode: pbSchemaSync,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     sourceHost,
			Port:     int32(sourcePort),
			Database: sourceDatabase,
			User:     sourceUser,
		},
		SkipSchemaCheck: skipSchemaCheck,
		SourceRemote:    sourceRemote,
	})
	if err != nil {
		return fmt.Errorf("gRPC call failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("complete failed: %s", resp.Error)
	}

	fmt.Println("Complete initialization started successfully!")
	fmt.Printf("  State: %s\n", resp.State.String())
	fmt.Println()
	fmt.Println("The node is now catching up with WAL changes from the source.")
	fmt.Println("Monitor progress with:")
	fmt.Printf("  steep-repl init progress --node %s --remote %s --insecure\n", targetNodeID, remoteAddr)

	return nil
}

// newInitCancelCmd creates the init cancel subcommand.
func newInitCancelCmd() *cobra.Command {
	var (
		nodeID     string
		remoteAddr string
		caFile     string
		insecure   bool
	)

	cmd := &cobra.Command{
		Use:   "cancel",
		Short: "Cancel in-progress initialization",
		Long: `Cancel an in-progress initialization. This will:
- Drop any partial subscriptions
- Clean up partial data on the target node
- Reset the node state to UNINITIALIZED

Use this if initialization is taking too long or has stalled.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeID == "" {
				return fmt.Errorf("--node flag is required")
			}

			fmt.Printf("Cancelling initialization for node %s...\n", nodeID)

			// If remote address specified, use gRPC
			if remoteAddr != "" {
				return runInitCancelGRPC(nodeID, remoteAddr, caFile, insecure)
			}

			// Otherwise show error
			fmt.Println("Error: IPC not implemented for init cancel")
			fmt.Println("Use --remote flag to connect to a running daemon via gRPC:")
			fmt.Printf("  steep-repl init cancel --node %s --remote localhost:5433 --insecure\n", nodeID)
			return nil
		},
	}

	cmd.Flags().StringVar(&nodeID, "node", "", "node ID to cancel initialization for (required)")
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "remote daemon address (host:port) for gRPC")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("node")

	return cmd
}

// runInitCancelGRPC cancels initialization via gRPC.
func runInitCancelGRPC(nodeID, remoteAddr, caFile string, insecure bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientCfg := replgrpc.ClientConfig{
		Address: remoteAddr,
		Timeout: 30 * time.Second,
	}
	if !insecure {
		clientCfg.CAFile = caFile
	}

	client, err := replgrpc.NewClient(ctx, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	resp, err := client.CancelInit(ctx, &pb.CancelInitRequest{
		NodeId: nodeID,
	})
	if err != nil {
		return fmt.Errorf("gRPC call failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("cancellation failed: %s", resp.Error)
	}

	fmt.Println("Initialization cancelled successfully")
	return nil
}

// newInitProgressCmd creates the init progress subcommand to check initialization progress.
func newInitProgressCmd() *cobra.Command {
	var (
		nodeID     string
		remoteAddr string
		caFile     string
		insecure   bool
	)

	cmd := &cobra.Command{
		Use:   "progress",
		Short: "Check initialization progress",
		Long: `Check the current initialization progress for a node.

Shows the current state, phase, progress percentage, and other details
about an ongoing or completed initialization.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeID == "" {
				return fmt.Errorf("--node flag is required")
			}

			// If remote address specified, use gRPC
			if remoteAddr != "" {
				return runInitProgressGRPC(nodeID, remoteAddr, caFile, insecure)
			}

			// Otherwise show error
			fmt.Println("Error: IPC not implemented for init progress")
			fmt.Println("Use --remote flag to connect to a running daemon via gRPC:")
			fmt.Printf("  steep-repl init progress --node %s --remote localhost:5433 --insecure\n", nodeID)
			return nil
		},
	}

	cmd.Flags().StringVar(&nodeID, "node", "", "node ID to check progress for (required)")
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "remote daemon address (host:port) for gRPC")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("node")

	return cmd
}

// runInitProgressGRPC gets progress via gRPC.
func runInitProgressGRPC(nodeID, remoteAddr, caFile string, insecure bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientCfg := replgrpc.ClientConfig{
		Address: remoteAddr,
		Timeout: 30 * time.Second,
	}
	if !insecure {
		clientCfg.CAFile = caFile
	}

	client, err := replgrpc.NewClient(ctx, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	resp, err := client.GetProgress(ctx, &pb.GetProgressRequest{
		NodeId: nodeID,
	})
	if err != nil {
		return fmt.Errorf("gRPC call failed: %w", err)
	}

	if !resp.HasProgress {
		fmt.Printf("No initialization in progress for node %s\n", nodeID)
		return nil
	}

	p := resp.Progress
	fmt.Printf("Node:     %s\n", p.NodeId)
	fmt.Printf("State:    %s\n", p.State.String())
	fmt.Printf("Phase:    %s\n", p.Phase)
	fmt.Printf("Progress: %.1f%%\n", p.OverallPercent)

	if p.TablesTotal > 0 {
		fmt.Printf("Tables:   %d/%d completed\n", p.TablesCompleted, p.TablesTotal)
	}
	if p.CurrentTable != "" {
		fmt.Printf("Current:  %s (%.1f%%)\n", p.CurrentTable, p.CurrentTablePercent)
	}
	if p.RowsCopied > 0 {
		fmt.Printf("Rows:     %d copied\n", p.RowsCopied)
	}
	if p.BytesCopied > 0 {
		fmt.Printf("Bytes:    %d copied\n", p.BytesCopied)
	}
	if p.ThroughputRowsSec > 0 {
		fmt.Printf("Speed:    %.0f rows/sec\n", p.ThroughputRowsSec)
	}
	if p.EtaSeconds > 0 {
		fmt.Printf("ETA:      %d seconds\n", p.EtaSeconds)
	}
	if p.ErrorMessage != "" {
		fmt.Printf("Error:    %s\n", p.ErrorMessage)
	}

	return nil
}

// newInitReinitCmd creates the init reinit subcommand to reinitialize a node.
func newInitReinitCmd() *cobra.Command {
	var (
		nodeID     string
		full       bool
		tables     []string
		schema     string
		remoteAddr string
		caFile     string
		insecure   bool
	)

	cmd := &cobra.Command{
		Use:   "reinit",
		Short: "Reinitialize a node",
		Long: `Reinitialize a node that is already synchronized or has diverged.

This resets the node state and allows it to be initialized again.
You can reinitialize the full node, specific tables, or an entire schema.

Examples:
  # Full reinit
  steep-repl init reinit --node target-node --full --remote localhost:15461 --insecure

  # Reinit specific tables
  steep-repl init reinit --node target-node --tables public.users,public.orders --remote localhost:15461 --insecure

  # Reinit entire schema
  steep-repl init reinit --node target-node --schema public --remote localhost:15461 --insecure`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeID == "" {
				return fmt.Errorf("--node flag is required")
			}

			// Validate scope - exactly one must be specified
			scopeCount := 0
			if full {
				scopeCount++
			}
			if len(tables) > 0 {
				scopeCount++
			}
			if schema != "" {
				scopeCount++
			}
			if scopeCount == 0 {
				return fmt.Errorf("one of --full, --tables, or --schema is required")
			}
			if scopeCount > 1 {
				return fmt.Errorf("only one of --full, --tables, or --schema can be specified")
			}

			fmt.Printf("Reinitializing node %s...\n", nodeID)

			// If remote address specified, use gRPC
			if remoteAddr != "" {
				return runInitReinitGRPC(nodeID, full, tables, schema, remoteAddr, caFile, insecure)
			}

			// Otherwise show error
			fmt.Println("Error: IPC not implemented for init reinit")
			fmt.Println("Use --remote flag to connect to a running daemon via gRPC:")
			fmt.Printf("  steep-repl init reinit --node %s --full --remote localhost:5433 --insecure\n", nodeID)
			return nil
		},
	}

	cmd.Flags().StringVar(&nodeID, "node", "", "node ID to reinitialize (required)")
	cmd.Flags().BoolVar(&full, "full", false, "full node reinitialization")
	cmd.Flags().StringSliceVar(&tables, "tables", nil, "specific tables to reinitialize (comma-separated, format: schema.table)")
	cmd.Flags().StringVar(&schema, "schema", "", "entire schema to reinitialize")
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "remote daemon address (host:port) for gRPC")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("node")

	return cmd
}

// runInitReinitGRPC starts reinitialization via gRPC.
func runInitReinitGRPC(nodeID string, full bool, tables []string, schema, remoteAddr, caFile string, insecure bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientCfg := replgrpc.ClientConfig{
		Address: remoteAddr,
		Timeout: 30 * time.Second,
	}
	if !insecure {
		clientCfg.CAFile = caFile
	}

	client, err := replgrpc.NewClient(ctx, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	req := &pb.StartReinitRequest{
		NodeId: nodeID,
		Scope:  &pb.ReinitScope{},
	}

	if full {
		req.Scope.Scope = &pb.ReinitScope_Full{Full: true}
	} else if len(tables) > 0 {
		req.Scope.Scope = &pb.ReinitScope_Tables{Tables: &pb.TableList{Tables: tables}}
	} else if schema != "" {
		req.Scope.Scope = &pb.ReinitScope_Schema{Schema: schema}
	}

	resp, err := client.StartReinit(ctx, req)
	if err != nil {
		return fmt.Errorf("gRPC call failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("reinitialization failed: %s", resp.Error)
	}

	fmt.Println("Reinitialization started successfully")
	fmt.Printf("New state: %s\n", resp.State.String())
	if resp.TablesAffected > 0 {
		fmt.Printf("Tables affected: %d\n", resp.TablesAffected)
	}

	// Only suggest init start for full reinit (state = UNINITIALIZED)
	// Partial reinit handles re-copying automatically
	if resp.State == pb.InitState_INIT_STATE_UNINITIALIZED {
		fmt.Printf("Now run 'init start' to begin initialization again.\n")
	}
	return nil
}
