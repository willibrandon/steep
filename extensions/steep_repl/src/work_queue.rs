//! Work queue table for steep_repl extension.
//!
//! This module creates the steep_repl.work_queue table for background job management.
//! Operations like snapshot_generate, snapshot_apply, and bidirectional_merge are
//! queued here and processed by the background worker.
//!
//! T001: Add work_queue table to extension schema

use pgrx::prelude::*;

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
}
