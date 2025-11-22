package sqlite

// initSchema creates the database schema if it doesn't exist.
func (db *DB) initSchema() error {
	schema := `
	-- Query statistics table
	CREATE TABLE IF NOT EXISTS query_stats (
		fingerprint INTEGER PRIMARY KEY,
		normalized_query TEXT NOT NULL,
		calls INTEGER NOT NULL DEFAULT 0,
		total_time_ms REAL NOT NULL DEFAULT 0,
		min_time_ms REAL,
		max_time_ms REAL,
		total_rows INTEGER NOT NULL DEFAULT 0,
		first_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_seen DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Indexes for common queries
	CREATE INDEX IF NOT EXISTS idx_query_stats_total_time ON query_stats(total_time_ms DESC);
	CREATE INDEX IF NOT EXISTS idx_query_stats_calls ON query_stats(calls DESC);
	CREATE INDEX IF NOT EXISTS idx_query_stats_total_rows ON query_stats(total_rows DESC);
	CREATE INDEX IF NOT EXISTS idx_query_stats_last_seen ON query_stats(last_seen DESC);
	`

	_, err := db.conn.Exec(schema)
	return err
}
