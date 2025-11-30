package models

import "time"

// MaintenanceOperation represents an in-progress or completed maintenance operation.
type MaintenanceOperation struct {
	ID           string
	Type         OperationType
	TargetSchema string
	TargetTable  string
	TargetIndex  string // For REINDEX INDEX
	Status       OperationStatus
	Progress     *OperationProgress
	BackendPID   int
	StartedAt    time.Time
	CompletedAt  *time.Time
	Duration     time.Duration // Calculated
	Error        error
	Result       *OperationResult
}

// OperationProgress contains real-time progress data for trackable operations.
type OperationProgress struct {
	PID              int // Backend PID (for cancellation)
	Phase            string
	HeapBlksTotal    int64
	HeapBlksScanned  int64
	HeapBlksVacuumed int64
	IndexVacuumCount int64
	IndexesTotal     int64
	IndexesProcessed int64
	PercentComplete  float64
	LastUpdated      time.Time
}

// CalculatePercent calculates the overall progress percentage.
func (p *OperationProgress) CalculatePercent() float64 {
	if p.HeapBlksTotal == 0 {
		return 0
	}
	return float64(p.HeapBlksScanned) / float64(p.HeapBlksTotal) * 100
}

// OperationResult contains result data for completed maintenance operations.
type OperationResult struct {
	DeadTuplesRemoved int64
	PagesReclaimed    int64
	IndexesRebuilt    int
	StatisticsUpdated bool
	Message           string
}

// OperationHistory tracks session-scoped history of executed operations (not persisted).
type OperationHistory struct {
	Operations []MaintenanceOperation
	MaxEntries int
}

// NewOperationHistory creates a new operation history with the specified max entries.
func NewOperationHistory(maxEntries int) *OperationHistory {
	if maxEntries <= 0 {
		maxEntries = 100
	}
	return &OperationHistory{
		Operations: make([]MaintenanceOperation, 0),
		MaxEntries: maxEntries,
	}
}

// Add adds an operation to the history, evicting oldest if at capacity.
func (h *OperationHistory) Add(op MaintenanceOperation) {
	if len(h.Operations) >= h.MaxEntries {
		// FIFO eviction - remove oldest
		h.Operations = h.Operations[1:]
	}
	h.Operations = append(h.Operations, op)
}

// Recent returns the most recent operations (up to count).
func (h *OperationHistory) Recent(count int) []MaintenanceOperation {
	if count <= 0 || len(h.Operations) == 0 {
		return nil
	}
	if count > len(h.Operations) {
		count = len(h.Operations)
	}
	// Return in reverse order (most recent first)
	result := make([]MaintenanceOperation, count)
	for i := 0; i < count; i++ {
		result[i] = h.Operations[len(h.Operations)-1-i]
	}
	return result
}

// Clear removes all operations from history.
func (h *OperationHistory) Clear() {
	h.Operations = h.Operations[:0]
}
