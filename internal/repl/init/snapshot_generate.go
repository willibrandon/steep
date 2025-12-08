package init

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/willibrandon/steep/internal/repl/models"
)

// =============================================================================
// Two-Phase Snapshot Generation (T080, T081, T082)
// =============================================================================

// TwoPhaseSnapshotOptions configures two-phase snapshot generation.
type TwoPhaseSnapshotOptions struct {
	OutputPath      string
	Compression     models.CompressionType
	ParallelWorkers int
	ProgressFn      func(progress TwoPhaseProgress)
}

// TwoPhaseProgress represents progress during two-phase snapshot operations.
type TwoPhaseProgress struct {
	SnapshotID          string
	Phase               string // "schema", "data", "sequences", "finalizing"
	OverallPercent      float32
	CurrentTable        string
	CurrentTablePercent float32
	BytesProcessed      int64
	ThroughputMBSec     float32
	ETASeconds          int
	LSN                 string
	Complete            bool
	Error               string
}

// SnapshotGenerator handles two-phase snapshot generation.
// This exports data to files for offline transfer and multi-target init.
type SnapshotGenerator struct {
	pool    *pgxpool.Pool
	manager *Manager
	logger  *Logger
}

// NewSnapshotGenerator creates a new snapshot generator.
func NewSnapshotGenerator(pool *pgxpool.Pool, manager *Manager) *SnapshotGenerator {
	return &SnapshotGenerator{
		pool:    pool,
		manager: manager,
		logger:  manager.logger,
	}
}

// Generate creates a two-phase snapshot to the specified output path.
// Implements T081: snapshot generation logic.
func (g *SnapshotGenerator) Generate(ctx context.Context, sourceNodeID string, opts TwoPhaseSnapshotOptions) (*models.SnapshotManifest, error) {
	startTime := time.Now()

	// Generate unique snapshot ID
	snapshotID := fmt.Sprintf("snap_%s_%s", time.Now().Format("20060102_150405"), sourceNodeID[:8])

	g.logger.Log(InitEvent{
		Level:  "info",
		Event:  "snapshot.generation_started",
		NodeID: sourceNodeID,
		Details: map[string]any{
			"snapshot_id":      snapshotID,
			"output_path":      opts.OutputPath,
			"compression":      opts.Compression,
			"parallel_workers": opts.ParallelWorkers,
		},
	})

	// Send initial progress
	g.sendProgress(opts.ProgressFn, TwoPhaseProgress{
		SnapshotID:     snapshotID,
		Phase:          "schema",
		OverallPercent: 0,
	})

	// Create output directory
	dataDir := filepath.Join(opts.OutputPath, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create replication slot to ensure consistent snapshot
	slotName := fmt.Sprintf("steep_snap_%s", sanitizeIdentifier(snapshotID))
	lsn, err := g.createSnapshotSlot(ctx, slotName)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot slot: %w", err)
	}

	g.logger.Log(InitEvent{
		Level:  "info",
		Event:  "snapshot.slot_created",
		NodeID: sourceNodeID,
		Details: map[string]any{
			"slot_name": slotName,
			"lsn":       lsn,
		},
	})

	// Get tables to export
	tables, err := g.getTablesForExport(ctx)
	if err != nil {
		g.dropSlot(ctx, slotName)
		return nil, fmt.Errorf("failed to get tables: %w", err)
	}

	g.sendProgress(opts.ProgressFn, TwoPhaseProgress{
		SnapshotID:     snapshotID,
		Phase:          "data",
		OverallPercent: 5,
		LSN:            lsn,
	})

	// Export tables (parallel if workers > 1)
	workers := max(opts.ParallelWorkers, 1)
	if workers > len(tables) {
		workers = len(tables)
	}

	tableEntries, totalBytes, err := g.exportTablesParallel(ctx, tables, dataDir, opts.Compression, workers, func(completed int, current string, bytes int64) {
		percent := float32(5 + (completed * 85 / len(tables)))
		g.sendProgress(opts.ProgressFn, TwoPhaseProgress{
			SnapshotID:     snapshotID,
			Phase:          "data",
			OverallPercent: percent,
			CurrentTable:   current,
			BytesProcessed: bytes,
			LSN:            lsn,
		})
	})
	if err != nil {
		g.dropSlot(ctx, slotName)
		return nil, err
	}

	// Export sequences
	g.sendProgress(opts.ProgressFn, TwoPhaseProgress{
		SnapshotID:     snapshotID,
		Phase:          "sequences",
		OverallPercent: 90,
		LSN:            lsn,
	})

	sequences, err := g.getSequences(ctx)
	if err != nil {
		g.dropSlot(ctx, slotName)
		return nil, fmt.Errorf("failed to get sequences: %w", err)
	}

	// Create manifest (T082)
	manifest := &models.SnapshotManifest{
		SnapshotID:      snapshotID,
		SourceNode:      sourceNodeID,
		LSN:             lsn,
		CreatedAt:       startTime,
		Tables:          tableEntries,
		Sequences:       sequences,
		TotalSizeBytes:  totalBytes,
		Compression:     opts.Compression,
		ParallelWorkers: opts.ParallelWorkers,
	}

	// Write manifest to file
	g.sendProgress(opts.ProgressFn, TwoPhaseProgress{
		SnapshotID:     snapshotID,
		Phase:          "finalizing",
		OverallPercent: 95,
		LSN:            lsn,
	})

	if err := g.writeManifest(opts.OutputPath, manifest); err != nil {
		g.dropSlot(ctx, slotName)
		return nil, fmt.Errorf("failed to write manifest: %w", err)
	}

	// Record snapshot in database
	if err := g.recordSnapshot(ctx, manifest, opts.OutputPath); err != nil {
		g.logger.Log(InitEvent{
			Level: "warn",
			Event: "snapshot.record_failed",
			Error: err.Error(),
		})
		// Non-fatal, continue
	}

	// Clean up the slot (we've captured the LSN, no longer needed)
	g.dropSlot(ctx, slotName)

	duration := time.Since(startTime)
	g.logger.Log(InitEvent{
		Level: "info",
		Event: "snapshot.generation_completed",
		Details: map[string]any{
			"snapshot_id":    snapshotID,
			"duration_ms":    duration.Milliseconds(),
			"total_bytes":    totalBytes,
			"table_count":    len(tableEntries),
			"sequence_count": len(sequences),
			"lsn":            lsn,
		},
	})

	// Final progress
	g.sendProgress(opts.ProgressFn, TwoPhaseProgress{
		SnapshotID:     snapshotID,
		Phase:          "complete",
		OverallPercent: 100,
		BytesProcessed: totalBytes,
		LSN:            lsn,
		Complete:       true,
	})

	return manifest, nil
}

// createSnapshotSlot creates a logical replication slot and returns the consistent LSN.
func (g *SnapshotGenerator) createSnapshotSlot(ctx context.Context, slotName string) (string, error) {
	var lsn string
	err := g.pool.QueryRow(ctx, `
		SELECT lsn::text FROM pg_create_logical_replication_slot($1, 'pgoutput')
	`, slotName).Scan(&lsn)
	if err != nil {
		return "", err
	}
	return lsn, nil
}

// dropSlot drops a replication slot.
func (g *SnapshotGenerator) dropSlot(ctx context.Context, slotName string) {
	_, err := g.pool.Exec(ctx, `SELECT pg_drop_replication_slot($1)`, slotName)
	if err != nil {
		g.logger.Log(InitEvent{
			Level: "warn",
			Event: "snapshot.slot_drop_failed",
			Error: err.Error(),
		})
	}
}

// getTablesForExport returns tables that should be included in the snapshot.
func (g *SnapshotGenerator) getTablesForExport(ctx context.Context) ([]TableInfo, error) {
	rows, err := g.pool.Query(ctx, `
		SELECT
			schemaname,
			tablename,
			schemaname || '.' || tablename as full_name,
			pg_table_size(schemaname || '.' || tablename) as size_bytes
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
		ORDER BY schemaname, tablename
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.SchemaName, &t.TableName, &t.FullName, &t.SizeBytes); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}

	return tables, rows.Err()
}

// exportTable exports a single table to a file using COPY.
func (g *SnapshotGenerator) exportTable(ctx context.Context, table TableInfo, dataDir string, compression models.CompressionType) (*models.SnapshotTableEntry, error) {
	// Determine output filename based on compression type
	filename := fmt.Sprintf("%s.%s.csv", table.SchemaName, table.TableName)
	switch compression {
	case models.CompressionGzip:
		filename += ".gz"
	case models.CompressionLZ4:
		filename += ".lz4"
	case models.CompressionZstd:
		filename += ".zst"
	}
	outputPath := filepath.Join(dataDir, filename)

	// Create output file
	file, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file %s: %w", outputPath, err)
	}
	defer file.Close()

	// Set up writer based on compression type
	var writer io.Writer = file
	var compressCloser io.Closer

	switch compression {
	case models.CompressionGzip:
		gzWriter := gzip.NewWriter(file)
		writer = gzWriter
		compressCloser = gzWriter
	case models.CompressionLZ4:
		lz4Writer := lz4.NewWriter(file)
		writer = lz4Writer
		compressCloser = lz4Writer
	case models.CompressionZstd:
		zstdWriter, err := zstd.NewWriter(file)
		if err != nil {
			return nil, fmt.Errorf("failed to create zstd writer: %w", err)
		}
		writer = zstdWriter
		compressCloser = zstdWriter
	}

	// Create a counting writer to track bytes
	countWriter := &countingWriter{w: writer}

	// Use COPY TO to export table data
	copyQuery := fmt.Sprintf(`COPY %s TO STDOUT WITH (FORMAT csv, HEADER true)`, table.FullName)

	conn, err := g.pool.Acquire(ctx)
	if err != nil {
		if compressCloser != nil {
			compressCloser.Close()
		}
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Execute COPY TO
	tag, err := conn.Conn().PgConn().CopyTo(ctx, countWriter, copyQuery)
	if err != nil {
		if compressCloser != nil {
			compressCloser.Close()
		}
		return nil, fmt.Errorf("COPY TO failed: %w", err)
	}

	// Close compression writer to flush any remaining data
	if compressCloser != nil {
		if err := compressCloser.Close(); err != nil {
			return nil, fmt.Errorf("failed to close compression writer: %w", err)
		}
	}

	// Get file size (compressed size if applicable)
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	// Calculate checksum of the output file
	checksum, err := g.calculateFileChecksum(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate checksum: %w", err)
	}

	entry := &models.SnapshotTableEntry{
		Schema:    table.SchemaName,
		Name:      table.TableName,
		RowCount:  tag.RowsAffected(),
		SizeBytes: fileInfo.Size(),
		Checksum:  checksum,
		File:      filepath.Join("data", filename),
	}

	g.logger.Log(InitEvent{
		Level: "debug",
		Event: "snapshot.table_exported",
		Details: map[string]any{
			"table":       table.FullName,
			"rows":        entry.RowCount,
			"size":        entry.SizeBytes,
			"file":        entry.File,
			"compression": string(compression),
		},
	})

	return entry, nil
}

// exportTableResult holds the result of exporting a single table.
type exportTableResult struct {
	entry *models.SnapshotTableEntry
	err   error
}

// exportTablesParallel exports tables using a worker pool.
func (g *SnapshotGenerator) exportTablesParallel(
	ctx context.Context,
	tables []TableInfo,
	dataDir string,
	compression models.CompressionType,
	workers int,
	progressFn func(completed int, current string, bytes int64),
) ([]models.SnapshotTableEntry, int64, error) {
	if len(tables) == 0 {
		return nil, 0, nil
	}

	// Channel for tables to process
	tableChan := make(chan TableInfo, len(tables))
	for _, t := range tables {
		tableChan <- t
	}
	close(tableChan)

	// Channel for results
	resultChan := make(chan exportTableResult, len(tables))

	// Progress tracking
	var completedCount int32
	var totalBytes int64
	var mu sync.Mutex

	// Context for cancellation on first error
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start workers
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for table := range tableChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				entry, err := g.exportTable(ctx, table, dataDir, compression)
				if err != nil {
					resultChan <- exportTableResult{err: fmt.Errorf("failed to export table %s: %w", table.FullName, err)}
					cancel() // Cancel other workers
					return
				}

				resultChan <- exportTableResult{entry: entry}

				// Update progress
				completed := atomic.AddInt32(&completedCount, 1)
				mu.Lock()
				totalBytes += entry.SizeBytes
				currentBytes := totalBytes
				mu.Unlock()

				if progressFn != nil {
					progressFn(int(completed), table.FullName, currentBytes)
				}
			}
		})
	}

	// Wait for workers and close results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	var entries []models.SnapshotTableEntry
	var firstErr error
	for result := range resultChan {
		if result.err != nil && firstErr == nil {
			firstErr = result.err
		}
		if result.entry != nil {
			entries = append(entries, *result.entry)
		}
	}

	if firstErr != nil {
		return nil, 0, firstErr
	}

	return entries, totalBytes, nil
}

// countingWriter wraps a writer and counts bytes written.
type countingWriter struct {
	w     io.Writer
	count int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.count += int64(n)
	return n, err
}

// calculateFileChecksum calculates SHA256 checksum of a file.
func (g *SnapshotGenerator) calculateFileChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

// getSequences returns all sequence values for the snapshot.
func (g *SnapshotGenerator) getSequences(ctx context.Context) ([]models.SnapshotSequenceEntry, error) {
	rows, err := g.pool.Query(ctx, `
		SELECT
			schemaname,
			sequencename,
			last_value
		FROM pg_sequences
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
		ORDER BY schemaname, sequencename
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sequences []models.SnapshotSequenceEntry
	for rows.Next() {
		var s models.SnapshotSequenceEntry
		var lastValue *int64
		if err := rows.Scan(&s.Schema, &s.Name, &lastValue); err != nil {
			return nil, err
		}
		if lastValue != nil {
			s.Value = *lastValue
		}
		sequences = append(sequences, s)
	}

	return sequences, rows.Err()
}

// writeManifest writes the manifest.json file.
// Implements T082: manifest.json generator.
func (g *SnapshotGenerator) writeManifest(outputPath string, manifest *models.SnapshotManifest) error {
	manifestPath := filepath.Join(outputPath, "manifest.json")

	data, err := manifest.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize manifest: %w", err)
	}

	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest file: %w", err)
	}

	// Calculate manifest checksum
	checksum := sha256.Sum256(data)
	checksumStr := hex.EncodeToString(checksum[:])

	g.logger.Log(InitEvent{
		Level: "info",
		Event: "snapshot.manifest_written",
		Details: map[string]any{
			"path":     manifestPath,
			"checksum": checksumStr,
		},
	})

	return nil
}

// recordSnapshot records the snapshot in the database.
func (g *SnapshotGenerator) recordSnapshot(ctx context.Context, manifest *models.SnapshotManifest, storagePath string) error {
	// Calculate manifest checksum
	data, err := manifest.ToJSON()
	if err != nil {
		return err
	}
	checksum := sha256.Sum256(data)
	checksumStr := "sha256:" + hex.EncodeToString(checksum[:])

	query := `
		INSERT INTO steep_repl.snapshots (
			snapshot_id, source_node_id, lsn, storage_path, size_bytes,
			table_count, compression, checksum, status, phase,
			overall_percent, tables_completed, completed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
		ON CONFLICT (snapshot_id) DO UPDATE SET
			lsn = EXCLUDED.lsn,
			storage_path = EXCLUDED.storage_path,
			size_bytes = EXCLUDED.size_bytes,
			table_count = EXCLUDED.table_count,
			checksum = EXCLUDED.checksum,
			status = EXCLUDED.status,
			phase = EXCLUDED.phase,
			overall_percent = EXCLUDED.overall_percent,
			tables_completed = EXCLUDED.tables_completed,
			completed_at = EXCLUDED.completed_at
	`

	_, err = g.pool.Exec(ctx, query,
		manifest.SnapshotID,
		manifest.SourceNode,
		manifest.LSN,
		storagePath,
		manifest.TotalSizeBytes,
		len(manifest.Tables),
		string(manifest.Compression),
		checksumStr,
		string(models.SnapshotStatusComplete),
		string(models.PhaseIdle),
		100.0,                // overall_percent
		len(manifest.Tables), // tables_completed
	)

	return err
}

// sendProgress sends a progress update if a callback is provided.
func (g *SnapshotGenerator) sendProgress(fn func(TwoPhaseProgress), progress TwoPhaseProgress) {
	if fn != nil {
		fn(progress)
	}
}

// ReadManifest reads and parses a manifest.json file.
func ReadManifest(manifestPath string) (*models.SnapshotManifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	manifest, err := models.ParseManifest(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return manifest, nil
}

// VerifySnapshot verifies the integrity of a snapshot by checking checksums.
func VerifySnapshot(snapshotPath string) ([]string, error) {
	manifestPath := filepath.Join(snapshotPath, "manifest.json")
	manifest, err := ReadManifest(manifestPath)
	if err != nil {
		return nil, err
	}

	var errors []string
	for _, table := range manifest.Tables {
		filePath := filepath.Join(snapshotPath, table.File)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			errors = append(errors, fmt.Sprintf("missing file: %s", table.File))
			continue
		}

		// Calculate checksum
		file, err := os.Open(filePath)
		if err != nil {
			errors = append(errors, fmt.Sprintf("cannot open %s: %v", table.File, err))
			continue
		}

		hasher := sha256.New()
		if _, err := io.Copy(hasher, file); err != nil {
			file.Close()
			errors = append(errors, fmt.Sprintf("cannot read %s: %v", table.File, err))
			continue
		}
		file.Close()

		actualChecksum := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
		if actualChecksum != table.Checksum {
			errors = append(errors, fmt.Sprintf("checksum mismatch for %s: expected %s, got %s",
				table.File, table.Checksum, actualChecksum))
		}
	}

	return errors, nil
}

// Ensure pgx.CopyFromSource is used (compile check)
var _ pgx.CopyFromSource = (*copyFromRows)(nil)

// copyFromRows implements pgx.CopyFromSource for bulk loading.
type copyFromRows struct {
	rows [][]any
	idx  int
	err  error
}

func (c *copyFromRows) Next() bool {
	c.idx++
	return c.idx < len(c.rows)
}

func (c *copyFromRows) Values() ([]any, error) {
	if c.idx >= len(c.rows) {
		return nil, io.EOF
	}
	return c.rows[c.idx], nil
}

func (c *copyFromRows) Err() error {
	return c.err
}
