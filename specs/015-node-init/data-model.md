# Data Model: Node Initialization & Snapshots

**Feature**: 015-node-init
**Date**: 2025-12-04

## Overview

This document defines the data model extensions for node initialization and snapshot management. It extends the existing `steep_repl` schema with new tables and columns.

## Entity Relationship Diagram

```
┌─────────────────┐       ┌─────────────────────┐
│     nodes       │       │   init_progress     │
│ (existing+ext)  │───────│                     │
│                 │  1:1  │                     │
└────────┬────────┘       └─────────────────────┘
         │
         │ 1:N
         ▼
┌─────────────────┐       ┌─────────────────────┐
│    snapshots    │       │ schema_fingerprints │
│                 │       │                     │
└─────────────────┘       └─────────────────────┘
```

## Entities

### 1. Node (Extended)

**Table**: `steep_repl.nodes`
**Purpose**: Add initialization state tracking to existing node registration.

#### New Columns

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| init_state | TEXT | NOT NULL DEFAULT 'uninitialized', CHECK | Current initialization state |
| init_source_node | TEXT | REFERENCES nodes(node_id) | Source node for initialization |
| init_started_at | TIMESTAMPTZ | | When initialization began |
| init_completed_at | TIMESTAMPTZ | | When initialization finished |

#### Init State Values

| State | Description | Valid Transitions To |
|-------|-------------|---------------------|
| uninitialized | Node registered but no data | preparing, failed |
| preparing | Creating slots, validating schemas | copying, failed |
| copying | Snapshot/backup restore in progress | catching_up, failed |
| catching_up | Applying WAL changes since snapshot | synchronized, failed |
| synchronized | Normal replication active | diverged |
| diverged | Node detected as out of sync | reinitializing, failed |
| failed | Initialization failed (human intervention) | uninitialized, reinitializing |
| reinitializing | Recovery in progress | copying, failed |

#### State Transition Diagram

```
                          ┌───────────────────────────────────────┐
                          │                                       │
                          ▼                                       │
UNINITIALIZED ──► PREPARING ──► COPYING ──► CATCHING_UP ──► SYNCHRONIZED
      ▲               │           │              │                │
      │               │           │              │                │
      │               ▼           ▼              ▼                ▼
      │            FAILED ◄──────────────────────────────────  DIVERGED
      │               │                                           │
      │               │                                           │
      └───────────────┴───────────► REINITIALIZING ◄──────────────┘
```

#### SQL Migration

```sql
-- Add init columns to existing nodes table
ALTER TABLE steep_repl.nodes
    ADD COLUMN init_state TEXT NOT NULL DEFAULT 'uninitialized',
    ADD COLUMN init_source_node TEXT REFERENCES steep_repl.nodes(node_id),
    ADD COLUMN init_started_at TIMESTAMPTZ,
    ADD COLUMN init_completed_at TIMESTAMPTZ;

-- Add CHECK constraint for valid states
ALTER TABLE steep_repl.nodes ADD CONSTRAINT nodes_init_state_check
    CHECK (init_state IN (
        'uninitialized', 'preparing', 'copying', 'catching_up',
        'synchronized', 'diverged', 'failed', 'reinitializing'
    ));

-- Index for state queries
CREATE INDEX idx_nodes_init_state ON steep_repl.nodes(init_state);
```

---

### 2. Init Progress

**Table**: `steep_repl.init_progress`
**Purpose**: Track real-time initialization progress for TUI display.

#### Columns

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| node_id | TEXT | PK, FK→nodes | Node being initialized |
| phase | TEXT | NOT NULL | Current phase: generation, application, catching_up |
| overall_percent | REAL | NOT NULL DEFAULT 0 | 0.0-100.0 overall progress |
| tables_total | INTEGER | NOT NULL DEFAULT 0 | Total tables to process |
| tables_completed | INTEGER | NOT NULL DEFAULT 0 | Tables finished |
| current_table | TEXT | | Table currently processing |
| current_table_percent | REAL | DEFAULT 0 | Progress within current table |
| rows_copied | BIGINT | DEFAULT 0 | Total rows copied so far |
| bytes_copied | BIGINT | DEFAULT 0 | Total bytes copied |
| throughput_rows_sec | REAL | DEFAULT 0 | Current throughput |
| started_at | TIMESTAMPTZ | NOT NULL DEFAULT now() | When phase started |
| eta_seconds | INTEGER | | Estimated seconds remaining |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT now() | Last progress update |
| parallel_workers | INTEGER | DEFAULT 1 | Active parallel workers |
| error_message | TEXT | | Last error if any |

#### Validation Rules

- `overall_percent` BETWEEN 0 AND 100
- `tables_completed` <= `tables_total`
- `current_table_percent` BETWEEN 0 AND 100
- `parallel_workers` BETWEEN 1 AND 16

#### SQL Definition

```sql
CREATE TABLE steep_repl.init_progress (
    node_id TEXT PRIMARY KEY REFERENCES steep_repl.nodes(node_id) ON DELETE CASCADE,
    phase TEXT NOT NULL CHECK (phase IN ('generation', 'application', 'catching_up')),
    overall_percent REAL NOT NULL DEFAULT 0 CHECK (overall_percent BETWEEN 0 AND 100),
    tables_total INTEGER NOT NULL DEFAULT 0 CHECK (tables_total >= 0),
    tables_completed INTEGER NOT NULL DEFAULT 0 CHECK (tables_completed >= 0),
    current_table TEXT,
    current_table_percent REAL DEFAULT 0 CHECK (current_table_percent BETWEEN 0 AND 100),
    rows_copied BIGINT DEFAULT 0 CHECK (rows_copied >= 0),
    bytes_copied BIGINT DEFAULT 0 CHECK (bytes_copied >= 0),
    throughput_rows_sec REAL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    eta_seconds INTEGER CHECK (eta_seconds >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    parallel_workers INTEGER DEFAULT 1 CHECK (parallel_workers BETWEEN 1 AND 16),
    error_message TEXT,
    CONSTRAINT progress_tables_check CHECK (tables_completed <= tables_total)
);

COMMENT ON TABLE steep_repl.init_progress IS 'Real-time initialization progress tracking';
```

---

### 3. Snapshot

**Table**: `steep_repl.snapshots`
**Purpose**: Track generated snapshots for two-phase initialization.

#### Columns

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| snapshot_id | TEXT | PK | Unique snapshot identifier |
| source_node_id | TEXT | NOT NULL, FK→nodes | Node snapshot was taken from |
| lsn | TEXT | NOT NULL | WAL position at snapshot time |
| storage_path | TEXT | NOT NULL | File system or S3 path |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT now() | When snapshot generated |
| size_bytes | BIGINT | NOT NULL | Total snapshot size |
| table_count | INTEGER | NOT NULL | Number of tables included |
| compression | TEXT | DEFAULT 'gzip' | Compression type |
| checksum | TEXT | NOT NULL | SHA256 of manifest |
| expires_at | TIMESTAMPTZ | | Auto-cleanup timestamp |
| status | TEXT | NOT NULL DEFAULT 'pending' | pending, complete, applied, expired |

#### Manifest JSON Structure

Stored at `{storage_path}/manifest.json`:

```json
{
    "snapshot_id": "snap_20251204_143022_abc123",
    "source_node": "node_a",
    "lsn": "0/1A234B00",
    "created_at": "2025-12-04T14:30:22Z",
    "tables": [
        {
            "schema": "public",
            "name": "orders",
            "row_count": 1500000,
            "size_bytes": 524288000,
            "checksum": "sha256:abc123...",
            "file": "data/public.orders.csv.gz"
        }
    ],
    "sequences": [
        {"schema": "public", "name": "orders_id_seq", "value": 1500001}
    ],
    "total_size_bytes": 4200000000,
    "compression": "gzip",
    "parallel_workers": 4
}
```

#### SQL Definition

```sql
CREATE TABLE steep_repl.snapshots (
    snapshot_id TEXT PRIMARY KEY,
    source_node_id TEXT NOT NULL REFERENCES steep_repl.nodes(node_id),
    lsn TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    table_count INTEGER NOT NULL CHECK (table_count >= 0),
    compression TEXT DEFAULT 'gzip' CHECK (compression IN ('none', 'gzip', 'lz4', 'zstd')),
    checksum TEXT NOT NULL,
    expires_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'complete', 'applied', 'expired'))
);

CREATE INDEX idx_snapshots_source ON steep_repl.snapshots(source_node_id);
CREATE INDEX idx_snapshots_status ON steep_repl.snapshots(status);
CREATE INDEX idx_snapshots_expires ON steep_repl.snapshots(expires_at) WHERE expires_at IS NOT NULL;

COMMENT ON TABLE steep_repl.snapshots IS 'Generated snapshot manifests for two-phase initialization';
```

---

### 4. Schema Fingerprint

**Table**: `steep_repl.schema_fingerprints`
**Purpose**: Store schema fingerprints for drift detection.

#### Columns

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| table_schema | TEXT | NOT NULL, PK | PostgreSQL schema name |
| table_name | TEXT | NOT NULL, PK | Table name |
| fingerprint | TEXT | NOT NULL | SHA256 of column definitions |
| column_count | INTEGER | NOT NULL | Number of columns |
| captured_at | TIMESTAMPTZ | NOT NULL DEFAULT now() | When fingerprint computed |
| column_definitions | JSONB | | Detailed column info for diff |

#### Fingerprint Algorithm

```sql
-- Compute fingerprint for a single table
CREATE FUNCTION steep_repl.compute_fingerprint(p_schema TEXT, p_table TEXT)
RETURNS TEXT AS $$
    SELECT encode(sha256(string_agg(
        column_name || ':' || data_type || ':' ||
        coalesce(column_default, 'NULL') || ':' || is_nullable,
        '|' ORDER BY ordinal_position
    )::bytea), 'hex')
    FROM information_schema.columns
    WHERE table_schema = p_schema AND table_name = p_table;
$$ LANGUAGE sql STABLE;

-- Capture fingerprint for a table (insert or update)
CREATE FUNCTION steep_repl.capture_fingerprint(p_schema TEXT, p_table TEXT)
RETURNS steep_repl.schema_fingerprints AS $$
    INSERT INTO steep_repl.schema_fingerprints (table_schema, table_name, fingerprint, column_count, column_definitions)
    SELECT
        p_schema,
        p_table,
        steep_repl.compute_fingerprint(p_schema, p_table),
        count(*),
        jsonb_agg(jsonb_build_object(
            'name', column_name,
            'type', data_type,
            'default', column_default,
            'nullable', is_nullable,
            'position', ordinal_position
        ) ORDER BY ordinal_position)
    FROM information_schema.columns
    WHERE table_schema = p_schema AND table_name = p_table
    GROUP BY 1, 2
    ON CONFLICT (table_schema, table_name) DO UPDATE SET
        fingerprint = EXCLUDED.fingerprint,
        column_count = EXCLUDED.column_count,
        column_definitions = EXCLUDED.column_definitions,
        captured_at = now()
    RETURNING *;
$$ LANGUAGE sql;

-- Capture all user tables
CREATE FUNCTION steep_repl.capture_all_fingerprints()
RETURNS INTEGER AS $$
DECLARE
    v_count INTEGER := 0;
BEGIN
    FOR rec IN
        SELECT schemaname, tablename
        FROM pg_tables
        WHERE schemaname NOT IN ('pg_catalog', 'information_schema', 'steep_repl')
    LOOP
        PERFORM steep_repl.capture_fingerprint(rec.schemaname, rec.tablename);
        v_count := v_count + 1;
    END LOOP;
    RETURN v_count;
END;
$$ LANGUAGE plpgsql;
```

#### SQL Definition

```sql
CREATE TABLE steep_repl.schema_fingerprints (
    table_schema TEXT NOT NULL,
    table_name TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    column_count INTEGER NOT NULL CHECK (column_count >= 0),
    captured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    column_definitions JSONB,
    PRIMARY KEY (table_schema, table_name)
);

CREATE INDEX idx_fingerprints_captured ON steep_repl.schema_fingerprints(captured_at);

COMMENT ON TABLE steep_repl.schema_fingerprints IS 'Schema fingerprints for drift detection';
COMMENT ON COLUMN steep_repl.schema_fingerprints.fingerprint IS 'SHA256 hash of column definitions';
```

---

### 5. Init Slot (Tracking)

**Table**: `steep_repl.init_slots`
**Purpose**: Track replication slots created for manual initialization.

#### Columns

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| slot_name | TEXT | PK | Replication slot name |
| node_id | TEXT | NOT NULL, FK→nodes | Node that owns the slot |
| lsn | TEXT | NOT NULL | LSN at slot creation |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT now() | When slot created |
| expires_at | TIMESTAMPTZ | | Auto-cleanup timestamp |
| used_by_node | TEXT | FK→nodes | Node that used this slot for init |
| used_at | TIMESTAMPTZ | | When slot was consumed |

#### SQL Definition

```sql
CREATE TABLE steep_repl.init_slots (
    slot_name TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES steep_repl.nodes(node_id),
    lsn TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    used_by_node TEXT REFERENCES steep_repl.nodes(node_id),
    used_at TIMESTAMPTZ
);

CREATE INDEX idx_init_slots_node ON steep_repl.init_slots(node_id);
CREATE INDEX idx_init_slots_expires ON steep_repl.init_slots(expires_at) WHERE expires_at IS NOT NULL;

COMMENT ON TABLE steep_repl.init_slots IS 'Replication slots for manual initialization workflow';
```

---

## Go Model Definitions

### InitState Enum

```go
// internal/repl/models/init_state.go

package models

// InitState represents the initialization state of a node.
type InitState string

const (
    InitStateUninitialized  InitState = "uninitialized"
    InitStatePreparing      InitState = "preparing"
    InitStateCopying        InitState = "copying"
    InitStateCatchingUp     InitState = "catching_up"
    InitStateSynchronized   InitState = "synchronized"
    InitStateDiverged       InitState = "diverged"
    InitStateFailed         InitState = "failed"
    InitStateReinitializing InitState = "reinitializing"
)

func (s InitState) IsValid() bool {
    switch s {
    case InitStateUninitialized, InitStatePreparing, InitStateCopying,
         InitStateCatchingUp, InitStateSynchronized, InitStateDiverged,
         InitStateFailed, InitStateReinitializing:
        return true
    }
    return false
}

func (s InitState) IsTerminal() bool {
    return s == InitStateSynchronized || s == InitStateFailed
}

func (s InitState) IsActive() bool {
    return s == InitStatePreparing || s == InitStateCopying ||
           s == InitStateCatchingUp || s == InitStateReinitializing
}
```

### InitProgress Model

```go
// internal/repl/models/progress.go

package models

import "time"

type InitProgress struct {
    NodeID              string    `db:"node_id" json:"node_id"`
    Phase               string    `db:"phase" json:"phase"`
    OverallPercent      float64   `db:"overall_percent" json:"overall_percent"`
    TablesTotal         int       `db:"tables_total" json:"tables_total"`
    TablesCompleted     int       `db:"tables_completed" json:"tables_completed"`
    CurrentTable        *string   `db:"current_table" json:"current_table,omitempty"`
    CurrentTablePercent float64   `db:"current_table_percent" json:"current_table_percent"`
    RowsCopied          int64     `db:"rows_copied" json:"rows_copied"`
    BytesCopied         int64     `db:"bytes_copied" json:"bytes_copied"`
    ThroughputRowsSec   float64   `db:"throughput_rows_sec" json:"throughput_rows_sec"`
    StartedAt           time.Time `db:"started_at" json:"started_at"`
    ETASeconds          *int      `db:"eta_seconds" json:"eta_seconds,omitempty"`
    UpdatedAt           time.Time `db:"updated_at" json:"updated_at"`
    ParallelWorkers     int       `db:"parallel_workers" json:"parallel_workers"`
    ErrorMessage        *string   `db:"error_message" json:"error_message,omitempty"`
}
```

### SchemaFingerprint Model

```go
// internal/repl/models/fingerprint.go

package models

import (
    "encoding/json"
    "time"
)

type SchemaFingerprint struct {
    TableSchema       string          `db:"table_schema" json:"table_schema"`
    TableName         string          `db:"table_name" json:"table_name"`
    Fingerprint       string          `db:"fingerprint" json:"fingerprint"`
    ColumnCount       int             `db:"column_count" json:"column_count"`
    CapturedAt        time.Time       `db:"captured_at" json:"captured_at"`
    ColumnDefinitions json.RawMessage `db:"column_definitions" json:"column_definitions,omitempty"`
}

type FingerprintComparison struct {
    TableSchema       string `json:"table_schema"`
    TableName         string `json:"table_name"`
    LocalFingerprint  string `json:"local_fingerprint"`
    RemoteFingerprint string `json:"remote_fingerprint"`
    Status            string `json:"status"` // MATCH, MISMATCH, LOCAL_ONLY, REMOTE_ONLY
}
```

---

## Configuration Extensions

Add to `config.yaml`:

```yaml
repl:
  # ... existing config ...

  initialization:
    method: snapshot              # snapshot | manual | two_phase
    parallel_workers: 4           # 1-16, PG18 parallel COPY
    snapshot_timeout: 24h
    large_table_threshold: 10GB
    large_table_method: pg_dump   # pg_dump | copy | basebackup

    schema_sync:
      mode: strict                # strict | auto | manual

    storage:
      type: local                 # local | s3 | gcs | azure | nfs
      path: /var/steep/snapshots
      retention: 168h             # 7 days

    generation:
      compression: gzip           # none | gzip | lz4 | zstd
      checksum: sha256

    application:
      verify_checksums: true
      sequence_sync: auto         # PG18 REFRESH SEQUENCES
```

---

## Migration Order

1. Add columns to `steep_repl.nodes` (ALTER TABLE)
2. Create `steep_repl.init_progress` table
3. Create `steep_repl.snapshots` table
4. Create `steep_repl.schema_fingerprints` table
5. Create `steep_repl.init_slots` table
6. Create fingerprint functions
7. Create comparison functions

All migrations are additive and backward-compatible with existing steep_repl schema.
