package init

import (
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/repl/models"
)

// =============================================================================
// Progress Tracking Infrastructure (T087c, T087d)
// =============================================================================

// SnapshotProgressTracker tracks and persists two-phase snapshot progress.
// This enables real-time visibility into generation and application progress.
type SnapshotProgressTracker struct {
	pool           *pgxpool.Pool
	snapshotID     string
	phase          models.SnapshotPhase
	startedAt      time.Time
	throughput     *models.RollingThroughput
	progressFn     func(models.SnapshotProgress)
	updateInterval time.Duration // How often to emit progress updates (default 500ms)
	lastUpdate     time.Time

	// Current progress state
	mu                     sync.RWMutex
	currentStep            models.SnapshotStep
	tablesTotal            int
	tablesCompleted        int
	currentTable           string
	currentTableBytes      int64
	currentTableTotalBytes int64
	bytesWritten           int64
	bytesTotal             int64
	rowsWritten            int64
	rowsTotal              int64
	compressionEnabled     bool
	compressionRatio       float64
	checksumVerifications  int
	checksumsVerified      int
	checksumsFailedField   int
	errorMessage           *string
}

// NewSnapshotProgressTracker creates a new progress tracker.
func NewSnapshotProgressTracker(pool *pgxpool.Pool, snapshotID string, phase models.SnapshotPhase) *SnapshotProgressTracker {
	return &SnapshotProgressTracker{
		pool:           pool,
		snapshotID:     snapshotID,
		phase:          phase,
		startedAt:      time.Now(),
		throughput:     models.NewRollingThroughput(10), // 10-second rolling average
		updateInterval: 500 * time.Millisecond,
		currentStep:    models.SnapshotStepSchema,
	}
}

// SetProgressCallback sets a callback function for progress updates.
func (t *SnapshotProgressTracker) SetProgressCallback(fn func(models.SnapshotProgress)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.progressFn = fn
}

// SetUpdateInterval sets how often progress updates are emitted.
func (t *SnapshotProgressTracker) SetUpdateInterval(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.updateInterval = d
}

// SetTotals sets the expected totals for progress calculation.
func (t *SnapshotProgressTracker) SetTotals(tablesTotal int, bytesTotal, rowsTotal int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tablesTotal = tablesTotal
	t.bytesTotal = bytesTotal
	t.rowsTotal = rowsTotal
}

// SetCompressionEnabled marks compression as enabled for ratio tracking.
func (t *SnapshotProgressTracker) SetCompressionEnabled(enabled bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.compressionEnabled = enabled
}

// SetChecksumTotal sets the total number of checksum verifications expected.
func (t *SnapshotProgressTracker) SetChecksumTotal(total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.checksumVerifications = total
}

// UpdateStep updates the current step of the operation.
func (t *SnapshotProgressTracker) UpdateStep(step models.SnapshotStep) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentStep = step
	t.emitProgressLocked()
}

// StartTable marks the beginning of processing a table.
func (t *SnapshotProgressTracker) StartTable(tableName string, totalBytes int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentTable = tableName
	t.currentTableBytes = 0
	t.currentTableTotalBytes = totalBytes
	t.emitProgressLocked()
}

// UpdateTableProgress updates progress within the current table.
func (t *SnapshotProgressTracker) UpdateTableProgress(bytesProcessed, rowsProcessed int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.currentTableBytes = bytesProcessed
	t.rowsWritten += rowsProcessed

	// Update throughput tracking
	t.throughput.Add(t.bytesWritten+bytesProcessed, t.rowsWritten)

	// Only emit if enough time has passed
	if time.Since(t.lastUpdate) >= t.updateInterval {
		t.emitProgressLocked()
	}
}

// CompleteTable marks a table as completed.
func (t *SnapshotProgressTracker) CompleteTable(bytesProcessed int64, compressionRatio float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.tablesCompleted++
	t.bytesWritten += bytesProcessed

	// Update compression ratio (average)
	if t.compressionEnabled && compressionRatio > 0 {
		if t.compressionRatio == 0 {
			t.compressionRatio = compressionRatio
		} else {
			// Rolling average
			t.compressionRatio = (t.compressionRatio*float64(t.tablesCompleted-1) + compressionRatio) / float64(t.tablesCompleted)
		}
	}

	t.throughput.Add(t.bytesWritten, t.rowsWritten)
	t.currentTable = ""
	t.currentTableBytes = 0
	t.currentTableTotalBytes = 0
	t.emitProgressLocked()
}

// RecordChecksumResult records the result of a checksum verification.
func (t *SnapshotProgressTracker) RecordChecksumResult(passed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if passed {
		t.checksumsVerified++
	} else {
		t.checksumsFailedField++
	}

	// Only emit if enough time has passed
	if time.Since(t.lastUpdate) >= t.updateInterval {
		t.emitProgressLocked()
	}
}

// SetError records an error in the progress.
func (t *SnapshotProgressTracker) SetError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err != nil {
		msg := err.Error()
		t.errorMessage = &msg
	}
	t.emitProgressLocked()
}

// GetProgress returns a snapshot of the current progress.
func (t *SnapshotProgressTracker) GetProgress() models.SnapshotProgress {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.buildProgressLocked()
}

// emitProgressLocked emits progress if a callback is set. Must be called with lock held.
func (t *SnapshotProgressTracker) emitProgressLocked() {
	t.lastUpdate = time.Now()

	if t.progressFn != nil {
		t.progressFn(t.buildProgressLocked())
	}
}

// buildProgressLocked builds a SnapshotProgress struct. Must be called with lock held.
func (t *SnapshotProgressTracker) buildProgressLocked() models.SnapshotProgress {
	progress := models.SnapshotProgress{
		SnapshotID:             t.snapshotID,
		Phase:                  t.phase,
		CurrentStep:            t.currentStep,
		TablesTotal:            t.tablesTotal,
		TablesCompleted:        t.tablesCompleted,
		CurrentTableBytes:      t.currentTableBytes,
		CurrentTableTotalBytes: t.currentTableTotalBytes,
		BytesWritten:           t.bytesWritten,
		BytesTotal:             t.bytesTotal,
		RowsWritten:            t.rowsWritten,
		RowsTotal:              t.rowsTotal,
		ThroughputBytesSec:     t.throughput.BytesPerSec(),
		ThroughputRowsSec:      t.throughput.RowsPerSec(),
		StartedAt:              t.startedAt,
		CompressionEnabled:     t.compressionEnabled,
		CompressionRatio:       t.compressionRatio,
		ChecksumVerifications:  t.checksumVerifications,
		ChecksumsVerified:      t.checksumsVerified,
		ChecksumsFailed:        t.checksumsFailedField,
		UpdatedAt:              time.Now(),
		ErrorMessage:           t.errorMessage,
	}

	if t.currentTable != "" {
		progress.CurrentTable = &t.currentTable
	}

	// Calculate overall percent
	progress.OverallPercent = t.calculateOverallPercent()

	// Calculate ETA
	remainingBytes := t.bytesTotal - t.bytesWritten
	progress.ETASeconds = t.throughput.EstimateETA(remainingBytes)

	return progress
}

// calculateOverallPercent calculates the overall progress percentage.
func (t *SnapshotProgressTracker) calculateOverallPercent() float64 {
	// Weight each step
	stepWeights := map[models.SnapshotStep]float64{
		models.SnapshotStepSchema:     5,
		models.SnapshotStepTables:     80,
		models.SnapshotStepSequences:  5,
		models.SnapshotStepChecksums:  5,
		models.SnapshotStepFinalizing: 5,
	}

	basePercent := 0.0
	currentWeight := stepWeights[t.currentStep]

	// Add completed step percentages
	for step, weight := range stepWeights {
		if t.stepCompleted(step) {
			basePercent += weight
		}
	}

	// Add progress within current step
	var currentStepProgress float64
	switch t.currentStep {
	case models.SnapshotStepTables:
		if t.tablesTotal > 0 {
			// Table completion + current table progress
			tablesDone := float64(t.tablesCompleted) / float64(t.tablesTotal)
			var currentTableProgress float64
			if t.currentTableTotalBytes > 0 {
				currentTableProgress = float64(t.currentTableBytes) / float64(t.currentTableTotalBytes) / float64(t.tablesTotal)
			}
			currentStepProgress = tablesDone + currentTableProgress
		}
	case models.SnapshotStepChecksums:
		if t.checksumVerifications > 0 {
			currentStepProgress = float64(t.checksumsVerified+t.checksumsFailedField) / float64(t.checksumVerifications)
		}
	default:
		// For other steps, we don't have granular progress
		currentStepProgress = 0.5 // Assume midway
	}

	return basePercent + (currentStepProgress * currentWeight)
}

// stepCompleted returns true if the given step is fully completed.
func (t *SnapshotProgressTracker) stepCompleted(step models.SnapshotStep) bool {
	stepOrder := []models.SnapshotStep{
		models.SnapshotStepSchema,
		models.SnapshotStepTables,
		models.SnapshotStepSequences,
		models.SnapshotStepChecksums,
		models.SnapshotStepFinalizing,
	}

	currentIdx := -1
	stepIdx := -1
	for i, s := range stepOrder {
		if s == t.currentStep {
			currentIdx = i
		}
		if s == step {
			stepIdx = i
		}
	}

	return stepIdx < currentIdx
}
