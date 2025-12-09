//! Work queue table for steep_repl extension.
//!
//! This module creates the steep_repl.work_queue table for background job management.
//! Operations like snapshot_generate, snapshot_apply, and bidirectional_merge are
//! queued here and processed by the background worker.
//!
//! T001: Add work_queue table to extension schema
//! T008: Implement work queue claim with FOR UPDATE SKIP LOCKED

use pgrx::prelude::*;

// =============================================================================
// Rust Work Queue API (T008)
// =============================================================================
// These functions use Spi::get_one() and Spi::run() instead of Spi::connect()
// because they need to work inside BackgroundWorker::transaction() context.

/// Work queue entry representing a claimed job.
///
/// This struct holds all the data needed to process a work item.
#[derive(Debug)]
pub struct WorkQueueEntry {
    /// Primary key ID
    pub id: i64,
    /// Operation type: snapshot_generate, snapshot_apply, bidirectional_merge
    pub operation: String,
    /// Snapshot ID (for snapshot operations)
    pub snapshot_id: Option<String>,
    /// Merge ID (for merge operations)
    pub merge_id: Option<pgrx::Uuid>,
    /// Operation parameters as JSON
    pub params: pgrx::JsonB,
}

/// Result of attempting to claim work from the queue.
#[derive(Debug)]
pub enum ClaimWorkResult {
    /// Successfully claimed a work item
    Claimed(WorkQueueEntry),
    /// No pending work available
    NoWork,
    /// Error during claim operation
    Error(String),
}

/// Claim the next pending work item using FOR UPDATE SKIP LOCKED.
///
/// This function:
/// 1. Selects the oldest pending job with row-level locking
/// 2. Marks it as 'running' with current timestamp and worker PID
/// 3. Returns the work entry for processing
///
/// Uses Spi::get_one() and Spi::run() to work inside BackgroundWorker::transaction().
///
/// # Example
///
/// ```ignore
/// BackgroundWorker::transaction(|| {
///     match claim_next_work() {
///         ClaimWorkResult::Claimed(entry) => {
///             // Process the work item
///             process_work(&entry);
///             complete_work_entry(entry.id);
///         }
///         ClaimWorkResult::NoWork => {
///             // No work available, sleep and retry
///         }
///         ClaimWorkResult::Error(e) => {
///             pgrx::warning!("Failed to claim work: {}", e);
///         }
///     }
/// });
/// ```
pub fn claim_next_work() -> ClaimWorkResult {
    // Step 1: Find and lock the next pending work item
    // Using FOR UPDATE SKIP LOCKED ensures:
    // - Row-level lock prevents other workers from claiming the same job
    // - SKIP LOCKED allows concurrent workers to claim different jobs
    //
    // Note: Spi::get_one() returns Err with "SpiTupleTable positioned before..."
    // when no rows are returned, so we need to check for that specific error.
    let id = match Spi::get_one::<i64>(
        r#"SELECT id FROM steep_repl.work_queue
           WHERE status = 'pending'
           ORDER BY created_at
           LIMIT 1
           FOR UPDATE SKIP LOCKED"#,
    ) {
        Ok(Some(work_id)) => work_id,
        Ok(None) => return ClaimWorkResult::NoWork,
        // Spi::get_one returns an error when no rows are found
        Err(e) if e.to_string().contains("positioned before") => return ClaimWorkResult::NoWork,
        Err(e) => return ClaimWorkResult::Error(format!("failed to query work queue: {}", e)),
    };

    // Step 2: Mark as running and set worker PID
    if let Err(e) = Spi::run(&format!(
        r#"UPDATE steep_repl.work_queue
           SET status = 'running', started_at = now(), worker_pid = pg_backend_pid()
           WHERE id = {}"#,
        id
    )) {
        return ClaimWorkResult::Error(format!("failed to mark work as running: {}", e));
    }

    // Step 3: Fetch the full work entry data
    let operation = match Spi::get_one::<String>(&format!(
        "SELECT operation FROM steep_repl.work_queue WHERE id = {}",
        id
    )) {
        Ok(Some(op)) => op,
        Ok(None) => return ClaimWorkResult::Error("operation is NULL".to_string()),
        Err(e) => return ClaimWorkResult::Error(format!("failed to get operation: {}", e)),
    };

    let snapshot_id = Spi::get_one::<String>(&format!(
        "SELECT snapshot_id FROM steep_repl.work_queue WHERE id = {}",
        id
    ))
    .ok()
    .flatten();

    let merge_id = Spi::get_one::<pgrx::Uuid>(&format!(
        "SELECT merge_id FROM steep_repl.work_queue WHERE id = {}",
        id
    ))
    .ok()
    .flatten();

    let params = Spi::get_one::<pgrx::JsonB>(&format!(
        "SELECT params FROM steep_repl.work_queue WHERE id = {}",
        id
    ))
    .ok()
    .flatten()
    .unwrap_or(pgrx::JsonB(serde_json::json!({})));

    ClaimWorkResult::Claimed(WorkQueueEntry {
        id,
        operation,
        snapshot_id,
        merge_id,
        params,
    })
}

/// Mark a work queue entry as complete.
///
/// Should only be called for entries in 'running' status.
pub fn complete_work_entry(id: i64) -> Result<(), String> {
    Spi::run(&format!(
        r#"UPDATE steep_repl.work_queue
           SET status = 'complete', completed_at = now()
           WHERE id = {} AND status = 'running'"#,
        id
    ))
    .map_err(|e| format!("failed to complete work: {}", e))
}

/// Mark a work queue entry as failed with an error message.
///
/// Should only be called for entries in 'running' status.
pub fn fail_work_entry(id: i64, error_message: &str) -> Result<(), String> {
    // Escape single quotes in error message for SQL safety
    let escaped_error = error_message.replace('\'', "''");
    Spi::run(&format!(
        r#"UPDATE steep_repl.work_queue
           SET status = 'failed', completed_at = now(), error_message = '{}'
           WHERE id = {} AND status = 'running'"#,
        escaped_error, id
    ))
    .map_err(|e| format!("failed to mark work as failed: {}", e))
}

/// Mark a work queue entry as cancelled.
///
/// Can be called for entries in 'pending' or 'running' status.
pub fn cancel_work_entry(id: i64) -> Result<bool, String> {
    let result = Spi::get_one::<i64>(&format!(
        r#"WITH updated AS (
               UPDATE steep_repl.work_queue
               SET status = 'cancelled', completed_at = now()
               WHERE id = {} AND status IN ('pending', 'running')
               RETURNING 1
           )
           SELECT count(*) FROM updated"#,
        id
    ))
    .map_err(|e| format!("failed to cancel work: {}", e))?;

    Ok(result.unwrap_or(0) > 0)
}

/// Queue a snapshot generate operation.
///
/// Returns the work queue entry ID on success.
pub fn queue_snapshot_generate(
    snapshot_id: &str,
    output_path: &str,
    compression: &str,
    parallel: i32,
) -> Result<i64, String> {
    let escaped_snapshot = snapshot_id.replace('\'', "''");
    let escaped_path = output_path.replace('\'', "''");
    let escaped_compression = compression.replace('\'', "''");

    Spi::get_one::<i64>(&format!(
        r#"INSERT INTO steep_repl.work_queue (operation, snapshot_id, params)
           VALUES (
               'snapshot_generate',
               '{}',
               jsonb_build_object(
                   'output_path', '{}',
                   'compression', '{}',
                   'parallel', {}
               )
           )
           RETURNING id"#,
        escaped_snapshot, escaped_path, escaped_compression, parallel
    ))
    .map_err(|e| format!("failed to queue snapshot generate: {}", e))?
    .ok_or_else(|| "no ID returned from insert".to_string())
}

/// Queue a snapshot apply operation.
///
/// Returns the work queue entry ID on success.
pub fn queue_snapshot_apply(
    snapshot_id: &str,
    input_path: &str,
    parallel: i32,
    verify: bool,
) -> Result<i64, String> {
    let escaped_snapshot = snapshot_id.replace('\'', "''");
    let escaped_path = input_path.replace('\'', "''");

    Spi::get_one::<i64>(&format!(
        r#"INSERT INTO steep_repl.work_queue (operation, snapshot_id, params)
           VALUES (
               'snapshot_apply',
               '{}',
               jsonb_build_object(
                   'input_path', '{}',
                   'parallel', {},
                   'verify', {}
               )
           )
           RETURNING id"#,
        escaped_snapshot, escaped_path, parallel, verify
    ))
    .map_err(|e| format!("failed to queue snapshot apply: {}", e))?
    .ok_or_else(|| "no ID returned from insert".to_string())
}

/// Queue a bidirectional merge operation.
///
/// Returns the work queue entry ID on success.
pub fn queue_bidirectional_merge(
    merge_id: pgrx::Uuid,
    peer_connstr: &str,
    tables: &[&str],
    strategy: &str,
    dry_run: bool,
) -> Result<i64, String> {
    let escaped_connstr = peer_connstr.replace('\'', "''");
    let escaped_strategy = strategy.replace('\'', "''");
    let tables_array = tables
        .iter()
        .map(|t| format!("'{}'", t.replace('\'', "''")))
        .collect::<Vec<_>>()
        .join(", ");

    Spi::get_one::<i64>(&format!(
        r#"INSERT INTO steep_repl.work_queue (operation, merge_id, params)
           VALUES (
               'bidirectional_merge',
               '{}',
               jsonb_build_object(
                   'peer_connstr', '{}',
                   'tables', ARRAY[{}]::text[],
                   'strategy', '{}',
                   'dry_run', {}
               )
           )
           RETURNING id"#,
        merge_id, escaped_connstr, tables_array, escaped_strategy, dry_run
    ))
    .map_err(|e| format!("failed to queue merge: {}", e))?
    .ok_or_else(|| "no ID returned from insert".to_string())
}

/// Get pending work count.
///
/// Returns the number of pending work items in the queue.
pub fn get_pending_work_count() -> Result<i64, String> {
    Spi::get_one::<i64>("SELECT count(*) FROM steep_repl.work_queue WHERE status = 'pending'")
        .map_err(|e| format!("failed to count pending work: {}", e))?
        .ok_or_else(|| "no count returned".to_string())
}

/// Get running work count.
///
/// Returns the number of currently running work items.
pub fn get_running_work_count() -> Result<i64, String> {
    Spi::get_one::<i64>("SELECT count(*) FROM steep_repl.work_queue WHERE status = 'running'")
        .map_err(|e| format!("failed to count running work: {}", e))?
        .ok_or_else(|| "no count returned".to_string())
}

/// Recover abandoned work items.
///
/// Marks running jobs with dead workers as failed.
/// Returns the number of recovered items.
pub fn recover_abandoned_work() -> Result<i64, String> {
    Spi::get_one::<i64>(
        r#"WITH recovered AS (
               UPDATE steep_repl.work_queue wq
               SET status = 'failed',
                   completed_at = now(),
                   error_message = 'Worker process terminated unexpectedly (PID ' || wq.worker_pid || ')'
               WHERE wq.status = 'running'
               AND NOT EXISTS (
                   SELECT 1 FROM pg_stat_activity
                   WHERE pid = wq.worker_pid
               )
               RETURNING 1
           )
           SELECT count(*) FROM recovered"#,
    )
    .map_err(|e| format!("failed to recover abandoned work: {}", e))?
    .ok_or_else(|| "no count returned".to_string())
}

extension_sql!(
    r#"
-- =============================================================================
-- Work Queue Table (T001)
-- =============================================================================
-- Background job queue for long-running operations.
-- Worker claims jobs using FOR UPDATE SKIP LOCKED for concurrent safety.

CREATE TABLE steep_repl.work_queue (
    -- Primary identifier
    id              BIGSERIAL PRIMARY KEY,

    -- Operation type
    operation       TEXT NOT NULL,

    -- Associated entity IDs (mutually exclusive based on operation)
    snapshot_id     TEXT,           -- For snapshot_generate, snapshot_apply
    merge_id        UUID,           -- For bidirectional_merge

    -- Operation parameters
    params          JSONB NOT NULL DEFAULT '{}',

    -- Status tracking
    status          TEXT NOT NULL DEFAULT 'pending',

    -- Worker tracking
    worker_pid      INTEGER,        -- PID of worker processing this entry

    -- Error handling
    error_message   TEXT,

    -- Timestamps
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,

    -- Constraints
    CONSTRAINT work_queue_operation_check CHECK (
        operation IN ('snapshot_generate', 'snapshot_apply', 'bidirectional_merge')
    ),
    CONSTRAINT work_queue_status_check CHECK (
        status IN ('pending', 'running', 'complete', 'failed', 'cancelled')
    ),
    CONSTRAINT work_queue_snapshot_required CHECK (
        (operation IN ('snapshot_generate', 'snapshot_apply') AND snapshot_id IS NOT NULL)
        OR (operation = 'bidirectional_merge')
    ),
    CONSTRAINT work_queue_merge_required CHECK (
        (operation = 'bidirectional_merge' AND merge_id IS NOT NULL)
        OR (operation IN ('snapshot_generate', 'snapshot_apply'))
    )
);

-- Partial index on pending jobs for efficient claiming
CREATE INDEX work_queue_pending_idx ON steep_repl.work_queue (created_at)
    WHERE status = 'pending';

-- Index for finding running jobs by worker
CREATE INDEX work_queue_running_idx ON steep_repl.work_queue (worker_pid)
    WHERE status = 'running';

-- Index for snapshot lookups
CREATE INDEX work_queue_snapshot_idx ON steep_repl.work_queue (snapshot_id)
    WHERE snapshot_id IS NOT NULL;

-- Index for merge lookups
CREATE INDEX work_queue_merge_idx ON steep_repl.work_queue (merge_id)
    WHERE merge_id IS NOT NULL;

-- Comments
COMMENT ON TABLE steep_repl.work_queue IS
    'Background job queue for long-running operations processed by the extension worker';

COMMENT ON COLUMN steep_repl.work_queue.id IS 'Unique job identifier';
COMMENT ON COLUMN steep_repl.work_queue.operation IS
    'Operation type: snapshot_generate, snapshot_apply, bidirectional_merge';
COMMENT ON COLUMN steep_repl.work_queue.snapshot_id IS
    'Associated snapshot ID (for snapshot operations)';
COMMENT ON COLUMN steep_repl.work_queue.merge_id IS
    'Associated merge ID (for merge operations)';
COMMENT ON COLUMN steep_repl.work_queue.params IS
    'Operation parameters as JSONB (output_path, compression, parallel, etc.)';
COMMENT ON COLUMN steep_repl.work_queue.status IS
    'Job status: pending, running, complete, failed, cancelled';
COMMENT ON COLUMN steep_repl.work_queue.worker_pid IS
    'PostgreSQL backend PID of worker processing this job';
COMMENT ON COLUMN steep_repl.work_queue.error_message IS
    'Error details if status is failed';
COMMENT ON COLUMN steep_repl.work_queue.created_at IS 'When job was queued';
COMMENT ON COLUMN steep_repl.work_queue.started_at IS 'When worker started processing';
COMMENT ON COLUMN steep_repl.work_queue.completed_at IS 'When job completed/failed';

-- =============================================================================
-- Work Queue Claim Function
-- =============================================================================
-- Claims the next pending job using FOR UPDATE SKIP LOCKED.
-- Returns NULL if no pending jobs are available.

CREATE FUNCTION steep_repl.claim_work()
RETURNS steep_repl.work_queue AS $$
DECLARE
    v_job steep_repl.work_queue;
BEGIN
    -- Claim next pending job
    SELECT * INTO v_job
    FROM steep_repl.work_queue
    WHERE status = 'pending'
    ORDER BY created_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED;

    IF v_job.id IS NOT NULL THEN
        -- Mark as running
        UPDATE steep_repl.work_queue
        SET status = 'running',
            started_at = now(),
            worker_pid = pg_backend_pid()
        WHERE id = v_job.id
        RETURNING * INTO v_job;
    END IF;

    RETURN v_job;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.claim_work() IS
    'Claims the next pending job for processing. Uses FOR UPDATE SKIP LOCKED for concurrent safety.';

-- =============================================================================
-- Work Queue Completion Functions
-- =============================================================================

-- Mark job as complete
CREATE FUNCTION steep_repl.complete_work(p_id BIGINT)
RETURNS VOID AS $$
    UPDATE steep_repl.work_queue
    SET status = 'complete',
        completed_at = now()
    WHERE id = p_id AND status = 'running';
$$ LANGUAGE sql;

COMMENT ON FUNCTION steep_repl.complete_work IS
    'Mark a running job as complete';

-- Mark job as failed
CREATE FUNCTION steep_repl.fail_work(p_id BIGINT, p_error TEXT)
RETURNS VOID AS $$
    UPDATE steep_repl.work_queue
    SET status = 'failed',
        completed_at = now(),
        error_message = p_error
    WHERE id = p_id AND status = 'running';
$$ LANGUAGE sql;

COMMENT ON FUNCTION steep_repl.fail_work IS
    'Mark a running job as failed with error message';

-- Cancel a pending or running job
CREATE FUNCTION steep_repl.cancel_work(p_id BIGINT)
RETURNS BOOLEAN AS $$
DECLARE
    v_updated INTEGER;
BEGIN
    UPDATE steep_repl.work_queue
    SET status = 'cancelled',
        completed_at = now()
    WHERE id = p_id AND status IN ('pending', 'running');

    GET DIAGNOSTICS v_updated = ROW_COUNT;
    RETURN v_updated > 0;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.cancel_work IS
    'Cancel a pending or running job. Returns true if job was cancelled.';

-- =============================================================================
-- Work Queue Recovery Function
-- =============================================================================
-- Mark abandoned jobs (running but worker died) as failed.
-- Called on extension startup.

CREATE FUNCTION steep_repl.recover_abandoned_work()
RETURNS INTEGER AS $$
DECLARE
    v_count INTEGER;
BEGIN
    -- Find running jobs where worker PID is no longer active
    UPDATE steep_repl.work_queue wq
    SET status = 'failed',
        completed_at = now(),
        error_message = 'Worker process terminated unexpectedly (PID ' || wq.worker_pid || ')'
    WHERE wq.status = 'running'
    AND NOT EXISTS (
        SELECT 1 FROM pg_stat_activity
        WHERE pid = wq.worker_pid
    );

    GET DIAGNOSTICS v_count = ROW_COUNT;
    RETURN v_count;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.recover_abandoned_work IS
    'Mark running jobs with dead workers as failed. Called on startup/recovery.';

-- =============================================================================
-- Work Queue Enqueue Functions
-- =============================================================================

-- Queue a snapshot generate operation
CREATE FUNCTION steep_repl.queue_snapshot_generate(
    p_snapshot_id TEXT,
    p_output_path TEXT,
    p_compression TEXT DEFAULT 'none',
    p_parallel INTEGER DEFAULT 4
)
RETURNS steep_repl.work_queue AS $$
    INSERT INTO steep_repl.work_queue (
        operation, snapshot_id, params
    ) VALUES (
        'snapshot_generate',
        p_snapshot_id,
        jsonb_build_object(
            'output_path', p_output_path,
            'compression', p_compression,
            'parallel', p_parallel
        )
    )
    RETURNING *;
$$ LANGUAGE sql;

COMMENT ON FUNCTION steep_repl.queue_snapshot_generate IS
    'Queue a snapshot generate operation for background processing';

-- Queue a snapshot apply operation
CREATE FUNCTION steep_repl.queue_snapshot_apply(
    p_snapshot_id TEXT,
    p_input_path TEXT,
    p_parallel INTEGER DEFAULT 4,
    p_verify BOOLEAN DEFAULT true
)
RETURNS steep_repl.work_queue AS $$
    INSERT INTO steep_repl.work_queue (
        operation, snapshot_id, params
    ) VALUES (
        'snapshot_apply',
        p_snapshot_id,
        jsonb_build_object(
            'input_path', p_input_path,
            'parallel', p_parallel,
            'verify', p_verify
        )
    )
    RETURNING *;
$$ LANGUAGE sql;

COMMENT ON FUNCTION steep_repl.queue_snapshot_apply IS
    'Queue a snapshot apply operation for background processing';

-- Queue a bidirectional merge operation
CREATE FUNCTION steep_repl.queue_merge(
    p_merge_id UUID,
    p_peer_connstr TEXT,
    p_tables TEXT[],
    p_strategy TEXT DEFAULT 'prefer-local',
    p_dry_run BOOLEAN DEFAULT false
)
RETURNS steep_repl.work_queue AS $$
    INSERT INTO steep_repl.work_queue (
        operation, merge_id, params
    ) VALUES (
        'bidirectional_merge',
        p_merge_id,
        jsonb_build_object(
            'peer_connstr', p_peer_connstr,
            'tables', p_tables,
            'strategy', p_strategy,
            'dry_run', p_dry_run
        )
    )
    RETURNING *;
$$ LANGUAGE sql;

COMMENT ON FUNCTION steep_repl.queue_merge IS
    'Queue a bidirectional merge operation for background processing';

-- =============================================================================
-- Work Queue Inspection Functions
-- =============================================================================

-- List operations in queue (for CLI/monitoring)
CREATE FUNCTION steep_repl.list_operations(
    p_status TEXT DEFAULT NULL,
    p_limit INTEGER DEFAULT 100
)
RETURNS SETOF steep_repl.work_queue AS $$
    SELECT *
    FROM steep_repl.work_queue
    WHERE p_status IS NULL OR status = p_status
    ORDER BY created_at DESC
    LIMIT p_limit;
$$ LANGUAGE sql STABLE;

COMMENT ON FUNCTION steep_repl.list_operations IS
    'List work queue entries, optionally filtered by status';

-- Prune completed/failed jobs older than specified interval
CREATE FUNCTION steep_repl.prune_work_queue(p_older_than INTERVAL)
RETURNS BIGINT AS $$
DECLARE
    v_deleted BIGINT;
BEGIN
    DELETE FROM steep_repl.work_queue
    WHERE status IN ('complete', 'failed', 'cancelled')
    AND completed_at < now() - p_older_than;

    GET DIAGNOSTICS v_deleted = ROW_COUNT;
    RETURN v_deleted;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION steep_repl.prune_work_queue IS
    'Delete completed/failed/cancelled jobs older than the specified interval';
"#,
    name = "create_work_queue_table",
    requires = ["create_schema"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_work_queue_table_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_tables
                WHERE schemaname = 'steep_repl' AND tablename = 'work_queue'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "work_queue table should exist");
    }

    #[pg_test]
    fn test_work_queue_columns() {
        let result = Spi::get_one::<i64>(
            "SELECT count(*) FROM information_schema.columns
             WHERE table_schema = 'steep_repl' AND table_name = 'work_queue'"
        );
        assert_eq!(result, Ok(Some(11)), "work_queue should have 11 columns");
    }

    #[pg_test]
    fn test_work_queue_indexes() {
        let indexes = vec![
            "work_queue_pending_idx",
            "work_queue_running_idx",
            "work_queue_snapshot_idx",
            "work_queue_merge_idx",
        ];

        for idx_name in indexes {
            let result = Spi::get_one::<bool>(&format!(
                "SELECT EXISTS(
                    SELECT 1 FROM pg_indexes
                    WHERE schemaname = 'steep_repl' AND indexname = '{}'
                )",
                idx_name
            ));
            assert_eq!(result, Ok(Some(true)), "index {} should exist", idx_name);
        }
    }

    #[pg_test]
    fn test_queue_snapshot_generate() {
        // First create a node for the snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('test-node-wq', 'Test Node', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        // Create a snapshot record
        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_wq_test', 'test-node-wq')"
        ).expect("snapshot insert should succeed");

        // Queue the operation
        let job_id = Spi::get_one::<i64>(
            "SELECT id FROM steep_repl.queue_snapshot_generate(
                'snap_wq_test', '/tmp/snapshot', 'zstd', 4
            )"
        );

        match job_id {
            Ok(Some(id)) => {
                assert!(id > 0, "should return positive job ID");

                // Verify job exists with correct fields
                let status = Spi::get_one::<String>(
                    "SELECT status FROM steep_repl.work_queue WHERE id = (
                        SELECT max(id) FROM steep_repl.work_queue
                    )"
                );
                assert_eq!(status, Ok(Some("pending".to_string())));
            }
            _ => panic!("queue_snapshot_generate should return a job ID"),
        }

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'snap_wq_test'")
            .expect("cleanup work_queue should succeed");
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_wq_test'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'test-node-wq'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_claim_work() {
        // Create test node and snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('claim-test-node', 'Claim Test', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_claim_test', 'claim-test-node')"
        ).expect("snapshot insert should succeed");

        // Queue a job
        Spi::run(
            "SELECT steep_repl.queue_snapshot_generate('snap_claim_test', '/tmp/test', 'none', 1)"
        ).expect("queue should succeed");

        // Claim the job
        let claimed_status = Spi::get_one::<String>(
            "SELECT status FROM steep_repl.claim_work()"
        );
        assert_eq!(claimed_status, Ok(Some("running".to_string())), "claimed job should be running");

        // Try to claim again - should get NULL since no pending jobs
        let second_claim = Spi::get_one::<i64>(
            "SELECT id FROM steep_repl.claim_work()"
        );
        assert_eq!(second_claim, Ok(None), "second claim should return NULL");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'snap_claim_test'")
            .expect("cleanup work_queue should succeed");
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_claim_test'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'claim-test-node'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_complete_and_fail_work() {
        // Create test node and snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('complete-test-node', 'Complete Test', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_complete_test', 'complete-test-node')"
        ).expect("snapshot insert should succeed");

        // Queue and claim a job
        Spi::run(
            "SELECT steep_repl.queue_snapshot_generate('snap_complete_test', '/tmp/test', 'none', 1)"
        ).expect("queue should succeed");

        let job_id = Spi::get_one::<i64>(
            "SELECT id FROM steep_repl.claim_work()"
        ).expect("claim should succeed").unwrap();

        // Complete the job
        Spi::run(&format!(
            "SELECT steep_repl.complete_work({})", job_id
        )).expect("complete should succeed");

        let status = Spi::get_one::<String>(&format!(
            "SELECT status FROM steep_repl.work_queue WHERE id = {}", job_id
        ));
        assert_eq!(status, Ok(Some("complete".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'snap_complete_test'")
            .expect("cleanup work_queue should succeed");
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_complete_test'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'complete-test-node'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_cancel_work() {
        // Create test node and snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('cancel-test-node', 'Cancel Test', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_cancel_test', 'cancel-test-node')"
        ).expect("snapshot insert should succeed");

        // Queue a job
        let job_id = Spi::get_one::<i64>(
            "SELECT id FROM steep_repl.queue_snapshot_generate('snap_cancel_test', '/tmp/test', 'none', 1)"
        ).expect("queue should succeed").unwrap();

        // Cancel while pending
        let cancelled = Spi::get_one::<bool>(&format!(
            "SELECT steep_repl.cancel_work({})", job_id
        ));
        assert_eq!(cancelled, Ok(Some(true)), "cancel should succeed");

        let status = Spi::get_one::<String>(&format!(
            "SELECT status FROM steep_repl.work_queue WHERE id = {}", job_id
        ));
        assert_eq!(status, Ok(Some("cancelled".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'snap_cancel_test'")
            .expect("cleanup work_queue should succeed");
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_cancel_test'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'cancel-test-node'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_list_operations() {
        // Create test node and snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('list-test-node', 'List Test', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_list_test', 'list-test-node')"
        ).expect("snapshot insert should succeed");

        // Queue a job
        Spi::run(
            "SELECT steep_repl.queue_snapshot_generate('snap_list_test', '/tmp/test', 'none', 1)"
        ).expect("queue should succeed");

        // List all operations
        let count = Spi::get_one::<i64>(
            "SELECT count(*) FROM steep_repl.list_operations()"
        );
        assert!(count.unwrap().unwrap() >= 1, "should have at least 1 operation");

        // List pending only
        let pending_count = Spi::get_one::<i64>(
            "SELECT count(*) FROM steep_repl.list_operations('pending')"
        );
        assert!(pending_count.unwrap().unwrap() >= 1, "should have at least 1 pending operation");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'snap_list_test'")
            .expect("cleanup work_queue should succeed");
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_list_test'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'list-test-node'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_queue_merge() {
        // Generate a test UUID
        let merge_id = Spi::get_one::<pgrx::Uuid>(
            "SELECT gen_random_uuid()"
        ).expect("generate uuid").unwrap();

        // Queue a merge operation
        let job_id = Spi::get_one::<i64>(&format!(
            "SELECT id FROM steep_repl.queue_merge(
                '{}'::uuid,
                'host=peer.example.com dbname=mydb',
                ARRAY['public.users', 'public.orders'],
                'prefer-local',
                false
            )",
            merge_id
        ));

        match job_id {
            Ok(Some(id)) => {
                assert!(id > 0, "should return positive job ID");

                // Verify operation type
                let op = Spi::get_one::<String>(&format!(
                    "SELECT operation FROM steep_repl.work_queue WHERE id = {}", id
                ));
                assert_eq!(op, Ok(Some("bidirectional_merge".to_string())));
            }
            _ => panic!("queue_merge should return a job ID"),
        }

        // Cleanup
        Spi::run(&format!(
            "DELETE FROM steep_repl.work_queue WHERE merge_id = '{}'",
            merge_id
        )).expect("cleanup should succeed");
    }

    #[pg_test]
    fn test_claim_work_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'claim_work'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "claim_work function should exist");
    }

    #[pg_test]
    fn test_recover_abandoned_work_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'recover_abandoned_work'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "recover_abandoned_work function should exist");
    }

    // =========================================================================
    // Tests for Rust API (T008)
    // =========================================================================

    #[pg_test]
    fn test_rust_claim_next_work_no_work() {
        // When queue is empty, should return NoWork
        let result = crate::work_queue::claim_next_work();
        match result {
            crate::work_queue::ClaimWorkResult::NoWork => (),
            crate::work_queue::ClaimWorkResult::Claimed(_) => {
                panic!("Expected NoWork, got Claimed");
            }
            crate::work_queue::ClaimWorkResult::Error(e) => {
                panic!("Expected NoWork, got Error: {}", e);
            }
        }
    }

    #[pg_test]
    fn test_rust_claim_next_work_with_work() {
        // Create test node and snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('rust-claim-test-node', 'Rust Claim Test', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_rust_claim_test', 'rust-claim-test-node')"
        ).expect("snapshot insert should succeed");

        // Queue a job using SQL function
        Spi::run(
            "SELECT steep_repl.queue_snapshot_generate('snap_rust_claim_test', '/tmp/test', 'none', 1)"
        ).expect("queue should succeed");

        // Claim using Rust API
        let result = crate::work_queue::claim_next_work();
        match result {
            crate::work_queue::ClaimWorkResult::Claimed(entry) => {
                assert_eq!(entry.operation, "snapshot_generate");
                assert_eq!(entry.snapshot_id, Some("snap_rust_claim_test".to_string()));

                // Verify status was updated to running
                let status = Spi::get_one::<String>(&format!(
                    "SELECT status FROM steep_repl.work_queue WHERE id = {}",
                    entry.id
                ));
                assert_eq!(status, Ok(Some("running".to_string())));
            }
            crate::work_queue::ClaimWorkResult::NoWork => {
                panic!("Expected Claimed, got NoWork");
            }
            crate::work_queue::ClaimWorkResult::Error(e) => {
                panic!("Expected Claimed, got Error: {}", e);
            }
        }

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'snap_rust_claim_test'")
            .expect("cleanup work_queue should succeed");
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_rust_claim_test'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'rust-claim-test-node'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_rust_complete_work_entry() {
        // Create test node and snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('rust-complete-test-node', 'Rust Complete Test', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_rust_complete_test', 'rust-complete-test-node')"
        ).expect("snapshot insert should succeed");

        // Queue and claim
        Spi::run(
            "SELECT steep_repl.queue_snapshot_generate('snap_rust_complete_test', '/tmp/test', 'none', 1)"
        ).expect("queue should succeed");

        let entry = match crate::work_queue::claim_next_work() {
            crate::work_queue::ClaimWorkResult::Claimed(e) => e,
            _ => panic!("Expected to claim work"),
        };

        // Complete using Rust API
        let result = crate::work_queue::complete_work_entry(entry.id);
        assert!(result.is_ok(), "complete_work_entry should succeed");

        // Verify status
        let status = Spi::get_one::<String>(&format!(
            "SELECT status FROM steep_repl.work_queue WHERE id = {}",
            entry.id
        ));
        assert_eq!(status, Ok(Some("complete".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'snap_rust_complete_test'")
            .expect("cleanup work_queue should succeed");
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_rust_complete_test'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'rust-complete-test-node'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_rust_fail_work_entry() {
        // Create test node and snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('rust-fail-test-node', 'Rust Fail Test', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_rust_fail_test', 'rust-fail-test-node')"
        ).expect("snapshot insert should succeed");

        // Queue and claim
        Spi::run(
            "SELECT steep_repl.queue_snapshot_generate('snap_rust_fail_test', '/tmp/test', 'none', 1)"
        ).expect("queue should succeed");

        let entry = match crate::work_queue::claim_next_work() {
            crate::work_queue::ClaimWorkResult::Claimed(e) => e,
            _ => panic!("Expected to claim work"),
        };

        // Fail using Rust API
        let result = crate::work_queue::fail_work_entry(entry.id, "Test error message");
        assert!(result.is_ok(), "fail_work_entry should succeed");

        // Verify status and error message
        let status = Spi::get_one::<String>(&format!(
            "SELECT status FROM steep_repl.work_queue WHERE id = {}",
            entry.id
        ));
        assert_eq!(status, Ok(Some("failed".to_string())));

        let error_msg = Spi::get_one::<String>(&format!(
            "SELECT error_message FROM steep_repl.work_queue WHERE id = {}",
            entry.id
        ));
        assert_eq!(error_msg, Ok(Some("Test error message".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'snap_rust_fail_test'")
            .expect("cleanup work_queue should succeed");
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_rust_fail_test'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'rust-fail-test-node'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_rust_fail_work_entry_escapes_quotes() {
        // Create test node and snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('rust-escape-test-node', 'Rust Escape Test', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_rust_escape_test', 'rust-escape-test-node')"
        ).expect("snapshot insert should succeed");

        // Queue and claim
        Spi::run(
            "SELECT steep_repl.queue_snapshot_generate('snap_rust_escape_test', '/tmp/test', 'none', 1)"
        ).expect("queue should succeed");

        let entry = match crate::work_queue::claim_next_work() {
            crate::work_queue::ClaimWorkResult::Claimed(e) => e,
            _ => panic!("Expected to claim work"),
        };

        // Fail with message containing quotes
        let result = crate::work_queue::fail_work_entry(entry.id, "Error: can't find file 'test.txt'");
        assert!(result.is_ok(), "fail_work_entry should handle quotes");

        // Verify error message was stored correctly
        let error_msg = Spi::get_one::<String>(&format!(
            "SELECT error_message FROM steep_repl.work_queue WHERE id = {}",
            entry.id
        ));
        assert_eq!(error_msg, Ok(Some("Error: can't find file 'test.txt'".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'snap_rust_escape_test'")
            .expect("cleanup work_queue should succeed");
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_rust_escape_test'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'rust-escape-test-node'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_rust_cancel_work_entry() {
        // Create test node and snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('rust-cancel-test-node', 'Rust Cancel Test', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_rust_cancel_test', 'rust-cancel-test-node')"
        ).expect("snapshot insert should succeed");

        // Queue a job (don't claim it - cancel while pending)
        let job_id = Spi::get_one::<i64>(
            "SELECT id FROM steep_repl.queue_snapshot_generate('snap_rust_cancel_test', '/tmp/test', 'none', 1)"
        ).expect("queue should succeed").unwrap();

        // Cancel using Rust API
        let result = crate::work_queue::cancel_work_entry(job_id);
        assert!(result.is_ok(), "cancel_work_entry should succeed");
        assert!(result.unwrap(), "cancel should return true");

        // Verify status
        let status = Spi::get_one::<String>(&format!(
            "SELECT status FROM steep_repl.work_queue WHERE id = {}",
            job_id
        ));
        assert_eq!(status, Ok(Some("cancelled".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'snap_rust_cancel_test'")
            .expect("cleanup work_queue should succeed");
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_rust_cancel_test'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'rust-cancel-test-node'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_rust_get_pending_work_count() {
        // Create test node and snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('rust-count-test-node', 'Rust Count Test', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_rust_count_test', 'rust-count-test-node')"
        ).expect("snapshot insert should succeed");

        // Get initial count
        let initial_count = crate::work_queue::get_pending_work_count();
        assert!(initial_count.is_ok(), "get_pending_work_count should succeed");
        let initial = initial_count.unwrap();

        // Queue a job
        Spi::run(
            "SELECT steep_repl.queue_snapshot_generate('snap_rust_count_test', '/tmp/test', 'none', 1)"
        ).expect("queue should succeed");

        // Count should have increased
        let new_count = crate::work_queue::get_pending_work_count();
        assert!(new_count.is_ok(), "get_pending_work_count should succeed");
        assert_eq!(new_count.unwrap(), initial + 1);

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'snap_rust_count_test'")
            .expect("cleanup work_queue should succeed");
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_rust_count_test'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'rust-count-test-node'")
            .expect("cleanup nodes should succeed");
    }

    #[pg_test]
    fn test_rust_queue_functions() {
        // Create test node and snapshot
        Spi::run(
            "INSERT INTO steep_repl.nodes (node_id, node_name, host, port, priority, status)
             VALUES ('rust-queue-test-node', 'Rust Queue Test', 'localhost', 5432, 50, 'healthy')"
        ).expect("node insert should succeed");

        Spi::run(
            "INSERT INTO steep_repl.snapshots (snapshot_id, source_node_id)
             VALUES ('snap_rust_queue_test', 'rust-queue-test-node')"
        ).expect("snapshot insert should succeed");

        // Test queue_snapshot_generate
        let gen_result = crate::work_queue::queue_snapshot_generate(
            "snap_rust_queue_test",
            "/tmp/test",
            "zstd",
            4
        );
        assert!(gen_result.is_ok(), "queue_snapshot_generate should succeed");
        let gen_id = gen_result.unwrap();
        assert!(gen_id > 0, "should return positive ID");

        // Verify params
        let compression = Spi::get_one::<String>(&format!(
            "SELECT params->>'compression' FROM steep_repl.work_queue WHERE id = {}",
            gen_id
        ));
        assert_eq!(compression, Ok(Some("zstd".to_string())));

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'snap_rust_queue_test'")
            .expect("cleanup work_queue should succeed");
        Spi::run("DELETE FROM steep_repl.snapshots WHERE snapshot_id = 'snap_rust_queue_test'")
            .expect("cleanup snapshots should succeed");
        Spi::run("DELETE FROM steep_repl.nodes WHERE node_id = 'rust-queue-test-node'")
            .expect("cleanup nodes should succeed");
    }
}
