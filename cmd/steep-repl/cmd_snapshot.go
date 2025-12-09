package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/willibrandon/steep/cmd/steep-repl/direct"
	directpkg "github.com/willibrandon/steep/internal/repl/direct"
	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
)

// newSnapshotCmd creates the snapshot command group.
func newSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Two-phase snapshot commands",
		Long: `Generate and apply two-phase snapshots for node initialization.

Two-phase snapshots allow generating a consistent snapshot to files,
then applying it to target nodes. This is useful for:
- Offline transfer of large databases
- Initializing multiple nodes from the same snapshot
- Network transfers where direct PostgreSQL connection isn't available

Available subcommands:
  steep-repl snapshot generate <source-node>    Generate a snapshot to disk
  steep-repl snapshot apply <target-node>       Apply a snapshot from disk

Examples:
  # Generate a snapshot from node-a
  steep-repl snapshot generate node-a --output /snapshots/node-a

  # Apply snapshot to node-b
  steep-repl snapshot apply node-b --input /snapshots/node-a`,
	}

	cmd.AddCommand(
		newSnapshotGenerateCmd(),
		newSnapshotApplyCmd(),
	)

	return cmd
}

// newSnapshotGenerateCmd creates the snapshot generate subcommand.
// Implements T085: Add snapshot generate CLI command.
// T024: Add --direct and -c flags for direct PostgreSQL execution.
func newSnapshotGenerateCmd() *cobra.Command {
	var (
		outputPath      string
		compression     string
		parallelWorkers int
		remoteAddr      string
		caFile          string
		insecure        bool
		progress        bool
		// T024: Direct mode flags
		directMode bool
		connString string
	)

	cmd := &cobra.Command{
		Use:   "generate <source-node>",
		Short: "Generate a two-phase snapshot to disk",
		Long: `Generate a consistent snapshot of a node's data to disk.

The snapshot includes:
- All user tables exported as CSV files
- Sequence values at the snapshot point
- A manifest.json with checksums and LSN
- Optional compression (gzip, lz4, zstd)

The snapshot captures a consistent point-in-time using a logical replication slot.

Connection modes:
  --direct    Connect directly to PostgreSQL using the steep_repl extension
  --remote    Connect to the daemon via gRPC (legacy mode)

If neither is specified, auto-detection tries direct mode first, then daemon.

Examples:
  # Generate snapshot using direct mode (preferred)
  steep-repl snapshot generate node-a --output /snapshots/node-a --direct -c "postgres://user@host/db"

  # Generate snapshot using daemon mode (legacy)
  steep-repl snapshot generate node-a --output /snapshots/node-a --remote localhost:9090 --insecure

  # Auto-detect mode (tries direct first)
  steep-repl snapshot generate node-a --output /snapshots/node-a

  # Generate with gzip compression
  steep-repl snapshot generate node-a --output /snapshots/node-a --compression gzip --direct

  # Generate with 8 parallel workers
  steep-repl snapshot generate node-a --output /snapshots/node-a --parallel 8 --direct`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourceNodeID := args[0]
			if outputPath == "" {
				return fmt.Errorf("--output flag is required")
			}

			// Detect execution mode
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			flags := direct.Flags{
				Direct:     directMode,
				Remote:     remoteAddr,
				ConnString: connString,
			}

			detector := direct.NewDetector(nil) // TODO: Load config if available
			result, err := detector.DetectForOperation(ctx, flags, "snapshot_generate")
			if err != nil {
				return fmt.Errorf("failed to detect execution mode: %w", err)
			}

			if result.Mode == direct.ModeUnavailable {
				return fmt.Errorf("no execution mode available: %s", result.Reason)
			}

			fmt.Printf("Generating snapshot from %s to %s\n", sourceNodeID, outputPath)
			fmt.Printf("  Mode: %s\n", result.Mode)
			fmt.Printf("  Compression: %s\n", compression)
			fmt.Printf("  Parallel workers: %d\n", parallelWorkers)
			if result.Warning != "" {
				fmt.Printf("  Warning: %s\n", result.Warning)
			}
			fmt.Println()

			switch result.Mode {
			case direct.ModeDirect:
				return runSnapshotGenerateDirect(ctx, sourceNodeID, outputPath, compression, parallelWorkers, connString, progress)
			case direct.ModeDaemon:
				return runSnapshotGenerate(sourceNodeID, outputPath, compression, parallelWorkers, remoteAddr, caFile, insecure, progress)
			default:
				return fmt.Errorf("unexpected mode: %s", result.Mode)
			}
		},
	}

	// Required flags
	cmd.Flags().StringVar(&outputPath, "output", "", "output directory path (required)")
	_ = cmd.MarkFlagRequired("output")

	// Options
	cmd.Flags().StringVar(&compression, "compression", "none", "compression type: none, gzip, lz4, zstd")
	cmd.Flags().IntVar(&parallelWorkers, "parallel", 4, "number of parallel workers (1-16)")
	cmd.Flags().BoolVar(&progress, "progress", true, "show progress updates")

	// T024: Direct mode flags
	cmd.Flags().BoolVar(&directMode, "direct", false, "use direct PostgreSQL connection via extension")
	cmd.Flags().StringVarP(&connString, "connection", "c", "", "PostgreSQL connection string for direct mode")

	// Connection flags (daemon mode)
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "daemon gRPC address (host:port)")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")

	// Mark flags as mutually exclusive
	cmd.MarkFlagsMutuallyExclusive("direct", "remote")

	return cmd
}

// runSnapshotGenerate executes the snapshot generate via gRPC (daemon mode).
func runSnapshotGenerate(sourceNodeID, outputPath, compression string, parallelWorkers int, remoteAddr, caFile string, insecure, showProgress bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour) // Long timeout for large databases
	defer cancel()

	clientCfg := replgrpc.ClientConfig{
		Address: remoteAddr,
		Timeout: 24 * time.Hour,
	}
	if !insecure {
		clientCfg.CAFile = caFile
	}

	client, err := replgrpc.NewClient(ctx, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	// Start the streaming RPC
	stream, err := client.GenerateSnapshot(ctx, &pb.GenerateSnapshotRequest{
		SourceNodeId:    sourceNodeID,
		OutputPath:      outputPath,
		Compression:     compression,
		ParallelWorkers: int32(parallelWorkers),
	})
	if err != nil {
		return fmt.Errorf("gRPC call failed: %w", err)
	}

	// Receive progress updates
	var lastProgress *pb.SnapshotProgress
	for {
		progress, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("stream error: %w", err)
		}

		lastProgress = progress

		if showProgress {
			if progress.Error != "" {
				fmt.Printf("\rError: %s\n", progress.Error)
			} else if progress.Complete {
				fmt.Printf("\rComplete! Snapshot: %s, LSN: %s\n", progress.SnapshotId, progress.Lsn)
			} else {
				fmt.Printf("\r[%s] %.1f%% | %s | %d bytes",
					progress.Phase,
					progress.OverallPercent,
					progress.CurrentTable,
					progress.BytesProcessed)
			}
		}
	}

	if lastProgress == nil {
		return fmt.Errorf("no progress updates received")
	}

	if lastProgress.Error != "" {
		return fmt.Errorf("snapshot generation failed: %s", lastProgress.Error)
	}

	fmt.Printf("\nSnapshot generated successfully!\n")
	fmt.Printf("  Snapshot ID: %s\n", lastProgress.SnapshotId)
	fmt.Printf("  LSN: %s\n", lastProgress.Lsn)
	fmt.Printf("  Output: %s\n", outputPath)

	return nil
}

// runSnapshotGenerateDirect executes the snapshot generate via direct PostgreSQL connection.
// T024: Direct mode implementation using the steep_repl extension.
func runSnapshotGenerateDirect(ctx context.Context, sourceNodeID, outputPath, compression string, parallelWorkers int, connString string, showProgress bool) error {
	// Create executor with connection string
	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString:   connString,
		ShowProgress: showProgress,
		Timeout:      24 * time.Hour,
	})
	if err != nil {
		return fmt.Errorf("failed to create direct executor: %w", err)
	}
	defer executor.Close()

	// Start the snapshot
	snapshotID, err := executor.GenerateSnapshot(ctx, direct.SnapshotGenerateOptions{
		OutputPath:  outputPath,
		Compression: compression,
		Parallel:    parallelWorkers,
	})
	if err != nil {
		return fmt.Errorf("failed to start snapshot: %w", err)
	}

	fmt.Printf("Started snapshot: %s (source: %s)\n", snapshotID, sourceNodeID)

	if showProgress {
		// Wait for completion with progress updates
		finalState, err := executor.WaitForSnapshot(ctx, snapshotID, func(state *directpkg.ProgressState) {
			if state.Error != "" {
				fmt.Printf("\rError: %s\n", state.Error)
			} else if state.IsComplete {
				fmt.Printf("\rComplete! Snapshot: %s\n", state.OperationID)
			} else {
				fmt.Printf("\r[%s] %.1f%% | %s | %d/%d tables | %d bytes",
					state.Phase,
					state.Percent,
					state.CurrentTable,
					state.TablesCompleted,
					state.TablesTotal,
					state.BytesProcessed)
			}
		})
		if err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
		}

		if finalState.IsFailed {
			return fmt.Errorf("snapshot generation failed: %s", finalState.Error)
		}
	}

	fmt.Printf("\nSnapshot generated successfully!\n")
	fmt.Printf("  Snapshot ID: %s\n", snapshotID)
	fmt.Printf("  Output: %s\n", outputPath)

	return nil
}

// newSnapshotApplyCmd creates the snapshot apply subcommand.
// Implements T086: Add snapshot apply CLI command.
// T024: Add --direct and -c flags for direct PostgreSQL execution.
func newSnapshotApplyCmd() *cobra.Command {
	var (
		inputPath       string
		sourceNodeID    string
		parallelWorkers int
		verifyChecksums bool
		remoteAddr      string
		caFile          string
		insecure        bool
		progress        bool
		// T024: Direct mode flags
		directMode bool
		connString string
	)

	cmd := &cobra.Command{
		Use:   "apply <target-node>",
		Short: "Apply a two-phase snapshot from disk",
		Long: `Apply a snapshot to a target node from disk.

The apply process:
1. Verifies checksums of all data files (optional)
2. Truncates and imports each table using COPY
3. Restores sequence values
4. Optionally creates a subscription to the source node

Connection modes:
  --direct    Connect directly to PostgreSQL using the steep_repl extension
  --remote    Connect to the daemon via gRPC (legacy mode)

If neither is specified, auto-detection tries direct mode first, then daemon.

Examples:
  # Apply snapshot using direct mode (preferred)
  steep-repl snapshot apply node-b --input /snapshots/node-a --direct -c "postgres://user@host/db"

  # Apply snapshot using daemon mode (legacy)
  steep-repl snapshot apply node-b --input /snapshots/node-a --remote localhost:9091 --insecure

  # Apply without checksum verification (faster but less safe)
  steep-repl snapshot apply node-b --input /snapshots/node-a --verify=false --direct

  # Apply with source node ID for subscription setup
  steep-repl snapshot apply node-b --input /snapshots/node-a --source node-a --direct`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetNodeID := args[0]
			if inputPath == "" {
				return fmt.Errorf("--input flag is required")
			}

			// Verify input path exists
			if _, err := os.Stat(inputPath); os.IsNotExist(err) {
				return fmt.Errorf("input path does not exist: %s", inputPath)
			}

			// Detect execution mode
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			flags := direct.Flags{
				Direct:     directMode,
				Remote:     remoteAddr,
				ConnString: connString,
			}

			detector := direct.NewDetector(nil) // TODO: Load config if available
			result, err := detector.DetectForOperation(ctx, flags, "snapshot_apply")
			if err != nil {
				return fmt.Errorf("failed to detect execution mode: %w", err)
			}

			if result.Mode == direct.ModeUnavailable {
				return fmt.Errorf("no execution mode available: %s", result.Reason)
			}

			fmt.Printf("Applying snapshot to %s from %s\n", targetNodeID, inputPath)
			fmt.Printf("  Mode: %s\n", result.Mode)
			fmt.Printf("  Verify checksums: %v\n", verifyChecksums)
			fmt.Printf("  Parallel workers: %d\n", parallelWorkers)
			if sourceNodeID != "" {
				fmt.Printf("  Source node: %s (for subscription)\n", sourceNodeID)
			}
			if result.Warning != "" {
				fmt.Printf("  Warning: %s\n", result.Warning)
			}
			fmt.Println()

			switch result.Mode {
			case direct.ModeDirect:
				return runSnapshotApplyDirect(ctx, targetNodeID, inputPath, sourceNodeID, parallelWorkers, verifyChecksums, connString, progress)
			case direct.ModeDaemon:
				return runSnapshotApply(targetNodeID, inputPath, sourceNodeID, parallelWorkers, verifyChecksums, remoteAddr, caFile, insecure, progress)
			default:
				return fmt.Errorf("unexpected mode: %s", result.Mode)
			}
		},
	}

	// Required flags
	cmd.Flags().StringVar(&inputPath, "input", "", "input directory path containing snapshot (required)")
	_ = cmd.MarkFlagRequired("input")

	// Options
	cmd.Flags().StringVar(&sourceNodeID, "source", "", "source node ID (for subscription setup)")
	cmd.Flags().IntVar(&parallelWorkers, "parallel", 4, "number of parallel workers (1-16)")
	cmd.Flags().BoolVar(&verifyChecksums, "verify", true, "verify checksums before import")
	cmd.Flags().BoolVar(&progress, "progress", true, "show progress updates")

	// T024: Direct mode flags
	cmd.Flags().BoolVar(&directMode, "direct", false, "use direct PostgreSQL connection via extension")
	cmd.Flags().StringVarP(&connString, "connection", "c", "", "PostgreSQL connection string for direct mode")

	// Connection flags (daemon mode)
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "daemon gRPC address (host:port)")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")

	// Mark flags as mutually exclusive
	cmd.MarkFlagsMutuallyExclusive("direct", "remote")

	return cmd
}

// runSnapshotApply executes the snapshot apply via gRPC.
func runSnapshotApply(targetNodeID, inputPath, sourceNodeID string, parallelWorkers int, verifyChecksums bool, remoteAddr, caFile string, insecure, showProgress bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour) // Long timeout for large databases
	defer cancel()

	clientCfg := replgrpc.ClientConfig{
		Address: remoteAddr,
		Timeout: 24 * time.Hour,
	}
	if !insecure {
		clientCfg.CAFile = caFile
	}

	client, err := replgrpc.NewClient(ctx, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	// Start the streaming RPC
	stream, err := client.ApplySnapshot(ctx, &pb.ApplySnapshotRequest{
		TargetNodeId:    targetNodeID,
		InputPath:       inputPath,
		SourceNodeId:    sourceNodeID,
		ParallelWorkers: int32(parallelWorkers),
		VerifyChecksums: verifyChecksums,
	})
	if err != nil {
		return fmt.Errorf("gRPC call failed: %w", err)
	}

	// Receive progress updates
	var lastProgress *pb.SnapshotProgress
	for {
		progress, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("stream error: %w", err)
		}

		lastProgress = progress

		if showProgress {
			if progress.Error != "" {
				fmt.Printf("\rError: %s\n", progress.Error)
			} else if progress.Complete {
				fmt.Printf("\rComplete! Snapshot: %s applied successfully\n", progress.SnapshotId)
			} else {
				fmt.Printf("\r[%s] %.1f%% | %s | %d bytes",
					progress.Phase,
					progress.OverallPercent,
					progress.CurrentTable,
					progress.BytesProcessed)
			}
		}
	}

	if lastProgress == nil {
		return fmt.Errorf("no progress updates received")
	}

	if lastProgress.Error != "" {
		return fmt.Errorf("snapshot application failed: %s", lastProgress.Error)
	}

	fmt.Printf("\nSnapshot applied successfully!\n")
	fmt.Printf("  Snapshot ID: %s\n", lastProgress.SnapshotId)
	fmt.Printf("  LSN: %s\n", lastProgress.Lsn)
	fmt.Printf("  Target: %s\n", targetNodeID)

	return nil
}

// runSnapshotApplyDirect executes the snapshot apply via direct PostgreSQL connection.
// T024: Direct mode implementation using the steep_repl extension.
func runSnapshotApplyDirect(ctx context.Context, targetNodeID, inputPath, sourceNodeID string, parallelWorkers int, verifyChecksums bool, connString string, showProgress bool) error {
	// Create executor with connection string
	executor, err := direct.NewExecutor(ctx, direct.ExecutorConfig{
		ConnString:   connString,
		ShowProgress: showProgress,
		Timeout:      24 * time.Hour,
	})
	if err != nil {
		return fmt.Errorf("failed to create direct executor: %w", err)
	}
	defer executor.Close()

	// Note: The start_apply function in the extension is T032, which comes later in US2.
	// For now, we return an error indicating the function is not yet implemented.
	// This will be completed in T032.
	_ = targetNodeID
	_ = inputPath
	_ = sourceNodeID
	_ = parallelWorkers
	_ = verifyChecksums
	_ = showProgress

	return fmt.Errorf("direct mode snapshot apply not yet implemented (pending T032: steep_repl.start_apply)")
}
