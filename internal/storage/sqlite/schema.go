package sqlite

import "strings"

// initSchema creates the database schema if it doesn't exist.
func (db *DB) initSchema() error {
	schema := `
	-- Query statistics table (supports multi-instance monitoring)
	-- Uses composite primary key (fingerprint, instance_name) so same query
	-- can be tracked separately for each PostgreSQL instance
	CREATE TABLE IF NOT EXISTS query_stats (
		fingerprint INTEGER NOT NULL,
		normalized_query TEXT NOT NULL,
		calls INTEGER NOT NULL DEFAULT 0,
		total_time_ms REAL NOT NULL DEFAULT 0,
		min_time_ms REAL,
		max_time_ms REAL,
		total_rows INTEGER NOT NULL DEFAULT 0,
		first_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
		sample_params TEXT,
		instance_name TEXT NOT NULL DEFAULT 'default',
		PRIMARY KEY (fingerprint, instance_name)
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

	-- SQL Editor query history (shell-style: fingerprint is unique, re-running updates timestamp)
	CREATE TABLE IF NOT EXISTS query_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		fingerprint INTEGER,
		query TEXT NOT NULL,
		executed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		duration_ms INTEGER DEFAULT 0,
		row_count INTEGER DEFAULT 0,
		error TEXT DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_query_history_executed_at ON query_history(executed_at DESC);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_query_history_fingerprint ON query_history(fingerprint);

	-- Log viewer command history (shell-style: content is unique, re-running updates timestamp)
	CREATE TABLE IF NOT EXISTS log_command_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		command TEXT NOT NULL UNIQUE,
		executed_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_log_command_history_executed_at ON log_command_history(executed_at DESC);

	-- Log viewer search history (shell-style: pattern is unique, re-searching updates timestamp)
	CREATE TABLE IF NOT EXISTS log_search_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		pattern TEXT NOT NULL UNIQUE,
		executed_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_log_search_history_executed_at ON log_search_history(executed_at DESC);

	-- Metrics history for visualization time-series data
	-- key column allows entity-specific metrics (e.g., table sizes use key='schema.table')
	-- Dashboard metrics use key='' (empty string)
	CREATE TABLE IF NOT EXISTS metrics_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		metric_name TEXT NOT NULL,
		key TEXT NOT NULL DEFAULT '',
		value REAL NOT NULL
	);

	-- Index for time-based cleanup (doesn't reference key column for backwards compat)
	CREATE INDEX IF NOT EXISTS idx_metrics_history_timestamp ON metrics_history(timestamp);

	-- Alert events history table
	CREATE TABLE IF NOT EXISTS alert_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		rule_name TEXT NOT NULL,
		prev_state TEXT NOT NULL CHECK(prev_state IN ('normal', 'warning', 'critical')),
		new_state TEXT NOT NULL CHECK(new_state IN ('normal', 'warning', 'critical')),
		metric_value REAL NOT NULL,
		threshold_value REAL,
		triggered_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		acknowledged_at DATETIME,
		acknowledged_by TEXT
	);

	-- Index for history view (most recent first)
	CREATE INDEX IF NOT EXISTS idx_alert_events_triggered ON alert_events(triggered_at DESC);

	-- Index for rule-specific queries
	CREATE INDEX IF NOT EXISTS idx_alert_events_rule ON alert_events(rule_name, triggered_at DESC);

	-- Index for filtering by severity
	CREATE INDEX IF NOT EXISTS idx_alert_events_state ON alert_events(new_state, triggered_at DESC);
	`

	_, err := db.conn.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: add sample_params column if it doesn't exist (for existing databases)
	_, _ = db.conn.Exec("ALTER TABLE query_stats ADD COLUMN sample_params TEXT")

	// Migration: add fingerprint column to query_history if it doesn't exist
	_, _ = db.conn.Exec("ALTER TABLE query_history ADD COLUMN fingerprint INTEGER")
	_, _ = db.conn.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_query_history_fingerprint ON query_history(fingerprint)")

	// Migration: add key column to metrics_history if it doesn't exist
	_, _ = db.conn.Exec("ALTER TABLE metrics_history ADD COLUMN key TEXT NOT NULL DEFAULT ''")
	// Drop old index and create new composite index
	_, _ = db.conn.Exec("DROP INDEX IF EXISTS idx_metrics_history_name_time")
	_, _ = db.conn.Exec("CREATE INDEX IF NOT EXISTS idx_metrics_history_name_key_time ON metrics_history(metric_name, key, timestamp)")

	// Migration: drop deprecated keyed_metrics_history table if it exists
	_, _ = db.conn.Exec("DROP TABLE IF EXISTS keyed_metrics_history")

	// Migration: add instance_name column to other tables for multi-instance agent support
	// (query_stats now has instance_name in the initial schema with composite PK)
	instanceNameMigrations := []string{
		"ALTER TABLE deadlock_events ADD COLUMN instance_name TEXT NOT NULL DEFAULT 'default'",
		"ALTER TABLE replication_lag_history ADD COLUMN instance_name TEXT NOT NULL DEFAULT 'default'",
		"ALTER TABLE metrics_history ADD COLUMN instance_name TEXT NOT NULL DEFAULT 'default'",
		"ALTER TABLE alert_events ADD COLUMN instance_name TEXT NOT NULL DEFAULT 'default'",
	}
	for _, migration := range instanceNameMigrations {
		// Ignore errors (column may already exist)
		_, _ = db.conn.Exec(migration)
	}

	// Create indexes for efficient instance-based queries
	instanceNameIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_query_stats_instance ON query_stats(instance_name)",
		"CREATE INDEX IF NOT EXISTS idx_deadlock_events_instance_time ON deadlock_events(instance_name, detected_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_replication_lag_instance_time ON replication_lag_history(instance_name, timestamp DESC)",
		"CREATE INDEX IF NOT EXISTS idx_metrics_history_instance_time ON metrics_history(instance_name, timestamp)",
		"CREATE INDEX IF NOT EXISTS idx_alert_events_instance_time ON alert_events(instance_name, triggered_at DESC)",
	}
	for _, idx := range instanceNameIndexes {
		_, _ = db.conn.Exec(idx)
	}

	// Migration: Recreate query_stats table with composite primary key for multi-instance support
	// This is needed for existing databases that have fingerprint as the sole PRIMARY KEY
	// Check if query_stats has the old schema (fingerprint as INTEGER PRIMARY KEY)
	var tableSQL string
	err = db.conn.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='query_stats'").Scan(&tableSQL)
	if err == nil && tableSQL != "" {
		// If the table has "fingerprint INTEGER PRIMARY KEY" but not "PRIMARY KEY (fingerprint, instance_name)"
		// we need to migrate it
		if !containsCompositePK(tableSQL) {
			// Add instance_name column if it doesn't exist (for very old databases)
			_, _ = db.conn.Exec("ALTER TABLE query_stats ADD COLUMN instance_name TEXT NOT NULL DEFAULT 'default'")

			// Recreate table with composite primary key
			migrations := []string{
				// Create new table with correct schema
				`CREATE TABLE IF NOT EXISTS query_stats_new (
					fingerprint INTEGER NOT NULL,
					normalized_query TEXT NOT NULL,
					calls INTEGER NOT NULL DEFAULT 0,
					total_time_ms REAL NOT NULL DEFAULT 0,
					min_time_ms REAL,
					max_time_ms REAL,
					total_rows INTEGER NOT NULL DEFAULT 0,
					first_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
					last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
					sample_params TEXT,
					instance_name TEXT NOT NULL DEFAULT 'default',
					PRIMARY KEY (fingerprint, instance_name)
				)`,
				// Copy data from old table
				`INSERT OR IGNORE INTO query_stats_new
					SELECT fingerprint, normalized_query, calls, total_time_ms, min_time_ms, max_time_ms,
					       total_rows, first_seen, last_seen, sample_params,
					       COALESCE(instance_name, 'default')
					FROM query_stats`,
				// Drop old table
				`DROP TABLE query_stats`,
				// Rename new table
				`ALTER TABLE query_stats_new RENAME TO query_stats`,
				// Recreate indexes
				`CREATE INDEX IF NOT EXISTS idx_query_stats_total_time ON query_stats(total_time_ms DESC)`,
				`CREATE INDEX IF NOT EXISTS idx_query_stats_calls ON query_stats(calls DESC)`,
				`CREATE INDEX IF NOT EXISTS idx_query_stats_total_rows ON query_stats(total_rows DESC)`,
				`CREATE INDEX IF NOT EXISTS idx_query_stats_last_seen ON query_stats(last_seen DESC)`,
				`CREATE INDEX IF NOT EXISTS idx_query_stats_instance ON query_stats(instance_name)`,
			}
			for _, m := range migrations {
				_, _ = db.conn.Exec(m)
			}
		}
	}

	return nil
}

// containsCompositePK checks if a CREATE TABLE SQL contains a composite primary key
func containsCompositePK(sql string) bool {
	// Look for PRIMARY KEY (fingerprint, instance_name) pattern
	return strings.Contains(sql, "PRIMARY KEY (fingerprint, instance_name)") ||
		strings.Contains(sql, "PRIMARY KEY(fingerprint, instance_name)")
}
