package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
)

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
