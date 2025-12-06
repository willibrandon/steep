package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
	replinit "github.com/willibrandon/steep/internal/repl/init"
)

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

				return runInitStartGRPC(targetNodeID, sourceNodeID, method, parallelWorkers, schemaSync, remoteAddr, caFile, insecure, sourceInfo)
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

	// Connection flags (required)
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "daemon gRPC address (host:port) - required")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("remote")

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

			return runInitPrepareGRPC(nodeID, slotName, remoteAddr, caFile, insecure)
		},
	}

	cmd.Flags().StringVar(&nodeID, "node", "", "node ID to prepare (required)")
	cmd.Flags().StringVar(&slotName, "slot", "", "replication slot name (default: steep_init_<node>)")
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "daemon gRPC address (host:port) - required")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("remote")

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

			return runInitCompleteGRPC(targetNodeID, sourceNodeID, sourceLSN, schemaSync, skipSchemaCheck,
				sourceHost, sourcePort, sourceDatabase, sourceUser, sourceRemote,
				remoteAddr, caFile, insecure)
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
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "daemon gRPC address (host:port) - required")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("source-host")
	_ = cmd.MarkFlagRequired("remote")

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

			return runInitCancelGRPC(nodeID, remoteAddr, caFile, insecure)
		},
	}

	cmd.Flags().StringVar(&nodeID, "node", "", "node ID to cancel initialization for (required)")
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "daemon gRPC address (host:port) - required")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("remote")

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

			return runInitProgressGRPC(nodeID, remoteAddr, caFile, insecure)
		},
	}

	cmd.Flags().StringVar(&nodeID, "node", "", "node ID to check progress for (required)")
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "daemon gRPC address (host:port) - required")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("remote")

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

			return runInitReinitGRPC(nodeID, full, tables, schema, remoteAddr, caFile, insecure)
		},
	}

	cmd.Flags().StringVar(&nodeID, "node", "", "node ID to reinitialize (required)")
	cmd.Flags().BoolVar(&full, "full", false, "full node reinitialization")
	cmd.Flags().StringSliceVar(&tables, "tables", nil, "specific tables to reinitialize (comma-separated, format: schema.table)")
	cmd.Flags().StringVar(&schema, "schema", "", "entire schema to reinitialize")
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "daemon gRPC address (host:port) - required")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("remote")

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
