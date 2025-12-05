// Package init provides node initialization and snapshot management for bidirectional replication.
package init

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/repl/config"
)

// Manager orchestrates node initialization operations.
type Manager struct {
	pool   *pgxpool.Pool
	config *config.InitConfig

	mu       sync.RWMutex
	active   map[string]*Operation // node_id -> active operation
	progress chan ProgressUpdate
}

// Operation represents an in-progress initialization operation.
type Operation struct {
	NodeID     string
	SourceNode string
	Method     config.InitMethod
	Cancel     context.CancelFunc
}

// ProgressUpdate represents a progress update for streaming to TUI.
type ProgressUpdate struct {
	NodeID           string
	Phase            string
	OverallPercent   float32
	TablesTotal      int
	TablesCompleted  int
	CurrentTable     string
	CurrentPercent   float32
	RowsCopied       int64
	BytesCopied      int64
	ThroughputRows   float32
	ETASeconds       int
	ParallelWorkers  int
	Error            string
}

// NewManager creates a new initialization manager.
func NewManager(pool *pgxpool.Pool, cfg *config.InitConfig) *Manager {
	return &Manager{
		pool:     pool,
		config:   cfg,
		active:   make(map[string]*Operation),
		progress: make(chan ProgressUpdate, 100),
	}
}

// Progress returns the channel for receiving progress updates.
func (m *Manager) Progress() <-chan ProgressUpdate {
	return m.progress
}

// IsActive returns true if an initialization is in progress for the given node.
func (m *Manager) IsActive(nodeID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.active[nodeID]
	return ok
}

// GetOperation returns the active operation for a node, if any.
func (m *Manager) GetOperation(nodeID string) (*Operation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	op, ok := m.active[nodeID]
	return op, ok
}
