//! Shared memory progress tracking for steep_repl extension.
//!
//! This module provides a shared memory struct for tracking snapshot/merge
//! operation progress. The background worker writes to this struct, and
//! SQL functions can read it to report real-time progress without table queries.
//!
//! T003: Create shared memory progress struct
//! T009: Implement shared memory progress read/write with PgLwLock

use pgrx::pg_shmem_init;
use pgrx::prelude::*;
use pgrx::lwlock::PgLwLock;
use pgrx::shmem::PGRXSharedMemory;

/// Maximum length for operation ID strings (snapshot_id, merge_id)
const OPERATION_ID_LEN: usize = 64;

/// Maximum length for current table name
const TABLE_NAME_LEN: usize = 128;

/// Maximum length for error message
const ERROR_MSG_LEN: usize = 256;

/// Progress phase enumeration
#[repr(i32)]
#[derive(Copy, Clone, Debug, PartialEq, Eq, Default)]
pub enum ProgressPhase {
    #[default]
    Idle = 0,
    Schema = 1,
    Data = 2,
    Sequences = 3,
    Indexes = 4,
    Constraints = 5,
    Verify = 6,
    Finalizing = 7,
    Complete = 8,
    Failed = 9,
}

impl ProgressPhase {
    /// Convert from i32 to ProgressPhase
    pub fn from_i32(value: i32) -> Self {
        match value {
            0 => ProgressPhase::Idle,
            1 => ProgressPhase::Schema,
            2 => ProgressPhase::Data,
            3 => ProgressPhase::Sequences,
            4 => ProgressPhase::Indexes,
            5 => ProgressPhase::Constraints,
            6 => ProgressPhase::Verify,
            7 => ProgressPhase::Finalizing,
            8 => ProgressPhase::Complete,
            9 => ProgressPhase::Failed,
            _ => ProgressPhase::Idle,
        }
    }

    /// Convert to string for SQL output
    pub fn as_str(&self) -> &'static str {
        match self {
            ProgressPhase::Idle => "idle",
            ProgressPhase::Schema => "schema",
            ProgressPhase::Data => "data",
            ProgressPhase::Sequences => "sequences",
            ProgressPhase::Indexes => "indexes",
            ProgressPhase::Constraints => "constraints",
            ProgressPhase::Verify => "verify",
            ProgressPhase::Finalizing => "finalizing",
            ProgressPhase::Complete => "complete",
            ProgressPhase::Failed => "failed",
        }
    }
}

/// Operation type enumeration
#[repr(i32)]
#[derive(Copy, Clone, Debug, PartialEq, Eq, Default)]
pub enum OperationType {
    #[default]
    None = 0,
    SnapshotGenerate = 1,
    SnapshotApply = 2,
    BidirectionalMerge = 3,
}

impl OperationType {
    /// Convert to string for SQL output
    pub fn as_str(&self) -> &'static str {
        match self {
            OperationType::None => "none",
            OperationType::SnapshotGenerate => "snapshot_generate",
            OperationType::SnapshotApply => "snapshot_apply",
            OperationType::BidirectionalMerge => "bidirectional_merge",
        }
    }
}

/// Shared memory progress struct for real-time operation tracking.
///
/// This struct is stored in PostgreSQL shared memory and protected by
/// a LWLock. The background worker holds an exclusive lock while writing,
/// and SQL functions acquire a shared lock while reading.
///
/// All strings are fixed-size arrays for shared memory compatibility.
/// Strings are null-terminated.
#[derive(Copy, Clone)]
pub struct OperationProgress {
    /// Whether an operation is currently active
    pub active: bool,

    /// Type of operation being tracked
    pub operation_type: i32,

    /// Operation ID (snapshot_id or merge_id as string)
    pub operation_id: [u8; OPERATION_ID_LEN],

    /// Current phase
    pub phase: i32,

    /// Overall progress percentage (0.0 - 100.0)
    pub overall_percent: f32,

    /// Number of tables completed
    pub tables_completed: i32,

    /// Total number of tables
    pub tables_total: i32,

    /// Bytes processed so far
    pub bytes_processed: i64,

    /// Total bytes to process (estimated)
    pub bytes_total: i64,

    /// Rows processed so far
    pub rows_processed: i64,

    /// Total rows to process (estimated)
    pub rows_total: i64,

    /// Current throughput in bytes per second
    pub throughput_bytes_sec: f32,

    /// Estimated time remaining in seconds
    pub eta_seconds: i32,

    /// Current table being processed (null-terminated)
    pub current_table: [u8; TABLE_NAME_LEN],

    /// Error message if failed (null-terminated)
    pub error_message: [u8; ERROR_MSG_LEN],

    /// Unix timestamp when operation started
    pub started_at: i64,

    /// Work queue job ID
    pub work_queue_id: i64,
}

impl Default for OperationProgress {
    fn default() -> Self {
        Self {
            active: false,
            operation_type: 0,
            operation_id: [0u8; OPERATION_ID_LEN],
            phase: 0,
            overall_percent: 0.0,
            tables_completed: 0,
            tables_total: 0,
            bytes_processed: 0,
            bytes_total: 0,
            rows_processed: 0,
            rows_total: 0,
            throughput_bytes_sec: 0.0,
            eta_seconds: 0,
            current_table: [0u8; TABLE_NAME_LEN],
            error_message: [0u8; ERROR_MSG_LEN],
            started_at: 0,
            work_queue_id: 0,
        }
    }
}

// SAFETY: OperationProgress contains only fixed-size primitive types and arrays
// of u8. No heap allocations, pointers, or references that could be invalidated.
// All field types (bool, i32, i64, f32, [u8; N]) are safe for shared memory.
unsafe impl PGRXSharedMemory for OperationProgress {}

impl OperationProgress {
    /// Reset progress to idle state
    pub fn reset(&mut self) {
        *self = Self::default();
    }

    /// Start tracking a new operation
    pub fn start(
        &mut self,
        operation_type: OperationType,
        operation_id: &str,
        work_queue_id: i64,
        tables_total: i32,
        bytes_total: i64,
        rows_total: i64,
    ) {
        self.reset();
        self.active = true;
        self.operation_type = operation_type as i32;
        self.set_operation_id(operation_id);
        self.work_queue_id = work_queue_id;
        self.tables_total = tables_total;
        self.bytes_total = bytes_total;
        self.rows_total = rows_total;
        self.phase = ProgressPhase::Schema as i32;
        self.started_at = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs() as i64)
            .unwrap_or(0);
    }

    /// Update progress during operation
    pub fn update(
        &mut self,
        phase: ProgressPhase,
        tables_completed: i32,
        bytes_processed: i64,
        rows_processed: i64,
        current_table: &str,
    ) {
        self.phase = phase as i32;
        self.tables_completed = tables_completed;
        self.bytes_processed = bytes_processed;
        self.rows_processed = rows_processed;
        self.set_current_table(current_table);

        // Calculate overall percent
        if self.bytes_total > 0 {
            self.overall_percent = (bytes_processed as f64 / self.bytes_total as f64 * 100.0) as f32;
        } else if self.rows_total > 0 {
            self.overall_percent = (rows_processed as f64 / self.rows_total as f64 * 100.0) as f32;
        } else if self.tables_total > 0 {
            self.overall_percent = (tables_completed as f64 / self.tables_total as f64 * 100.0) as f32;
        }

        // Calculate throughput and ETA
        let elapsed = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs() as i64)
            .unwrap_or(0) - self.started_at;

        if elapsed > 0 && bytes_processed > 0 {
            self.throughput_bytes_sec = (bytes_processed as f64 / elapsed as f64) as f32;
            let remaining_bytes = self.bytes_total - bytes_processed;
            if self.throughput_bytes_sec > 0.0 {
                self.eta_seconds = (remaining_bytes as f64 / self.throughput_bytes_sec as f64) as i32;
            }
        }
    }

    /// Mark operation as complete
    pub fn complete(&mut self) {
        self.phase = ProgressPhase::Complete as i32;
        self.overall_percent = 100.0;
        self.eta_seconds = 0;
        self.active = false;
    }

    /// Mark operation as failed
    pub fn fail(&mut self, error: &str) {
        self.phase = ProgressPhase::Failed as i32;
        self.set_error_message(error);
        self.active = false;
    }

    /// Set operation ID (null-terminated string copy)
    fn set_operation_id(&mut self, id: &str) {
        let bytes = id.as_bytes();
        let len = bytes.len().min(OPERATION_ID_LEN - 1);
        self.operation_id[..len].copy_from_slice(&bytes[..len]);
        self.operation_id[len] = 0;
    }

    /// Set current table (null-terminated string copy)
    fn set_current_table(&mut self, table: &str) {
        let bytes = table.as_bytes();
        let len = bytes.len().min(TABLE_NAME_LEN - 1);
        self.current_table[..len].copy_from_slice(&bytes[..len]);
        self.current_table[len] = 0;
        // Zero out rest
        for i in (len + 1)..TABLE_NAME_LEN {
            self.current_table[i] = 0;
        }
    }

    /// Set error message (null-terminated string copy)
    fn set_error_message(&mut self, msg: &str) {
        let bytes = msg.as_bytes();
        let len = bytes.len().min(ERROR_MSG_LEN - 1);
        self.error_message[..len].copy_from_slice(&bytes[..len]);
        self.error_message[len] = 0;
    }

    /// Get operation ID as string
    pub fn get_operation_id(&self) -> String {
        // Find null terminator
        let len = self.operation_id.iter().position(|&c| c == 0).unwrap_or(OPERATION_ID_LEN);
        String::from_utf8_lossy(&self.operation_id[..len]).to_string()
    }

    /// Get current table as string
    pub fn get_current_table(&self) -> String {
        let len = self.current_table.iter().position(|&c| c == 0).unwrap_or(TABLE_NAME_LEN);
        String::from_utf8_lossy(&self.current_table[..len]).to_string()
    }

    /// Get error message as string
    pub fn get_error_message(&self) -> String {
        let len = self.error_message.iter().position(|&c| c == 0).unwrap_or(ERROR_MSG_LEN);
        String::from_utf8_lossy(&self.error_message[..len]).to_string()
    }

    /// Get phase as enum
    pub fn get_phase(&self) -> ProgressPhase {
        ProgressPhase::from_i32(self.phase)
    }

    /// Get operation type as enum
    pub fn get_operation_type(&self) -> OperationType {
        match self.operation_type {
            1 => OperationType::SnapshotGenerate,
            2 => OperationType::SnapshotApply,
            3 => OperationType::BidirectionalMerge,
            _ => OperationType::None,
        }
    }
}

/// Global shared memory progress struct protected by LWLock.
///
/// Usage:
/// - Background worker acquires exclusive lock for writes
/// - SQL functions acquire shared lock for reads
pub static OPERATION_PROGRESS: PgLwLock<OperationProgress> = unsafe {
    PgLwLock::new(c"steep_repl_progress")
};

/// Initialize shared memory for progress tracking.
/// This must be called from _PG_init().
pub fn init_shared_memory() {
    pg_shmem_init!(OPERATION_PROGRESS);
}

// =============================================================================
// SQL Functions for Progress Access
// =============================================================================

/// Check if an operation is currently running (internal implementation).
#[pg_extern]
fn _steep_repl_is_operation_active() -> bool {
    let progress = OPERATION_PROGRESS.share();
    progress.active
}

/// Get the current operation's overall progress percentage (internal implementation).
/// Returns NULL if no operation is active.
#[pg_extern]
fn _steep_repl_get_progress_percent() -> Option<f32> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active || progress.phase == ProgressPhase::Complete as i32 {
        Some(progress.overall_percent)
    } else {
        None
    }
}

/// Get the current operation's phase as a string (internal implementation).
/// Returns NULL if no operation is active.
#[pg_extern]
fn _steep_repl_get_progress_phase() -> Option<String> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active || progress.phase == ProgressPhase::Complete as i32 || progress.phase == ProgressPhase::Failed as i32 {
        Some(progress.get_phase().as_str().to_string())
    } else {
        None
    }
}

/// Get the current table being processed (internal implementation).
/// Returns NULL if no operation is active or no table is being processed.
#[pg_extern]
fn _steep_repl_get_progress_current_table() -> Option<String> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active {
        let table = progress.get_current_table();
        if table.is_empty() {
            None
        } else {
            Some(table)
        }
    } else {
        None
    }
}

/// Get the current operation's ETA in seconds (internal implementation).
/// Returns NULL if no operation is active or ETA cannot be calculated.
#[pg_extern]
fn _steep_repl_get_progress_eta_seconds() -> Option<i32> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active && progress.eta_seconds > 0 {
        Some(progress.eta_seconds)
    } else {
        None
    }
}

/// Get error message if last operation failed (internal implementation).
/// Returns NULL if no error.
#[pg_extern]
fn _steep_repl_get_progress_error() -> Option<String> {
    let progress = OPERATION_PROGRESS.share();
    if progress.phase == ProgressPhase::Failed as i32 {
        let error = progress.get_error_message();
        if error.is_empty() {
            None
        } else {
            Some(error)
        }
    } else {
        None
    }
}

/// Get work queue ID of current operation (internal implementation).
/// Returns NULL if no operation is active.
#[pg_extern]
fn _steep_repl_get_progress_work_queue_id() -> Option<i64> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active || progress.phase != ProgressPhase::Idle as i32 {
        Some(progress.work_queue_id)
    } else {
        None
    }
}

/// Get operation ID of current operation (internal implementation).
/// Returns NULL if no operation is active.
#[pg_extern]
fn _steep_repl_get_progress_operation_id() -> Option<String> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active || progress.phase != ProgressPhase::Idle as i32 {
        Some(progress.get_operation_id())
    } else {
        None
    }
}

/// Get operation type of current operation (internal implementation).
/// Returns NULL if no operation is active.
#[pg_extern]
fn _steep_repl_get_progress_operation_type() -> Option<String> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active || progress.phase != ProgressPhase::Idle as i32 {
        Some(progress.get_operation_type().as_str().to_string())
    } else {
        None
    }
}

/// Get tables completed count (internal implementation).
/// Returns NULL if no operation is active.
#[pg_extern]
fn _steep_repl_get_progress_tables_completed() -> Option<i32> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active || progress.phase != ProgressPhase::Idle as i32 {
        Some(progress.tables_completed)
    } else {
        None
    }
}

/// Get total tables count (internal implementation).
/// Returns NULL if no operation is active.
#[pg_extern]
fn _steep_repl_get_progress_tables_total() -> Option<i32> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active || progress.phase != ProgressPhase::Idle as i32 {
        Some(progress.tables_total)
    } else {
        None
    }
}

/// Get bytes processed (internal implementation).
/// Returns NULL if no operation is active.
#[pg_extern]
fn _steep_repl_get_progress_bytes_processed() -> Option<i64> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active || progress.phase != ProgressPhase::Idle as i32 {
        Some(progress.bytes_processed)
    } else {
        None
    }
}

/// Get total bytes (internal implementation).
/// Returns NULL if no operation is active.
#[pg_extern]
fn _steep_repl_get_progress_bytes_total() -> Option<i64> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active || progress.phase != ProgressPhase::Idle as i32 {
        Some(progress.bytes_total)
    } else {
        None
    }
}

/// Get rows processed (internal implementation).
/// Returns NULL if no operation is active.
#[pg_extern]
fn _steep_repl_get_progress_rows_processed() -> Option<i64> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active || progress.phase != ProgressPhase::Idle as i32 {
        Some(progress.rows_processed)
    } else {
        None
    }
}

/// Get total rows (internal implementation).
/// Returns NULL if no operation is active.
#[pg_extern]
fn _steep_repl_get_progress_rows_total() -> Option<i64> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active || progress.phase != ProgressPhase::Idle as i32 {
        Some(progress.rows_total)
    } else {
        None
    }
}

/// Get throughput in bytes per second (internal implementation).
/// Returns NULL if no operation is active.
#[pg_extern]
fn _steep_repl_get_progress_throughput() -> Option<f32> {
    let progress = OPERATION_PROGRESS.share();
    if progress.active {
        Some(progress.throughput_bytes_sec)
    } else {
        None
    }
}

// =============================================================================
// Rust API for Background Worker Progress Updates (T009)
// =============================================================================
// These functions allow the background worker to update shared memory progress.
// They acquire an exclusive lock, so should be called judiciously.

/// Start tracking a new operation in shared memory.
///
/// This function is called by the background worker when starting a new operation.
/// It acquires an exclusive lock and updates the shared memory progress struct.
///
/// # Arguments
/// * `operation_type` - Type of operation (SnapshotGenerate, SnapshotApply, BidirectionalMerge)
/// * `operation_id` - Unique identifier (snapshot_id or merge_id as string)
/// * `work_queue_id` - ID from the work_queue table
/// * `tables_total` - Total number of tables to process
/// * `bytes_total` - Estimated total bytes to process
/// * `rows_total` - Estimated total rows to process
pub fn start_progress(
    operation_type: OperationType,
    operation_id: &str,
    work_queue_id: i64,
    tables_total: i32,
    bytes_total: i64,
    rows_total: i64,
) {
    let mut progress = OPERATION_PROGRESS.exclusive();
    progress.start(
        operation_type,
        operation_id,
        work_queue_id,
        tables_total,
        bytes_total,
        rows_total,
    );
}

/// Update progress during an operation.
///
/// This function is called periodically by the background worker to report progress.
/// It acquires an exclusive lock and updates the shared memory progress struct.
///
/// # Arguments
/// * `phase` - Current phase of the operation
/// * `tables_completed` - Number of tables completed so far
/// * `bytes_processed` - Bytes processed so far
/// * `rows_processed` - Rows processed so far
/// * `current_table` - Name of the table currently being processed
pub fn update_progress(
    phase: ProgressPhase,
    tables_completed: i32,
    bytes_processed: i64,
    rows_processed: i64,
    current_table: &str,
) {
    let mut progress = OPERATION_PROGRESS.exclusive();
    progress.update(
        phase,
        tables_completed,
        bytes_processed,
        rows_processed,
        current_table,
    );
}

/// Mark the current operation as complete.
///
/// This function is called by the background worker when an operation finishes successfully.
pub fn complete_progress() {
    let mut progress = OPERATION_PROGRESS.exclusive();
    progress.complete();
}

/// Mark the current operation as failed.
///
/// This function is called by the background worker when an operation fails.
///
/// # Arguments
/// * `error` - Error message describing the failure
pub fn fail_progress(error: &str) {
    let mut progress = OPERATION_PROGRESS.exclusive();
    progress.fail(error);
}

/// Reset progress to idle state.
///
/// This function is called to clear progress information, typically after
/// the client has acknowledged completion or failure.
pub fn reset_progress() {
    let mut progress = OPERATION_PROGRESS.exclusive();
    progress.reset();
}

/// Check if an operation is currently active.
///
/// This is a convenience function for the background worker to check
/// if there's already an operation in progress.
pub fn is_progress_active() -> bool {
    let progress = OPERATION_PROGRESS.share();
    progress.active
}

/// Get the current work queue ID being processed.
///
/// Returns 0 if no operation is active.
pub fn get_current_work_queue_id() -> i64 {
    let progress = OPERATION_PROGRESS.share();
    progress.work_queue_id
}

/// Get current progress snapshot for reading.
///
/// Returns a copy of the progress struct for safe reading without holding locks.
pub fn get_progress_snapshot() -> OperationProgress {
    let progress = OPERATION_PROGRESS.share();
    *progress
}

// Schema-qualified wrapper functions
extension_sql!(
    r#"
-- Wrapper functions in steep_repl schema that call the internal implementations
CREATE FUNCTION steep_repl.is_operation_active() RETURNS boolean
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_is_operation_active() $$;

CREATE FUNCTION steep_repl.get_progress_percent() RETURNS real
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_percent() $$;

CREATE FUNCTION steep_repl.get_progress_phase() RETURNS text
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_phase() $$;

CREATE FUNCTION steep_repl.get_progress_current_table() RETURNS text
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_current_table() $$;

CREATE FUNCTION steep_repl.get_progress_eta_seconds() RETURNS integer
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_eta_seconds() $$;

CREATE FUNCTION steep_repl.get_progress_error() RETURNS text
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_error() $$;

CREATE FUNCTION steep_repl.get_progress_work_queue_id() RETURNS bigint
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_work_queue_id() $$;

CREATE FUNCTION steep_repl.get_progress_operation_id() RETURNS text
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_operation_id() $$;

CREATE FUNCTION steep_repl.get_progress_operation_type() RETURNS text
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_operation_type() $$;

CREATE FUNCTION steep_repl.get_progress_tables_completed() RETURNS integer
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_tables_completed() $$;

CREATE FUNCTION steep_repl.get_progress_tables_total() RETURNS integer
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_tables_total() $$;

CREATE FUNCTION steep_repl.get_progress_bytes_processed() RETURNS bigint
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_bytes_processed() $$;

CREATE FUNCTION steep_repl.get_progress_bytes_total() RETURNS bigint
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_bytes_total() $$;

CREATE FUNCTION steep_repl.get_progress_rows_processed() RETURNS bigint
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_rows_processed() $$;

CREATE FUNCTION steep_repl.get_progress_rows_total() RETURNS bigint
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_rows_total() $$;

CREATE FUNCTION steep_repl.get_progress_throughput() RETURNS real
    LANGUAGE sql STABLE AS $$ SELECT _steep_repl_get_progress_throughput() $$;

COMMENT ON FUNCTION steep_repl.is_operation_active() IS 'Check if an operation is currently running';
COMMENT ON FUNCTION steep_repl.get_progress_percent() IS 'Get the current operation progress percentage (0-100), NULL if inactive';
COMMENT ON FUNCTION steep_repl.get_progress_phase() IS 'Get the current operation phase, NULL if inactive';
COMMENT ON FUNCTION steep_repl.get_progress_current_table() IS 'Get the table currently being processed, NULL if inactive or none';
COMMENT ON FUNCTION steep_repl.get_progress_eta_seconds() IS 'Get estimated time remaining in seconds, NULL if inactive or unknown';
COMMENT ON FUNCTION steep_repl.get_progress_error() IS 'Get error message if last operation failed, NULL otherwise';
COMMENT ON FUNCTION steep_repl.get_progress_work_queue_id() IS 'Get the work queue ID of the current operation';
COMMENT ON FUNCTION steep_repl.get_progress_operation_id() IS 'Get the operation ID (snapshot_id or merge_id)';
COMMENT ON FUNCTION steep_repl.get_progress_operation_type() IS 'Get the operation type (snapshot_generate, snapshot_apply, bidirectional_merge)';
COMMENT ON FUNCTION steep_repl.get_progress_tables_completed() IS 'Get count of tables completed so far';
COMMENT ON FUNCTION steep_repl.get_progress_tables_total() IS 'Get total count of tables to process';
COMMENT ON FUNCTION steep_repl.get_progress_bytes_processed() IS 'Get bytes processed so far';
COMMENT ON FUNCTION steep_repl.get_progress_bytes_total() IS 'Get total bytes to process (estimated)';
COMMENT ON FUNCTION steep_repl.get_progress_rows_processed() IS 'Get rows processed so far';
COMMENT ON FUNCTION steep_repl.get_progress_rows_total() IS 'Get total rows to process (estimated)';
COMMENT ON FUNCTION steep_repl.get_progress_throughput() IS 'Get current throughput in bytes per second';
"#,
    name = "create_progress_functions",
    requires = [
        "create_schema",
        _steep_repl_is_operation_active,
        _steep_repl_get_progress_percent,
        _steep_repl_get_progress_phase,
        _steep_repl_get_progress_current_table,
        _steep_repl_get_progress_eta_seconds,
        _steep_repl_get_progress_error,
        _steep_repl_get_progress_work_queue_id,
        _steep_repl_get_progress_operation_id,
        _steep_repl_get_progress_operation_type,
        _steep_repl_get_progress_tables_completed,
        _steep_repl_get_progress_tables_total,
        _steep_repl_get_progress_bytes_processed,
        _steep_repl_get_progress_bytes_total,
        _steep_repl_get_progress_rows_processed,
        _steep_repl_get_progress_rows_total,
        _steep_repl_get_progress_throughput
    ],
);

// =============================================================================
// SQL Type for Progress Output
// =============================================================================

extension_sql!(
    r#"
-- Create composite type for progress output
CREATE TYPE steep_repl.operation_progress_type AS (
    operation_id TEXT,
    operation_type TEXT,
    phase TEXT,
    overall_percent REAL,
    tables_completed INTEGER,
    tables_total INTEGER,
    bytes_processed BIGINT,
    bytes_total BIGINT,
    rows_processed BIGINT,
    rows_total BIGINT,
    throughput_bytes_sec REAL,
    eta_seconds INTEGER,
    current_table TEXT,
    error_message TEXT,
    work_queue_id BIGINT
);

COMMENT ON TYPE steep_repl.operation_progress_type IS
    'Composite type for operation progress information from shared memory';

-- Comprehensive function to get all progress fields in one call
-- Returns NULL if no operation is active and no recent operation completed/failed
CREATE FUNCTION steep_repl.get_progress() RETURNS steep_repl.operation_progress_type
LANGUAGE sql STABLE
AS $$
    SELECT (
        steep_repl.get_progress_operation_id(),
        steep_repl.get_progress_operation_type(),
        steep_repl.get_progress_phase(),
        steep_repl.get_progress_percent(),
        steep_repl.get_progress_tables_completed(),
        steep_repl.get_progress_tables_total(),
        steep_repl.get_progress_bytes_processed(),
        steep_repl.get_progress_bytes_total(),
        steep_repl.get_progress_rows_processed(),
        steep_repl.get_progress_rows_total(),
        steep_repl.get_progress_throughput(),
        steep_repl.get_progress_eta_seconds(),
        steep_repl.get_progress_current_table(),
        steep_repl.get_progress_error(),
        steep_repl.get_progress_work_queue_id()
    )::steep_repl.operation_progress_type
    WHERE steep_repl.get_progress_operation_id() IS NOT NULL
$$;

COMMENT ON FUNCTION steep_repl.get_progress() IS
    'Get all progress fields from shared memory as a single composite row, NULL if no operation';
"#,
    name = "create_progress_type",
    requires = ["create_schema", "create_progress_functions"],
);

#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use super::*;

    #[pg_test]
    fn test_progress_type_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_type t
                JOIN pg_namespace n ON t.typnamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND t.typname = 'operation_progress_type'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "operation_progress_type should exist");
    }

    #[pg_test]
    fn test_is_operation_active_false_initially() {
        let result = Spi::get_one::<bool>(
            "SELECT steep_repl.is_operation_active()"
        );
        assert_eq!(result, Ok(Some(false)), "should not be active initially");
    }

    #[pg_test]
    fn test_get_progress_percent_null_when_inactive() {
        let result = Spi::get_one::<f32>(
            "SELECT steep_repl.get_progress_percent()"
        );
        assert_eq!(result, Ok(None), "should return NULL when no operation active");
    }

    #[pg_test]
    fn test_get_progress_phase_null_when_inactive() {
        let result = Spi::get_one::<String>(
            "SELECT steep_repl.get_progress_phase()"
        );
        assert_eq!(result, Ok(None), "should return NULL when no operation active");
    }

    #[pg_test]
    fn test_progress_phase_conversion() {
        assert_eq!(ProgressPhase::from_i32(0).as_str(), "idle");
        assert_eq!(ProgressPhase::from_i32(1).as_str(), "schema");
        assert_eq!(ProgressPhase::from_i32(2).as_str(), "data");
        assert_eq!(ProgressPhase::from_i32(8).as_str(), "complete");
        assert_eq!(ProgressPhase::from_i32(9).as_str(), "failed");
        assert_eq!(ProgressPhase::from_i32(99).as_str(), "idle"); // Invalid -> idle
    }

    #[pg_test]
    fn test_operation_progress_struct() {
        let mut progress = OperationProgress::default();
        assert!(!progress.active);
        assert_eq!(progress.phase, 0);

        progress.start(
            OperationType::SnapshotGenerate,
            "test_snap_001",
            42,
            10,
            1000000,
            50000,
        );

        assert!(progress.active);
        assert_eq!(progress.get_operation_id(), "test_snap_001");
        assert_eq!(progress.work_queue_id, 42);
        assert_eq!(progress.tables_total, 10);

        progress.update(
            ProgressPhase::Data,
            5,
            500000,
            25000,
            "public.users",
        );

        assert_eq!(progress.get_phase(), ProgressPhase::Data);
        assert_eq!(progress.tables_completed, 5);
        assert_eq!(progress.get_current_table(), "public.users");
        assert!(progress.overall_percent > 0.0);

        progress.complete();
        assert!(!progress.active);
        assert_eq!(progress.get_phase(), ProgressPhase::Complete);
        assert_eq!(progress.overall_percent, 100.0);
    }

    #[pg_test]
    fn test_operation_progress_fail() {
        let mut progress = OperationProgress::default();
        progress.start(
            OperationType::SnapshotApply,
            "test_snap_002",
            43,
            5,
            500000,
            10000,
        );

        progress.fail("Connection refused");

        assert!(!progress.active);
        assert_eq!(progress.get_phase(), ProgressPhase::Failed);
        assert_eq!(progress.get_error_message(), "Connection refused");
    }

    #[pg_test]
    fn test_progress_string_truncation() {
        let mut progress = OperationProgress::default();

        // Test long operation ID gets truncated
        let long_id = "x".repeat(100);
        progress.set_operation_id(&long_id);
        let result = progress.get_operation_id();
        assert!(result.len() < 100);
        assert!(result.len() <= OPERATION_ID_LEN - 1);

        // Test long table name gets truncated
        let long_table = "y".repeat(200);
        progress.set_current_table(&long_table);
        let result = progress.get_current_table();
        assert!(result.len() < 200);
        assert!(result.len() <= TABLE_NAME_LEN - 1);
    }

    // =========================================================================
    // Tests for T009: Shared Memory Progress Read/Write with PgLwLock
    // =========================================================================
    // NOTE: These tests use global shared memory, so they test the Rust API
    // directly rather than checking SQL results which could be affected by
    // other concurrent tests.

    #[pg_test]
    fn test_get_progress_function_exists() {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(
                SELECT 1 FROM pg_proc p
                JOIN pg_namespace n ON p.pronamespace = n.oid
                WHERE n.nspname = 'steep_repl' AND p.proname = 'get_progress'
            )"
        );
        assert_eq!(result, Ok(Some(true)), "get_progress function should exist");
    }

    #[pg_test]
    fn test_rust_api_lifecycle() {
        // Test complete lifecycle: start -> update -> complete -> reset
        // Using Rust API directly to avoid race conditions with other tests

        // Start
        crate::progress::start_progress(
            crate::progress::OperationType::SnapshotGenerate,
            "test_lifecycle",
            999,
            5,
            500000,
            10000,
        );

        let snapshot = crate::progress::get_progress_snapshot();
        assert!(snapshot.active);
        assert_eq!(snapshot.get_operation_id(), "test_lifecycle");
        assert_eq!(snapshot.work_queue_id, 999);
        assert_eq!(snapshot.get_operation_type(), crate::progress::OperationType::SnapshotGenerate);

        // Update
        crate::progress::update_progress(
            crate::progress::ProgressPhase::Data,
            3,
            300000,
            6000,
            "public.test_table",
        );

        let snapshot = crate::progress::get_progress_snapshot();
        assert_eq!(snapshot.get_phase(), crate::progress::ProgressPhase::Data);
        assert_eq!(snapshot.tables_completed, 3);
        assert_eq!(snapshot.bytes_processed, 300000);
        assert_eq!(snapshot.get_current_table(), "public.test_table");
        assert!(snapshot.overall_percent > 0.0);

        // Complete
        crate::progress::complete_progress();

        let snapshot = crate::progress::get_progress_snapshot();
        assert!(!snapshot.active);
        assert_eq!(snapshot.get_phase(), crate::progress::ProgressPhase::Complete);
        assert_eq!(snapshot.overall_percent, 100.0);

        // Reset
        crate::progress::reset_progress();

        let snapshot = crate::progress::get_progress_snapshot();
        assert!(!snapshot.active);
        assert_eq!(snapshot.get_phase(), crate::progress::ProgressPhase::Idle);
    }

    #[pg_test]
    fn test_rust_api_fail_lifecycle() {
        // Test failure lifecycle: start -> fail -> reset

        crate::progress::start_progress(
            crate::progress::OperationType::BidirectionalMerge,
            "test_fail_lifecycle",
            888,
            2,
            50000,
            1000,
        );

        // Verify started
        assert!(crate::progress::is_progress_active());

        // Fail with error
        crate::progress::fail_progress("Test error: connection timeout");

        let snapshot = crate::progress::get_progress_snapshot();
        assert!(!snapshot.active);
        assert_eq!(snapshot.get_phase(), crate::progress::ProgressPhase::Failed);
        assert_eq!(snapshot.get_error_message(), "Test error: connection timeout");

        // Reset
        crate::progress::reset_progress();
    }

    #[pg_test]
    fn test_rust_api_is_progress_active_fn() {
        // Test the is_progress_active helper function

        crate::progress::reset_progress();
        let was_active_after_reset = crate::progress::is_progress_active();

        crate::progress::start_progress(
            crate::progress::OperationType::SnapshotApply,
            "test_is_active",
            777,
            1,
            1000,
            100,
        );
        let was_active_after_start = crate::progress::is_progress_active();

        crate::progress::complete_progress();
        let was_active_after_complete = crate::progress::is_progress_active();

        crate::progress::reset_progress();

        // Verify in sequence
        assert!(!was_active_after_reset, "should not be active after reset");
        assert!(was_active_after_start, "should be active after start");
        assert!(!was_active_after_complete, "should not be active after complete");
    }

    #[pg_test]
    fn test_rust_api_get_current_work_queue_id() {
        crate::progress::start_progress(
            crate::progress::OperationType::SnapshotGenerate,
            "test_wq_id",
            12345,
            1,
            1000,
            100,
        );

        let wq_id = crate::progress::get_current_work_queue_id();
        assert_eq!(wq_id, 12345);

        crate::progress::reset_progress();
    }

    #[pg_test]
    fn test_new_progress_functions_exist() {
        // Test all new SQL wrapper functions exist
        let functions = vec![
            "get_progress_work_queue_id",
            "get_progress_operation_id",
            "get_progress_operation_type",
            "get_progress_tables_completed",
            "get_progress_tables_total",
            "get_progress_bytes_processed",
            "get_progress_bytes_total",
            "get_progress_rows_processed",
            "get_progress_rows_total",
            "get_progress_throughput",
            "get_progress",
        ];

        for func in functions {
            let result = Spi::get_one::<bool>(&format!(
                "SELECT EXISTS(
                    SELECT 1 FROM pg_proc p
                    JOIN pg_namespace n ON p.pronamespace = n.oid
                    WHERE n.nspname = 'steep_repl' AND p.proname = '{}'
                )",
                func
            ));
            assert_eq!(result, Ok(Some(true)), "function {} should exist", func);
        }
    }

    #[pg_test]
    fn test_sql_functions_callable() {
        // Just verify the SQL functions can be called without errors
        // Don't check exact values due to shared memory race conditions

        // These should work without error even if returning NULL
        let _ = Spi::get_one::<bool>("SELECT steep_repl.is_operation_active()");
        let _ = Spi::get_one::<f32>("SELECT steep_repl.get_progress_percent()");
        let _ = Spi::get_one::<String>("SELECT steep_repl.get_progress_phase()");
        let _ = Spi::get_one::<String>("SELECT steep_repl.get_progress_current_table()");
        let _ = Spi::get_one::<i32>("SELECT steep_repl.get_progress_eta_seconds()");
        let _ = Spi::get_one::<String>("SELECT steep_repl.get_progress_error()");
        let _ = Spi::get_one::<i64>("SELECT steep_repl.get_progress_work_queue_id()");
        let _ = Spi::get_one::<String>("SELECT steep_repl.get_progress_operation_id()");
        let _ = Spi::get_one::<String>("SELECT steep_repl.get_progress_operation_type()");
        let _ = Spi::get_one::<i32>("SELECT steep_repl.get_progress_tables_completed()");
        let _ = Spi::get_one::<i32>("SELECT steep_repl.get_progress_tables_total()");
        let _ = Spi::get_one::<i64>("SELECT steep_repl.get_progress_bytes_processed()");
        let _ = Spi::get_one::<i64>("SELECT steep_repl.get_progress_bytes_total()");
        let _ = Spi::get_one::<i64>("SELECT steep_repl.get_progress_rows_processed()");
        let _ = Spi::get_one::<i64>("SELECT steep_repl.get_progress_rows_total()");
        let _ = Spi::get_one::<f32>("SELECT steep_repl.get_progress_throughput()");
    }
}
