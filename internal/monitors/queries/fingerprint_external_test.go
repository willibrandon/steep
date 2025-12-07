package queries_test

import (
	"testing"

	"github.com/willibrandon/steep/internal/monitors/queries"
)

func TestFingerprinter_Fingerprint(t *testing.T) {
	fp := queries.NewFingerprinter()

	tests := []struct {
		name           string
		query1         string
		query2         string
		shouldMatch    bool
		wantNormalized string
	}{
		{
			name:           "same query with different values",
			query1:         "SELECT * FROM users WHERE id = 1",
			query2:         "SELECT * FROM users WHERE id = 999",
			shouldMatch:    true,
			wantNormalized: "SELECT * FROM users WHERE id = $1",
		},
		{
			name:           "same query with different string values",
			query1:         "SELECT * FROM users WHERE name = 'alice'",
			query2:         "SELECT * FROM users WHERE name = 'bob'",
			shouldMatch:    true,
			wantNormalized: "SELECT * FROM users WHERE name = $1",
		},
		{
			name:        "different queries",
			query1:      "SELECT * FROM users",
			query2:      "SELECT * FROM orders",
			shouldMatch: false,
		},
		{
			name:           "insert with different values",
			query1:         "INSERT INTO users (name) VALUES ('alice')",
			query2:         "INSERT INTO users (name) VALUES ('bob')",
			shouldMatch:    true,
			wantNormalized: "INSERT INTO users (name) VALUES ($1)",
		},
		{
			name:           "update with different values",
			query1:         "UPDATE users SET name = 'alice' WHERE id = 1",
			query2:         "UPDATE users SET name = 'bob' WHERE id = 2",
			shouldMatch:    true,
			wantNormalized: "UPDATE users SET name = $1 WHERE id = $2",
		},
		{
			name:           "in list with same count",
			query1:         "SELECT * FROM users WHERE id IN (1, 2, 3)",
			query2:         "SELECT * FROM users WHERE id IN (4, 5, 6)",
			shouldMatch:    true,
			wantNormalized: "SELECT * FROM users WHERE id IN ($1, $2, $3)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp1, norm1, err := fp.Fingerprint(tt.query1)
			if err != nil {
				t.Fatalf("Fingerprint(%q) error: %v", tt.query1, err)
			}

			fp2, _, err := fp.Fingerprint(tt.query2)
			if err != nil {
				t.Fatalf("Fingerprint(%q) error: %v", tt.query2, err)
			}

			if tt.shouldMatch && fp1 != fp2 {
				t.Errorf("Expected fingerprints to match, got %d != %d", fp1, fp2)
			}

			if !tt.shouldMatch && fp1 == fp2 {
				t.Errorf("Expected fingerprints to differ, got %d == %d", fp1, fp2)
			}

			if tt.wantNormalized != "" && norm1 != tt.wantNormalized {
				t.Errorf("Normalized query = %q, want %q", norm1, tt.wantNormalized)
			}
		})
	}
}

func TestFingerprinter_Normalize(t *testing.T) {
	fp := queries.NewFingerprinter()

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "select with integer",
			query: "SELECT * FROM users WHERE id = 42",
			want:  "SELECT * FROM users WHERE id = $1",
		},
		{
			name:  "select with string",
			query: "SELECT * FROM users WHERE name = 'test'",
			want:  "SELECT * FROM users WHERE name = $1",
		},
		{
			name:  "multiple parameters",
			query: "SELECT * FROM users WHERE id = 1 AND name = 'test'",
			want:  "SELECT * FROM users WHERE id = $1 AND name = $2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fp.Normalize(tt.query)
			if err != nil {
				t.Fatalf("Normalize(%q) error: %v", tt.query, err)
			}
			if got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestFingerprinter_InvalidSQL(t *testing.T) {
	fp := queries.NewFingerprinter()

	// Invalid SQL should still return a fingerprint (using original query)
	_, normalized, err := fp.Fingerprint("NOT VALID SQL AT ALL")
	if err != nil {
		t.Logf("Got expected error for invalid SQL: %v", err)
	}

	// Should return something (either normalized or original)
	if normalized == "" {
		t.Error("Expected non-empty normalized query for invalid SQL")
	}
}
