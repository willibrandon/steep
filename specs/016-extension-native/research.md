# Research: Extension-Native Architecture

**Feature**: 016-extension-native
**Date**: 2025-12-08
**Status**: Complete

## Research Topics

### 1. pgrx Background Workers

**Decision**: Use pgrx `BackgroundWorkerBuilder` with latch-based work processing.

**Rationale**: pgrx provides a high-level Rust API over PostgreSQL's background worker infrastructure. The latch-based approach is more efficient than polling and integrates with PostgreSQL's signal handling.

**Alternatives Considered**:
- Raw PostgreSQL C API: More complex, requires unsafe FFI, no benefit
- External process monitoring: Defeats purpose of extension-native architecture
- cron/pg_cron: Not suitable for long-running operations with progress

**Key Implementation Pattern**:

```rust
#[pg_guard]
pub extern "C-unwind" fn _PG_init() {
    // CRITICAL: Must be loaded via shared_preload_libraries
    if unsafe { !pgrx::pg_sys::process_shared_preload_libraries_in_progress } {
        pgrx::error!("steep_repl must be loaded via shared_preload_libraries for background worker support");
    }

    BackgroundWorkerBuilder::new("steep_repl_worker")
        .set_function("steep_repl_worker_main")
        .set_library("steep_repl")
        .enable_spi_access()  // Required for database access
        .set_restart_time(Some(Duration::from_secs(5)))
        .load();
}

#[pg_guard]
#[unsafe(no_mangle)]
pub extern "C-unwind" fn steep_repl_worker_main(_arg: pg_sys::Datum) {
    BackgroundWorker::attach_signal_handlers(SignalWakeFlags::SIGHUP | SignalWakeFlags::SIGTERM);
    BackgroundWorker::connect_worker_to_spi(Some("postgres"), None);

    while BackgroundWorker::wait_latch(Some(Duration::from_secs(1))) {
        if BackgroundWorker::sighup_received() {
            // Reload configuration
        }

        // Process work queue within transaction
        let _ = BackgroundWorker::transaction(|| {
            Spi::connect_mut(|client| {
                // Poll work_queue, execute operations, update progress
                Ok(())
            })
        });
    }
}
```

**Key Requirements**:
- Extension MUST be in `shared_preload_libraries` for background worker
- SPI access requires `RecoveryFinished` start time (automatic with `enable_spi_access()`)
- Only primitive types can be passed as arguments (use work_queue table instead)
- Worker automatically restarts on crash if `set_restart_time()` configured

---

### 2. pgrx Shared Memory for Progress Tracking

**Decision**: Use `PgLwLock<SnapshotProgress>` for cross-session progress visibility.

**Rationale**: PostgreSQL's LWLock provides efficient reader-writer access to shared memory. This allows the background worker to update progress while SQL functions and CLI query it without blocking.

**Alternatives Considered**:
- `PgAtomic`: Only suitable for simple counters, not complex progress struct
- Table-based progress: Higher overhead, requires transaction for every update
- `PgSpinLock`: Only for very short critical sections (few CPU instructions)

**Key Implementation Pattern**:

```rust
#[derive(Copy, Clone, Default)]
pub struct SnapshotProgress {
    pub active: bool,
    pub snapshot_id: [u8; 64],    // Fixed-size for shared memory
    pub phase: i32,               // 0=idle, 1=schema, 2=data, 3=sequences, 4=finalizing
    pub overall_percent: f32,
    pub tables_completed: i32,
    pub tables_total: i32,
    pub bytes_processed: i64,
    pub current_table: [u8; 128],
    pub error: [u8; 256],
    pub started_at: i64,          // Unix timestamp
}

// SAFETY: All fields are fixed-size primitives or arrays
unsafe impl PGRXSharedMemory for SnapshotProgress {}

static SNAPSHOT_PROGRESS: PgLwLock<SnapshotProgress> =
    unsafe { PgLwLock::new(c"steep_repl_snapshot_progress") };

#[pg_guard]
pub extern "C-unwind" fn _PG_init() {
    pg_shmem_init!(SNAPSHOT_PROGRESS);
    // ... background worker registration
}

// SQL function to read progress (read lock)
#[pg_extern]
fn snapshot_progress() -> Option<SnapshotProgress> {
    let progress = SNAPSHOT_PROGRESS.share();
    if progress.active {
        Some(*progress)
    } else {
        None
    }
}

// Background worker updates progress (write lock)
fn update_progress(phase: i32, table: &str, percent: f32) {
    let mut progress = SNAPSHOT_PROGRESS.exclusive();
    progress.phase = phase;
    progress.overall_percent = percent;
    // Copy table name to fixed-size array
    let bytes = table.as_bytes();
    let len = bytes.len().min(127);
    progress.current_table[..len].copy_from_slice(&bytes[..len]);
    progress.current_table[len] = 0;
}
```

**Key Constraints**:
- All shared memory types MUST implement `Copy + Clone`
- NO heap allocations (`String`, `Vec`, `HashMap`) - use fixed-size arrays
- Lock names MUST be unique across all extensions
- `pg_shmem_init!()` MUST be called in `_PG_init()`

---

### 3. LISTEN/NOTIFY for Real-Time Progress

**Decision**: Use PostgreSQL's native `pg_notify()` for real-time progress streaming.

**Rationale**: LISTEN/NOTIFY is built into PostgreSQL, requires no additional infrastructure, and integrates seamlessly with pgx client library. The CLI can subscribe and receive notifications without polling.

**Key Implementation Pattern**:

```rust
// In background worker - send notification
fn notify_progress(snapshot_id: &str, phase: &str, percent: f32, table: &str) {
    let payload = format!(
        r#"{{"snapshot_id":"{}","phase":"{}","percent":{:.1},"table":"{}"}}"#,
        snapshot_id, phase, percent, table
    );

    Spi::run(&format!(
        "SELECT pg_notify('steep_repl_progress', '{}')",
        payload.replace('\'', "''")
    )).ok();
}

// In CLI (Go) - listen for notifications
func listenProgress(ctx context.Context, conn *pgx.Conn, snapshotID string) error {
    _, err := conn.Exec(ctx, "LISTEN steep_repl_progress")
    if err != nil {
        return err
    }

    for {
        notification, err := conn.WaitForNotification(ctx)
        if err != nil {
            return err
        }

        var progress ProgressPayload
        if err := json.Unmarshal([]byte(notification.Payload), &progress); err != nil {
            continue
        }

        if progress.SnapshotID == snapshotID {
            displayProgress(progress)
            if progress.Phase == "complete" || progress.Phase == "failed" {
                return nil
            }
        }
    }
}
```

**Key Considerations**:
- Payload max size: 8000 bytes (use JSON for structured data)
- Notifications are per-connection (CLI must maintain connection)
- Notifications are transactional (sent on commit)
- Multiple listeners receive same notification (filter by operation ID)

---

### 4. Work Queue Pattern

**Decision**: Use `steep_repl.work_queue` table with `FOR UPDATE SKIP LOCKED` for job distribution.

**Rationale**: Table-based work queue is robust, survives PostgreSQL restart, and supports multiple workers. The `SKIP LOCKED` pattern allows concurrent job claiming without blocking.

**Key Implementation Pattern**:

```sql
CREATE TABLE steep_repl.work_queue (
    id BIGSERIAL PRIMARY KEY,
    operation TEXT NOT NULL,  -- snapshot_generate, snapshot_apply, merge
    snapshot_id TEXT,
    merge_id TEXT,
    params JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending',  -- pending, running, complete, failed
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error_message TEXT,
    worker_pid INTEGER,

    CONSTRAINT work_queue_operation_check CHECK (
        operation IN ('snapshot_generate', 'snapshot_apply', 'bidirectional_merge')
    ),
    CONSTRAINT work_queue_status_check CHECK (
        status IN ('pending', 'running', 'complete', 'failed', 'cancelled')
    )
);

CREATE INDEX work_queue_pending_idx ON steep_repl.work_queue (created_at)
    WHERE status = 'pending';
```

**Worker claim pattern**:

```rust
fn claim_work(client: &mut SpiClient) -> Option<WorkItem> {
    let result = client.select(
        "SELECT id, operation, params
         FROM steep_repl.work_queue
         WHERE status = 'pending'
         ORDER BY created_at
         LIMIT 1
         FOR UPDATE SKIP LOCKED",
        None,
        &[],
    ).ok()?;

    if let Some(row) = result.first() {
        let id: i64 = row.get_datum_by_ordinal(1)?.value()?;
        let operation: &str = row.get_datum_by_ordinal(2)?.value()?;
        let params: pgrx::JsonB = row.get_datum_by_ordinal(3)?.value()?;

        // Mark as running
        client.update(
            "UPDATE steep_repl.work_queue
             SET status = 'running', started_at = now(), worker_pid = pg_backend_pid()
             WHERE id = $1",
            None,
            &[id.into()],
        ).ok()?;

        Some(WorkItem { id, operation: operation.to_string(), params })
    } else {
        None
    }
}
```

---

### 5. CLI Direct Mode Implementation

**Decision**: Add `--direct` flag to CLI commands that connects directly to PostgreSQL via pgx.

**Rationale**: Leverages existing pgx/v5 driver already used throughout the codebase. Connection handling, SSL, and pooling are already well-understood.

**Key Implementation Pattern**:

```go
// cmd/steep-repl/cmd_snapshot.go

var snapshotGenerateCmd = &cobra.Command{
    Use:   "generate <node-id>",
    Short: "Generate a snapshot",
    RunE: func(cmd *cobra.Command, args []string) error {
        if directMode {
            return generateSnapshotDirect(cmd.Context(), args[0], outputPath)
        }
        return generateSnapshotRemote(cmd.Context(), args[0], outputPath)
    },
}

func generateSnapshotDirect(ctx context.Context, nodeID, outputPath string) error {
    conn, err := pgx.Connect(ctx, connectionString)
    if err != nil {
        return fmt.Errorf("connect: %w", err)
    }
    defer conn.Close(ctx)

    // Start snapshot via SQL function
    var snapshotID string
    err = conn.QueryRow(ctx,
        "SELECT snapshot_id FROM steep_repl.start_snapshot($1, $2, $3)",
        outputPath, compression, parallel,
    ).Scan(&snapshotID)
    if err != nil {
        return fmt.Errorf("start snapshot: %w", err)
    }

    // Subscribe to progress
    _, err = conn.Exec(ctx, "LISTEN steep_repl_progress")
    if err != nil {
        return fmt.Errorf("listen: %w", err)
    }

    // Display progress until complete
    for {
        notification, err := conn.WaitForNotification(ctx)
        if err != nil {
            return err
        }

        progress := parseProgress(notification.Payload)
        if progress.SnapshotID != snapshotID {
            continue
        }

        displayProgressBar(progress)

        if progress.Phase == "complete" {
            return nil
        }
        if progress.Phase == "failed" {
            return fmt.Errorf("snapshot failed: %s", progress.Error)
        }
    }
}
```

---

### 6. Auto-Detection Strategy (FR-012)

**Decision**: CLI tries direct mode first, falls back to daemon only when auto-detecting.

**Rationale**: Per DD-001, no coordination between daemon and extension. CLI makes independent decision based on flags and extension capabilities.

**Key Implementation Pattern**:

```go
// internal/repl/direct/detector.go

func DetectMode(ctx context.Context, cfg *config.Config, flags Flags) (Mode, error) {
    // Explicit flags take precedence
    if flags.Remote != "" {
        return ModeDaemon, nil
    }
    if flags.Direct {
        return ModeDirect, nil
    }

    // Auto-detect: try extension first
    conn, err := pgx.Connect(ctx, cfg.PostgreSQLConnString())
    if err != nil {
        // Can't connect to PostgreSQL, try daemon
        return tryDaemonFallback(ctx, cfg)
    }
    defer conn.Close(ctx)

    // Check if extension supports the operation
    var supported bool
    err = conn.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM pg_proc WHERE proname = 'start_snapshot' AND pronamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'steep_repl'))",
    ).Scan(&supported)

    if err == nil && supported {
        return ModeDirect, nil
    }

    // Extension doesn't support operation, try daemon
    return tryDaemonFallback(ctx, cfg)
}

func tryDaemonFallback(ctx context.Context, cfg *config.Config) (Mode, error) {
    if cfg.GRPC.Port == 0 {
        return ModeNone, fmt.Errorf("no daemon configured and extension not available")
    }

    // Try to connect to daemon
    client, err := grpc.NewClient(ctx, cfg.GRPCConfig())
    if err != nil {
        return ModeNone, fmt.Errorf("neither extension nor daemon available: %w", err)
    }
    client.Close()

    return ModeDaemon, nil
}
```

---

## Summary

| Topic | Decision | Key Technology |
|-------|----------|---------------|
| Background Workers | pgrx BackgroundWorkerBuilder | Latch-based processing, SPI access |
| Progress Tracking | PgLwLock<SnapshotProgress> | Shared memory, fixed-size struct |
| Real-Time Updates | pg_notify() | LISTEN/NOTIFY, JSON payload |
| Work Queue | PostgreSQL table | FOR UPDATE SKIP LOCKED |
| CLI Direct Mode | pgx/v5 connection | Connection + LISTEN |
| Auto-Detection | Extension-first | Check pg_proc for functions |

All research items resolved. Ready for Phase 1: Data Model and Contracts.
