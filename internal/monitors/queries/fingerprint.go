// Package queries provides query performance monitoring.
package queries

import (
	"regexp"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// limitOffsetRe matches LIMIT and OFFSET clauses with placeholders at end of query.
// This handles various patterns:
//   - LIMIT $N
//   - OFFSET $N
//   - LIMIT $N OFFSET $N
//   - LIMIT $N OFFSET $N (in any order)
var limitOffsetRe = regexp.MustCompile(`(?i)\s+(LIMIT\s+\$\d+(\s+OFFSET\s+\$\d+)?|OFFSET\s+\$\d+(\s+LIMIT\s+\$\d+)?)\s*$`)

// Fingerprinter normalizes SQL queries and generates fingerprints.
type Fingerprinter struct{}

// NewFingerprinter creates a new Fingerprinter.
func NewFingerprinter() *Fingerprinter {
	return &Fingerprinter{}
}

// Fingerprint normalizes a query and returns its fingerprint hash.
// The fingerprint is a unique identifier for the query pattern.
func (f *Fingerprinter) Fingerprint(query string) (uint64, string, error) {
	// Strip trailing semicolon before normalizing to ensure consistent fingerprints
	// across different log formats (some include semicolon, some don't)
	query = strings.TrimSpace(query)
	query = strings.TrimSuffix(query, ";")

	// Normalize the query (replaces literals with placeholders)
	normalized, err := pg_query.Normalize(query)
	if err != nil {
		// If normalization fails, use the original query
		normalized = query
	}

	// Strip LIMIT/OFFSET from the normalized query for fingerprinting and display.
	// This ensures that queries with pagination (e.g., from SQL editor) match
	// the same fingerprint as queries without pagination, and display without
	// the pagination clauses.
	fingerprintQuery := stripLimitOffset(normalized)

	// Generate fingerprint hash from the stripped query
	fingerprint := pg_query.HashXXH3_64([]byte(fingerprintQuery), 0)

	return fingerprint, fingerprintQuery, nil
}

// stripLimitOffset removes LIMIT and OFFSET clauses from a normalized query.
// This is used to ensure consistent fingerprints regardless of pagination.
func stripLimitOffset(query string) string {
	return limitOffsetRe.ReplaceAllString(query, "")
}

// Normalize normalizes a query by replacing literals with placeholders.
func (f *Fingerprinter) Normalize(query string) (string, error) {
	return pg_query.Normalize(query)
}
