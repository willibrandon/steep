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
		last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
		sample_params TEXT
	);

	-- Indexes for common queries
	CREATE INDEX IF NOT EXISTS idx_query_stats_total_time ON query_stats(total_time_ms DESC);
	CREATE INDEX IF NOT EXISTS idx_query_stats_calls ON query_stats(calls DESC);
	CREATE INDEX IF NOT EXISTS idx_query_stats_total_rows ON query_stats(total_rows DESC);
	CREATE INDEX IF NOT EXISTS idx_query_stats_last_seen ON query_stats(last_seen DESC);

	-- Deadlock events table
	CREATE TABLE IF NOT EXISTS deadlock_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		detected_at DATETIME NOT NULL,
		database_name TEXT NOT NULL,
		resolved_by_pid INTEGER,
		detection_time_ms INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_deadlock_events_detected_at ON deadlock_events(detected_at DESC);
	CREATE INDEX IF NOT EXISTS idx_deadlock_events_database ON deadlock_events(database_name);

	-- Processes involved in deadlocks (supports N-way deadlocks)
	CREATE TABLE IF NOT EXISTS deadlock_processes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id INTEGER NOT NULL,
		pid INTEGER NOT NULL,
		username TEXT,
		application_name TEXT,
		client_addr TEXT,
		backend_start DATETIME,
		xact_start DATETIME,
		lock_type TEXT,
		lock_mode TEXT,
		relation_name TEXT,
		query TEXT NOT NULL,
		query_fingerprint INTEGER,
		blocked_by_pid INTEGER,
		FOREIGN KEY (event_id) REFERENCES deadlock_events(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_deadlock_processes_event_id ON deadlock_processes(event_id);
	CREATE INDEX IF NOT EXISTS idx_deadlock_processes_pid ON deadlock_processes(pid);
	CREATE INDEX IF NOT EXISTS idx_deadlock_processes_relation ON deadlock_processes(relation_name);
	CREATE INDEX IF NOT EXISTS idx_deadlock_processes_fingerprint ON deadlock_processes(query_fingerprint);

	-- Log file positions for resuming parsing
	CREATE TABLE IF NOT EXISTS log_positions (
		file_path TEXT PRIMARY KEY,
		position INTEGER NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Replication lag history for trend analysis
	CREATE TABLE IF NOT EXISTS replication_lag_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		replica_name TEXT NOT NULL,
		sent_lsn TEXT,
		write_lsn TEXT,
		flush_lsn TEXT,
		replay_lsn TEXT,
		byte_lag INTEGER,
		time_lag_ms INTEGER,
		sync_state TEXT,
		direction TEXT DEFAULT 'outbound',
		conflict_count INTEGER DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_lag_history_time ON replication_lag_history(timestamp, replica_name);
	CREATE INDEX IF NOT EXISTS idx_lag_history_replica ON replication_lag_history(replica_name, timestamp);
	`

	_, err := db.conn.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: add sample_params column if it doesn't exist (for existing databases)
	_, _ = db.conn.Exec("ALTER TABLE query_stats ADD COLUMN sample_params TEXT")

	return nil
}
