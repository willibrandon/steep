// Package queries provides query performance monitoring.
package queries

import (
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// Fingerprinter normalizes SQL queries and generates fingerprints.
type Fingerprinter struct{}

// NewFingerprinter creates a new Fingerprinter.
func NewFingerprinter() *Fingerprinter {
	return &Fingerprinter{}
}

// Fingerprint normalizes a query and returns its fingerprint hash.
// The fingerprint is a unique identifier for the query pattern.
func (f *Fingerprinter) Fingerprint(query string) (uint64, string, error) {
	// Normalize the query (replaces literals with placeholders)
	normalized, err := pg_query.Normalize(query)
	if err != nil {
		// If normalization fails, use the original query
		normalized = query
	}

	// Generate fingerprint hash
	fingerprint := pg_query.HashXXH3_64([]byte(normalized), 0)

	return fingerprint, normalized, nil
}

// Normalize normalizes a query by replacing literals with placeholders.
func (f *Fingerprinter) Normalize(query string) (string, error) {
	return pg_query.Normalize(query)
}
