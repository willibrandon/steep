package metrics

import (
	"sync"
	"time"
)

// DefaultBufferCapacity is the default maximum number of data points.
const DefaultBufferCapacity = 10000

// CircularBuffer is a fixed-size ring buffer for DataPoints.
// It is thread-safe and automatically evicts oldest entries when full.
type CircularBuffer struct {
	data     []DataPoint
	capacity int
	head     int // Next write position
	size     int // Current element count
	mu       sync.RWMutex
}

// NewCircularBuffer creates a new CircularBuffer with the specified capacity.
func NewCircularBuffer(capacity int) *CircularBuffer {
	if capacity <= 0 {
		capacity = DefaultBufferCapacity
	}
	return &CircularBuffer{
		data:     make([]DataPoint, capacity),
		capacity: capacity,
	}
}

// Push adds a new data point, evicting the oldest if at capacity.
func (b *CircularBuffer) Push(dp DataPoint) {
	if !dp.IsValid() {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.data[b.head] = dp
	b.head = (b.head + 1) % b.capacity

	if b.size < b.capacity {
		b.size++
	}
}

// GetRecent returns the n most recent data points in chronological order.
func (b *CircularBuffer) GetRecent(n int) []DataPoint {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if n <= 0 || b.size == 0 {
		return nil
	}

	if n > b.size {
		n = b.size
	}

	result := make([]DataPoint, n)
	// Start from (head - n) and go forward
	start := (b.head - n + b.capacity) % b.capacity
	for i := 0; i < n; i++ {
		idx := (start + i) % b.capacity
		result[i] = b.data[idx]
	}

	return result
}

// GetSince returns all data points since the given timestamp in chronological order.
func (b *CircularBuffer) GetSince(since time.Time) []DataPoint {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.size == 0 {
		return nil
	}

	// Find starting position - oldest data point
	oldestIdx := (b.head - b.size + b.capacity) % b.capacity

	var result []DataPoint
	for i := 0; i < b.size; i++ {
		idx := (oldestIdx + i) % b.capacity
		if !b.data[idx].Timestamp.Before(since) {
			result = append(result, b.data[idx])
		}
	}

	return result
}

// GetValues returns all values as a float64 slice in chronological order.
// This is suitable for passing directly to asciigraph.Plot().
func (b *CircularBuffer) GetValues() []float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.size == 0 {
		return nil
	}

	result := make([]float64, b.size)
	oldestIdx := (b.head - b.size + b.capacity) % b.capacity

	for i := 0; i < b.size; i++ {
		idx := (oldestIdx + i) % b.capacity
		result[i] = b.data[idx].Value
	}

	return result
}

// GetValuesForWindow returns values for the specified time window.
func (b *CircularBuffer) GetValuesForWindow(window TimeWindow) []float64 {
	since := time.Now().Add(-window.Duration())
	points := b.GetSince(since)

	if len(points) == 0 {
		return nil
	}

	result := make([]float64, len(points))
	for i, p := range points {
		result[i] = p.Value
	}
	return result
}

// Len returns the current number of elements in the buffer.
func (b *CircularBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}

// Cap returns the capacity of the buffer.
func (b *CircularBuffer) Cap() int {
	return b.capacity
}

// Clear resets the buffer, removing all data points.
func (b *CircularBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.head = 0
	b.size = 0
	// Zero out the data for GC
	for i := range b.data {
		b.data[i] = DataPoint{}
	}
}

// Latest returns the most recent data point.
// Returns zero DataPoint and false if buffer is empty.
func (b *CircularBuffer) Latest() (DataPoint, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.size == 0 {
		return DataPoint{}, false
	}

	// head points to next write position, so latest is at head-1
	latestIdx := (b.head - 1 + b.capacity) % b.capacity
	return b.data[latestIdx], true
}

// IsEmpty returns true if the buffer contains no data points.
func (b *CircularBuffer) IsEmpty() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size == 0
}

// IsFull returns true if the buffer is at capacity.
func (b *CircularBuffer) IsFull() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size == b.capacity
}
