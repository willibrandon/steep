# Data Model: Replication Monitoring & Setup

**Feature**: 006-replication-monitoring
**Date**: 2025-11-24
**Source**: [spec.md](./spec.md)

---

## Entity Overview

This feature introduces 5 primary entities and 2 aggregate/view models:

| Entity | Source | Persistence | Purpose |
|--------|--------|-------------|---------|
| Replica | pg_stat_replication | In-memory | Streaming replication standby status |
| ReplicationSlot | pg_replication_slots | In-memory | Slot status and WAL retention |
| Publication | pg_publication, pg_publication_tables | In-memory | Logical replication publication |
| Subscription | pg_subscription, pg_stat_subscription | In-memory | Logical replication subscription |
| LagHistoryEntry | SQLite | Persistent | Time-series lag data for trends |
| ReplicationData | Aggregate | In-memory | Combined data for single refresh |
| ReplicationConfig | pg_settings | In-memory | Configuration readiness check |

---

## 1. Replica

Represents a streaming replication standby server connected to the primary.

**Source**: `pg_stat_replication` (PostgreSQL 10+)

### Fields

| Field | Type | Source Column | Description |
|-------|------|---------------|-------------|
| ApplicationName | string | application_name | Replica identifier |
| ClientAddr | string | client_addr | IP address of replica |
| State | string | state | Connection state (streaming, catchup, startup, backup) |
| SyncState | ReplicationSyncState | sync_state | Synchronization mode (sync, async, potential, quorum) |
| SentLSN | string | sent_lsn | Last WAL position sent |
| WriteLSN | string | write_lsn | Last WAL position written to disk |
| FlushLSN | string | flush_lsn | Last WAL position flushed to disk |
| ReplayLSN | string | replay_lsn | Last WAL position replayed |
| ByteLag | int64 | calculated | pg_wal_lsn_diff(sent_lsn, replay_lsn) |
| WriteLag | time.Duration | write_lag | Time lag for write |
| FlushLag | time.Duration | flush_lag | Time lag for flush |
| ReplayLag | time.Duration | replay_lag | Time lag for replay |
| BackendStart | time.Time | backend_start | Connection start time |
| Upstream | string | derived | For cascading: name of upstream server |

### Derived Values

```go
// LagSeverity returns color-coded severity based on byte lag
func (r *Replica) LagSeverity() LagSeverity {
    switch {
    case r.ByteLag < 1024*1024:          // < 1MB
        return LagSeverityHealthy
    case r.ByteLag < 10*1024*1024:       // < 10MB
        return LagSeverityWarning
    default:                              // >= 10MB
        return LagSeverityCritical
    }
}

// FormatByteLag returns human-readable lag (e.g., "1.2 MB")
func (r *Replica) FormatByteLag() string
```

### Enum: ReplicationSyncState

```go
type ReplicationSyncState int

const (
    SyncStateAsync ReplicationSyncState = iota
    SyncStateSync
    SyncStatePotential
    SyncStateQuorum
)
```

### Enum: LagSeverity

```go
type LagSeverity int

const (
    LagSeverityHealthy LagSeverity = iota  // Green
    LagSeverityWarning                      // Yellow
    LagSeverityCritical                     // Red
)
```

---

## 2. ReplicationSlot

Represents a physical or logical replication slot.

**Source**: `pg_replication_slots` (PostgreSQL 9.4+, wal_status 13+)

### Fields

| Field | Type | Source Column | Description |
|-------|------|---------------|-------------|
| SlotName | string | slot_name | Unique slot identifier |
| SlotType | SlotType | slot_type | physical or logical |
| Database | string | database | Database name (logical only) |
| Active | bool | active | Whether slot is in use |
| ActivePID | int | active_pid | PID using the slot |
| RestartLSN | string | restart_lsn | Oldest WAL needed |
| ConfirmedFlushLSN | string | confirmed_flush_lsn | Last confirmed position |
| RetainedBytes | int64 | calculated | WAL retained by this slot |
| WALStatus | string | wal_status | reserved, extended, unreserved, lost (PG13+) |
| SafeWALSize | int64 | safe_wal_size | Remaining WAL before slot removal (PG13+) |

### Derived Values

```go
// IsOrphaned returns true if slot is inactive for extended period
func (s *ReplicationSlot) IsOrphaned(threshold time.Duration) bool

// RetentionWarning returns true if retained WAL is concerning
func (s *ReplicationSlot) RetentionWarning(availableSpace int64) bool {
    return float64(s.RetainedBytes) / float64(availableSpace) > 0.8
}

// FormatRetainedBytes returns human-readable size
func (s *ReplicationSlot) FormatRetainedBytes() string
```

### Enum: SlotType

```go
type SlotType int

const (
    SlotTypePhysical SlotType = iota
    SlotTypeLogical
)
```

---

## 3. Publication

Represents a logical replication publication on the primary.

**Source**: `pg_publication`, `pg_publication_tables` (PostgreSQL 10+)

### Fields

| Field | Type | Source Column | Description |
|-------|------|---------------|-------------|
| Name | string | pubname | Publication name |
| AllTables | bool | puballtables | Publishes all tables |
| Insert | bool | pubinsert | Publishes INSERT |
| Update | bool | pubupdate | Publishes UPDATE |
| Delete | bool | pubdelete | Publishes DELETE |
| Truncate | bool | pubtruncate | Publishes TRUNCATE (PG11+) |
| Tables | []string | joined | List of published table names |
| TableCount | int | count | Number of tables in publication |
| SubscriberCount | int | derived | Number of active subscribers |

### Derived Values

```go
// OperationFlags returns formatted string of enabled operations
func (p *Publication) OperationFlags() string {
    // Returns e.g., "I U D" for insert, update, delete
}
```

---

## 4. Subscription

Represents a logical replication subscription on the subscriber.

**Source**: `pg_subscription`, `pg_stat_subscription` (PostgreSQL 10+, stats 12+)

### Fields

| Field | Type | Source Column | Description |
|-------|------|---------------|-------------|
| Name | string | subname | Subscription name |
| Enabled | bool | subenabled | Whether subscription is active |
| ConnInfo | string | subconninfo | Connection string to publisher |
| Publications | []string | subpublications | Array of publication names |
| ReceivedLSN | string | received_lsn | Last LSN received |
| LatestEndLSN | string | latest_end_lsn | Latest end LSN from publisher |
| ByteLag | int64 | calculated | Lag in bytes |
| LastMsgSendTime | time.Time | last_msg_send_time | Last message time |
| LastMsgReceiptTime | time.Time | last_msg_receipt_time | Last receipt time |

### Derived Values

```go
// LagSeverity returns severity based on byte lag (same thresholds as Replica)
func (s *Subscription) LagSeverity() LagSeverity

// IsStale returns true if no messages received recently
func (s *Subscription) IsStale(threshold time.Duration) bool
```

---

## 5. LagHistoryEntry

Time-series record for persistent lag trend analysis.

**Storage**: SQLite (`replication_lag_history` table)

### Fields

| Field | Type | SQLite Column | Description |
|-------|------|---------------|-------------|
| ID | int64 | id | Auto-increment primary key |
| Timestamp | time.Time | timestamp | Measurement time |
| ReplicaName | string | replica_name | Application name of replica |
| SentLSN | string | sent_lsn | Sent LSN at time of measurement |
| WriteLSN | string | write_lsn | Write LSN at measurement |
| FlushLSN | string | flush_lsn | Flush LSN at measurement |
| ReplayLSN | string | replay_lsn | Replay LSN at measurement |
| ByteLag | int64 | byte_lag | Calculated byte lag |
| TimeLagMs | int64 | time_lag_ms | Replay lag in milliseconds |
| SyncState | string | sync_state | Sync state at measurement |
| Direction | string | direction | Future: outbound/inbound for multi-master |
| ConflictCount | int | conflict_count | Future: conflicts for multi-master |

### SQLite Schema

```sql
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

CREATE INDEX IF NOT EXISTS idx_lag_history_time
    ON replication_lag_history(timestamp, replica_name);

CREATE INDEX IF NOT EXISTS idx_lag_history_replica
    ON replication_lag_history(replica_name, timestamp);
```

### Retention Policy

- Default: 24 hours
- Maximum: 7 days (configurable)
- Cleanup query:

```sql
DELETE FROM replication_lag_history
WHERE timestamp < datetime('now', '-' || ? || ' hours');
```

---

## 6. ReplicationData (Aggregate)

Combined data structure for a single monitor refresh cycle.

### Fields

```go
type ReplicationData struct {
    // Server role
    IsPrimary bool

    // Streaming replication
    Replicas []Replica

    // Replication slots
    Slots []ReplicationSlot

    // Logical replication (primary)
    Publications []Publication

    // Logical replication (subscriber)
    Subscriptions []Subscription

    // Lag history for sparklines (in-memory ring buffer)
    LagHistory map[string][]float64 // keyed by replica name

    // Metadata
    RefreshTime time.Time
    QueryDuration time.Duration
}
```

### Constructor

```go
func NewReplicationData() *ReplicationData {
    return &ReplicationData{
        Replicas:      make([]Replica, 0),
        Slots:         make([]ReplicationSlot, 0),
        Publications:  make([]Publication, 0),
        Subscriptions: make([]Subscription, 0),
        LagHistory:    make(map[string][]float64),
        RefreshTime:   time.Now(),
    }
}
```

---

## 7. ReplicationConfig

Configuration readiness check results.

**Source**: `pg_settings`

### Fields

```go
type ReplicationConfig struct {
    WALLevel           ConfigParam
    MaxWALSenders      ConfigParam
    MaxReplicationSlots ConfigParam
    WALKeepSize        ConfigParam
    HotStandby         ConfigParam
    ArchiveMode        ConfigParam
}

type ConfigParam struct {
    Name          string
    CurrentValue  string
    RequiredValue string // Minimum required for replication
    Unit          string
    IsValid       bool
    NeedsRestart  bool   // true if change requires restart
    Context       string // sighup, postmaster, etc.
}
```

### Derived Values

```go
// IsReady returns true if all required params are configured
func (c *ReplicationConfig) IsReady() bool

// RequiresRestart returns true if any changes need server restart
func (c *ReplicationConfig) RequiresRestart() bool

// GetIssues returns list of misconfigured parameters
func (c *ReplicationConfig) GetIssues() []ConfigParam
```

---

## Relationships

```
┌─────────────────────────────────────────────────────────────────┐
│                        ReplicationData                          │
│                       (Aggregate Root)                          │
└───────┬──────────────┬──────────────┬──────────────┬───────────┘
        │              │              │              │
        ▼              ▼              ▼              ▼
   ┌─────────┐   ┌───────────┐  ┌───────────┐  ┌────────────┐
   │ Replica │   │   Slot    │  │Publication│  │Subscription│
   └────┬────┘   └───────────┘  └───────────┘  └────────────┘
        │
        ▼
   ┌─────────────────┐
   │ LagHistoryEntry │ (persisted to SQLite)
   └─────────────────┘
```

---

## Query Mappings

### Replica Population

```go
// Query: see research.md Section 4.1
func (q *ReplicationQueries) GetReplicas(ctx context.Context) ([]Replica, error)
```

### Slot Population

```go
// Query: see research.md Section 4.2
func (q *ReplicationQueries) GetSlots(ctx context.Context) ([]ReplicationSlot, error)
```

### Publication Population

```go
// Query: see research.md Section 4.4
func (q *ReplicationQueries) GetPublications(ctx context.Context) ([]Publication, error)
```

### Subscription Population

```go
// Query: see research.md Section 4.5
func (q *ReplicationQueries) GetSubscriptions(ctx context.Context) ([]Subscription, error)
```

### Lag History Persistence

```go
// Store lag entry
func (s *ReplicationStore) SaveLagEntry(ctx context.Context, entry LagHistoryEntry) error

// Query lag history for sparklines
func (s *ReplicationStore) GetLagHistory(ctx context.Context, replicaName string,
    since time.Time) ([]LagHistoryEntry, error)

// Cleanup old entries
func (s *ReplicationStore) PruneLagHistory(ctx context.Context,
    retentionHours int) (int64, error)
```

---

## Version Compatibility Notes

| Entity/Field | Min Version | Notes |
|--------------|-------------|-------|
| Replica.ByteLag | PG12 | pg_wal_lsn_diff() available PG10, but consistent in 12+ |
| ReplicationSlot.WALStatus | PG13 | Use NULL for earlier versions |
| ReplicationSlot.SafeWALSize | PG13 | Use NULL for earlier versions |
| Publication.Truncate | PG11 | Default false for PG10 |
| Subscription stats | PG12 | pg_stat_subscription added in PG12 |

---

## Future Considerations (Multi-Master)

The data model includes placeholder fields for future multi-master/BDR support:

- `LagHistoryEntry.Direction`: "outbound" (default) or "inbound"
- `LagHistoryEntry.ConflictCount`: Number of replication conflicts

These fields are populated with defaults now but provide schema stability for future enhancements.
