package metrics

import (
	"math"
	"time"
)

// DataPoint represents a single metric measurement at a point in time.
type DataPoint struct {
	Timestamp time.Time
	Value     float64
}

// IsValid returns true if the data point has valid values.
// A data point is invalid if the value is Inf, NaN, or the timestamp is zero.
func (dp DataPoint) IsValid() bool {
	if dp.Timestamp.IsZero() {
		return false
	}
	if math.IsInf(dp.Value, 0) || math.IsNaN(dp.Value) {
		return false
	}
	return true
}

// NewDataPoint creates a new DataPoint with the current timestamp.
func NewDataPoint(value float64) DataPoint {
	return DataPoint{
		Timestamp: time.Now(),
		Value:     value,
	}
}

// NewDataPointAt creates a new DataPoint with the specified timestamp.
func NewDataPointAt(timestamp time.Time, value float64) DataPoint {
	return DataPoint{
		Timestamp: timestamp,
		Value:     value,
	}
}
