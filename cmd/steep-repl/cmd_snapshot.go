package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
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
  steep-repl snapshot generate    Generate a snapshot to disk
  steep-repl snapshot apply       Apply a snapshot from disk

Examples:
  # Generate a snapshot from node-a
  steep-repl snapshot generate --source node-a --output /snapshots/node-a

  # Apply snapshot to node-b
  steep-repl snapshot apply --target node-b --input /snapshots/node-a`,
	}

	cmd.AddCommand(
		newSnapshotGenerateCmd(),
		newSnapshotApplyCmd(),
	)

	return cmd
}

// newSnapshotGenerateCmd creates the snapshot generate subcommand.
// Implements T085: Add snapshot generate CLI command.
func newSnapshotGenerateCmd() *cobra.Command {
	var (
		sourceNodeID    string
		outputPath      string
		compression     string
		parallelWorkers int
		remoteAddr      string
		caFile          string
		insecure        bool
		progress        bool
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a two-phase snapshot to disk",
		Long: `Generate a consistent snapshot of a node's data to disk.

The snapshot includes:
- All user tables exported as CSV files
- Sequence values at the snapshot point
- A manifest.json with checksums and LSN
- Optional compression (gzip, lz4, zstd)

The snapshot captures a consistent point-in-time using a logical replication slot.

Examples:
  # Generate uncompressed snapshot
  steep-repl snapshot generate --source node-a --output /snapshots/node-a --remote localhost:9090 --insecure

  # Generate with gzip compression
  steep-repl snapshot generate --source node-a --output /snapshots/node-a --compression gzip --remote localhost:9090 --insecure

  # Generate with 8 parallel workers
  steep-repl snapshot generate --source node-a --output /snapshots/node-a --parallel 8 --remote localhost:9090 --insecure`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if sourceNodeID == "" {
				return fmt.Errorf("--source flag is required")
			}
			if outputPath == "" {
				return fmt.Errorf("--output flag is required")
			}

			fmt.Printf("Generating snapshot from %s to %s\n", sourceNodeID, outputPath)
			fmt.Printf("  Compression: %s\n", compression)
			fmt.Printf("  Parallel workers: %d\n", parallelWorkers)
			fmt.Println()

			return runSnapshotGenerate(sourceNodeID, outputPath, compression, parallelWorkers, remoteAddr, caFile, insecure, progress)
		},
	}

	// Required flags
	cmd.Flags().StringVar(&sourceNodeID, "source", "", "source node ID to snapshot (required)")
	cmd.Flags().StringVar(&outputPath, "output", "", "output directory path (required)")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("output")

	// Options
	cmd.Flags().StringVar(&compression, "compression", "none", "compression type: none, gzip, lz4, zstd")
	cmd.Flags().IntVar(&parallelWorkers, "parallel", 4, "number of parallel workers (1-16)")
	cmd.Flags().BoolVar(&progress, "progress", true, "show progress updates")

	// Connection flags
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "daemon gRPC address (host:port) - required")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("remote")

	return cmd
}

// runSnapshotGenerate executes the snapshot generate via gRPC.
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

// newSnapshotApplyCmd creates the snapshot apply subcommand.
// Implements T086: Add snapshot apply CLI command.
func newSnapshotApplyCmd() *cobra.Command {
	var (
		targetNodeID    string
		inputPath       string
		sourceNodeID    string
		parallelWorkers int
		verifyChecksums bool
		remoteAddr      string
		caFile          string
		insecure        bool
		progress        bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply a two-phase snapshot from disk",
		Long: `Apply a snapshot to a target node from disk.

The apply process:
1. Verifies checksums of all data files (optional)
2. Truncates and imports each table using COPY
3. Restores sequence values
4. Optionally creates a subscription to the source node

Examples:
  # Apply snapshot to target node
  steep-repl snapshot apply --target node-b --input /snapshots/node-a --remote localhost:9091 --insecure

  # Apply without checksum verification (faster but less safe)
  steep-repl snapshot apply --target node-b --input /snapshots/node-a --verify=false --remote localhost:9091 --insecure

  # Apply with source node ID for subscription setup
  steep-repl snapshot apply --target node-b --input /snapshots/node-a --source node-a --remote localhost:9091 --insecure`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if targetNodeID == "" {
				return fmt.Errorf("--target flag is required")
			}
			if inputPath == "" {
				return fmt.Errorf("--input flag is required")
			}

			// Verify input path exists
			if _, err := os.Stat(inputPath); os.IsNotExist(err) {
				return fmt.Errorf("input path does not exist: %s", inputPath)
			}

			fmt.Printf("Applying snapshot to %s from %s\n", targetNodeID, inputPath)
			fmt.Printf("  Verify checksums: %v\n", verifyChecksums)
			fmt.Printf("  Parallel workers: %d\n", parallelWorkers)
			if sourceNodeID != "" {
				fmt.Printf("  Source node: %s (for subscription)\n", sourceNodeID)
			}
			fmt.Println()

			return runSnapshotApply(targetNodeID, inputPath, sourceNodeID, parallelWorkers, verifyChecksums, remoteAddr, caFile, insecure, progress)
		},
	}

	// Required flags
	cmd.Flags().StringVar(&targetNodeID, "target", "", "target node ID to apply snapshot to (required)")
	cmd.Flags().StringVar(&inputPath, "input", "", "input directory path containing snapshot (required)")
	_ = cmd.MarkFlagRequired("target")
	_ = cmd.MarkFlagRequired("input")

	// Options
	cmd.Flags().StringVar(&sourceNodeID, "source", "", "source node ID (for subscription setup)")
	cmd.Flags().IntVar(&parallelWorkers, "parallel", 4, "number of parallel workers (1-16)")
	cmd.Flags().BoolVar(&verifyChecksums, "verify", true, "verify checksums before import")
	cmd.Flags().BoolVar(&progress, "progress", true, "show progress updates")

	// Connection flags
	cmd.Flags().StringVar(&remoteAddr, "remote", "", "daemon gRPC address (host:port) - required")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("remote")

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
