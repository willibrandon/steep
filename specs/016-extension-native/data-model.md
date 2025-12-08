# Data Model: Extension-Native Architecture

**Feature**: 016-extension-native
**Date**: 2025-12-08

## Entities

### 1. Work Queue Entry

Represents a queued, running, or completed background operation.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| id | BIGSERIAL | PRIMARY KEY | Unique identifier |
| operation | TEXT | NOT NULL, CHECK | Operation type: snapshot_generate, snapshot_apply, bidirectional_merge |
| snapshot_id | TEXT | NULL | Associated snapshot ID (for snapshot operations) |
| merge_id | TEXT | NULL | Associated merge ID (for merge operations) |
| params | JSONB | NOT NULL DEFAULT '{}' | Operation parameters |
| status | TEXT | NOT NULL DEFAULT 'pending' | pending, running, complete, failed, cancelled |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT now() | When queued |
| started_at | TIMESTAMPTZ | NULL | When worker started processing |
| completed_at | TIMESTAMPTZ | NULL | When operation completed/failed |
| error_message | TEXT | NULL | Error details if failed |
| worker_pid | INTEGER | NULL | PID of worker processing this entry |

**Indexes**:
- `work_queue_pending_idx`: Partial index on `(created_at) WHERE status = 'pending'`

**Relationships**:
- References `steep_repl.snapshots` via `snapshot_id`
- References `steep_repl.merge_audit_log` via `merge_id`

**State Transitions**:
```
pending → running (worker claims job)
running → complete (success)
running → failed (error)
running → cancelled (user cancellation)
pending → cancelled (cancelled before start)
```

---

### 2. Snapshot Progress (Shared Memory)

Real-time progress state for snapshot operations, stored in PostgreSQL shared memory for cross-session visibility.

| Field | Type | Size | Description |
|-------|------|------|-------------|
| active | bool | 1 byte | Whether an operation is in progress |
| snapshot_id | [u8; 64] | 64 bytes | Current snapshot ID |
| phase | i32 | 4 bytes | 0=idle, 1=schema, 2=data, 3=sequences, 4=finalizing |
| overall_percent | f32 | 4 bytes | 0.0 - 100.0 |
| tables_completed | i32 | 4 bytes | Number of tables exported |
| tables_total | i32 | 4 bytes | Total tables to export |
| bytes_processed | i64 | 8 bytes | Bytes written |
| current_table | [u8; 128] | 128 bytes | Current table being processed (null-terminated) |
| error | [u8; 256] | 256 bytes | Error message if failed (null-terminated) |
| started_at | i64 | 8 bytes | Unix timestamp when started |

**Total Size**: ~485 bytes (fits in single shared memory allocation)

**Access Pattern**:
- Background worker: exclusive write lock
- SQL functions: shared read lock
- CLI: reads via SQL function

---

### 3. Progress Notification (NOTIFY Payload)

JSON structure sent via PostgreSQL LISTEN/NOTIFY for real-time CLI updates.

```json
{
  "op": "snapshot",
  "snapshot_id": "snap_20251208_123456_abc12345",
  "phase": "data",
  "percent": 45.2,
  "table": "public.orders",
  "tables_completed": 5,
  "tables_total": 12,
  "bytes": 1073741824,
  "eta_seconds": 3600,
  "error": null
}
```

| Field | Type | Description |
|-------|------|-------------|
| op | string | Operation type: snapshot, apply, merge |
| snapshot_id | string | Operation identifier |
| phase | string | Current phase: schema, data, sequences, finalizing, complete, failed |
| percent | number | Overall progress 0-100 |
| table | string | Current table being processed |
| tables_completed | number | Tables finished |
| tables_total | number | Total tables |
| bytes | number | Bytes processed |
| eta_seconds | number | Estimated time remaining |
| error | string? | Error message if failed |

**Channel**: `steep_repl_progress`
**Max Payload**: 8000 bytes

---

### 4. Snapshots Table (Extended)

The existing `steep_repl.snapshots` table is extended to support background worker operations.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| snapshot_id | TEXT | PRIMARY KEY | Unique snapshot identifier |
| node_id | TEXT | REFERENCES nodes | Source node |
| status | TEXT | NOT NULL | pending, running, complete, failed |
| phase | TEXT | NOT NULL | Current phase |
| storage_path | TEXT | NOT NULL | Output directory |
| compression | TEXT | NOT NULL DEFAULT 'none' | none, gzip, lz4, zstd |
| parallel | INTEGER | NOT NULL DEFAULT 4 | Parallel workers |
| table_count | INTEGER | DEFAULT 0 | Total tables |
| tables_completed | INTEGER | DEFAULT 0 | Tables exported |
| overall_percent | FLOAT | DEFAULT 0 | Progress percentage |
| current_table | TEXT | NULL | Current table |
| bytes_processed | BIGINT | DEFAULT 0 | Bytes written |
| bytes_total | BIGINT | DEFAULT 0 | Estimated total bytes |
| eta_seconds | INTEGER | NULL | Estimated time remaining |
| error_message | TEXT | NULL | Error details |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT now() | Creation time |
| started_at | TIMESTAMPTZ | NULL | Processing start |
| completed_at | TIMESTAMPTZ | NULL | Completion time |

**Note**: Progress fields in this table are updated periodically (every 5 seconds) from shared memory for persistence and query via `steep_repl.snapshot_progress()`. Real-time progress uses shared memory + NOTIFY.

---

### 5. Merge Audit Log (Extended)

The existing `steep_repl.merge_audit_log` table extended for background operations.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| merge_id | TEXT | PRIMARY KEY | Unique merge identifier |
| status | TEXT | NOT NULL | pending, running, complete, failed |
| tables | TEXT[] | NOT NULL | Tables being merged |
| strategy | TEXT | NOT NULL | prefer-local, prefer-remote, last-modified |
| peer_connstr | TEXT | NOT NULL | Peer connection string (redacted for logging) |
| dry_run | BOOLEAN | NOT NULL DEFAULT false | Whether this is a dry run |
| local_only_count | BIGINT | DEFAULT 0 | Rows only on local |
| remote_only_count | BIGINT | DEFAULT 0 | Rows only on remote |
| match_count | BIGINT | DEFAULT 0 | Matching rows |
| conflict_count | BIGINT | DEFAULT 0 | Conflicting rows |
| rows_merged | BIGINT | DEFAULT 0 | Rows actually merged |
| current_table | TEXT | NULL | Current table |
| error_message | TEXT | NULL | Error details |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT now() | Creation time |
| started_at | TIMESTAMPTZ | NULL | Processing start |
| completed_at | TIMESTAMPTZ | NULL | Completion time |

---

## Entity Relationships

```
┌─────────────────┐       ┌─────────────────┐
│   work_queue    │───────│    snapshots    │
│   (operations)  │       │  (persistence)  │
└─────────────────┘       └─────────────────┘
        │
        │ snapshot_id / merge_id
        ▼
┌─────────────────┐       ┌─────────────────────┐
│ merge_audit_log │       │  SnapshotProgress   │
│  (persistence)  │       │  (shared memory)    │
└─────────────────┘       └─────────────────────┘
                                    │
                                    │ pg_notify
                                    ▼
                          ┌─────────────────────┐
                          │  NOTIFY payload     │
                          │  (real-time CLI)    │
                          └─────────────────────┘
```

---

## Validation Rules

### Work Queue
- `operation` must be one of: `snapshot_generate`, `snapshot_apply`, `bidirectional_merge`
- `status` must be one of: `pending`, `running`, `complete`, `failed`, `cancelled`
- `snapshot_id` required when `operation` IN (`snapshot_generate`, `snapshot_apply`)
- `merge_id` required when `operation` = `bidirectional_merge`

### Snapshot Progress
- `phase` values: 0 (idle), 1 (schema), 2 (data), 3 (sequences), 4 (finalizing)
- `overall_percent` clamped to 0.0 - 100.0
- Fixed-size strings null-terminated for C interop

### NOTIFY Payload
- JSON must be valid UTF-8
- Total payload ≤ 8000 bytes
- `phase` string values: `schema`, `data`, `sequences`, `finalizing`, `complete`, `failed`

---

## Migration Notes

### New Tables
- `steep_repl.work_queue` - New table for background job management

### Modified Tables
- `steep_repl.snapshots` - Add progress tracking columns
- `steep_repl.merge_audit_log` - Add background operation tracking

### New Shared Memory
- `steep_repl_snapshot_progress` - LWLock-protected progress struct

### New NOTIFY Channels
- `steep_repl_progress` - Real-time progress notifications
- `steep_repl_work` - Signal to wake background worker
