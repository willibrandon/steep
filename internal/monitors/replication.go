// Package monitors provides background goroutines for fetching PostgreSQL data.
package monitors

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/logger"
	"github.com/willibrandon/steep/internal/storage/sqlite"
	"github.com/willibrandon/steep/internal/ui"
)

// ReplicationMonitor fetches replication data at regular intervals.
type ReplicationMonitor struct {
	pool     *pgxpool.Pool
	interval time.Duration
	store    *sqlite.ReplicationStore

	// In-memory lag history ring buffers (keyed by replica name)
	lagHistory     map[string]*lagRingBuffer
	lagHistoryLock sync.RWMutex

	// Configuration
	ringBufferSize   int           // Max entries in memory per replica
	persistInterval  int           // Persist every N samples
	sampleCount      int           // Current sample count
	retentionHours   int           // SQLite retention period
	lastPruneTime    time.Time     // Last time we pruned old data
	pruneInterval    time.Duration // How often to prune (default: 1 hour)
}

// lagRingBuffer is a fixed-size circular buffer for lag values.
type lagRingBuffer struct {
	values []float64
	head   int
	size   int
	cap    int
}

// newLagRingBuffer creates a new ring buffer with the given capacity.
func newLagRingBuffer(capacity int) *lagRingBuffer {
	return &lagRingBuffer{
		values: make([]float64, capacity),
		cap:    capacity,
	}
}

// Add adds a value to the ring buffer.
func (b *lagRingBuffer) Add(value float64) {
	b.values[b.head] = value
	b.head = (b.head + 1) % b.cap
	if b.size < b.cap {
		b.size++
	}
}

// GetAll returns all values in chronological order.
func (b *lagRingBuffer) GetAll() []float64 {
	if b.size == 0 {
		return nil
	}

	result := make([]float64, b.size)
	if b.size < b.cap {
		// Buffer not full yet, values are at start
		copy(result, b.values[:b.size])
	} else {
		// Buffer is full, head points to oldest value
		copy(result, b.values[b.head:])
		copy(result[b.cap-b.head:], b.values[:b.head])
	}
	return result
}

// NewReplicationMonitor creates a new ReplicationMonitor.
// If store is nil, lag history will only be kept in memory.
func NewReplicationMonitor(pool *pgxpool.Pool, interval time.Duration, store *sqlite.ReplicationStore) *ReplicationMonitor {
	return &ReplicationMonitor{
		pool:             pool,
		interval:         interval,
		store:            store,
		lagHistory:       make(map[string]*lagRingBuffer),
		ringBufferSize:   60, // ~2 minutes at 2-second intervals
		persistInterval:  30, // Persist every 30 samples (~1 minute)
		retentionHours:   24, // Default 24-hour retention
		pruneInterval:    time.Hour,
		lastPruneTime:    time.Now(),
	}
}

// SetRetentionHours sets the SQLite retention period for lag history.
func (m *ReplicationMonitor) SetRetentionHours(hours int) {
	m.retentionHours = hours
}

// FetchOnce fetches replication data once and returns the result.
func (m *ReplicationMonitor) FetchOnce(ctx context.Context) ui.ReplicationDataMsg {
	start := time.Now()
	data := models.NewReplicationData()

	// Detect server role
	isPrimary, err := queries.IsPrimary(ctx, m.pool)
	if err != nil {
		return ui.ReplicationDataMsg{
			Data:      data,
			FetchedAt: time.Now(),
			Error:     err,
		}
	}
	data.IsPrimary = isPrimary

	// Fetch replicas (only meaningful on primary)
	if isPrimary {
		replicas, err := queries.GetReplicas(ctx, m.pool)
		if err != nil {
			// Return partial data with error
			data.QueryDuration = time.Since(start)
			return ui.ReplicationDataMsg{
				Data:      data,
				FetchedAt: time.Now(),
				Error:     err,
			}
		}
		data.Replicas = replicas

		// Update in-memory lag history for each replica
		m.updateLagHistory(replicas)
	} else {
		// On standby, get WAL receiver status
		walReceiver, err := queries.GetWALReceiverStatus(ctx, m.pool)
		if err == nil && walReceiver != nil {
			data.WALReceiverStatus = walReceiver
		}
	}

	// Fetch replication slots
	slots, err := queries.GetSlots(ctx, m.pool)
	if err != nil {
		logger.Error("GetSlots failed", "error", err)
	} else {
		data.Slots = slots
		logger.Debug("GetSlots", "count", len(slots))
	}

	// Fetch publications (primary only for outgoing)
	publications, err := queries.GetPublications(ctx, m.pool)
	if err == nil {
		data.Publications = publications
	}

	// Fetch subscriptions
	subscriptions, err := queries.GetSubscriptions(ctx, m.pool)
	if err == nil {
		data.Subscriptions = subscriptions
	}

	// Copy lag history to data
	m.lagHistoryLock.RLock()
	for name, buf := range m.lagHistory {
		data.LagHistory[name] = buf.GetAll()
	}
	m.lagHistoryLock.RUnlock()

	data.QueryDuration = time.Since(start)
	data.RefreshTime = time.Now()

	// Handle persistence
	m.handlePersistence(ctx, data)

	return ui.ReplicationDataMsg{
		Data:      data,
		FetchedAt: time.Now(),
	}
}

// updateLagHistory updates the in-memory ring buffers with new replica data.
func (m *ReplicationMonitor) updateLagHistory(replicas []models.Replica) {
	m.lagHistoryLock.Lock()
	defer m.lagHistoryLock.Unlock()

	for _, r := range replicas {
		buf, exists := m.lagHistory[r.ApplicationName]
		if !exists {
			buf = newLagRingBuffer(m.ringBufferSize)
			m.lagHistory[r.ApplicationName] = buf
		}
		buf.Add(float64(r.ByteLag))
	}
}

// handlePersistence manages SQLite persistence and pruning.
func (m *ReplicationMonitor) handlePersistence(ctx context.Context, data *models.ReplicationData) {
	if m.store == nil {
		return
	}

	m.sampleCount++

	// Persist lag entries periodically
	if m.sampleCount >= m.persistInterval {
		m.sampleCount = 0
		m.persistLagEntries(ctx, data)
	}

	// Prune old data periodically
	if time.Since(m.lastPruneTime) > m.pruneInterval {
		m.lastPruneTime = time.Now()
		go func() {
			pruneCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			m.store.PruneLagHistory(pruneCtx, m.retentionHours)
		}()
	}
}

// persistLagEntries saves current lag data to SQLite.
func (m *ReplicationMonitor) persistLagEntries(ctx context.Context, data *models.ReplicationData) {
	if len(data.Replicas) == 0 {
		return
	}

	entries := make([]models.LagHistoryEntry, 0, len(data.Replicas))
	now := time.Now()

	for _, r := range data.Replicas {
		entry := models.LagHistoryEntry{
			Timestamp:   now,
			ReplicaName: r.ApplicationName,
			SentLSN:     r.SentLSN,
			WriteLSN:    r.WriteLSN,
			FlushLSN:    r.FlushLSN,
			ReplayLSN:   r.ReplayLSN,
			ByteLag:     r.ByteLag,
			TimeLagMs:   r.ReplayLag.Milliseconds(),
			SyncState:   r.SyncState.String(),
			Direction:   "outbound",
		}
		entries = append(entries, entry)
	}

	// Save in background to not block monitor
	go func() {
		saveCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		m.store.SaveLagEntries(saveCtx, entries)
	}()
}

// Run starts the monitor goroutine that sends data to the provided channel.
// It runs until the context is cancelled.
func (m *ReplicationMonitor) Run(ctx context.Context, dataChan chan<- ui.ReplicationDataMsg) {
	// Send initial data immediately
	dataChan <- m.FetchOnce(ctx)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dataChan <- m.FetchOnce(ctx)
		}
	}
}

// GetLagHistory retrieves historical lag data from SQLite for sparkline rendering.
// If store is nil or no data exists, returns the in-memory ring buffer data.
func (m *ReplicationMonitor) GetLagHistory(ctx context.Context, replicaName string, since time.Time, limit int) ([]float64, error) {
	// Try SQLite first if available
	if m.store != nil {
		values, err := m.store.GetLagValues(ctx, replicaName, since, limit)
		if err == nil && len(values) > 0 {
			return values, nil
		}
	}

	// Fall back to in-memory ring buffer
	m.lagHistoryLock.RLock()
	defer m.lagHistoryLock.RUnlock()

	if buf, exists := m.lagHistory[replicaName]; exists {
		return buf.GetAll(), nil
	}

	return nil, nil
}
