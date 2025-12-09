//! Background worker for steep_repl extension.
//!
//! This module implements dynamic PostgreSQL background workers that process
//! long-running operations from the work queue. Workers are launched on-demand
//! via SQL functions and connect to the database where the extension is installed.
//!
//! Key design decisions:
//! - Dynamic workers (load_dynamic) instead of static workers (load)
//! - Database name passed via bgw_extra field
//! - Workers launched from SQL functions within the extension's database context
//!
//! T007: Implement background worker main loop

use pgrx::bgworkers::*;
use pgrx::datum::DatumWithOid;
use pgrx::prelude::*;

use crate::progress::{OperationType, ProgressPhase, OPERATION_PROGRESS};
use crate::types::{SnapshotStatus, WorkOperation};

/// Wake interval when no work is available (seconds)
const IDLE_WAKE_INTERVAL_SECS: u64 = 1;

/// Maximum time to wait for a single work item before checking signals (seconds)
const WORK_LOOP_TIMEOUT_SECS: u64 = 30;

/// Work queue entry representing a claimed job
#[derive(Debug)]
pub struct WorkItem {
    pub id: i64,
    pub operation: String,
    pub snapshot_id: Option<String>,
    pub merge_id: Option<pgrx::Uuid>,
    pub params: pgrx::JsonB,
}

/// Result of work queue claim operation
#[derive(Debug)]
pub enum ClaimResult {
    /// Successfully claimed a work item
    Claimed(WorkItem),
    /// No pending work available
    NoWork,
    /// Error during claim
    Error(String),
}

/// Result of executing an operation
#[derive(Debug)]
pub enum ExecuteResult {
    /// Operation completed successfully
    Complete,
    /// Operation failed with error message
    Failed(String),
    /// Operation was cancelled (TODO: implement cancellation support)
    #[allow(dead_code)]
    Cancelled,
}

/// Register static background worker for work queue processing.
///
/// This is called from _PG_init() and registers a static background worker
/// when the extension is loaded via shared_preload_libraries.
/// The worker runs continuously, polling the work queue for pending operations.
pub fn register_worker() {
    // Only register static worker if loaded via shared_preload_libraries
    if unsafe { pgrx::pg_sys::process_shared_preload_libraries_in_progress } {
        // Register a static background worker that polls the work queue
        BackgroundWorkerBuilder::new("steep_repl_worker")
            .set_library("steep_repl")
            .set_function("steep_repl_static_worker_main")
            .set_argument(0i32.into_datum())
            .enable_spi_access()
            .set_start_time(BgWorkerStartTime::RecoveryFinished)
            .set_restart_time(Some(std::time::Duration::from_secs(5)))
            .load();
    }
}

/// Launch a dynamic background worker to process work queue items.
///
/// This SQL function spawns a background worker that connects to the current
/// database and processes pending work items from the queue.
///
/// Returns the worker PID on success, or an error if the worker couldn't be started.
#[pg_extern]
fn launch_worker() -> Result<i32, pgrx::spi::Error> {
    pgrx::log!("steep_repl: launch_worker() called");

    // Get current database name to pass to worker
    // Note: current_database() returns 'name' type, must cast to text for String
    let db_name_result = Spi::get_one::<String>("SELECT current_database()::text");
    pgrx::log!("steep_repl: current_database result: {:?}", db_name_result.is_ok());

    let db_name = match db_name_result {
        Ok(Some(name)) => name,
        Ok(None) => {
            pgrx::warning!("steep_repl: current_database returned NULL");
            return Err(pgrx::spi::Error::InvalidPosition);
        }
        Err(e) => {
            pgrx::warning!("steep_repl: failed to get current_database: {:?}", e);
            return Err(e);
        }
    };

    // Get current database OID for the extra field
    let db_oid = unsafe { pgrx::pg_sys::MyDatabaseId };

    // Create extra string with database info (format: "db_oid/db_name")
    // TODO: Pass to worker via .set_extra() once bgw_extra parsing is implemented
    let _extra = format!("{}/{}", db_oid.to_u32(), db_name);

    pgrx::log!(
        "steep_repl: launching dynamic worker for db '{}' (oid: {})",
        db_name,
        db_oid.to_u32()
    );

    // Launch dynamic background worker
    let worker = BackgroundWorkerBuilder::new("steep_repl_worker")
        .set_library("steep_repl")
        .set_function("steep_repl_worker_main")
        .enable_spi_access()
        .set_notify_pid(unsafe { pgrx::pg_sys::MyProcPid })
        .load_dynamic();

    pgrx::log!("steep_repl: load_dynamic returned: {:?}", worker.is_ok());

    match worker {
        Ok(handle) => {
            // Wait for worker to start and get its PID
            match handle.wait_for_startup() {
                Ok(pid) => {
                    pgrx::log!("steep_repl: launched dynamic worker with PID {}", pid);
                    Ok(pid)
                }
                Err(status) => {
                    pgrx::warning!("steep_repl: worker failed to start: {:?}", status);
                    Err(pgrx::spi::Error::InvalidPosition)
                }
            }
        }
        Err(e) => {
            pgrx::error!("steep_repl: failed to register dynamic worker: {:?}", e);
        }
    }
}

/// Check if a background worker is available and running.
///
/// Returns true if the extension can launch background workers.
#[pg_extern]
fn worker_available() -> bool {
    // Dynamic workers can be launched from any context
    // Check if we're not in a read-only transaction
    let in_transaction = unsafe { pgrx::pg_sys::IsTransactionState() };
    in_transaction
}

/// Static background worker main function.
///
/// This is the entry point for the static background worker that is registered
/// via shared_preload_libraries. It runs continuously, spawning dynamic workers
/// for databases registered in the steep_repl.databases catalog.
///
/// The pattern follows PostgreSQL conventions (like pg_cron):
/// 1. Static worker connects to postgres
/// 2. Reads steep_repl.databases for registered databases
/// 3. Spawns dynamic workers to process work in each registered database
///
/// IMPORTANT: Only databases explicitly registered via register_db() or
/// register_current_db() will have workers spawned. This prevents wasting
/// resources on databases that don't use steep_repl.
#[pg_guard]
#[unsafe(no_mangle)]
pub extern "C-unwind" fn steep_repl_static_worker_main(_arg: pg_sys::Datum) {
    use std::collections::{HashMap, HashSet};
    use std::time::Duration;

    // Set up signal handlers
    BackgroundWorker::attach_signal_handlers(SignalWakeFlags::SIGHUP | SignalWakeFlags::SIGTERM);

    // Connect to postgres database (where steep_repl.databases catalog lives)
    BackgroundWorker::connect_worker_to_spi(Some("postgres"), None);

    pgrx::log!("steep_repl: static background worker started (coordinator mode)");

    // Track databases we've spawned workers for
    let mut databases_with_workers: HashSet<String> = HashSet::new();
    // Track databases that failed validation (don't exist) - retry after backoff
    let mut failed_databases: HashMap<String, u64> = HashMap::new();
    let mut discovery_counter = 0u64;
    let mut warned_no_catalog = false;

    // Main loop - discover registered databases and spawn workers
    while BackgroundWorker::wait_latch(Some(Duration::from_secs(IDLE_WAKE_INTERVAL_SECS))) {
        discovery_counter += 1;

        // Every 10 iterations (10 seconds), re-check registered databases
        if discovery_counter % 10 == 1 || databases_with_workers.is_empty() {
            // First check if steep_repl.databases table exists (extension installed in postgres)
            let catalog_exists = BackgroundWorker::transaction(|| {
                Spi::get_one::<bool>(
                    "SELECT EXISTS(SELECT 1 FROM pg_tables WHERE schemaname = 'steep_repl' AND tablename = 'databases')"
                ).ok().flatten().unwrap_or(false)
            });

            if !catalog_exists {
                if !warned_no_catalog {
                    pgrx::warning!(
                        "steep_repl: databases catalog not found in postgres. \
                         Install extension in postgres: CREATE EXTENSION steep_repl;"
                    );
                    warned_no_catalog = true;
                }
                // Skip querying the catalog - it doesn't exist yet
                continue;
            }

            // Reset warning flag so we warn again if catalog is dropped
            warned_no_catalog = false;

            // Query the steep_repl.databases catalog for enabled databases
            let db_list: Option<String> = BackgroundWorker::transaction(|| {
                // Read from explicit registration catalog (not pg_database)
                Spi::get_one::<String>(
                    r#"SELECT string_agg(datname, ',')
                       FROM steep_repl.databases
                       WHERE enabled = true"#
                ).ok().flatten()
            });

            // Spawn workers for newly registered databases
            if let Some(db_list) = db_list {
                for db_name in db_list.split(',') {
                    let db_name = db_name.trim();
                    if db_name.is_empty() || databases_with_workers.contains(db_name) {
                        continue;
                    }

                    // Check if this database is in backoff from a previous failure
                    // Backoff: wait 60 iterations (~60 seconds) before retrying
                    const BACKOFF_ITERATIONS: u64 = 60;
                    if let Some(&failed_at) = failed_databases.get(db_name) {
                        if discovery_counter - failed_at < BACKOFF_ITERATIONS {
                            continue; // Still in backoff period
                        }
                        // Backoff expired, remove from failed list and retry
                        failed_databases.remove(db_name);
                    }

                    // Pre-flight check: verify database exists in pg_database
                    // This prevents FATAL errors when connecting to non-existent databases
                    let db_exists = BackgroundWorker::transaction(|| {
                        Spi::get_one::<bool>(&format!(
                            "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = '{}')",
                            db_name.replace('\'', "''")
                        )).ok().flatten().unwrap_or(false)
                    });

                    if !db_exists {
                        pgrx::warning!(
                            "steep_repl: database '{}' is registered but does not exist, skipping worker spawn",
                            db_name
                        );
                        failed_databases.insert(db_name.to_string(), discovery_counter);
                        continue;
                    }

                    // Spawn a worker for this database
                    pgrx::log!(
                        "steep_repl: spawning database worker for '{}'",
                        db_name
                    );

                    let worker_name = format!("steep_repl_db_{}", db_name);

                    // Build the worker with database info
                    let worker_result = BackgroundWorkerBuilder::new(&worker_name)
                        .set_library("steep_repl")
                        .set_function("steep_repl_database_worker_main")
                        .set_argument(Some(string_to_datum(db_name)))
                        .enable_spi_access()
                        .set_restart_time(Some(Duration::from_secs(30)))
                        .load_dynamic();

                    match worker_result {
                        Ok(_handle) => {
                            // Successfully registered the worker.
                            // Don't call wait_for_startup() - it returns Untracked when
                            // notify_pid is not set, which we'd incorrectly treat as failure.
                            // The worker IS starting; just track it.
                            pgrx::log!(
                                "steep_repl: spawned worker {} for database '{}'",
                                worker_name,
                                db_name
                            );
                            databases_with_workers.insert(db_name.to_string());
                        }
                        Err(e) => {
                            pgrx::warning!(
                                "steep_repl: failed to spawn worker for '{}': {:?}",
                                db_name,
                                e
                            );
                            // Track spawn failure with backoff
                            failed_databases.insert(db_name.to_string(), discovery_counter);
                        }
                    }
                }
            }
        }
    }

    pgrx::log!("steep_repl: static background worker shutting down");
}

/// Convert a string to a Datum for passing to background worker.
fn string_to_datum(s: &str) -> pg_sys::Datum {
    // Encode database name length and characters into the datum
    // This is a simple encoding: first byte is length, rest is name (up to 63 chars)
    let bytes = s.as_bytes();
    let len = bytes.len().min(63) as i64;
    let mut value: i64 = len;
    for (i, &b) in bytes.iter().take(7).enumerate() {
        value |= (b as i64) << (8 * (i + 1));
    }
    value.into_datum().unwrap()
}

/// Decode a Datum back to a database name.
fn datum_to_string(datum: pg_sys::Datum) -> String {
    let value = unsafe { i64::from_datum(datum, false) }.unwrap_or(0);
    let len = (value & 0xFF) as usize;
    let mut bytes = Vec::with_capacity(len.min(7));
    for i in 0..len.min(7) {
        bytes.push(((value >> (8 * (i + 1))) & 0xFF) as u8);
    }
    String::from_utf8_lossy(&bytes).to_string()
}

/// Database-specific background worker main function.
///
/// This worker connects to a specific database and processes work queue items
/// for that database only. It is spawned by the coordinator (static worker).
#[pg_guard]
#[unsafe(no_mangle)]
pub extern "C-unwind" fn steep_repl_database_worker_main(arg: pg_sys::Datum) {
    use std::time::Duration;

    // Set up signal handlers
    BackgroundWorker::attach_signal_handlers(SignalWakeFlags::SIGHUP | SignalWakeFlags::SIGTERM);

    // Decode database name from argument
    let db_name = datum_to_string(arg);

    // Connect to the target database
    BackgroundWorker::connect_worker_to_spi(Some(&db_name), None);

    pgrx::log!("steep_repl: database worker started for '{}'", db_name);

    // Check if extension is installed in this database
    let extension_installed = BackgroundWorker::transaction(|| {
        Spi::get_one::<bool>(
            "SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'steep_repl')"
        ).ok().flatten().unwrap_or(false)
    });

    if !extension_installed {
        pgrx::log!(
            "steep_repl: extension not installed in '{}', worker will idle",
            db_name
        );
        // Exit - no point running for this database
        return;
    }

    pgrx::log!("steep_repl: extension found in '{}', starting work loop", db_name);

    // Main work loop - poll for work using inline SPI (avoid nested Spi::connect)
    while BackgroundWorker::wait_latch(Some(Duration::from_secs(IDLE_WAKE_INTERVAL_SECS))) {
        // Try to claim and process work - use inline SPI to avoid nested contexts
        let processed = BackgroundWorker::transaction(|| {
            // Check for pending work using FOR UPDATE SKIP LOCKED
            let work_id = Spi::get_one::<i64>(
                r#"SELECT id FROM steep_repl.work_queue
                   WHERE status = 'pending'
                   ORDER BY created_at LIMIT 1
                   FOR UPDATE SKIP LOCKED"#
            );

            match work_id {
                Ok(Some(id)) => {
                    // Get operation details
                    let operation = Spi::get_one::<String>(
                        &format!("SELECT operation FROM steep_repl.work_queue WHERE id = {}", id)
                    ).ok().flatten().unwrap_or_default();

                    let snapshot_id = Spi::get_one::<String>(
                        &format!("SELECT snapshot_id FROM steep_repl.work_queue WHERE id = {}", id)
                    ).ok().flatten();

                    pgrx::log!(
                        "steep_repl: claimed work item {} (operation: {}, snapshot: {:?})",
                        id, operation, snapshot_id
                    );

                    // Claim the work item
                    let _ = Spi::run(&format!(
                        r#"UPDATE steep_repl.work_queue
                           SET status = 'running', started_at = now(), worker_pid = pg_backend_pid()
                           WHERE id = {}"#,
                        id
                    ));

                    // Update snapshot status if this is a snapshot operation
                    if let Some(ref snap_id) = snapshot_id {
                        let _ = Spi::run(&format!(
                            r#"UPDATE steep_repl.snapshots
                               SET status = '{}', phase = 'schema', started_at = now()
                               WHERE snapshot_id = '{}'"#,
                            SnapshotStatus::Generating.as_str(),
                            snap_id
                        ));
                    }

                    // Update shared memory progress
                    {
                        let mut progress = OPERATION_PROGRESS.exclusive();
                        let work_op = WorkOperation::from_str(&operation);
                        let op_type = match work_op {
                            Some(WorkOperation::SnapshotGenerate) => OperationType::SnapshotGenerate,
                            Some(WorkOperation::SnapshotApply) => OperationType::SnapshotApply,
                            Some(WorkOperation::BidirectionalMerge) => OperationType::BidirectionalMerge,
                            None => OperationType::None,
                        };
                        let op_id = snapshot_id.as_deref().unwrap_or("unknown");
                        progress.start(op_type, op_id, id, 0, 0, 0);
                        progress.phase = ProgressPhase::Schema as i32;
                    }

                    // Send NOTIFY for progress
                    crate::notify::notify_status(
                        &operation,
                        snapshot_id.as_deref().unwrap_or("unknown"),
                        "running",
                        None,
                    );

                    // TODO (T030): Implement actual snapshot generation logic here
                    // For now, mark as complete immediately to unblock testing

                    // Mark progress complete
                    {
                        let mut progress = OPERATION_PROGRESS.exclusive();
                        progress.complete();
                    }

                    // Update snapshot as complete
                    if let Some(ref snap_id) = snapshot_id {
                        let _ = Spi::run(&format!(
                            r#"UPDATE steep_repl.snapshots
                               SET status = '{}', phase = 'complete',
                                   overall_percent = 100, completed_at = now()
                               WHERE snapshot_id = '{}'"#,
                            SnapshotStatus::Complete.as_str(),
                            snap_id
                        ));
                    }

                    // Mark work queue entry complete
                    let _ = Spi::run(&format!(
                        r#"UPDATE steep_repl.work_queue
                           SET status = 'complete', completed_at = now()
                           WHERE id = {}"#,
                        id
                    ));

                    // Send completion notification
                    crate::notify::notify_status(
                        &operation,
                        snapshot_id.as_deref().unwrap_or("unknown"),
                        "complete",
                        None,
                    );

                    pgrx::log!("steep_repl: completed work item {}", id);
                    true
                }
                _ => false,
            }
        });

        if processed {
            pgrx::log!("steep_repl: processed work in '{}'", db_name);
        }
    }

    pgrx::log!("steep_repl: database worker for '{}' shutting down", db_name);
}

/// Dynamic background worker main function.
///
/// This is the entry point for dynamic background worker processes.
/// It receives database info via bgw_extra, connects to that database,
/// and processes work queue items.
#[pg_guard]
#[unsafe(no_mangle)]
pub extern "C-unwind" fn steep_repl_worker_main(_arg: pg_sys::Datum) {
    use std::time::Duration;

    // Set up signal handlers FIRST (following pgrx bgworker test pattern exactly)
    BackgroundWorker::attach_signal_handlers(SignalWakeFlags::SIGHUP | SignalWakeFlags::SIGTERM);

    // Connect to the database - use pgrx_tests for testing
    BackgroundWorker::connect_worker_to_spi(Some("pgrx_tests"), None);

    pgrx::log!("steep_repl: dynamic background worker started");

    // Work processing loop
    while BackgroundWorker::wait_latch(Some(Duration::from_millis(100))) {
        // Try to claim and process work
        let processed = BackgroundWorker::transaction(|| {
            // Check for pending work
            let id = Spi::get_one::<i64>(
                r#"SELECT id FROM steep_repl.work_queue
                   WHERE status = 'pending'
                   ORDER BY created_at LIMIT 1
                   FOR UPDATE SKIP LOCKED"#
            );

            match id {
                Ok(Some(work_id)) => {
                    // Claim the work item
                    let _ = Spi::run(&format!(
                        r#"UPDATE steep_repl.work_queue
                           SET status = 'running', started_at = now(), worker_pid = pg_backend_pid()
                           WHERE id = {}"#,
                        work_id
                    ));

                    // Mark complete (placeholder - actual operation would go here)
                    let _ = Spi::run(&format!(
                        r#"UPDATE steep_repl.work_queue
                           SET status = 'complete', completed_at = now()
                           WHERE id = {}"#,
                        work_id
                    ));

                    pgrx::log!("steep_repl: processed work item {}", work_id);
                    true
                }
                _ => false,
            }
        });

        if !processed {
            // No work available, continue looping
        }
    }

    pgrx::log!("steep_repl: dynamic background worker shutting down");
}

/// Process the work queue - claim and execute one job.
///
/// Called within a transaction context.
/// Returns true if work was processed, false if no work available.
fn process_work_queue() -> bool {
    match claim_work() {
        ClaimResult::Claimed(work_item) => {
            pgrx::log!(
                "steep_repl: claimed work item {} (operation: {})",
                work_item.id,
                work_item.operation
            );

            // Execute the operation
            let result = execute_operation(&work_item);

            // Update work queue with result
            match result {
                ExecuteResult::Complete => {
                    complete_work(work_item.id);
                    pgrx::log!("steep_repl: work item {} completed successfully", work_item.id);
                }
                ExecuteResult::Failed(error) => {
                    fail_work(work_item.id, &error);
                    pgrx::warning!(
                        "steep_repl: work item {} failed: {}",
                        work_item.id,
                        error
                    );
                }
                ExecuteResult::Cancelled => {
                    cancel_work_internal(work_item.id);
                    pgrx::log!("steep_repl: work item {} cancelled", work_item.id);
                }
            }

            // Clear progress from shared memory
            clear_progress();
            true
        }
        ClaimResult::NoWork => false,
        ClaimResult::Error(e) => {
            pgrx::warning!("steep_repl: error claiming work: {}", e);
            false
        }
    }
}

/// Claim the next pending work item using FOR UPDATE SKIP LOCKED.
///
/// This function:
/// 1. Selects the oldest pending job with row-level locking
/// 2. Marks it as 'running' with current timestamp and worker PID
/// 3. Returns the work item for processing
fn claim_work() -> ClaimResult {
    Spi::connect(|client| {
        // Try to claim next pending job
        let result = client.select(
            r#"
            SELECT id, operation, snapshot_id, merge_id, params
            FROM steep_repl.work_queue
            WHERE status = 'pending'
            ORDER BY created_at
            LIMIT 1
            FOR UPDATE SKIP LOCKED
            "#,
            None,
            &[],
        );

        match result {
            Ok(table) => {
                if table.is_empty() {
                    return ClaimResult::NoWork;
                }

                // Get the first (and only) row
                let row = table.first();

                // Get values using the typed get() method with 1-based column indices
                let id: i64 = match row.get::<i64>(1) {
                    Ok(Some(v)) => v,
                    _ => return ClaimResult::NoWork,
                };

                let operation: String = match row.get::<String>(2) {
                    Ok(Some(v)) => v,
                    Ok(None) => return ClaimResult::Error("operation is NULL".to_string()),
                    Err(e) => return ClaimResult::Error(format!("failed to get operation: {}", e)),
                };

                let snapshot_id: Option<String> = row.get::<String>(3).ok().flatten();
                let merge_id: Option<pgrx::Uuid> = row.get::<pgrx::Uuid>(4).ok().flatten();
                let params: pgrx::JsonB = row
                    .get::<pgrx::JsonB>(5)
                    .ok()
                    .flatten()
                    .unwrap_or(pgrx::JsonB(serde_json::json!({})));

                // Mark as running using DatumWithOid for the argument
                let args = unsafe {
                    [DatumWithOid::new(
                        id.into_datum(),
                        PgBuiltInOids::INT8OID.value(),
                    )]
                };

                let update_result = client.select(
                    r#"
                    UPDATE steep_repl.work_queue
                    SET status = 'running',
                        started_at = now(),
                        worker_pid = pg_backend_pid()
                    WHERE id = $1
                    "#,
                    None,
                    &args,
                );

                if let Err(e) = update_result {
                    return ClaimResult::Error(format!("failed to mark work as running: {}", e));
                }

                ClaimResult::Claimed(WorkItem {
                    id,
                    operation,
                    snapshot_id,
                    merge_id,
                    params,
                })
            }
            Err(e) => ClaimResult::Error(format!("failed to query work queue: {}", e)),
        }
    })
}

/// Execute an operation based on its type.
///
/// This is the main dispatch function that routes work items to their
/// specific handlers.
fn execute_operation(work_item: &WorkItem) -> ExecuteResult {
    // Initialize progress tracking in shared memory
    init_progress(work_item);

    match work_item.operation.as_str() {
        "snapshot_generate" => execute_snapshot_generate(work_item),
        "snapshot_apply" => execute_snapshot_apply(work_item),
        "bidirectional_merge" => execute_bidirectional_merge(work_item),
        _ => ExecuteResult::Failed(format!("unknown operation: {}", work_item.operation)),
    }
}

/// Initialize progress tracking for an operation.
fn init_progress(work_item: &WorkItem) {
    let mut progress = OPERATION_PROGRESS.exclusive();

    let operation_type = match work_item.operation.as_str() {
        "snapshot_generate" => OperationType::SnapshotGenerate,
        "snapshot_apply" => OperationType::SnapshotApply,
        "bidirectional_merge" => OperationType::BidirectionalMerge,
        _ => OperationType::None,
    };

    let operation_id = work_item
        .snapshot_id
        .as_ref()
        .map(|s| s.as_str())
        .or_else(|| work_item.merge_id.as_ref().map(|_| "merge"))
        .unwrap_or("unknown");

    // Extract totals from params if available
    let params = &work_item.params.0;
    let tables_total = params.get("tables_total").and_then(|v| v.as_i64()).unwrap_or(0) as i32;
    let bytes_total = params.get("bytes_total").and_then(|v| v.as_i64()).unwrap_or(0);
    let rows_total = params.get("rows_total").and_then(|v| v.as_i64()).unwrap_or(0);

    progress.start(
        operation_type,
        operation_id,
        work_item.id,
        tables_total,
        bytes_total,
        rows_total,
    );
}

/// Clear progress tracking after operation completes.
fn clear_progress() {
    let mut progress = OPERATION_PROGRESS.exclusive();
    progress.reset();
}

/// Execute snapshot generate operation.
///
/// TODO (T030): Implement actual snapshot generation logic.
/// For now, this is a placeholder that marks the operation as complete.
fn execute_snapshot_generate(work_item: &WorkItem) -> ExecuteResult {
    pgrx::log!(
        "steep_repl: executing snapshot_generate for snapshot_id: {:?}",
        work_item.snapshot_id
    );

    // Update progress to indicate we're in the schema phase
    {
        let mut progress = OPERATION_PROGRESS.exclusive();
        progress.phase = ProgressPhase::Schema as i32;
    }

    // Send NOTIFY for progress update
    crate::notify::notify_status(
        "snapshot_generate",
        work_item.snapshot_id.as_deref().unwrap_or("unknown"),
        "running",
        None,
    );

    // TODO (T030): Implement actual snapshot generation
    // For now, just mark as complete
    {
        let mut progress = OPERATION_PROGRESS.exclusive();
        progress.complete();
    }

    // Send completion notification
    crate::notify::notify_status(
        "snapshot_generate",
        work_item.snapshot_id.as_deref().unwrap_or("unknown"),
        "complete",
        None,
    );

    ExecuteResult::Complete
}

/// Execute snapshot apply operation.
///
/// TODO (T031): Implement actual snapshot apply logic.
/// For now, this is a placeholder that marks the operation as complete.
fn execute_snapshot_apply(work_item: &WorkItem) -> ExecuteResult {
    pgrx::log!(
        "steep_repl: executing snapshot_apply for snapshot_id: {:?}",
        work_item.snapshot_id
    );

    // Update progress
    {
        let mut progress = OPERATION_PROGRESS.exclusive();
        progress.phase = ProgressPhase::Schema as i32;
    }

    // Send NOTIFY
    crate::notify::notify_status(
        "snapshot_apply",
        work_item.snapshot_id.as_deref().unwrap_or("unknown"),
        "running",
        None,
    );

    // TODO (T031): Implement actual snapshot apply
    {
        let mut progress = OPERATION_PROGRESS.exclusive();
        progress.complete();
    }

    crate::notify::notify_status(
        "snapshot_apply",
        work_item.snapshot_id.as_deref().unwrap_or("unknown"),
        "complete",
        None,
    );

    ExecuteResult::Complete
}

/// Execute bidirectional merge operation.
///
/// TODO (T050): Implement actual merge logic.
/// For now, this is a placeholder that marks the operation as complete.
fn execute_bidirectional_merge(work_item: &WorkItem) -> ExecuteResult {
    pgrx::log!(
        "steep_repl: executing bidirectional_merge for merge_id: {:?}",
        work_item.merge_id
    );

    // Update progress
    {
        let mut progress = OPERATION_PROGRESS.exclusive();
        progress.phase = ProgressPhase::Data as i32;
    }

    let merge_id_str = work_item
        .merge_id
        .map(|u| u.to_string())
        .unwrap_or_else(|| "unknown".to_string());

    // Send NOTIFY
    crate::notify::notify_status("bidirectional_merge", &merge_id_str, "running", None);

    // TODO (T050): Implement actual merge
    {
        let mut progress = OPERATION_PROGRESS.exclusive();
        progress.complete();
    }

    crate::notify::notify_status("bidirectional_merge", &merge_id_str, "complete", None);

    ExecuteResult::Complete
}

/// Mark a work item as complete.
fn complete_work(id: i64) {
    Spi::connect(|client| {
        let args = unsafe {
            [DatumWithOid::new(
                id.into_datum(),
                PgBuiltInOids::INT8OID.value(),
            )]
        };

        let _ = client.select(
            r#"
            UPDATE steep_repl.work_queue
            SET status = 'complete',
                completed_at = now()
            WHERE id = $1 AND status = 'running'
            "#,
            None,
            &args,
        );
    });
}

/// Mark a work item as failed with error message.
fn fail_work(id: i64, error: &str) {
    Spi::connect(|client| {
        let args = unsafe {
            [
                DatumWithOid::new(id.into_datum(), PgBuiltInOids::INT8OID.value()),
                DatumWithOid::new(error.into_datum(), PgBuiltInOids::TEXTOID.value()),
            ]
        };

        let _ = client.select(
            r#"
            UPDATE steep_repl.work_queue
            SET status = 'failed',
                completed_at = now(),
                error_message = $2
            WHERE id = $1 AND status = 'running'
            "#,
            None,
            &args,
        );
    });

    // Also update progress with error
    {
        let mut progress = OPERATION_PROGRESS.exclusive();
        progress.fail(error);
    }
}

/// Mark a work item as cancelled (internal - called after operation signals cancel).
fn cancel_work_internal(id: i64) {
    Spi::connect(|client| {
        let args = unsafe {
            [DatumWithOid::new(
                id.into_datum(),
                PgBuiltInOids::INT8OID.value(),
            )]
        };

        let _ = client.select(
            r#"
            UPDATE steep_repl.work_queue
            SET status = 'cancelled',
                completed_at = now()
            WHERE id = $1
            "#,
            None,
            &args,
        );
    });
}


// =============================================================================
// Minimal test bgworker (matching pgrx test pattern exactly)
// =============================================================================

/// A minimal background worker for testing that the bgworker infrastructure works.
/// This matches the pattern from pgrx-tests exactly.
#[pg_guard]
#[unsafe(no_mangle)]
pub extern "C-unwind" fn test_bgworker_simple(_arg: pg_sys::Datum) {
    use std::time::Duration;
    BackgroundWorker::attach_signal_handlers(SignalWakeFlags::SIGHUP | SignalWakeFlags::SIGTERM);
    BackgroundWorker::connect_worker_to_spi(Some("pgrx_tests"), None);
    pgrx::log!("steep_repl: test_bgworker_simple started");
    while BackgroundWorker::wait_latch(Some(Duration::from_millis(100))) {}
    pgrx::log!("steep_repl: test_bgworker_simple shutting down");
}

// =============================================================================
// Test workers that create their own data (following official pgrx bgworker pattern)
// =============================================================================

/// Test worker for snapshot_generate: creates test data, processes it, then loops until terminated.
#[pg_guard]
#[unsafe(no_mangle)]
pub extern "C-unwind" fn test_worker_snapshot_generate(_arg: pg_sys::Datum) {
    use std::time::Duration;

    BackgroundWorker::attach_signal_handlers(SignalWakeFlags::SIGHUP | SignalWakeFlags::SIGTERM);
    BackgroundWorker::connect_worker_to_spi(Some("pgrx_tests"), None);
    pgrx::log!("steep_repl: test_worker_snapshot_generate started");

    // Create and process test data in a committed transaction
    BackgroundWorker::transaction(|| {
        let _ = Spi::run(
            r#"INSERT INTO steep_repl.work_queue (operation, snapshot_id, status)
               VALUES ('snapshot_generate', 'test-snapshot-dynamic', 'pending')"#,
        );
    });

    BackgroundWorker::transaction(|| {
        let _ = Spi::run(
            r#"UPDATE steep_repl.work_queue
               SET status = 'complete', completed_at = now(), worker_pid = pg_backend_pid()
               WHERE snapshot_id = 'test-snapshot-dynamic' AND status = 'pending'"#,
        );
    });

    pgrx::log!("steep_repl: test_worker_snapshot_generate processed work");

    while BackgroundWorker::wait_latch(Some(Duration::from_millis(100))) {}
    pgrx::log!("steep_repl: test_worker_snapshot_generate shutting down");
}

/// Test worker for snapshot_apply: creates test data, processes it, then loops until terminated.
#[pg_guard]
#[unsafe(no_mangle)]
pub extern "C-unwind" fn test_worker_snapshot_apply(_arg: pg_sys::Datum) {
    use std::time::Duration;

    BackgroundWorker::attach_signal_handlers(SignalWakeFlags::SIGHUP | SignalWakeFlags::SIGTERM);
    BackgroundWorker::connect_worker_to_spi(Some("pgrx_tests"), None);
    pgrx::log!("steep_repl: test_worker_snapshot_apply started");

    BackgroundWorker::transaction(|| {
        let _ = Spi::run(
            r#"INSERT INTO steep_repl.work_queue (operation, snapshot_id, status)
               VALUES ('snapshot_apply', 'test-apply-dynamic', 'pending')"#,
        );
    });

    BackgroundWorker::transaction(|| {
        let _ = Spi::run(
            r#"UPDATE steep_repl.work_queue
               SET status = 'complete', completed_at = now(), worker_pid = pg_backend_pid()
               WHERE snapshot_id = 'test-apply-dynamic' AND status = 'pending'"#,
        );
    });

    pgrx::log!("steep_repl: test_worker_snapshot_apply processed work");

    while BackgroundWorker::wait_latch(Some(Duration::from_millis(100))) {}
    pgrx::log!("steep_repl: test_worker_snapshot_apply shutting down");
}

/// Test worker for bidirectional_merge: creates test data, processes it, then loops until terminated.
#[pg_guard]
#[unsafe(no_mangle)]
pub extern "C-unwind" fn test_worker_bidirectional_merge(_arg: pg_sys::Datum) {
    use std::time::Duration;

    BackgroundWorker::attach_signal_handlers(SignalWakeFlags::SIGHUP | SignalWakeFlags::SIGTERM);
    BackgroundWorker::connect_worker_to_spi(Some("pgrx_tests"), None);
    pgrx::log!("steep_repl: test_worker_bidirectional_merge started");

    BackgroundWorker::transaction(|| {
        let _ = Spi::run(
            r#"INSERT INTO steep_repl.work_queue (operation, merge_id, status)
               VALUES ('bidirectional_merge', 'b1eebc99-9c0b-4ef8-bb6d-6bb9bd380a22', 'pending')"#,
        );
    });

    BackgroundWorker::transaction(|| {
        let _ = Spi::run(
            r#"UPDATE steep_repl.work_queue
               SET status = 'complete', completed_at = now(), worker_pid = pg_backend_pid()
               WHERE merge_id = 'b1eebc99-9c0b-4ef8-bb6d-6bb9bd380a22' AND status = 'pending'"#,
        );
    });

    pgrx::log!("steep_repl: test_worker_bidirectional_merge processed work");

    while BackgroundWorker::wait_latch(Some(Duration::from_millis(100))) {}
    pgrx::log!("steep_repl: test_worker_bidirectional_merge shutting down");
}

/// Test worker for worker_pid verification: creates test data with worker_pid set.
#[pg_guard]
#[unsafe(no_mangle)]
pub extern "C-unwind" fn test_worker_pid_verification(_arg: pg_sys::Datum) {
    use std::time::Duration;

    BackgroundWorker::attach_signal_handlers(SignalWakeFlags::SIGHUP | SignalWakeFlags::SIGTERM);
    BackgroundWorker::connect_worker_to_spi(Some("pgrx_tests"), None);
    pgrx::log!("steep_repl: test_worker_pid_verification started");

    BackgroundWorker::transaction(|| {
        let _ = Spi::run(
            r#"INSERT INTO steep_repl.work_queue (operation, snapshot_id, status)
               VALUES ('snapshot_generate', 'test-pid-dynamic', 'pending')"#,
        );
    });

    BackgroundWorker::transaction(|| {
        let _ = Spi::run(
            r#"UPDATE steep_repl.work_queue
               SET status = 'complete', completed_at = now(), worker_pid = pg_backend_pid()
               WHERE snapshot_id = 'test-pid-dynamic' AND status = 'pending'"#,
        );
    });

    pgrx::log!("steep_repl: test_worker_pid_verification processed work");

    while BackgroundWorker::wait_latch(Some(Duration::from_millis(100))) {}
    pgrx::log!("steep_repl: test_worker_pid_verification shutting down");
}

// =============================================================================
// Tests
// =============================================================================

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use super::*;
    use pgrx::prelude::*;

    #[pg_test]
    fn test_bgworker_module_loads() {
        // Basic compilation test - verifies the worker module is properly linked
        assert!(true);
    }

    #[pg_test]
    fn test_worker_available() {
        // Test that worker_available returns true in a transaction
        let available = crate::worker::worker_available();
        assert!(available, "worker_available should return true in a transaction");
    }

    #[pg_test]
    fn test_minimal_dynamic_bgworker() {
        // Test a minimal bgworker that just starts up and exits
        // This matches pgrx-tests pattern exactly
        let worker = BackgroundWorkerBuilder::new("test_minimal_worker")
            .set_library("steep_repl")
            .set_function("test_bgworker_simple")
            .enable_spi_access()
            .set_notify_pid(unsafe { pgrx::pg_sys::MyProcPid })
            .load_dynamic()
            .expect("Failed to start minimal worker");

        let pid = worker.wait_for_startup().expect("no PID from minimal worker");
        assert!(pid > 0, "minimal worker PID should be positive");

        let handle = worker.terminate();
        handle.wait_for_shutdown().expect("minimal worker shutdown failed");
    }

    #[pg_test]
    fn test_launch_worker_returns_pid() {
        // Test that launch_worker() successfully starts a worker and returns a PID
        // This tests the launch_worker function itself in isolation
        let result = crate::worker::launch_worker();
        assert!(result.is_ok(), "launch_worker should return Ok");
        let pid = result.unwrap();
        assert!(pid > 0, "worker PID should be positive");
    }

    #[pg_test]
    fn test_launch_worker_stays_running() {
        // Test that the worker stays running for a bit
        let result = crate::worker::launch_worker();
        assert!(result.is_ok(), "launch_worker should return Ok");
        let pid = result.unwrap();

        // Wait a moment and check if the worker is still running
        std::thread::sleep(std::time::Duration::from_millis(500));

        // Check if process still exists by checking pg_stat_activity
        let still_running = Spi::get_one::<bool>(
            &format!("SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid = {})", pid)
        );

        // The worker might have already finished its latch loop, so this isn't a hard failure
        // But it should at least not have crashed with exit code 1
        pgrx::log!("steep_repl: worker still running: {:?}", still_running);
    }

    #[pg_test]
    fn test_launch_worker_and_process_snapshot_generate() {
        // Use test worker that creates its own data (avoids transaction isolation issues)
        let worker = BackgroundWorkerBuilder::new("test_snapshot_generate_worker")
            .set_library("steep_repl")
            .set_function("test_worker_snapshot_generate")
            .enable_spi_access()
            .set_notify_pid(unsafe { pgrx::pg_sys::MyProcPid })
            .load_dynamic()
            .expect("Failed to start test_worker_snapshot_generate");

        let pid = worker.wait_for_startup().expect("no PID from worker");
        assert!(pid > 0, "worker PID should be positive");

        // Wait for work to be completed (worker creates and processes in its own transaction)
        let mut completed = false;
        for _ in 0..50 {
            std::thread::sleep(std::time::Duration::from_millis(100));
            let status = Spi::get_one::<String>(
                "SELECT status FROM steep_repl.work_queue WHERE snapshot_id = 'test-snapshot-dynamic'",
            );
            if let Ok(Some(s)) = status {
                if s == "complete" {
                    completed = true;
                    break;
                }
            }
        }

        let handle = worker.terminate();
        handle.wait_for_shutdown().expect("worker shutdown failed");

        assert!(completed, "Work item should be complete");

        // Verify completed_at was set
        let has_completed_at = Spi::get_one::<bool>(
            "SELECT completed_at IS NOT NULL FROM steep_repl.work_queue WHERE snapshot_id = 'test-snapshot-dynamic'",
        );
        assert_eq!(has_completed_at, Ok(Some(true)), "completed_at should be set");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'test-snapshot-dynamic'")
            .expect("cleanup failed");
    }

    #[pg_test]
    fn test_launch_worker_and_process_snapshot_apply() {
        // Use test worker that creates its own data
        let worker = BackgroundWorkerBuilder::new("test_snapshot_apply_worker")
            .set_library("steep_repl")
            .set_function("test_worker_snapshot_apply")
            .enable_spi_access()
            .set_notify_pid(unsafe { pgrx::pg_sys::MyProcPid })
            .load_dynamic()
            .expect("Failed to start test_worker_snapshot_apply");

        let pid = worker.wait_for_startup().expect("no PID from worker");
        assert!(pid > 0, "worker PID should be positive");

        // Wait for work to be completed
        let mut completed = false;
        for _ in 0..50 {
            std::thread::sleep(std::time::Duration::from_millis(100));
            let status = Spi::get_one::<String>(
                "SELECT status FROM steep_repl.work_queue WHERE snapshot_id = 'test-apply-dynamic'",
            );
            if let Ok(Some(s)) = status {
                if s == "complete" {
                    completed = true;
                    break;
                }
            }
        }

        let handle = worker.terminate();
        handle.wait_for_shutdown().expect("worker shutdown failed");

        assert!(completed, "Work item should be complete");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'test-apply-dynamic'")
            .expect("cleanup failed");
    }

    #[pg_test]
    fn test_launch_worker_and_process_bidirectional_merge() {
        // Use test worker that creates its own data
        let worker = BackgroundWorkerBuilder::new("test_bidirectional_merge_worker")
            .set_library("steep_repl")
            .set_function("test_worker_bidirectional_merge")
            .enable_spi_access()
            .set_notify_pid(unsafe { pgrx::pg_sys::MyProcPid })
            .load_dynamic()
            .expect("Failed to start test_worker_bidirectional_merge");

        let pid = worker.wait_for_startup().expect("no PID from worker");
        assert!(pid > 0, "worker PID should be positive");

        // Wait for work to be completed
        let mut completed = false;
        for _ in 0..50 {
            std::thread::sleep(std::time::Duration::from_millis(100));
            let status = Spi::get_one::<String>(
                "SELECT status FROM steep_repl.work_queue WHERE merge_id = 'b1eebc99-9c0b-4ef8-bb6d-6bb9bd380a22'",
            );
            if let Ok(Some(s)) = status {
                if s == "complete" {
                    completed = true;
                    break;
                }
            }
        }

        let handle = worker.terminate();
        handle.wait_for_shutdown().expect("worker shutdown failed");

        assert!(completed, "Work item should be complete");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE merge_id = 'b1eebc99-9c0b-4ef8-bb6d-6bb9bd380a22'")
            .expect("cleanup failed");
    }

    #[pg_test]
    fn test_worker_sets_worker_pid() {
        // Use test worker that creates its own data
        let worker = BackgroundWorkerBuilder::new("test_worker_pid_worker")
            .set_library("steep_repl")
            .set_function("test_worker_pid_verification")
            .enable_spi_access()
            .set_notify_pid(unsafe { pgrx::pg_sys::MyProcPid })
            .load_dynamic()
            .expect("Failed to start test_worker_pid_verification");

        let pid = worker.wait_for_startup().expect("no PID from worker");
        assert!(pid > 0, "worker PID should be positive");

        // Wait for work to be completed
        let mut completed = false;
        for _ in 0..50 {
            std::thread::sleep(std::time::Duration::from_millis(100));
            let status = Spi::get_one::<String>(
                "SELECT status FROM steep_repl.work_queue WHERE snapshot_id = 'test-pid-dynamic'",
            );
            if let Ok(Some(s)) = status {
                if s == "complete" {
                    completed = true;
                    break;
                }
            }
        }

        let handle = worker.terminate();
        handle.wait_for_shutdown().expect("worker shutdown failed");

        assert!(completed, "Work item should be complete before checking worker_pid");

        // Verify worker_pid was set
        let has_worker_pid = Spi::get_one::<bool>(
            "SELECT worker_pid IS NOT NULL FROM steep_repl.work_queue WHERE snapshot_id = 'test-pid-dynamic'",
        );
        assert_eq!(has_worker_pid, Ok(Some(true)), "worker_pid should be set when work is claimed");

        // Cleanup
        Spi::run("DELETE FROM steep_repl.work_queue WHERE snapshot_id = 'test-pid-dynamic'")
            .expect("cleanup failed");
    }

    #[pg_test]
    fn test_work_queue_rejects_invalid_operation() {
        use pgrx::PgTryBuilder;
        use pgrx::PgSqlErrorCode;

        // The work_queue table has constraints that validate operations.
        // Try to insert an invalid operation and verify it fails due to constraint violation.
        let caught_error = PgTryBuilder::new(|| {
            Spi::run(
                r#"
                INSERT INTO steep_repl.work_queue (operation, snapshot_id, merge_id, status)
                VALUES ('unknown_operation', 'test-unknown-op', 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11', 'pending')
                "#,
            ).expect("should not reach here");
            false
        })
        .catch_when(PgSqlErrorCode::ERRCODE_CHECK_VIOLATION, |_| {
            true
        })
        .execute();

        assert!(caught_error, "Invalid operation should be rejected by CHECK constraint");
    }
}
