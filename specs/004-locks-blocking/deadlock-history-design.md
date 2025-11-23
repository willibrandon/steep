# Deadlock History Storage Design for Steep

## Overview
Implement deadlock detection and historical storage for pattern analysis, providing DBAs with comprehensive incident investigation capabilities.

## Architecture Decision: Hybrid Approach

### Two-Tier System
1. **Detection Layer**: PostgreSQL logs with `log_lock_waits = on`
2. **Storage Layer**: SQLite database for structured queries and historical analysis

### Why Not In-Memory Ring Buffer?
- ❌ Loses history on restart
- ❌ Can't query patterns across days/weeks
- ❌ Limited investigation capabilities
- ❌ Deadlocks are rare, serious events worth persisting

### Why SQLite Like query_stats?
- ✅ Persistence across restarts
- ✅ Pattern analysis queries ("which tables deadlock most?")
- ✅ Historical trending
- ✅ Correlation with query performance data
- ✅ Standard SQL interface for complex queries

## Database Schema

```sql
-- Main deadlock events table
CREATE TABLE deadlock_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    detected_at TIMESTAMP NOT NULL,
    database_name TEXT NOT NULL,
    
    -- Deadlock resolution
    resolved_by_pid INTEGER,  -- Which process was killed
    detection_time_ms INTEGER, -- How long before detection
    
    -- Metadata
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    INDEX idx_detected_at (detected_at),
    INDEX idx_database (database_name)
);

-- Processes involved in deadlock (supports N-way deadlocks)
CREATE TABLE deadlock_processes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id INTEGER NOT NULL,
    pid INTEGER NOT NULL,
    username TEXT,
    application_name TEXT,
    client_addr TEXT,
    backend_start TIMESTAMP,
    xact_start TIMESTAMP,
    
    -- Lock information
    lock_type TEXT,  -- relation, transactionid, tuple, etc.
    lock_mode TEXT,  -- ShareLock, AccessExclusiveLock, etc.
    relation_name TEXT,  -- Table name if available
    
    -- Query that caused/held the lock
    query TEXT NOT NULL,
    query_fingerprint TEXT,  -- Normalized query for grouping
    
    -- Blocked by
    blocked_by_pid INTEGER,  -- References another process in this deadlock
    
    FOREIGN KEY (event_id) REFERENCES deadlock_events(id) ON DELETE CASCADE,
    INDEX idx_event_id (event_id),
    INDEX idx_pid (pid),
    INDEX idx_relation (relation_name),
    INDEX idx_fingerprint (query_fingerprint)
);

-- Pre-computed patterns for fast dashboard queries
CREATE TABLE deadlock_patterns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern_key TEXT UNIQUE NOT NULL,  -- e.g., "table:users|lock:AccessExclusiveLock"
    first_seen TIMESTAMP NOT NULL,
    last_seen TIMESTAMP NOT NULL,
    occurrence_count INTEGER DEFAULT 1,
    affected_tables TEXT,  -- JSON array
    common_queries TEXT,   -- JSON array of query fingerprints
    
    INDEX idx_last_seen (last_seen),
    INDEX idx_count (occurrence_count)
);
```

## Detection Methods (Priority Order)

### Method 1: PostgreSQL Log Parsing (PRIMARY - P1)
**Pros:**
- Most comprehensive data
- Includes full deadlock graph
- No polling overhead
- Works with `log_lock_waits = on`

**Cons:**
- Requires log access
- Need log parser
- Depends on PostgreSQL logging config

**Implementation:**
```go
// Log format example (PostgreSQL):
// ERROR: deadlock detected
// DETAIL: Process 123 waits for ShareLock on transaction 456; blocked by process 789.
// Process 789 waits for ShareLock on transaction 456; blocked by process 123.
// HINT: See server log for query details.
// CONTEXT: while updating tuple (1,3) in relation "users"
// STATEMENT: UPDATE users SET status='active' WHERE id=1

type LogParser struct {
    logPath    string
    position   int64  // Track last read position
    db         *sql.DB // SQLite connection
}

func (p *LogParser) ParseDeadlockEntry(logEntry string) (*DeadlockEvent, error) {
    // Parse multi-line deadlock log entry
    // Extract PIDs, lock types, queries, relations
    // Store in SQLite
}
```

**Configuration Required:**
```yaml
# postgresql.conf settings (document in README)
log_lock_waits = on
deadlock_timeout = 1s  # Default, can tune higher for debugging
log_destination = 'stderr'  # Or 'csvlog' for easier parsing
logging_collector = on
```

### Method 2: pg_stat_database Counter Polling (FALLBACK - P2)
**Pros:**
- No log parsing required
- Simple query
- Low overhead

**Cons:**
- Only provides count, not details
- Can't capture queries or lock types
- Requires correlation with pg_stat_activity at exact moment

**Implementation:**
```go
// Track counter increases to detect new deadlocks
SELECT 
    datname,
    deadlocks,
    deadlocks - COALESCE(prev_deadlocks, 0) as new_deadlocks
FROM pg_stat_database
WHERE datname = current_database()

// When counter increases, capture current pg_stat_activity
// This is "best effort" and may miss details
```

### Method 3: Real-time Lock Monitoring (EXPERIMENTAL - P3)
**Approach:** 
- Poll pg_locks + pg_stat_activity every 100ms
- Detect circular dependencies in lock waits
- Proactive detection before PostgreSQL kills transaction

**Pros:**
- Could detect deadlocks before resolution
- Full context capture

**Cons:**
- High overhead (polling every 100ms)
- Complex circular dependency detection
- May have false positives
- Not recommended for production

## Data Capture Flow

```
┌─────────────────────────────────────────────────┐
│ PostgreSQL Server                               │
│                                                 │
│  Deadlock occurs → log_lock_waits triggers     │
│  └─> ERROR: deadlock detected written to log   │
└────────────────────┬────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────┐
│ Steep - Log Monitor (Goroutine)                │
│                                                 │
│  1. Tail PostgreSQL log file                   │
│  2. Detect "deadlock detected" pattern         │
│  3. Parse multi-line log entry                 │
│  4. Extract: PIDs, locks, queries, relations   │
└────────────────────┬────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────┐
│ SQLite Storage (steep_data.db)                 │
│                                                 │
│  • deadlock_events table                       │
│  • deadlock_processes table                    │
│  • deadlock_patterns table (aggregates)        │
└────────────────────┬────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────┐
│ Steep UI - Deadlock History View               │
│                                                 │
│  • Recent deadlocks (last 7 days)              │
│  • Pattern analysis                            │
│  • Affected tables ranking                     │
│  • Query correlation with pg_stat_statements   │
└─────────────────────────────────────────────────┘
```

## UI Design

### Locks View - History Tab

```
┌─ Deadlock History ──────────────────────────────────────────┐
│                                                              │
│ Last 7 Days: 3 deadlocks   Most Affected: users, orders    │
│                                                              │
│ ┌────────────────────────────────────────────────────────┐ │
│ │ Timestamp            │ Tables   │ Processes │ Duration │ │
│ ├────────────────────────────────────────────────────────┤ │
│ │ 2025-11-22 14:23:45 │ users    │ 2 (⚠)    │ 1.2s     │ │
│ │ 2025-11-21 09:15:32 │ orders   │ 2 (⚠)    │ 0.8s     │ │
│ │ 2025-11-20 18:45:12 │ users    │ 3 (⚠⚠)   │ 2.1s     │ │
│ └────────────────────────────────────────────────────────┘ │
│                                                              │
│ [d] Details  [p] Patterns  [f] Filter  [?] Help            │
└──────────────────────────────────────────────────────────────┘
```

### Deadlock Detail View

```
┌─ Deadlock Detail ─────────────────────────────────────────┐
│ Event ID: 123                                             │
│ Detected: 2025-11-22 14:23:45                            │
│ Detection Time: 1.2s                                      │
│ Resolved: Killed PID 5678                                │
│                                                            │
│ Process Tree:                                             │
│ ├── [PID 1234] 🔒 Blocker                               │
│ │   ├── Lock: ROW EXCLUSIVE on users                     │
│ │   ├── User: app_user                                   │
│ │   └── Query: UPDATE users SET status='active'...       │
│ │                                                          │
│ └── [PID 5678] ⏸ Blocked (KILLED)                       │
│     ├── Lock: ROW EXCLUSIVE on users (waiting)           │
│     ├── User: app_user                                   │
│     └── Query: UPDATE users SET email='test@...'         │
│                                                            │
│ Pattern: Similar deadlock on 'users' table 2x this week  │
│                                                            │
│ [c] Copy Query  [s] Similar Events  [ESC] Back           │
└────────────────────────────────────────────────────────────┘
```

### Pattern Analysis View

```
┌─ Deadlock Patterns ──────────────────────────────────────┐
│                                                           │
│ Top Tables by Deadlock Frequency (Last 30 Days)         │
│ ┌───────────────────────────────────────────────────┐   │
│ │ Table    │ Count │ Last Seen          │ Trend     │   │
│ ├───────────────────────────────────────────────────┤   │
│ │ users    │ 12    │ 2 hours ago        │ ↑ +25%   │   │
│ │ orders   │ 8     │ 1 day ago          │ → stable │   │
│ │ payments │ 3     │ 3 days ago         │ ↓ -50%   │   │
│ └───────────────────────────────────────────────────┘   │
│                                                           │
│ Common Lock Conflicts:                                   │
│ • ROW EXCLUSIVE vs ROW EXCLUSIVE (83%)                   │
│ • ACCESS EXCLUSIVE vs SHARE (12%)                        │
│ • Other (5%)                                             │
│                                                           │
│ Recommendations:                                          │
│ ⚠ users table: Consider row-level locking strategy      │
│ ⚠ orders table: Review transaction isolation levels     │
│                                                           │
└───────────────────────────────────────────────────────────┘
```

## Implementation Phases

### Phase 1: Core Storage (P1)
- [ ] Create SQLite schema
- [ ] Implement deadlock event insert/query functions
- [ ] Add migration for existing steep_data.db
- [ ] Write unit tests for storage layer

### Phase 2: Log Parser (P1)
- [ ] Implement PostgreSQL log tail reader
- [ ] Parse deadlock log entries (regex patterns)
- [ ] Extract PIDs, locks, queries, relations
- [ ] Handle multi-line log entries
- [ ] Test with various log formats

### Phase 3: UI Integration (P1)
- [ ] Add "History" tab to Locks view
- [ ] Display recent deadlocks in table
- [ ] Implement detail view with tree visualization
- [ ] Add filtering by date/table/query
- [ ] Keyboard shortcuts

### Phase 4: Pattern Analysis (P2)
- [ ] Compute deadlock_patterns aggregates
- [ ] Pattern detection queries
- [ ] Trend analysis (week over week)
- [ ] Recommendations engine
- [ ] Pattern view UI

### Phase 5: Advanced Features (P3)
- [ ] Correlation with query stats
- [ ] Export deadlock report (JSON/CSV)
- [ ] Deadlock replay simulation
- [ ] Email/webhook alerts for patterns

## Configuration

### Required PostgreSQL Settings
Document in README with detection:
```go
func CheckDeadlockConfig(db *pgxpool.Pool) []string {
    var warnings []string
    
    // Check log_lock_waits
    if !getSetting(db, "log_lock_waits") {
        warnings = append(warnings, 
            "log_lock_waits is OFF - deadlock history requires this setting")
    }
    
    // Check log accessibility
    logDest := getSetting(db, "log_destination")
    if logDest != "stderr" && logDest != "csvlog" {
        warnings = append(warnings, 
            "Unusual log_destination - may affect deadlock capture")
    }
    
    return warnings
}
```

### Steep Configuration
```yaml
deadlock_history:
  enabled: true
  log_path: "/var/log/postgresql/postgresql-14-main.log"  # Auto-detect if possible
  retention_days: 90  # How long to keep history
  pattern_analysis: true
  alert_threshold: 3  # Alert if same pattern occurs N times in 24h
```

## Query Examples

### Recent Deadlocks
```sql
SELECT 
    de.id,
    de.detected_at,
    de.database_name,
    COUNT(dp.id) as process_count,
    GROUP_CONCAT(DISTINCT dp.relation_name) as tables,
    de.detection_time_ms
FROM deadlock_events de
LEFT JOIN deadlock_processes dp ON dp.event_id = de.id
WHERE de.detected_at > datetime('now', '-7 days')
GROUP BY de.id
ORDER BY de.detected_at DESC
LIMIT 50;
```

### Pattern Analysis
```sql
SELECT 
    relation_name,
    COUNT(DISTINCT event_id) as deadlock_count,
    MAX(de.detected_at) as last_occurrence,
    GROUP_CONCAT(DISTINCT lock_mode) as lock_modes
FROM deadlock_processes dp
JOIN deadlock_events de ON de.id = dp.event_id
WHERE 
    de.detected_at > datetime('now', '-30 days')
    AND relation_name IS NOT NULL
GROUP BY relation_name
ORDER BY deadlock_count DESC
LIMIT 10;
```

### Correlate with Query Stats
```sql
-- Find queries that appear in both deadlocks and slow query stats
SELECT 
    dp.query_fingerprint,
    COUNT(DISTINCT dp.event_id) as deadlock_count,
    qs.total_time,
    qs.calls,
    qs.mean_time
FROM deadlock_processes dp
JOIN query_stats qs ON qs.query_fingerprint = dp.query_fingerprint
WHERE dp.query_fingerprint IS NOT NULL
GROUP BY dp.query_fingerprint
ORDER BY deadlock_count DESC, qs.total_time DESC
LIMIT 20;
```

## Testing Strategy

### Unit Tests
```go
func TestDeadlockEventStorage(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    
    event := &DeadlockEvent{
        DetectedAt:      time.Now(),
        DatabaseName:    "test_db",
        DetectionTimeMs: 1200,
    }
    
    id, err := InsertDeadlockEvent(db, event)
    assert.NoError(t, err)
    assert.Greater(t, id, 0)
    
    // Retrieve and verify
    retrieved, err := GetDeadlockEvent(db, id)
    assert.NoError(t, err)
    assert.Equal(t, event.DatabaseName, retrieved.DatabaseName)
}
```

### Integration Tests
```go
func TestLogParser(t *testing.T) {
    // Create test log file with deadlock entry
    logContent := `
2025-11-22 14:23:45.123 UTC [1234] ERROR: deadlock detected
2025-11-22 14:23:45.123 UTC [1234] DETAIL: Process 1234 waits for ShareLock on transaction 5678; blocked by process 5678.
2025-11-22 14:23:45.123 UTC [1234] Process 5678 waits for ShareLock on transaction 1234; blocked by process 1234.
2025-11-22 14:23:45.123 UTC [1234] HINT: See server log for query details.
2025-11-22 14:23:45.123 UTC [1234] CONTEXT: while updating tuple (1,3) in relation "users"
2025-11-22 14:23:45.123 UTC [1234] STATEMENT: UPDATE users SET status='active' WHERE id=1
`
    
    parser := NewLogParser(createTempLog(t, logContent), db)
    events, err := parser.ParseNewEntries()
    
    assert.NoError(t, err)
    assert.Len(t, events, 1)
    assert.Equal(t, 2, len(events[0].Processes))
}
```

## Performance Considerations

1. **Log Tailing**: Use inotify/fsnotify for efficient log monitoring
2. **Batch Inserts**: Buffer and batch-insert deadlock events
3. **Indexes**: Proper indexing on timestamp, relation_name, fingerprint
4. **Retention**: Auto-delete old events (configurable retention period)
5. **Pattern Computation**: Background job, not on-demand

## Alternatives Considered

### ❌ In-Memory Ring Buffer
- Too simple for serious incident investigation
- Loses data on restart
- No pattern analysis capabilities

### ❌ PostgreSQL Table Storage  
- Adds load to monitored database
- Risk of deadlock while recording deadlock
- Complicates connection management

### ❌ Real-time Polling Detection
- Too much overhead (100ms polling)
- Complex circular dependency detection
- False positives

### ✅ SQLite + Log Parsing (CHOSEN)
- Best balance of capability and simplicity
- No load on monitored database
- Rich querying for patterns
- Persistent across restarts
- Follows proven query_stats pattern

## Documentation Needs

1. **README Section**: PostgreSQL configuration requirements
2. **Troubleshooting Guide**: Log access issues, parsing failures
3. **Pattern Analysis Guide**: How to interpret patterns, common fixes
4. **Architecture Docs**: Storage schema, capture flow diagram

## Success Metrics

- ✅ Capture 100% of deadlocks from logs
- ✅ Parse accuracy >99% (handle various log formats)
- ✅ Storage query performance <100ms for recent history
- ✅ Pattern analysis query <500ms
- ✅ UI updates within 5 seconds of deadlock detection
- ✅ Zero impact on monitored database performance