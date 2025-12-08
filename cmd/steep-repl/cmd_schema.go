package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
)

// =============================================================================
// Schema Commands (T058-T060)
// =============================================================================

// newSchemaCmd creates the schema command group for schema fingerprinting and comparison.
func newSchemaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Schema fingerprinting and comparison commands",
		Long: `Schema fingerprinting and comparison for detecting schema drift
between nodes before and during replication.

Available subcommands:
  steep-repl schema capture <node>                                Capture fingerprints
  steep-repl schema compare <node-a> <node-b>                     Compare schemas
  steep-repl schema diff <node-a> <node-b> --table <schema.table> Show column diff

Examples:
  # Capture fingerprints for local node
  steep-repl schema capture node-a --remote localhost:15460 --insecure

  # Compare schemas between two nodes
  steep-repl schema compare node-a node-b --remote-a localhost:15460 --remote-b localhost:15461 --insecure

  # Show detailed column differences for a table
  steep-repl schema diff node-a node-b --table public.users --remote-a localhost:15460 --remote-b localhost:15461 --insecure`,
	}

	cmd.AddCommand(
		newSchemaCompareCmd(),
		newSchemaDiffCmd(),
		newSchemaCaptureCmd(),
	)

	return cmd
}

// newSchemaCompareCmd creates the schema compare subcommand (T058).
func newSchemaCompareCmd() *cobra.Command {
	var (
		remoteAddrA string
		remoteAddrB string
		caFile      string
		insecure    bool
		schemas     []string
	)

	cmd := &cobra.Command{
		Use:   "compare <node-a> <node-b>",
		Short: "Compare schemas between two nodes",
		Long: `Compare schema fingerprints between two nodes to detect drift.

Each node's fingerprints are retrieved via gRPC from their respective daemons.
The comparison shows:
  - MATCH:       Table exists on both with identical schema
  - MISMATCH:    Table exists on both but schemas differ
  - LOCAL_ONLY:  Table exists only on node-a
  - REMOTE_ONLY: Table exists only on node-b

For detailed differences on mismatched tables, use 'schema diff'.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeA := args[0]
			nodeB := args[1]

			if remoteAddrA == "" {
				return fmt.Errorf("--remote-a flag is required")
			}
			if remoteAddrB == "" {
				return fmt.Errorf("--remote-b flag is required")
			}

			return runSchemaCompare(nodeA, nodeB, remoteAddrA, remoteAddrB, caFile, insecure, schemas)
		},
	}

	cmd.Flags().StringVar(&remoteAddrA, "remote-a", "", "gRPC address of node A daemon (host:port)")
	cmd.Flags().StringVar(&remoteAddrB, "remote-b", "", "gRPC address of node B daemon (host:port)")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	cmd.Flags().StringSliceVar(&schemas, "schemas", nil, "schemas to compare (default: all)")
	_ = cmd.MarkFlagRequired("remote-a")
	_ = cmd.MarkFlagRequired("remote-b")

	return cmd
}

// runSchemaCompare executes schema comparison between two nodes.
func runSchemaCompare(nodeA, nodeB, remoteAddrA, remoteAddrB, caFile string, insecure bool, schemas []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Connect to node A
	clientCfgA := replgrpc.ClientConfig{
		Address: remoteAddrA,
		Timeout: 30 * time.Second,
	}
	if !insecure {
		clientCfgA.CAFile = caFile
	}

	clientA, err := replgrpc.NewClient(ctx, clientCfgA)
	if err != nil {
		return fmt.Errorf("failed to connect to node A (%s): %w", remoteAddrA, err)
	}
	defer clientA.Close()

	// Connect to node B
	clientCfgB := replgrpc.ClientConfig{
		Address: remoteAddrB,
		Timeout: 30 * time.Second,
	}
	if !insecure {
		clientCfgB.CAFile = caFile
	}

	clientB, err := replgrpc.NewClient(ctx, clientCfgB)
	if err != nil {
		return fmt.Errorf("failed to connect to node B (%s): %w", remoteAddrB, err)
	}
	defer clientB.Close()

	// Get fingerprints from both nodes
	fmt.Printf("Fetching fingerprints from %s (%s)...\n", nodeA, remoteAddrA)
	respA, err := clientA.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{
		Schemas: schemas,
	})
	if err != nil {
		return fmt.Errorf("failed to get fingerprints from node A: %w", err)
	}
	if !respA.Success {
		return fmt.Errorf("node A error: %s", respA.Error)
	}

	fmt.Printf("Fetching fingerprints from %s (%s)...\n", nodeB, remoteAddrB)
	respB, err := clientB.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{
		Schemas: schemas,
	})
	if err != nil {
		return fmt.Errorf("failed to get fingerprints from node B: %w", err)
	}
	if !respB.Success {
		return fmt.Errorf("node B error: %s", respB.Error)
	}

	// Column definition for JSON parsing
	type colDef struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Default  string `json:"default"`
		Nullable string `json:"nullable"`
		Position int    `json:"position"`
	}

	// Build maps with fingerprint and column definitions
	type tableInfo struct {
		Fingerprint string
		Columns     []colDef
	}
	fpA := make(map[string]tableInfo)
	fpB := make(map[string]tableInfo)

	for _, fp := range respA.Fingerprints {
		key := fp.SchemaName + "." + fp.TableName
		var cols []colDef
		if fp.ColumnDefinitions != "" {
			_ = json.Unmarshal([]byte(fp.ColumnDefinitions), &cols)
		}
		fpA[key] = tableInfo{Fingerprint: fp.Fingerprint, Columns: cols}
	}
	for _, fp := range respB.Fingerprints {
		key := fp.SchemaName + "." + fp.TableName
		var cols []colDef
		if fp.ColumnDefinitions != "" {
			_ = json.Unmarshal([]byte(fp.ColumnDefinitions), &cols)
		}
		fpB[key] = tableInfo{Fingerprint: fp.Fingerprint, Columns: cols}
	}

	// Compare
	var matchCount, mismatchCount, localOnlyCount, remoteOnlyCount int
	var mismatches, localOnly, remoteOnly []string
	var hasBlockingIssues bool

	fmt.Println()
	fmt.Printf("Comparing schemas: %s vs %s\n", nodeA, nodeB)
	fmt.Println("─────────────────────────────────────────────────────────────────")

	for key, localInfo := range fpA {
		if remoteInfo, exists := fpB[key]; exists {
			if localInfo.Fingerprint == remoteInfo.Fingerprint {
				matchCount++
			} else {
				mismatchCount++
				mismatches = append(mismatches, key)
			}
		} else {
			localOnlyCount++
			localOnly = append(localOnly, key)
		}
	}

	for key := range fpB {
		if _, exists := fpA[key]; !exists {
			remoteOnlyCount++
			remoteOnly = append(remoteOnly, key)
		}
	}

	// Print results
	if matchCount > 0 {
		fmt.Printf("\n✓ MATCH (%d tables): Schemas are identical\n", matchCount)
	}

	if mismatchCount > 0 {
		fmt.Printf("\n✗ MISMATCH (%d tables): Schema differences detected\n", mismatchCount)
		for _, t := range mismatches {
			fmt.Printf("    %s\n", t)

			// Compare column definitions locally
			localCols := fpA[t].Columns
			remoteCols := fpB[t].Columns

			// Build maps by column name
			localByName := make(map[string]colDef)
			remoteByName := make(map[string]colDef)
			for _, c := range localCols {
				localByName[c.Name] = c
			}
			for _, c := range remoteCols {
				remoteByName[c.Name] = c
			}

			// Find differences - categorize as blocking vs informational
			var hasBlockingDiff bool
			for name, lc := range localByName {
				if rc, exists := remoteByName[name]; exists {
					if lc.Type != rc.Type {
						fmt.Printf("      └─ %s: TYPE MISMATCH (breaks replication)\n", name)
						fmt.Printf("         %s: %s\n", nodeA, lc.Type)
						fmt.Printf("         %s: %s\n", nodeB, rc.Type)
						hasBlockingDiff = true
					} else if lc.Default != rc.Default {
						// Different defaults are OK - only affects local inserts
						fmt.Printf("      └─ %s: different DEFAULT (ok for replication)\n", name)
						fmt.Printf("         %s: %s\n", nodeA, formatDefault(lc.Default))
						fmt.Printf("         %s: %s\n", nodeB, formatDefault(rc.Default))
					} else if lc.Nullable != rc.Nullable {
						fmt.Printf("      └─ %s: NULLABLE MISMATCH (may cause issues)\n", name)
						fmt.Printf("         %s: %s\n", nodeA, lc.Nullable)
						fmt.Printf("         %s: %s\n", nodeB, rc.Nullable)
						hasBlockingDiff = true
					}
				} else {
					fmt.Printf("      └─ %s: MISSING on %s (breaks replication)\n", name, nodeB)
					hasBlockingDiff = true
				}
			}
			for name := range remoteByName {
				if _, exists := localByName[name]; !exists {
					fmt.Printf("      └─ %s: MISSING on %s (breaks replication)\n", name, nodeA)
					hasBlockingDiff = true
				}
			}
			if hasBlockingDiff {
				hasBlockingIssues = true
			}
		}
	}

	if localOnlyCount > 0 {
		fmt.Printf("\n✗ LOCAL_ONLY (%d tables): Only on %s - will not replicate\n", localOnlyCount, nodeA)
		for _, t := range localOnly {
			fmt.Printf("    %s\n", t)
		}
		hasBlockingIssues = true
	}

	if remoteOnlyCount > 0 {
		fmt.Printf("\n✗ REMOTE_ONLY (%d tables): Only on %s - will not replicate\n", remoteOnlyCount, nodeB)
		for _, t := range remoteOnly {
			fmt.Printf("    %s\n", t)
		}
		hasBlockingIssues = true
	}

	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────────────────────")
	fmt.Printf("Summary: match=%d, mismatch=%d, local_only=%d, remote_only=%d\n",
		matchCount, mismatchCount, localOnlyCount, remoteOnlyCount)

	if hasBlockingIssues {
		fmt.Println("\n✗ Schema incompatibilities detected that will break replication.")
		os.Exit(1)
	}

	if mismatchCount > 0 {
		// Only non-blocking differences (like defaults)
		fmt.Println("\n✓ Schemas compatible for replication (non-blocking differences noted above)")
	} else {
		fmt.Println("\n✓ Schemas are identical")
	}
	return nil
}

// formatDefault formats a column default for display.
func formatDefault(d string) string {
	if d == "" {
		return "(none)"
	}
	return d
}

// newSchemaDiffCmd creates the schema diff subcommand (T059).
func newSchemaDiffCmd() *cobra.Command {
	var (
		tableName   string
		remoteAddrA string
		remoteAddrB string
		caFile      string
		insecure    bool
	)

	cmd := &cobra.Command{
		Use:   "diff <node-a> <node-b>",
		Short: "Show column differences for a table",
		Long: `Show detailed column-level differences between two nodes for a specific table.

This helps diagnose schema mismatches by showing:
  - Missing columns: Column exists on one node but not the other
  - Extra columns: Column exists on one node but not the other
  - Type changes: Column exists on both but with different data types
  - Default changes: Column exists on both but with different defaults

The --table flag specifies the table in schema.table format (e.g., public.users).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeA := args[0]
			nodeB := args[1]

			if tableName == "" {
				return fmt.Errorf("--table flag is required")
			}
			if remoteAddrA == "" {
				return fmt.Errorf("--remote-a flag is required")
			}
			if remoteAddrB == "" {
				return fmt.Errorf("--remote-b flag is required")
			}

			return runSchemaDiff(nodeA, nodeB, tableName, remoteAddrA, remoteAddrB, caFile, insecure)
		},
	}

	cmd.Flags().StringVar(&tableName, "table", "", "table name in schema.table format (required)")
	cmd.Flags().StringVar(&remoteAddrA, "remote-a", "", "gRPC address of node A daemon (host:port)")
	cmd.Flags().StringVar(&remoteAddrB, "remote-b", "", "gRPC address of node B daemon (host:port)")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	_ = cmd.MarkFlagRequired("table")
	_ = cmd.MarkFlagRequired("remote-a")
	_ = cmd.MarkFlagRequired("remote-b")

	return cmd
}

// runSchemaDiff shows column differences for a table.
func runSchemaDiff(nodeA, nodeB, tableName, remoteAddrA, remoteAddrB, caFile string, insecure bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Parse table name
	parts := splitTableName(tableName)
	schemaName := parts[0]
	tableNameOnly := parts[1]
	tableKey := schemaName + "." + tableNameOnly

	// Connect to node A
	clientCfgA := replgrpc.ClientConfig{
		Address: remoteAddrA,
		Timeout: 30 * time.Second,
	}
	if !insecure {
		clientCfgA.CAFile = caFile
	}
	clientA, err := replgrpc.NewClient(ctx, clientCfgA)
	if err != nil {
		return fmt.Errorf("failed to connect to node A: %w", err)
	}
	defer clientA.Close()

	// Connect to node B
	clientCfgB := replgrpc.ClientConfig{
		Address: remoteAddrB,
		Timeout: 30 * time.Second,
	}
	if !insecure {
		clientCfgB.CAFile = caFile
	}
	clientB, err := replgrpc.NewClient(ctx, clientCfgB)
	if err != nil {
		return fmt.Errorf("failed to connect to node B: %w", err)
	}
	defer clientB.Close()

	// Get fingerprints from both nodes
	respA, err := clientA.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		return fmt.Errorf("failed to get fingerprints from node A: %w", err)
	}
	if !respA.Success {
		return fmt.Errorf("node A error: %s", respA.Error)
	}

	respB, err := clientB.GetSchemaFingerprints(ctx, &pb.GetSchemaFingerprintsRequest{})
	if err != nil {
		return fmt.Errorf("failed to get fingerprints from node B: %w", err)
	}
	if !respB.Success {
		return fmt.Errorf("node B error: %s", respB.Error)
	}

	// Column definition for JSON parsing
	type colDef struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Default  string `json:"default"`
		Nullable string `json:"nullable"`
		Position int    `json:"position"`
	}

	// Find the table in both responses
	var colsA, colsB []colDef
	for _, fp := range respA.Fingerprints {
		key := fp.SchemaName + "." + fp.TableName
		if key == tableKey && fp.ColumnDefinitions != "" {
			_ = json.Unmarshal([]byte(fp.ColumnDefinitions), &colsA)
			break
		}
	}
	for _, fp := range respB.Fingerprints {
		key := fp.SchemaName + "." + fp.TableName
		if key == tableKey && fp.ColumnDefinitions != "" {
			_ = json.Unmarshal([]byte(fp.ColumnDefinitions), &colsB)
			break
		}
	}

	fmt.Printf("\nColumn differences for %s\n", tableKey)
	fmt.Printf("Comparing: %s vs %s\n", nodeA, nodeB)
	fmt.Println("─────────────────────────────────────────────────────────────────")

	if len(colsA) == 0 && len(colsB) == 0 {
		return fmt.Errorf("table %s not found on either node", tableKey)
	}
	if len(colsA) == 0 {
		fmt.Printf("\n✗ Table only exists on %s\n", nodeB)
		return nil
	}
	if len(colsB) == 0 {
		fmt.Printf("\n✗ Table only exists on %s\n", nodeA)
		return nil
	}

	// Build maps by column name
	colsByNameA := make(map[string]colDef)
	colsByNameB := make(map[string]colDef)
	for _, c := range colsA {
		colsByNameA[c.Name] = c
	}
	for _, c := range colsB {
		colsByNameB[c.Name] = c
	}

	// Find differences - categorize as blocking vs informational
	var blockingCount, infoCount int
	for name, lc := range colsByNameA {
		if rc, exists := colsByNameB[name]; exists {
			if lc.Type != rc.Type {
				fmt.Printf("\n✗ TYPE MISMATCH: %s (breaks replication)\n", name)
				fmt.Printf("    %s: %s\n", nodeA, lc.Type)
				fmt.Printf("    %s: %s\n", nodeB, rc.Type)
				blockingCount++
			} else if lc.Default != rc.Default {
				fmt.Printf("\n  Different DEFAULT: %s (ok for replication)\n", name)
				fmt.Printf("    %s: %s\n", nodeA, formatDefault(lc.Default))
				fmt.Printf("    %s: %s\n", nodeB, formatDefault(rc.Default))
				infoCount++
			} else if lc.Nullable != rc.Nullable {
				fmt.Printf("\n⚠ NULLABLE MISMATCH: %s (may cause issues)\n", name)
				fmt.Printf("    %s: %s\n", nodeA, lc.Nullable)
				fmt.Printf("    %s: %s\n", nodeB, rc.Nullable)
				blockingCount++
			}
		} else {
			fmt.Printf("\n✗ MISSING: %s (only on %s, breaks replication)\n", name, nodeA)
			blockingCount++
		}
	}
	for name := range colsByNameB {
		if _, exists := colsByNameA[name]; !exists {
			fmt.Printf("\n✗ MISSING: %s (only on %s, breaks replication)\n", name, nodeB)
			blockingCount++
		}
	}

	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────────────────────")
	if blockingCount == 0 && infoCount == 0 {
		fmt.Println("✓ No differences found - table schemas are identical")
	} else if blockingCount == 0 {
		fmt.Printf("✓ Compatible for replication (%d non-blocking difference(s))\n", infoCount)
	} else {
		fmt.Printf("✗ %d blocking issue(s) that will break replication\n", blockingCount)
	}

	return nil
}

// splitTableName splits "schema.table" into [schema, table].
func splitTableName(name string) [2]string {
	var schema, table string
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			schema = name[:i]
			table = name[i+1:]
			return [2]string{schema, table}
		}
	}
	// Default to public schema if no dot found
	return [2]string{"public", name}
}

// newSchemaCaptureCmd creates the schema capture subcommand (T060).
func newSchemaCaptureCmd() *cobra.Command {
	var (
		remoteAddr string
		caFile     string
		insecure   bool
		schemas    []string
	)

	cmd := &cobra.Command{
		Use:   "capture <node>",
		Short: "Capture schema fingerprints for a node",
		Long: `Capture and store schema fingerprints for a node's database.

Fingerprints are SHA256 hashes of column definitions that enable fast
schema comparison. They are stored in the steep_repl.schema_fingerprints
table and can be retrieved via gRPC for remote comparison.

Run this command periodically or before initializing replication to ensure
accurate schema drift detection.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID := args[0]
			if remoteAddr == "" {
				return fmt.Errorf("--remote flag is required")
			}

			return runSchemaCapture(nodeID, remoteAddr, caFile, insecure, schemas)
		},
	}

	cmd.Flags().StringVar(&remoteAddr, "remote", "", "gRPC address of daemon (host:port)")
	cmd.Flags().StringVar(&caFile, "ca", "", "CA certificate file for TLS")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS (not recommended)")
	cmd.Flags().StringSliceVar(&schemas, "schemas", nil, "schemas to capture (default: all)")
	_ = cmd.MarkFlagRequired("remote")

	return cmd
}

// runSchemaCapture captures fingerprints for a node.
func runSchemaCapture(nodeID, remoteAddr, caFile string, insecure bool, schemas []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	clientCfg := replgrpc.ClientConfig{
		Address: remoteAddr,
		Timeout: 60 * time.Second,
	}
	if !insecure {
		clientCfg.CAFile = caFile
	}

	client, err := replgrpc.NewClient(ctx, clientCfg)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	fmt.Printf("Capturing schema fingerprints for %s...\n", nodeID)

	// Use CaptureFingerprints RPC
	resp, err := client.CaptureFingerprints(ctx, &pb.CaptureFingerprintsRequest{
		NodeId:  nodeID,
		Schemas: schemas,
	})
	if err != nil {
		return fmt.Errorf("failed to capture fingerprints: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("error: %s", resp.Error)
	}

	fmt.Printf("\n✓ Captured %d table fingerprints\n", resp.TableCount)

	// Display fingerprints in a table format
	if len(resp.Fingerprints) > 0 {
		fmt.Println()
		fmt.Printf("  %-15s %-25s %s\n", "SCHEMA", "TABLE", "FINGERPRINT")
		fmt.Printf("  %-15s %-25s %s\n", "------", "-----", "-----------")
		for _, fp := range resp.Fingerprints {
			fmt.Printf("  %-15s %-25s %s\n", fp.SchemaName, fp.TableName, fp.Fingerprint)
		}
	}

	fmt.Println("\nFingerprints stored in steep_repl.schema_fingerprints table.")
	fmt.Println("Use 'steep-repl schema compare' to compare with another node.")

	return nil
}
