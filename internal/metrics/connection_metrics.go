package metrics

import (
	"sync"
	"time"
)

// DefaultConnectionHistorySize is the number of duration samples per connection.
const DefaultConnectionHistorySize = 10

// DefaultMaxConnections is the maximum number of connections to track.
const DefaultMaxConnections = 1000

// ConnectionDuration represents a single query duration sample.
type ConnectionDuration struct {
	Duration  time.Duration
	Timestamp time.Time
}

// ConnectionHistory tracks duration history for a single connection.
type ConnectionHistory struct {
	PID          int
	Durations    []ConnectionDuration
	LastAccessed time.Time
}

// ConnectionMetrics tracks query duration history per connection for sparkline rendering.
// It uses an LRU eviction policy when the connection count exceeds the maximum.
type ConnectionMetrics struct {
	mu          sync.RWMutex
	connections map[int]*ConnectionHistory
	maxConns    int
	historySize int
}

// NewConnectionMetrics creates a new ConnectionMetrics tracker.
func NewConnectionMetrics() *ConnectionMetrics {
	return &ConnectionMetrics{
		connections: make(map[int]*ConnectionHistory),
		maxConns:    DefaultMaxConnections,
		historySize: DefaultConnectionHistorySize,
	}
}

// NewConnectionMetricsWithSize creates a ConnectionMetrics with custom limits.
func NewConnectionMetricsWithSize(maxConns, historySize int) *ConnectionMetrics {
	if maxConns <= 0 {
		maxConns = DefaultMaxConnections
	}
	if historySize <= 0 {
		historySize = DefaultConnectionHistorySize
	}
	return &ConnectionMetrics{
		connections: make(map[int]*ConnectionHistory),
		maxConns:    maxConns,
		historySize: historySize,
	}
}

// Record adds a duration sample for the given PID.
// If the connection doesn't exist, it creates a new history.
// If at capacity, evicts the least recently accessed connection.
func (cm *ConnectionMetrics) Record(pid int, duration time.Duration) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	now := time.Now()

	// Get or create connection history
	history, exists := cm.connections[pid]
	if !exists {
		// Check capacity and evict if needed
		if len(cm.connections) >= cm.maxConns {
			cm.evictLRU()
		}
		history = &ConnectionHistory{
			PID:       pid,
			Durations: make([]ConnectionDuration, 0, cm.historySize),
		}
		cm.connections[pid] = history
	}

	// Update last accessed time
	history.LastAccessed = now

	// Add new duration sample
	sample := ConnectionDuration{
		Duration:  duration,
		Timestamp: now,
	}

	// Maintain fixed size by removing oldest if at capacity
	if len(history.Durations) >= cm.historySize {
		// Shift left, dropping oldest
		copy(history.Durations, history.Durations[1:])
		history.Durations[len(history.Durations)-1] = sample
	} else {
		history.Durations = append(history.Durations, sample)
	}
}

// GetDurations returns the duration history for a PID as float64 seconds.
// Returns nil if PID not found.
func (cm *ConnectionMetrics) GetDurations(pid int) []float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	history, exists := cm.connections[pid]
	if !exists || len(history.Durations) == 0 {
		return nil
	}

	result := make([]float64, len(history.Durations))
	for i, d := range history.Durations {
		result[i] = d.Duration.Seconds()
	}
	return result
}

// GetHistory returns the ConnectionHistory for a PID.
// Returns nil if PID not found.
func (cm *ConnectionMetrics) GetHistory(pid int) *ConnectionHistory {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	history, exists := cm.connections[pid]
	if !exists {
		return nil
	}

	// Return a copy to avoid concurrent access issues
	copyHistory := &ConnectionHistory{
		PID:          history.PID,
		LastAccessed: history.LastAccessed,
		Durations:    make([]ConnectionDuration, len(history.Durations)),
	}
	copy(copyHistory.Durations, history.Durations)
	return copyHistory
}

// Remove deletes the history for a PID (e.g., when connection ends).
func (cm *ConnectionMetrics) Remove(pid int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.connections, pid)
}

// Clear removes all connection histories.
func (cm *ConnectionMetrics) Clear() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.connections = make(map[int]*ConnectionHistory)
}

// Len returns the number of tracked connections.
func (cm *ConnectionMetrics) Len() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.connections)
}

// evictLRU removes the least recently accessed connection.
// Must be called with lock held.
func (cm *ConnectionMetrics) evictLRU() {
	if len(cm.connections) == 0 {
		return
	}

	var oldestPID int
	var oldestTime time.Time
	first := true

	for pid, history := range cm.connections {
		if first || history.LastAccessed.Before(oldestTime) {
			oldestPID = pid
			oldestTime = history.LastAccessed
			first = false
		}
	}

	delete(cm.connections, oldestPID)
}

// Prune removes connections that haven't been accessed since the given time.
// Useful for cleaning up stale connection data.
func (cm *ConnectionMetrics) Prune(since time.Time) int {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	pruned := 0
	for pid, history := range cm.connections {
		if history.LastAccessed.Before(since) {
			delete(cm.connections, pid)
			pruned++
		}
	}
	return pruned
}

// UpdateFromConnections updates metrics from a list of active connections.
// This is called after each activity data refresh.
func (cm *ConnectionMetrics) UpdateFromConnections(connections []ConnectionInfo) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	now := time.Now()
	activePIDs := make(map[int]bool)

	for _, conn := range connections {
		activePIDs[conn.PID] = true

		// Skip if not actively running a query
		if conn.DurationSeconds <= 0 {
			continue
		}

		duration := time.Duration(conn.DurationSeconds) * time.Second

		// Get or create connection history
		history, exists := cm.connections[conn.PID]
		if !exists {
			if len(cm.connections) >= cm.maxConns {
				cm.evictLRU()
			}
			history = &ConnectionHistory{
				PID:       conn.PID,
				Durations: make([]ConnectionDuration, 0, cm.historySize),
			}
			cm.connections[conn.PID] = history
		}

		history.LastAccessed = now

		// Only add if this is a new/different duration
		// (avoid recording same query multiple times)
		shouldAdd := true
		if len(history.Durations) > 0 {
			last := history.Durations[len(history.Durations)-1]
			// If duration is similar and recent, skip
			if now.Sub(last.Timestamp) < 500*time.Millisecond {
				shouldAdd = false
			}
		}

		if shouldAdd {
			sample := ConnectionDuration{
				Duration:  duration,
				Timestamp: now,
			}

			if len(history.Durations) >= cm.historySize {
				copy(history.Durations, history.Durations[1:])
				history.Durations[len(history.Durations)-1] = sample
			} else {
				history.Durations = append(history.Durations, sample)
			}
		}
	}
}

// ConnectionInfo is a minimal interface for connection data.
type ConnectionInfo struct {
	PID             int
	DurationSeconds int
}
