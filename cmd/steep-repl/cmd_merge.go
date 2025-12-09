package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	replinit "github.com/willibrandon/steep/internal/repl/init"
)

// newAnalyzeOverlapCmd creates the analyze-overlap command for overlap analysis.
func newAnalyzeOverlapCmd() *cobra.Command {
	var (
		tables       string
		outputJSON   bool
		detailed     bool
		remoteServer string
	)

	cmd := &cobra.Command{
		Use:   "analyze-overlap <node-a-conn> <node-b-conn>",
		Short: "Analyze data overlap between two nodes",
		Long: `Analyze data overlap between two nodes for bidirectional merge planning.

This command compares tables between two PostgreSQL nodes and reports:
- Matches: Rows that are identical on both nodes
- Conflicts: Rows with same primary key but different data
- Local-only: Rows that exist only on the local node (A)
- Remote-only: Rows that exist only on the remote node (B)

The analysis uses hash-based comparison for efficiency, transferring only
primary keys and 8-byte hashes across the network instead of full rows.

Examples:
  # Analyze specific tables
  steep-repl analyze-overlap localhost:5432 remotehost:5432 --tables users,orders

  # Output as JSON for scripting
  steep-repl analyze-overlap localhost:5432 remotehost:5432 --tables users --json

  # Show detailed row-by-row analysis
  steep-repl analyze-overlap localhost:5432 remotehost:5432 --tables users --detailed`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeAAddr := args[0]
			nodeBAddr := args[1]

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			// Parse table list
			tableList := strings.Split(tables, ",")
			if len(tableList) == 0 || (len(tableList) == 1 && tableList[0] == "") {
				return fmt.Errorf("--tables is required")
			}

			// Connect to both nodes
			localPool, err := connectToNode(ctx, nodeAAddr)
			if err != nil {
				return fmt.Errorf("connect to node A (%s): %w", nodeAAddr, err)
			}
			defer localPool.Close()

			remotePool, err := connectToNode(ctx, nodeBAddr)
			if err != nil {
				return fmt.Errorf("connect to node B (%s): %w", nodeBAddr, err)
			}
			defer remotePool.Close()

			// Create merger
			merger := replinit.NewMerger(localPool, remotePool, nil)

			// Get PK columns for each table
			var tablesToAnalyze []replinit.MergeTableInfo
			for _, t := range tableList {
				parts := strings.Split(strings.TrimSpace(t), ".")
				var schema, table string
				if len(parts) == 2 {
					schema = parts[0]
					table = parts[1]
				} else {
					schema = "public"
					table = parts[0]
				}

				// Get PK columns from database
				pkCols, err := getPrimaryKeyColumns(ctx, localPool, schema, table)
				if err != nil {
					return fmt.Errorf("get PK columns for %s.%s: %w", schema, table, err)
				}
				if len(pkCols) == 0 {
					return fmt.Errorf("table %s.%s has no primary key", schema, table)
				}

				tablesToAnalyze = append(tablesToAnalyze, replinit.MergeTableInfo{
					Schema:    schema,
					Name:      table,
					PKColumns: pkCols,
				})
			}

			// Ensure remote server is set up for FDW comparison
			if remoteServer == "" {
				remoteServer = "node_b_fdw"
			}

			// Setup postgres_fdw if needed
			if err := setupForeignServer(ctx, localPool, remoteServer, nodeBAddr); err != nil {
				return fmt.Errorf("setup foreign server: %w", err)
			}

			if detailed {
				// Detailed row-by-row analysis
				for _, t := range tablesToAnalyze {
					results, err := merger.AnalyzeOverlapDetailed(ctx, t.Schema, t.Name, t.PKColumns, remoteServer)
					if err != nil {
						return fmt.Errorf("analyze %s.%s: %w", t.Schema, t.Name, err)
					}

					if outputJSON {
						enc := json.NewEncoder(os.Stdout)
						enc.SetIndent("", "  ")
						if err := enc.Encode(results); err != nil {
							return err
						}
					} else {
						fmt.Printf("\n=== %s.%s ===\n", t.Schema, t.Name)
						fmt.Printf("%-40s %-15s %s\n", "PK", "Category", "Hashes (local/remote)")
						fmt.Println(strings.Repeat("-", 80))
						for _, r := range results {
							pkStr := formatPK(r.PKValue)
							hashStr := formatHashes(r.LocalHash, r.RemoteHash)
							fmt.Printf("%-40s %-15s %s\n", pkStr, r.Category, hashStr)
						}
					}
				}
			} else {
				// Summary analysis
				summaries, err := merger.AnalyzeAllTables(ctx, tablesToAnalyze, remoteServer)
				if err != nil {
					return err
				}

				if outputJSON {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					if err := enc.Encode(summaries); err != nil {
						return err
					}
				} else {
					fmt.Println("\nOverlap Analysis Results")
					fmt.Println(strings.Repeat("=", 80))
					fmt.Printf("%-30s %10s %10s %10s %10s %10s\n",
						"Table", "Total", "Matches", "Conflicts", "LocalOnly", "RemoteOnly")
					fmt.Println(strings.Repeat("-", 80))

					var totalRows, totalMatches, totalConflicts, totalLocalOnly, totalRemoteOnly int64
					for _, s := range summaries {
						fmt.Printf("%-30s %10d %10d %10d %10d %10d\n",
							fmt.Sprintf("%s.%s", s.TableSchema, s.TableName),
							s.TotalRows, s.Matches, s.Conflicts, s.LocalOnly, s.RemoteOnly)
						totalRows += s.TotalRows
						totalMatches += s.Matches
						totalConflicts += s.Conflicts
						totalLocalOnly += s.LocalOnly
						totalRemoteOnly += s.RemoteOnly
					}

					fmt.Println(strings.Repeat("-", 80))
					fmt.Printf("%-30s %10d %10d %10d %10d %10d\n",
						"TOTAL", totalRows, totalMatches, totalConflicts, totalLocalOnly, totalRemoteOnly)

					if totalConflicts > 0 {
						fmt.Printf("\n⚠ %d conflicts detected. Use --detailed to see specific rows.\n", totalConflicts)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&tables, "tables", "", "Comma-separated list of tables to analyze")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output results as JSON")
	cmd.Flags().BoolVar(&detailed, "detailed", false, "Show detailed row-by-row analysis")
	cmd.Flags().StringVar(&remoteServer, "remote-server", "node_b_fdw", "Name of postgres_fdw foreign server")

	cmd.MarkFlagRequired("tables")

	return cmd
}

// newMergeCmd creates the merge command group.
func newMergeCmd() *cobra.Command {
	var (
		tables       string
		strategy     string
		dryRun       bool
		remoteServer string
	)

	cmd := &cobra.Command{
		Use:   "merge <node-a-conn> <node-b-conn>",
		Short: "Bidirectional merge operations",
		Long: `Perform bidirectional merge operations between two nodes.

This command merges existing data between two PostgreSQL nodes and sets up
bidirectional replication. It handles:

1. Quiescing writes on both nodes
2. Analyzing overlap (matches, conflicts, unique rows)
3. Resolving conflicts using the specified strategy
4. Transferring unique rows in both directions
5. Enabling bidirectional replication with origin=none

Conflict Resolution Strategies:
  prefer-local    - Always keep local node's value for conflicts
  prefer-remote   - Always keep remote node's value for conflicts
  last-modified   - Keep the most recently modified value (requires track_commit_timestamp)
  manual          - Generate conflict report for manual resolution

Examples:
  # Merge with prefer-local strategy
  steep-repl merge localhost:5432 remotehost:5432 --tables users,orders --strategy prefer-local

  # Dry run to preview changes
  steep-repl merge localhost:5432 remotehost:5432 --tables users --dry-run

  # Generate manual conflict report
  steep-repl merge localhost:5432 remotehost:5432 --tables users --strategy manual`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeAAddr := args[0]
			nodeBAddr := args[1]

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()

			// Parse strategy
			var conflictStrategy replinit.ConflictStrategy
			switch strategy {
			case "prefer-local":
				conflictStrategy = replinit.StrategyPreferLocal
			case "prefer-remote":
				conflictStrategy = replinit.StrategyPreferRemote
			case "last-modified":
				conflictStrategy = replinit.StrategyLastModified
			case "manual":
				conflictStrategy = replinit.StrategyManual
			default:
				return fmt.Errorf("invalid strategy: %s (valid: prefer-local, prefer-remote, last-modified, manual)", strategy)
			}

			// Parse table list
			tableList := strings.Split(tables, ",")
			if len(tableList) == 0 || (len(tableList) == 1 && tableList[0] == "") {
				return fmt.Errorf("--tables is required")
			}

			// Connect to both nodes
			localPool, err := connectToNode(ctx, nodeAAddr)
			if err != nil {
				return fmt.Errorf("connect to node A (%s): %w", nodeAAddr, err)
			}
			defer localPool.Close()

			remotePool, err := connectToNode(ctx, nodeBAddr)
			if err != nil {
				return fmt.Errorf("connect to node B (%s): %w", nodeBAddr, err)
			}
			defer remotePool.Close()

			// Create merger
			merger := replinit.NewMerger(localPool, remotePool, nil)

			// Get table info with PKs
			var tablesToMerge []replinit.MergeTableInfo
			for _, t := range tableList {
				parts := strings.Split(strings.TrimSpace(t), ".")
				var schema, table string
				if len(parts) == 2 {
					schema = parts[0]
					table = parts[1]
				} else {
					schema = "public"
					table = parts[0]
				}

				pkCols, err := getPrimaryKeyColumns(ctx, localPool, schema, table)
				if err != nil {
					return fmt.Errorf("get PK columns for %s.%s: %w", schema, table, err)
				}
				if len(pkCols) == 0 {
					return fmt.Errorf("table %s.%s has no primary key", schema, table)
				}

				tablesToMerge = append(tablesToMerge, replinit.MergeTableInfo{
					Schema:    schema,
					Name:      table,
					PKColumns: pkCols,
				})
			}

			// Run pre-flight checks
			preflight, err := merger.RunPreflightChecks(ctx, tablesToMerge)
			if err != nil {
				return fmt.Errorf("pre-flight checks: %w", err)
			}

			if len(preflight.Errors) > 0 {
				fmt.Println("Pre-flight check errors:")
				for _, e := range preflight.Errors {
					fmt.Printf("  ✗ %s\n", e)
				}
				return fmt.Errorf("pre-flight checks failed")
			}

			if len(preflight.Warnings) > 0 {
				fmt.Println("Pre-flight check warnings:")
				for _, w := range preflight.Warnings {
					fmt.Printf("  ⚠ %s\n", w)
				}
			}

			if strategy == "last-modified" && !preflight.TrackCommitTimestamp {
				return fmt.Errorf("last-modified strategy requires track_commit_timestamp=on")
			}

			// Ensure remote server is set up
			if remoteServer == "" {
				remoteServer = "node_b_fdw"
			}
			if err := setupForeignServer(ctx, localPool, remoteServer, nodeBAddr); err != nil {
				return fmt.Errorf("setup foreign server: %w", err)
			}

			// Get FK dependencies and sort tables
			deps, err := merger.GetFKDependencies(ctx, tablesToMerge)
			if err != nil {
				return fmt.Errorf("get FK dependencies: %w", err)
			}

			sortedTables, err := merger.TopologicalSort(tablesToMerge, deps)
			if err != nil {
				return err
			}

			fmt.Println("\nMerge order (respecting foreign keys):")
			for i, t := range sortedTables {
				fmt.Printf("  %d. %s.%s\n", i+1, t.Schema, t.Name)
			}

			if dryRun {
				fmt.Println("\n=== DRY RUN ===")
				fmt.Println("Analyzing overlap without making changes...")

				summaries, err := merger.AnalyzeAllTables(ctx, sortedTables, remoteServer)
				if err != nil {
					return err
				}

				fmt.Printf("\nDry Run Summary (strategy: %s):\n", strategy)
				fmt.Println(strings.Repeat("-", 60))

				var totalConflicts, totalLocalOnly, totalRemoteOnly int64
				for _, s := range summaries {
					fmt.Printf("%s.%s:\n", s.TableSchema, s.TableName)
					fmt.Printf("  Conflicts to resolve: %d\n", s.Conflicts)
					fmt.Printf("  Rows to transfer A→B: %d\n", s.LocalOnly)
					fmt.Printf("  Rows to transfer B→A: %d\n", s.RemoteOnly)
					totalConflicts += s.Conflicts
					totalLocalOnly += s.LocalOnly
					totalRemoteOnly += s.RemoteOnly
				}

				fmt.Println(strings.Repeat("-", 60))
				fmt.Printf("Total conflicts: %d\n", totalConflicts)
				fmt.Printf("Total rows A→B: %d\n", totalLocalOnly)
				fmt.Printf("Total rows B→A: %d\n", totalRemoteOnly)
				fmt.Println("\nNo changes made. Run without --dry-run to execute.")
				return nil
			}

			// Execute full merge workflow
			fmt.Println("\n=== EXECUTING MERGE ===")
			fmt.Printf("Strategy: %s\n", strategy)
			fmt.Println()

			mergeConfig := replinit.MergeConfig{
				Tables:           sortedTables,
				Strategy:         conflictStrategy,
				RemoteServer:     remoteServer,
				QuiesceTimeoutMs: 30000, // 30 second timeout for quiesce
				DryRun:           false,
			}

			result, err := merger.ExecuteMerge(ctx, mergeConfig)
			if err != nil {
				return fmt.Errorf("execute merge: %w", err)
			}

			// Print results
			fmt.Printf("\nMerge ID: %s\n", result.MergeID)
			fmt.Printf("Duration: %s\n", result.CompletedAt.Sub(result.StartedAt))
			fmt.Println(strings.Repeat("-", 60))
			fmt.Printf("Matches:           %d\n", result.TotalMatches)
			fmt.Printf("Conflicts:         %d\n", result.TotalConflicts)
			fmt.Printf("Resolved:          %d\n", result.ConflictsResolved)
			fmt.Printf("Transferred A→B:   %d\n", result.RowsTransferredAToB)
			fmt.Printf("Transferred B→A:   %d\n", result.RowsTransferredBToA)

			if len(result.Errors) > 0 {
				fmt.Println("\nWarnings/Errors:")
				for _, e := range result.Errors {
					fmt.Printf("  - %s\n", e)
				}
			}

			if result.TotalConflicts > 0 && conflictStrategy == replinit.StrategyManual {
				fmt.Printf("\n%d conflicts require manual resolution.\n", result.TotalConflicts)
				fmt.Println("Use 'analyze-overlap --detailed' to see conflict details.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&tables, "tables", "", "Comma-separated list of tables to merge")
	cmd.Flags().StringVar(&strategy, "strategy", "prefer-local", "Conflict resolution strategy")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying")
	cmd.Flags().StringVar(&remoteServer, "remote-server", "node_b_fdw", "Name of postgres_fdw foreign server")

	cmd.MarkFlagRequired("tables")

	return cmd
}

// Helper functions

func formatPK(pk map[string]any) string {
	if len(pk) == 1 {
		for _, v := range pk {
			return fmt.Sprintf("%v", v)
		}
	}
	b, _ := json.Marshal(pk)
	return string(b)
}

func formatHashes(local, remote *int64) string {
	l := "nil"
	r := "nil"
	if local != nil {
		l = fmt.Sprintf("%x", *local)
	}
	if remote != nil {
		r = fmt.Sprintf("%x", *remote)
	}
	return fmt.Sprintf("%s / %s", l, r)
}

// parseConnectionInfo extracts host, port, dbname, and user from a connection string.
// Supports postgres:// URIs, key=value format, and simple host:port format.
func parseConnectionInfo(connStr string) (host string, port uint16, dbname, user string, err error) {
	// Normalize simple host:port format to key=value
	if !strings.Contains(connStr, "=") && !strings.HasPrefix(connStr, "postgres://") {
		parts := strings.Split(connStr, ":")
		host = parts[0]
		port = 5432
		if len(parts) > 1 {
			var p uint64
			p, err = strconv.ParseUint(parts[1], 10, 16)
			if err != nil {
				return "", 0, "", "", fmt.Errorf("invalid port %q: %w", parts[1], err)
			}
			port = uint16(p)
		}
		return host, port, "", "", nil
	}

	// Use pgx's robust parsing for all other formats
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return "", 0, "", "", fmt.Errorf("parse connection string: %w", err)
	}

	connConfig := config.ConnConfig
	host = connConfig.Host
	port = connConfig.Port
	dbname = connConfig.Database
	user = connConfig.User

	return host, port, dbname, user, nil
}

func setupForeignServer(ctx context.Context, pool *pgxpool.Pool, serverName, connStr string) error {
	host, port, dbname, user, err := parseConnectionInfo(connStr)
	if err != nil {
		return fmt.Errorf("parse connection string: %w", err)
	}

	// Create extension if not exists
	_, err = pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS postgres_fdw")
	if err != nil {
		return fmt.Errorf("create postgres_fdw extension: %w", err)
	}

	// Drop existing server if it exists (CASCADE removes dependent objects)
	_, err = pool.Exec(ctx, "DROP SERVER IF EXISTS "+serverName+" CASCADE")
	if err != nil {
		return fmt.Errorf("drop existing server: %w", err)
	}

	// Build server options - use parameterized values where possible
	// Note: CREATE SERVER doesn't support $1 parameters, but we validate inputs
	serverOpts := fmt.Sprintf("host '%s', port '%d'", host, port)
	if dbname != "" {
		serverOpts += fmt.Sprintf(", dbname '%s'", dbname)
	} else {
		// Use current database if not specified
		var currentDB string
		if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&currentDB); err != nil {
			return fmt.Errorf("get current database: %w", err)
		}
		serverOpts += fmt.Sprintf(", dbname '%s'", currentDB)
	}

	createServerSQL := fmt.Sprintf(
		"CREATE SERVER %s FOREIGN DATA WRAPPER postgres_fdw OPTIONS (%s)",
		serverName, serverOpts,
	)
	_, err = pool.Exec(ctx, createServerSQL)
	if err != nil {
		return fmt.Errorf("create foreign server: %w", err)
	}

	// Drop existing user mapping if it exists
	_, err = pool.Exec(ctx, "DROP USER MAPPING IF EXISTS FOR CURRENT_USER SERVER "+serverName)
	if err != nil {
		return fmt.Errorf("drop existing user mapping: %w", err)
	}

	// Build user mapping options
	userOpts := ""
	if user != "" {
		userOpts = fmt.Sprintf("OPTIONS (user '%s')", user)
	} else {
		// Use current user if not specified
		var currentUser string
		if err := pool.QueryRow(ctx, "SELECT current_user").Scan(&currentUser); err != nil {
			return fmt.Errorf("get current user: %w", err)
		}
		userOpts = fmt.Sprintf("OPTIONS (user '%s')", currentUser)
	}

	createMappingSQL := fmt.Sprintf(
		"CREATE USER MAPPING FOR CURRENT_USER SERVER %s %s",
		serverName, userOpts,
	)
	_, err = pool.Exec(ctx, createMappingSQL)
	if err != nil {
		return fmt.Errorf("create user mapping: %w", err)
	}

	return nil
}

// connectToNode creates a connection pool to a PostgreSQL node.
// Supports postgres:// URIs, key=value format, and simple host:port format.
func connectToNode(ctx context.Context, connStr string) (*pgxpool.Pool, error) {
	// Normalize simple host:port format to key=value for pgx parsing
	if !strings.Contains(connStr, "=") && !strings.HasPrefix(connStr, "postgres://") {
		parts := strings.Split(connStr, ":")
		host := parts[0]
		port := "5432"
		if len(parts) > 1 {
			if _, err := strconv.ParseUint(parts[1], 10, 16); err != nil {
				return nil, fmt.Errorf("invalid port %q: %w", parts[1], err)
			}
			port = parts[1]
		}
		connStr = fmt.Sprintf("host=%s port=%s", host, port)
	}

	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse connection string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	return pool, nil
}

// getPrimaryKeyColumns returns the primary key column names for a table.
func getPrimaryKeyColumns(ctx context.Context, pool *pgxpool.Pool, schema, table string) ([]string, error) {
	query := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = ($1 || '.' || $2)::regclass
		  AND i.indisprimary
		ORDER BY array_position(i.indkey, a.attnum)
	`

	rows, err := pool.Query(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		columns = append(columns, col)
	}

	return columns, rows.Err()
}
