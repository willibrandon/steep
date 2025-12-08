package main

import (
	"os"

	"github.com/spf13/cobra"
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
		Short: "Steep replication daemon",
		Long: `steep-repl is a background daemon that coordinates replication
across PostgreSQL servers. It manages node registration, schema synchronization,
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
		newTLSCmd(),
		newNodeCmd(),
		newSchemaCmd(),
		newAnalyzeOverlapCmd(),
		newMergeCmd(),
		newSnapshotCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		// Error already printed by cobra
		os.Exit(1)
	}
}
