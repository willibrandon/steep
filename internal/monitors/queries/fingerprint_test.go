package queries

import "testing"

func TestStripLimitOffset(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no limit/offset",
			input:    "SELECT * FROM users WHERE id = $1",
			expected: "SELECT * FROM users WHERE id = $1",
		},
		{
			name:     "limit only",
			input:    "SELECT * FROM users WHERE id = $1 LIMIT $2",
			expected: "SELECT * FROM users WHERE id = $1",
		},
		{
			name:     "offset only",
			input:    "SELECT * FROM users WHERE id = $1 OFFSET $2",
			expected: "SELECT * FROM users WHERE id = $1",
		},
		{
			name:     "limit and offset",
			input:    "SELECT * FROM users WHERE id = $1 LIMIT $2 OFFSET $3",
			expected: "SELECT * FROM users WHERE id = $1",
		},
		{
			name:     "offset and limit (reversed order)",
			input:    "SELECT * FROM users WHERE id = $1 OFFSET $2 LIMIT $3",
			expected: "SELECT * FROM users WHERE id = $1",
		},
		{
			name:     "lowercase limit/offset",
			input:    "SELECT * FROM users WHERE id = $1 limit $2 offset $3",
			expected: "SELECT * FROM users WHERE id = $1",
		},
		{
			name:     "mixed case",
			input:    "SELECT * FROM users WHERE id = $1 Limit $2 OFFSET $3",
			expected: "SELECT * FROM users WHERE id = $1",
		},
		{
			name:     "limit in subquery preserved",
			input:    "SELECT * FROM (SELECT * FROM users LIMIT 10) AS sub WHERE id = $1",
			expected: "SELECT * FROM (SELECT * FROM users LIMIT 10) AS sub WHERE id = $1",
		},
		{
			name:     "real world example with count",
			input:    `SELECT count(*) FROM public."Inventory" WHERE "ID" > $1 LIMIT $3 OFFSET $2`,
			expected: `SELECT count(*) FROM public."Inventory" WHERE "ID" > $1`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := stripLimitOffset(tc.input)
			if result != tc.expected {
				t.Errorf("stripLimitOffset(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestFingerprintIgnoresLimitOffset(t *testing.T) {
	fp := NewFingerprinter()

	// Two queries that should have the same fingerprint
	q1 := "SELECT * FROM users WHERE id = 1"
	q2 := "SELECT * FROM users WHERE id = 2 LIMIT 100 OFFSET 0"

	hash1, _, err := fp.Fingerprint(q1)
	if err != nil {
		t.Fatalf("Fingerprint(%q) error: %v", q1, err)
	}

	hash2, _, err := fp.Fingerprint(q2)
	if err != nil {
		t.Fatalf("Fingerprint(%q) error: %v", q2, err)
	}

	if hash1 != hash2 {
		t.Errorf("Fingerprints should match: %d != %d", hash1, hash2)
	}
}
