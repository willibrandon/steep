package models

import (
	"encoding/json"
	"time"
)

// CoordinatorState represents a key-value entry for cluster-wide coordination data.
// Used by the elected coordinator to store cluster state.
type CoordinatorState struct {
	Key       string    `db:"key" json:"key"`
	Value     []byte    `db:"value" json:"-"` // JSONB stored as bytes
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// Reserved state keys (populated by later features).
const (
	// StateKeyClusterVersion tracks schema version for upgrade detection.
	StateKeyClusterVersion = "cluster_version"
	// StateKeyRangeAllocator tracks next available identity range (014-c).
	StateKeyRangeAllocator = "range_allocator"
	// StateKeyDDLSequence tracks DDL operation sequence number (014-e).
	StateKeyDDLSequence = "ddl_sequence"
)

// GetValueAs unmarshals the JSONB value into the provided target.
func (s *CoordinatorState) GetValueAs(target interface{}) error {
	if s.Value == nil {
		return nil
	}
	return json.Unmarshal(s.Value, target)
}

// SetValue marshals the provided value to JSONB bytes.
func (s *CoordinatorState) SetValue(value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.Value = data
	return nil
}

// Validate checks that the state entry has valid field values.
func (s *CoordinatorState) Validate() error {
	if s.Key == "" {
		return ErrStateKeyRequired
	}
	if s.Value == nil {
		return ErrStateValueRequired
	}
	// Verify value is valid JSON
	var js json.RawMessage
	if err := json.Unmarshal(s.Value, &js); err != nil {
		return ErrInvalidJSONValue
	}
	return nil
}
