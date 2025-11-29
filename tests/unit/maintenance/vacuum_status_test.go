// Package maintenance_test contains unit tests for maintenance functionality.
package maintenance_test

import (
	"testing"
	"time"

	"github.com/willibrandon/steep/internal/db/queries"
)

// TestFormatVacuumTimestamp verifies relative timestamp formatting.
func TestFormatVacuumTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		input    *time.Time
		expected string
	}{
		{
			name:     "nil timestamp",
			input:    nil,
			expected: "never",
		},
		{
			name:     "30 minutes ago",
			input:    timePtr(time.Now().Add(-30 * time.Minute)),
			expected: "30m ago",
		},
		{
			name:     "5 hours ago",
			input:    timePtr(time.Now().Add(-5 * time.Hour)),
			expected: "5h ago",
		},
		{
			name:     "3 days ago",
			input:    timePtr(time.Now().Add(-3 * 24 * time.Hour)),
			expected: "3d ago",
		},
		{
			name:     "10 days ago shows date",
			input:    timePtr(time.Now().Add(-10 * 24 * time.Hour)),
			expected: time.Now().Add(-10 * 24 * time.Hour).Format("Jan 02"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := queries.FormatVacuumTimestamp(tt.input)
			if result != tt.expected {
				t.Errorf("FormatVacuumTimestamp() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestGetVacuumStatusIndicator verifies status indicator logic.
func TestGetVacuumStatusIndicator(t *testing.T) {
	config := queries.StaleVacuumConfig{
		StaleThreshold:   7 * 24 * time.Hour,
		WarningThreshold: 3 * 24 * time.Hour,
	}

	tests := []struct {
		name           string
		lastVacuum     *time.Time
		lastAutovacuum *time.Time
		expected       queries.VacuumIndicator
	}{
		{
			name:           "never vacuumed - critical",
			lastVacuum:     nil,
			lastAutovacuum: nil,
			expected:       queries.VacuumIndicatorCritical,
		},
		{
			name:           "recently vacuumed - OK",
			lastVacuum:     timePtr(time.Now().Add(-1 * time.Hour)),
			lastAutovacuum: nil,
			expected:       queries.VacuumIndicatorOK,
		},
		{
			name:           "recently autovacuumed - OK",
			lastVacuum:     nil,
			lastAutovacuum: timePtr(time.Now().Add(-2 * time.Hour)),
			expected:       queries.VacuumIndicatorOK,
		},
		{
			name:           "5 days ago - warning",
			lastVacuum:     timePtr(time.Now().Add(-5 * 24 * time.Hour)),
			lastAutovacuum: nil,
			expected:       queries.VacuumIndicatorWarning,
		},
		{
			name:           "10 days ago - critical",
			lastVacuum:     timePtr(time.Now().Add(-10 * 24 * time.Hour)),
			lastAutovacuum: nil,
			expected:       queries.VacuumIndicatorCritical,
		},
		{
			name:           "old vacuum but recent autovacuum - OK",
			lastVacuum:     timePtr(time.Now().Add(-30 * 24 * time.Hour)),
			lastAutovacuum: timePtr(time.Now().Add(-1 * time.Hour)),
			expected:       queries.VacuumIndicatorOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := queries.GetVacuumStatusIndicator(tt.lastVacuum, tt.lastAutovacuum, config)
			if result != tt.expected {
				t.Errorf("GetVacuumStatusIndicator() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
