package logs

import (
	"sort"
	"time"

	"github.com/willibrandon/steep/internal/db/models"
)

const (
	// DefaultBufferCapacity is the default maximum number of log entries.
	DefaultBufferCapacity = 10000
)

// LogBuffer is a fixed-size ring buffer for log entries.
type LogBuffer struct {
	entries []models.LogEntry
	head    int    // Next write index
	size    int    // Current number of entries
	cap     int    // Maximum capacity
	seq     uint64 // Sequence number, increments on wrap
}

// NewLogBuffer creates a new log buffer with the given capacity.
func NewLogBuffer(capacity int) *LogBuffer {
	if capacity <= 0 {
		capacity = DefaultBufferCapacity
	}
	return &LogBuffer{
		entries: make([]models.LogEntry, capacity),
		cap:     capacity,
	}
}

// Add adds a log entry to the buffer.
// If the buffer is full, the oldest entry is overwritten.
func (b *LogBuffer) Add(entry models.LogEntry) {
	b.entries[b.head] = entry
	b.head = (b.head + 1) % b.cap
	if b.size < b.cap {
		b.size++
	} else {
		// Buffer wrapped, increment sequence
		b.seq++
	}
}

// Seq returns the current sequence number.
// This increments each time the buffer wraps and overwrites old entries.
func (b *LogBuffer) Seq() uint64 {
	return b.seq
}

// AddAll adds multiple log entries to the buffer.
func (b *LogBuffer) AddAll(entries []models.LogEntry) {
	for _, entry := range entries {
		b.Add(entry)
	}
}

// GetRange returns entries from start index with count limit.
// Entries are returned in chronological order (oldest first).
func (b *LogBuffer) GetRange(start, count int) []models.LogEntry {
	if b.size == 0 || count <= 0 || start >= b.size {
		return nil
	}

	// Clamp count to available entries
	if start+count > b.size {
		count = b.size - start
	}

	result := make([]models.LogEntry, count)

	if b.size < b.cap {
		// Buffer not full, entries are at start of slice
		copy(result, b.entries[start:start+count])
	} else {
		// Buffer is full, head points to oldest entry
		oldest := b.head
		for i := 0; i < count; i++ {
			idx := (oldest + start + i) % b.cap
			result[i] = b.entries[idx]
		}
	}

	return result
}

// GetAll returns all entries in chronological order (oldest first).
func (b *LogBuffer) GetAll() []models.LogEntry {
	return b.GetRange(0, b.size)
}

// Get returns the entry at the given index (0 = oldest).
func (b *LogBuffer) Get(index int) (models.LogEntry, bool) {
	if index < 0 || index >= b.size {
		return models.LogEntry{}, false
	}

	if b.size < b.cap {
		return b.entries[index], true
	}

	// Buffer is full, head points to oldest entry
	idx := (b.head + index) % b.cap
	return b.entries[idx], true
}

// Newest returns the most recent entry.
func (b *LogBuffer) Newest() (models.LogEntry, bool) {
	if b.size == 0 {
		return models.LogEntry{}, false
	}

	idx := b.head - 1
	if idx < 0 {
		idx = b.cap - 1
	}
	return b.entries[idx], true
}

// Oldest returns the oldest entry.
func (b *LogBuffer) Oldest() (models.LogEntry, bool) {
	if b.size == 0 {
		return models.LogEntry{}, false
	}

	if b.size < b.cap {
		return b.entries[0], true
	}

	// Buffer is full, head points to oldest entry
	return b.entries[b.head], true
}

// Clear removes all entries from the buffer.
func (b *LogBuffer) Clear() {
	b.head = 0
	b.size = 0
}

// Len returns the current number of entries.
func (b *LogBuffer) Len() int {
	return b.size
}

// Cap returns the maximum capacity.
func (b *LogBuffer) Cap() int {
	return b.cap
}

// IsFull returns true if the buffer is at capacity.
func (b *LogBuffer) IsFull() bool {
	return b.size == b.cap
}

// FindByTimestamp finds the index of the entry closest to the given timestamp.
// Returns the index of the entry at or just before the timestamp.
// Returns -1 if the buffer is empty or timestamp is before all entries.
func (b *LogBuffer) FindByTimestamp(ts time.Time) int {
	return b.FindByTimestampWithDirection(ts, SearchClosest)
}

// FindByTimestampWithDirection finds an entry by timestamp with direction control.
// - SearchClosest: finds the entry closest to the target time
// - SearchAfter: finds the first entry at or after the target time
// - SearchBefore: finds the last entry at or before the target time
// Returns -1 if no matching entry is found.
func (b *LogBuffer) FindByTimestampWithDirection(ts time.Time, direction SearchDirection) int {
	if b.size == 0 {
		return -1
	}

	entries := b.GetAll()

	// Binary search for the first entry after timestamp
	idx := sort.Search(len(entries), func(i int) bool {
		return entries[i].Timestamp.After(ts)
	})

	switch direction {
	case SearchAfter:
		// Find first entry at or after ts
		// idx is the first entry strictly after ts
		// Check if idx-1 is exactly at ts
		if idx > 0 && !entries[idx-1].Timestamp.Before(ts) {
			return idx - 1
		}
		if idx >= len(entries) {
			return -1 // No entry at or after ts
		}
		return idx

	case SearchBefore:
		// Find last entry at or before ts
		if idx == 0 {
			return -1 // No entry at or before ts
		}
		return idx - 1

	default: // SearchClosest
		if idx == 0 {
			return 0
		}
		if idx >= len(entries) {
			return len(entries) - 1
		}

		// Check which is closer: idx or idx-1
		diffBefore := ts.Sub(entries[idx-1].Timestamp)
		diffAfter := entries[idx].Timestamp.Sub(ts)

		if diffBefore < 0 {
			diffBefore = -diffBefore
		}
		if diffAfter < 0 {
			diffAfter = -diffAfter
		}

		if diffBefore <= diffAfter {
			return idx - 1
		}
		return idx
	}
}
