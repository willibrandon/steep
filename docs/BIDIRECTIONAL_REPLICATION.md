# Steep Bidirectional Replication System

## Design Document v0.8 (Draft)

**Status**: Draft - Iterating
**Author**: Brandon
**Date**: 2025-12-03

---

## 1. Executive Summary

This document describes the design of a bidirectional replication system for PostgreSQL, integrated with the Steep monitoring application. The system provides:

- **Bidirectional logical replication** between PostgreSQL nodes
- **Conflict detection and resolution** with configurable policies
- **Automatic DDL replication** via ProcessUtility hook
- **Identity range management** using CHECK constraints (SQL Server merge replication pattern)
- **Monitoring and management UI** through Steep's existing replication view

### Design Principles

1. **Proven patterns over novel approaches** - Leverage SQL Server merge replication's 20+ year track record where applicable
2. **PostgreSQL 18 native features first** - Build on top of native logical replication, don't replace it
3. **Simple defaults, flexible options** - Works out of the box, configurable for complex scenarios
4. **Fail loudly** - Constraint violations and conflicts surface immediately, no silent corruption
5. **Schema-change minimal** - Works with existing SERIAL/IDENTITY columns via CHECK constraints
6. **Cross-platform from day one** - Windows, Linux, macOS support; Windows is first deployment target

---

## 2. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                              STEEP TUI                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │ Replication  │  │ Conflict     │  │ DDL          │              │
│  │ Topology     │  │ Resolution   │  │ Queue        │              │
│  └──────────────┘  └──────────────┘  └──────────────┘              │
└─────────────────────────────────────────┬───────────────────────────┘
                                          │ gRPC / Unix Socket
┌─────────────────────────────────────────▼───────────────────────────┐
│                         STEEP-REPL DAEMON (Go)                       │
│                                                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │ Range        │  │ Conflict     │  │ DDL          │              │
│  │ Coordinator  │  │ Arbitrator   │  │ Coordinator  │              │
│  └──────────────┘  └──────────────┘  └──────────────┘              │
│                                                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │ Topology     │  │ Health       │  │ Metrics      │              │
│  │ Manager      │  │ Monitor      │  │ Collector    │              │
│  └──────────────┘  └──────────────┘  └──────────────┘              │
└─────────────────────────────────────────┬───────────────────────────┘
                                          │ libpq / pgx
┌─────────────────────────────────────────▼───────────────────────────┐
│                    POSTGRESQL 18 + STEEP_REPL EXTENSION              │
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │ steep_repl Extension (Rust/pgrx)                               │ │
│  │  • ProcessUtility hook → DDL capture                           │ │
│  │  • Conflict metadata tables                                    │ │
│  │  • Range allocation tracking                                   │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │ PostgreSQL 18 Native Logical Replication                       │ │
│  │  • Publications with origin = none (loop prevention)          │ │
│  │  • Conflict logging (pg_stat_subscription_stats)               │ │
│  │  • Parallel apply workers                                      │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │ CHECK Constraints (Identity Range Enforcement)                 │ │
│  │  • steep_range_<table> constraints per replicated table       │ │
│  │  • Self-enforcing, self-documenting                           │ │
│  └────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Language | Responsibility |
|-----------|----------|----------------|
| steep_repl extension | Rust (pgrx) | DDL capture, conflict metadata, range tracking |
| steep-repl daemon | Go | Coordination, arbitration, health monitoring |
| steep-agent daemon | Go | Metrics collection, monitoring (existing, separate) |
| Steep TUI | Go | Visualization, manual resolution, configuration |
| PostgreSQL 18 | - | Transport layer (logical replication) |

### Daemon Separation

**steep-repl** and **steep-agent** are separate daemons with distinct responsibilities:

```
┌─────────────────────────────────────────────────────────────────┐
│                        STEEP DAEMONS                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  steep-agent (existing)          steep-repl (new)               │
│  ┌─────────────────────┐        ┌─────────────────────┐        │
│  │ • Metrics collection│        │ • Range coordination│        │
│  │ • Query stats       │        │ • Conflict arbiter  │        │
│  │ • Replication lag   │        │ • DDL coordination  │        │
│  │ • Lock monitoring   │        │ • Topology manager  │        │
│  │ • Background alerts │        │ • Node health       │        │
│  └─────────────────────┘        └─────────────────────┘        │
│           │                              │                      │
│           │    Shared Config File        │                      │
│           └──────────┬───────────────────┘                      │
│                      ▼                                          │
│              ~/.config/steep/config.yaml                        │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘

Deployment Options:
  1. TUI only           - No daemons, direct monitoring
  2. TUI + agent        - Background metrics collection
  3. TUI + agent + repl - Full bidirectional replication
  4. agent + repl only  - Headless replication node
```

**Why separate?**
- **Different lifecycles**: steep-agent runs on all monitored nodes; steep-repl only on bidirectional replication participants
- **Different failure modes**: Agent failure affects monitoring; repl failure affects replication coordination
- **Simpler deployment**: Not all users need bidirectional replication
- **Independent upgrades**: Can update replication logic without affecting monitoring

---

## 3. PostgreSQL Version Requirements

### 3.1 Supported Versions

**Required**: PostgreSQL 18 only.

PostgreSQL 18 introduces several features critical for bidirectional replication that are not available in earlier versions:

| Feature | PostgreSQL 18 | Benefit for Bidirectional Replication |
|---------|---------------|---------------------------------------|
| **Logical Replication for DDL** | ✓ | Schema changes (CREATE TABLE, ALTER, etc.) replicate automatically |
| **Sequence Synchronization** | ✓ | Sequences sync via `REFRESH SEQUENCES`, critical for identity ranges |
| **Parallel COPY FROM** | ✓ | Faster initial snapshots with multi-worker bulk loading |
| Native conflict logging | ✓ | Built-in conflict detection in `pg_stat_subscription_stats` |
| Row-level filtering | ✓ | Fine-grained control over what data replicates |
| Column-level filtering | ✓ | Exclude sensitive columns from replication |
| Parallel apply workers | ✓ | Higher throughput for apply operations |
| `track_commit_timestamp` | ✓ | Timestamp-based conflict resolution |

### 3.2 Key PostgreSQL 18 Features

#### Logical Replication for DDL

Previously, PostgreSQL logical replication could only replicate DML (INSERT/UPDATE/DELETE). PostgreSQL 18 extends this to include DDL statements (CREATE TABLE, ALTER, DROP, etc.), eliminating the need for manual schema synchronization:

```sql
-- DDL changes now replicate automatically
ALTER TABLE orders ADD COLUMN priority integer DEFAULT 0;
-- ^ Automatically replicated to all subscribers
```

**Impact on steep-repl**:
- Reduces complexity of DDL coordination layer
- ProcessUtility hook becomes optional (for conflict detection only)
- Schema drift is prevented at the PostgreSQL level

#### Sequence Synchronization

PostgreSQL 18 adds native sequence replication with `REFRESH SEQUENCES`:

```sql
-- Sync sequence values to subscriber
ALTER SUBSCRIPTION mysub REFRESH SEQUENCES;
```

**Impact on steep-repl**:
- Identity range coordination can leverage native sequence sync
- Post-snapshot sequence sync is automatic
- Reduces custom sequence handling code

#### Parallel COPY FROM

Initial table synchronization uses COPY FROM, which is now parallelized in PostgreSQL 18:

```
                     Single-threaded COPY          Parallel COPY (4 workers)
Data Size: 10GB      ~15 minutes                   ~4 minutes
Data Size: 100GB     ~2.5 hours                    ~40 minutes
Data Size: 1TB       ~25 hours                     ~7 hours
```

**Impact on steep-repl**:
- Faster initial snapshot generation
- Shorter maintenance windows for node initialization
- Better utilization of multi-core systems

### 3.3 Version Validation

steep-repl validates PostgreSQL version on startup:

```bash
steep-repl start
[INFO] PostgreSQL version: 18.0
[INFO] DDL replication: available
[INFO] Sequence synchronization: available
[INFO] Parallel COPY: available (4 workers)
[INFO] Version check: PASSED
```

```bash
steep-repl start
[ERROR] PostgreSQL version 17.2 is not supported.
[ERROR] Steep bidirectional replication requires PostgreSQL 18.
[ERROR]
[ERROR] PostgreSQL 18 features required:
[ERROR]   • Logical replication for DDL
[ERROR]   • Sequence synchronization
[ERROR]   • Parallel COPY FROM
[ERROR]
[ERROR] Please upgrade to PostgreSQL 18 before continuing.
```

---

## 4. Cross-Platform Compatibility

### 4.1 Overview

The entire system must run on Windows, Linux, and macOS. **Windows is the first deployment target.**

| Component | Windows | Linux | macOS |
|-----------|---------|-------|-------|
| steep (TUI) | ✓ ConPTY | ✓ PTY | ✓ PTY |
| steep-agent | ✓ SCM | ✓ systemd | ✓ launchd |
| steep-repl | ✓ SCM | ✓ systemd | ✓ launchd |
| steep_repl extension | ✓ DLL | ✓ .so | ✓ .dylib |
| PostgreSQL | ✓ Native | ✓ Native | ✓ Native |

### 4.2 IPC: Named Pipes vs Unix Sockets

Unix sockets don't exist on Windows. We use **named pipes** on Windows and Unix sockets elsewhere:

```go
// internal/repl/ipc/listener.go
func NewListener(name string) (net.Listener, error) {
    if runtime.GOOS == "windows" {
        return winio.ListenPipe(`\\.\pipe\steep-repl`, nil)
    }
    return net.Listen("unix", filepath.Join(os.TempDir(), "steep-repl.sock"))
}
```

| Platform | IPC Mechanism | Path/Name |
|----------|---------------|-----------|
| Windows | Named Pipe | `\\.\pipe\steep-repl` |
| Linux | Unix Socket | `/tmp/steep-repl.sock` or `$XDG_RUNTIME_DIR/steep-repl.sock` |
| macOS | Unix Socket | `/tmp/steep-repl.sock` |

**Library**: Use `github.com/Microsoft/go-winio` for Windows named pipes (already used by Docker, containerd).

### 4.3 Service Management

steep-repl uses `kardianos/service` (same as steep-agent) for cross-platform service management:

```go
// Already proven pattern from steep-agent
svcConfig := &service.Config{
    Name:        "steep-repl",
    DisplayName: "Steep Replication Coordinator",
    Description: "Coordinates bidirectional PostgreSQL replication",
}
```

| Platform | Service Manager | Install Command |
|----------|-----------------|-----------------|
| Windows | SCM | `steep-repl.exe install` (as Administrator) |
| Linux | systemd | `sudo steep-repl install` |
| macOS | launchd | `steep-repl install --user` |

### 4.4 File Paths

Use platform-appropriate paths:

```go
// internal/config/paths.go (existing pattern from steep)
func DataDir() string {
    switch runtime.GOOS {
    case "windows":
        // User service: %APPDATA%\steep
        // System service: %PROGRAMDATA%\steep
        if isSystemService() {
            return filepath.Join(os.Getenv("PROGRAMDATA"), "steep")
        }
        return filepath.Join(os.Getenv("APPDATA"), "steep")
    case "darwin":
        return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "steep")
    default:
        // XDG Base Directory Specification
        if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
            return filepath.Join(xdg, "steep")
        }
        return filepath.Join(os.Getenv("HOME"), ".local", "share", "steep")
    }
}
```

### 4.5 steep_repl Extension (pgrx)

pgrx supports Windows, but requires careful setup:

#### Windows Build Requirements

```powershell
# Prerequisites
# 1. Visual Studio Build Tools (C++ workload)
# 2. Rust toolchain (MSVC target)
# 3. PostgreSQL development files

# Install Rust
winget install Rustlang.Rust.MSVC

# Install pgrx
cargo install cargo-pgrx

# Initialize for Windows PostgreSQL
cargo pgrx init --pg18 "C:\Program Files\PostgreSQL\18\bin\pg_config"

# Build extension
cargo pgrx package --pg-config "C:\Program Files\PostgreSQL\18\bin\pg_config"
```

#### Extension File Locations

| Platform | Extension Directory | Library Extension |
|----------|--------------------|--------------------|
| Windows | `C:\Program Files\PostgreSQL\18\lib` | `.dll` |
| Linux | `/usr/lib/postgresql/18/lib` or `/usr/pgsql-18/lib` | `.so` |
| macOS | `/opt/homebrew/lib/postgresql@18` | `.dylib` |

#### Build Matrix

```yaml
# .github/workflows/build-extension.yml
strategy:
  matrix:
    os: [windows-latest, ubuntu-latest, macos-latest]
    pg: [16, 17, 18]
```

### 4.6 gRPC Cross-Platform

gRPC works identically across platforms. Use TLS for node-to-node communication:

```go
// Node-to-node (same on all platforms)
grpc.Dial(
    "node-b.example.com:5433",
    grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
)
```

### 4.7 Testing Strategy

```
┌─────────────────────────────────────────────────────────────────┐
│                    Cross-Platform Test Matrix                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Development (Windows + macOS, toggling):                       │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ Windows 11 + PostgreSQL 18                              │   │
│  │ • Primary deployment target                              │   │
│  │ • Service install/uninstall (SCM)                       │   │
│  │ • Named pipe IPC validation                              │   │
│  ├─────────────────────────────────────────────────────────┤   │
│  │ macOS + PostgreSQL 18                                   │   │
│  │ • Parallel development environment                       │   │
│  │ • Unix socket IPC validation                            │   │
│  │ • launchd service testing                               │   │
│  │ • Catches platform-specific issues early                │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  CI/CD (GitHub Actions):                                        │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ windows-latest: Unit tests, integration tests, build    │   │
│  │ ubuntu-latest:  Unit tests, integration tests, build    │   │
│  │ macos-latest:   Unit tests, build (no Docker)           │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Multi-Node Testing:                                            │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ Mixed OS topology tests:                                 │   │
│  │ • Windows primary ↔ Linux replica                       │   │
│  │ • Linux primary ↔ Windows replica                       │   │
│  │ • Three-node mesh (Windows, Linux, Linux)               │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 4.8 Windows-Specific Considerations

| Issue | Solution |
|-------|----------|
| Path separators | Use `filepath.Join()` everywhere, never hardcode `/` |
| Case sensitivity | Windows filesystem is case-insensitive; normalize table names |
| File locking | Use `os.O_EXCL` for lock files; Windows is stricter |
| Signal handling | `SIGTERM` works via `golang.org/x/sys/windows` |
| Console output | Use `golang.org/x/term` for terminal detection |
| Long paths | Enable long path support in manifest or use `\\?\` prefix |
| Firewall | Document port 5433 requirement for gRPC |

### 4.9 Docker Support (Optional)

For users who prefer containers, provide multi-arch images:

```dockerfile
# Dockerfile.steep-repl
FROM --platform=$TARGETPLATFORM golang:1.22 AS builder
# ... build steps ...

FROM --platform=$TARGETPLATFORM alpine:3.19
COPY --from=builder /steep-repl /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/steep-repl"]
```

```bash
# Build multi-arch
docker buildx build --platform linux/amd64,linux/arm64 -t steep/steep-repl .
```

Note: Windows containers are possible but not prioritized; native Windows installation preferred.

---

## 5. Identity Range Management

### 5.1 Overview

Identity ranges prevent primary key collisions in bidirectional replication by ensuring each node generates IDs from a non-overlapping range. This approach is borrowed directly from SQL Server merge replication.

### 4.2 Mechanism

Each replicated table with an auto-generated primary key gets:

1. **CHECK constraint** - Enforces the valid range for this node
2. **Sequence reseeding** - Sequence starts at beginning of allocated range
3. **Threshold monitoring** - Alert when approaching range exhaustion

```sql
-- Node A: Allocated range 1-10000
ALTER TABLE orders
    ADD CONSTRAINT steep_range_orders
    CHECK (order_id >= 1 AND order_id <= 10000);

ALTER SEQUENCE orders_order_id_seq RESTART WITH 1;

-- Node B: Allocated range 10001-20000
ALTER TABLE orders
    ADD CONSTRAINT steep_range_orders
    CHECK (order_id >= 10001 AND order_id <= 20000);

ALTER SEQUENCE orders_order_id_seq RESTART WITH 10001;
```

### 4.3 Range Lifecycle

```
┌─────────────────────────────────────────────────────────────────┐
│                      Range Lifecycle                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  1. ALLOCATION                                                  │
│     ┌─────────┐      Request Range      ┌─────────────────┐   │
│     │  Node   │ ───────────────────────►│  Coordinator    │   │
│     └─────────┘                          │  (steep-repl)   │   │
│                                          └────────┬────────┘   │
│                                                   │             │
│  2. ENFORCEMENT                                   ▼             │
│     ┌─────────────────────────────────────────────────────┐    │
│     │ BEGIN;                                              │    │
│     │ ALTER TABLE t DROP CONSTRAINT IF EXISTS steep_range_t;│   │
│     │ ALTER TABLE t ADD CONSTRAINT steep_range_t          │    │
│     │     CHECK (id >= 10001 AND id <= 20000);            │    │
│     │ ALTER SEQUENCE t_id_seq RESTART WITH 10001;         │    │
│     │ COMMIT;                                             │    │
│     └─────────────────────────────────────────────────────┘    │
│                                                                 │
│  3. MONITORING                                                  │
│     ┌─────────┐                                                │
│     │  Node   │  Threshold: 80% consumed → pre-allocate next   │
│     │         │  Threshold: 100% consumed → block INSERTs      │
│     └─────────┘                                                │
│                                                                 │
│  4. EXPANSION                                                   │
│     When threshold hit:                                         │
│     - Coordinator allocates next range                         │
│     - Constraint updated to span both ranges OR                │
│     - Next range cached for seamless transition                │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 4.4 Configuration

```yaml
# ~/.config/steep/config.yaml
replication:
  identity_ranges:
    enabled: true
    default_range_size: 10000
    threshold_percent: 80        # Request next range at 80%

    # Per-table overrides
    tables:
      orders:
        range_size: 100000       # High-volume table
      audit_log:
        enabled: false           # Uses UUIDs, no range needed
```

### 4.5 Failure Scenarios

| Scenario | Behavior |
|----------|----------|
| Range exhausted, coordinator unreachable | INSERTs fail with constraint violation |
| Node crashes mid-range | Unused IDs in range are "lost" (acceptable waste) |
| Coordinator crashes | New coordinator reads state from DB, continues |
| Constraint accidentally dropped | Next INSERT may collide; daemon detects and recreates |

### 4.6 Temporarily Disabling Range Constraints

PostgreSQL doesn't have SQL Server's `NOCHECK` option for constraints. Instead, we use a **function-based constraint** with a session bypass setting:

#### Implementation

```sql
-- Custom GUC for bypass control
-- (Registered by steep_repl extension on load)
SELECT pg_catalog.set_config('steep_repl.bypass_range_check', 'off', false);

-- Range check function (called by constraint)
CREATE FUNCTION steep_repl.check_id_range(
    p_table_schema TEXT,
    p_table_name TEXT,
    p_id BIGINT
) RETURNS BOOLEAN AS $$
DECLARE
    v_bypass TEXT;
    v_range_start BIGINT;
    v_range_end BIGINT;
BEGIN
    -- Check if bypass is enabled for this session
    v_bypass := current_setting('steep_repl.bypass_range_check', true);
    IF v_bypass = 'on' THEN
        RETURN true;
    END IF;

    -- Normal range check
    SELECT range_start, range_end INTO v_range_start, v_range_end
    FROM steep_repl.identity_ranges
    WHERE table_schema = p_table_schema
      AND table_name = p_table_name
      AND status = 'active';

    RETURN p_id >= v_range_start AND p_id <= v_range_end;
END;
$$ LANGUAGE plpgsql STABLE;

-- Constraint uses the function (instead of inline expression)
ALTER TABLE orders ADD CONSTRAINT steep_range_orders
    CHECK (steep_repl.check_id_range('public', 'orders', order_id));
```

#### Usage: Disabling for Maintenance

```sql
-- Disable range checking for this session (bulk imports, migrations, etc.)
SET steep_repl.bypass_range_check = 'on';

-- Perform bulk operations
INSERT INTO orders (order_id, ...) VALUES (999999, ...);  -- Out of range, allowed
COPY orders FROM '/path/to/data.csv';                      -- Mixed IDs, allowed

-- Re-enable range checking
SET steep_repl.bypass_range_check = 'off';

-- Or use transaction-scoped bypass
BEGIN;
SET LOCAL steep_repl.bypass_range_check = 'on';
-- ... operations ...
COMMIT;  -- Bypass automatically reverts
```

#### Steep TUI Integration

```
┌─ Identity Ranges ─────────────────────────────────────────────────┐
│                                                                   │
│  Range Checking: ENABLED                                          │
│                                                                   │
│  [B]ypass mode (current session)                                 │
│                                                                   │
│  ⚠ Bypass mode allows out-of-range IDs which may cause          │
│    conflicts during replication. Use only for:                   │
│    • Bulk data imports                                           │
│    • Disaster recovery                                           │
│    • Schema migrations                                           │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

#### CLI Support

```bash
# Via steep CLI
steep repl range bypass --enable
steep repl range bypass --disable

# Via steep-repl daemon
steep-repl bypass --table orders --duration 30m
```

#### Security Considerations

| Control | Mechanism |
|---------|-----------|
| Audit logging | Bypass enable/disable logged to steep_repl.audit_log |
| Role restriction | Optional: `steep_repl.bypass_allowed_roles` GUC |
| Time limit | Optional: Auto-revert after configurable duration |
| Notification | steep-repl daemon alerts when bypass enabled |

### 4.7 Monitoring in Steep

```
┌─ Identity Ranges ─────────────────────────────────────────────────┐
│                                                                   │
│  Table           This Node           Peer Nodes       Next Avail  │
│  ──────────────────────────────────────────────────────────────── │
│  orders          1-10000 (87%)       B: 10001-20000   20001       │
│  customers       5001-10000 (34%)    B: 1-5000        10001       │
│  line_items      1-50000 (92%) ⚠     B: 50001-100000  100001      │
│                                                                   │
│  ⚠ line_items approaching threshold - next range pre-allocated   │
│                                                                   │
│  [R]eallocate  [V]iew constraints  [H]istory                     │
└───────────────────────────────────────────────────────────────────┘
```

### 5.8 Composite Primary Keys

Tables with composite primary keys are handled based on their structure:

#### Parent-Child Relationships (Automatic)

When a composite PK includes a foreign key to a parent table that has range management, the child table **automatically inherits** range partitioning:

```sql
-- Parent table with single-column PK and range management
CREATE TABLE orders (
    order_id BIGINT PRIMARY KEY,  -- Range-managed: 1-10000 (node A), 10001-20000 (node B)
    customer_id BIGINT,
    order_date TIMESTAMP
);

-- Child table with composite PK including FK to parent
CREATE TABLE order_lines (
    order_id BIGINT REFERENCES orders(order_id),  -- Inherits range from parent
    line_number INT,
    product_id BIGINT,
    quantity INT,
    PRIMARY KEY (order_id, line_number)
);
```

In this case, `order_lines` automatically partitions by `order_id`:
- Node A creates order_lines for orders 1-10000
- Node B creates order_lines for orders 10001-20000
- No additional range constraint needed on `order_lines`

#### Orphan Composite Keys

Tables with composite PKs that don't reference a range-managed parent fall back to **conflict resolution only**:

```sql
-- Example: composite PK with no FK to parent
CREATE TABLE sensor_readings (
    device_id VARCHAR(50),
    reading_timestamp TIMESTAMP,
    value NUMERIC,
    PRIMARY KEY (device_id, reading_timestamp)
);
```

For these tables:
- No range constraints are applied
- Conflict detection uses the full composite key
- Resolution follows configured policy (last-writer-wins, priority, etc.)

#### Detection During Setup

steep-repl analyzes table structures during `steep-repl add-table`:

```bash
steep-repl add-table sensor_readings

[INFO] Table: sensor_readings
[INFO] Primary key: (device_id, reading_timestamp)
[INFO] No FK to range-managed parent detected
[WARN] This table will use conflict resolution only (no range partitioning)
[INFO] Recommended: Use last-writer-wins or priority-based resolution

Continue? [y/N]:
```

#### Table Classification Summary

| Table Structure | Range Management | Conflict Handling |
|----------------|------------------|-------------------|
| Single-column SERIAL/IDENTITY PK | ✓ CHECK constraint | Rare (range violation only) |
| Composite PK with FK to range-managed parent | ✓ Inherited from parent | Rare (follows parent) |
| Composite PK without FK to parent | ✗ None | Standard resolution policies |
| No PK (REPLICA IDENTITY FULL) | ✗ None | Standard resolution policies |

---

## 6. Node Initialization and Snapshots

### 6.1 Overview

Before replication can begin, nodes must be synchronized to a common baseline. This can happen through:

1. **Snapshot initialization** - Automated, system-managed
2. **Manual initialization from backup** - User-provided pg_dump/pg_basebackup
3. **Reinitialization** - Recovering a diverged or corrupted node

### 6.2 Two-Phase Snapshot Initialization

Steep separates snapshot initialization into two distinct phases for better control over large database initialization:

```
┌─────────────────────────────────────────────────────────────────┐
│            Two-Phase Snapshot Initialization                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  PHASE 1: SNAPSHOT GENERATION (Source Node)                     │
│  ─────────────────────────────────────────                      │
│     ┌──────────┐                                                │
│     │ Source   │ ──► Export to file/storage                    │
│     │ (pub)    │     (pg_dump, COPY, or basebackup)            │
│     └──────────┘                                                │
│                                                                 │
│     • Create replication slot to capture WAL from this point   │
│     • Export schema (DDL) separately from data                  │
│     • Export data with parallel COPY (PG18 feature)            │
│     • Record LSN at snapshot time                               │
│     • Snapshot stored locally, S3, NFS, or transferred         │
│                                                                 │
│  ═══════════════════════════════════════════════════════════   │
│                                                                 │
│  PHASE 2: SNAPSHOT APPLICATION (Target Node)                    │
│  ────────────────────────────────────────────                   │
│                       ┌──────────┐                              │
│     Import from ──►   │  Target  │                              │
│     file/storage      │  (sub)   │                              │
│                       └──────────┘                              │
│                                                                 │
│     • Apply schema first (CREATE EXTENSION, tables, etc.)       │
│     • Load data with parallel COPY FROM (PG18 feature)         │
│     • Sync sequences (PG18 REFRESH SEQUENCES)                   │
│     • Install steep_repl metadata and range constraints        │
│     • Create subscription starting from captured LSN            │
│     • Apply queued WAL changes since snapshot                   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Why Two Phases?

| Benefit | Description |
|---------|-------------|
| **Decoupled timing** | Generate snapshot during low-traffic window, apply later |
| **Portable snapshots** | Export once, apply to multiple target nodes |
| **Resumable application** | If application fails, retry without regenerating |
| **Network efficiency** | Compress and transfer snapshot separately |
| **Parallel COPY** | PG18 parallelizes both generation and application |

#### Phase 1: Snapshot Generation

```bash
# Generate snapshot with replication slot
steep-repl snapshot generate \
    --source node_a \
    --output /snapshots/2025-12-03/ \
    --parallel 4 \
    --compress gzip

# Output structure:
# /snapshots/2025-12-03/
# ├── manifest.json           # LSN, tables, checksums
# ├── schema.sql              # DDL statements
# ├── data/
# │   ├── customers.csv.gz    # Table data (parallel COPY)
# │   ├── orders.csv.gz
# │   └── ...
# └── sequences.json          # Sequence values at snapshot time
```

```
┌─ Snapshot Generation ─────────────────────────────────────────────┐
│                                                                   │
│  Generating snapshot from node_a                                 │
│  Slot: steep_snapshot_20251203_143022                            │
│  LSN: 0/1A234B00                                                 │
│                                                                   │
│  Phase: Data Export                                              │
│  Overall: ████████████░░░░░░░░ 62%  (14 of 23 tables)           │
│                                                                   │
│  Current: orders (1.2GB) - 4 parallel workers                    │
│           ██████████████░░░░░░ 71%  168,000 rows/sec             │
│           ETA: 52s                                               │
│                                                                   │
│  Output: /snapshots/2025-12-03/ (3.2GB written)                  │
│                                                                   │
│  [C]ancel  [P]ause                                               │
└───────────────────────────────────────────────────────────────────┘
```

#### Phase 2: Snapshot Application

```bash
# Apply snapshot to target node
steep-repl snapshot apply \
    --target node_b \
    --input /snapshots/2025-12-03/ \
    --parallel 4 \
    --source node_a  # For subscription creation

# Or apply from remote storage
steep-repl snapshot apply \
    --target node_b \
    --input s3://steep-snapshots/2025-12-03/ \
    --parallel 8
```

```
┌─ Snapshot Application ────────────────────────────────────────────┐
│                                                                   │
│  Applying snapshot to node_b                                     │
│  Source LSN: 0/1A234B00                                          │
│  Current LSN: 0/1A236F80 (+10KB WAL to apply)                    │
│                                                                   │
│  Phase: Data Import (3 of 4)                                     │
│  ├─ [✓] Schema applied (23 tables, 45 indexes)                   │
│  ├─ [✓] Extensions installed (steep_repl)                        │
│  ├─ [▶] Data loading...                                          │
│  └─ [ ] WAL catch-up                                             │
│                                                                   │
│  Current: orders (1.2GB) - 4 parallel workers                    │
│           ██████████████░░░░░░ 71%  252,000 rows/sec             │
│           ETA: 35s                                               │
│                                                                   │
│  [C]ancel                                                         │
└───────────────────────────────────────────────────────────────────┘
```

#### Configuration

```yaml
replication:
  initialization:
    method: two_phase            # two_phase | direct | manual

    # Phase 1: Generation
    generation:
      parallel_workers: 4        # PG18 parallel COPY workers
      compression: gzip          # none | gzip | lz4 | zstd
      output_format: directory   # directory | tar | custom
      checksum: sha256           # Verify data integrity

    # Phase 2: Application
    application:
      parallel_workers: 4        # PG18 parallel COPY FROM workers
      verify_checksums: true     # Validate before applying
      sequence_sync: auto        # Use PG18 REFRESH SEQUENCES

    # Shared settings
    snapshot_timeout: 24h        # Max time for either phase
    large_table_threshold: 10GB  # Tables above this get special handling

    # Storage options
    storage:
      type: local               # local | s3 | gcs | azure | nfs
      path: /var/steep/snapshots
      # s3:
      #   bucket: steep-snapshots
      #   prefix: production/
```

#### Direct Mode (Single-Phase, Smaller Databases)

For databases under 100GB, a simpler direct mode combines both phases:

```bash
# Direct initialization (combines both phases)
steep-repl init node_b --from node_a --method direct

# Equivalent to:
# steep-repl snapshot generate --source node_a --output /tmp/... && \
# steep-repl snapshot apply --target node_b --input /tmp/... && \
# rm -rf /tmp/...
```

#### Progress Tracking in Steep UI

```
┌─ Node Initialization ─────────────────────────────────────────────┐
│                                                                   │
│  Initializing node_b from node_a (Two-Phase)                     │
│                                                                   │
│  [✓] Phase 1: Snapshot Generated                                 │
│      Location: /snapshots/2025-12-03/                            │
│      Size: 4.2GB (compressed)                                    │
│      LSN: 0/1A234B00                                             │
│                                                                   │
│  [▶] Phase 2: Applying Snapshot                                  │
│      Overall: ████████████░░░░░░░░ 62%  (14 of 23 tables)       │
│                                                                   │
│      Current: orders (1.2GB) - 4 workers                         │
│               ██████████████░░░░░░ 71%  252,000 rows/sec         │
│               ETA: 35s                                           │
│                                                                   │
│  Completed:                                                       │
│    ✓ customers (245MB) - 58s                                     │
│    ✓ products (89MB) - 12s                                       │
│    ✓ categories (1.2MB) - <1s                                    │
│                                                                   │
│  Pending: line_items, inventory, audit_log, ...                  │
│                                                                   │
│  [C]ancel initialization                                         │
└───────────────────────────────────────────────────────────────────┘
```

### 6.3 Manual Initialization from Backup

For large databases where snapshot is impractical (multi-TB), users can initialize from their own backups:

```
┌─────────────────────────────────────────────────────────────────┐
│                  Manual Initialization Flow                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  1. User creates backup with replication slot:                  │
│     pg_basebackup -D /backup -S steep_init_slot -X stream       │
│     -- OR --                                                    │
│     pg_dump -Fc -f backup.dump mydb                             │
│                                                                 │
│  2. User restores to target node:                               │
│     pg_restore -d mydb backup.dump                              │
│                                                                 │
│  3. User tells steep-repl to complete setup:                    │
│     steep-repl init complete --node node_b \                   │
│         --source-lsn 0/1234ABCD \                              │
│         --backup-time "2025-12-03 14:30:00"                    │
│                                                                 │
│  4. steep-repl:                                                 │
│     • Verifies schema matches source                            │
│     • Installs steep_repl extension and metadata               │
│     • Allocates identity ranges                                 │
│     • Creates subscription starting from LSN                    │
│     • Applies any changes since backup                          │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### CLI Commands

```bash
# Option 1: Automatic snapshot (small/medium databases)
steep-repl init node_b --from node_a --method snapshot

# Option 2: Manual from backup (large databases)
# Step 1: On source, create consistent backup
steep-repl init prepare --node node_a --slot steep_init_slot

# Step 2: User performs backup/restore (their tooling)
pg_basebackup -D /backup -S steep_init_slot ...
# ... restore on target ...

# Step 3: Complete initialization
steep-repl init complete --node node_b \
    --source node_a \
    --source-lsn 0/1234ABCD

# Option 3: Initialize from existing replica
steep-repl init from-replica --node node_b --replica-of node_a
```

### 6.4 Reinitialization (Recovery)

When a node falls too far behind or becomes corrupted:

| Scenario | Detection | Recovery |
|----------|-----------|----------|
| Replication slot dropped | Subscription fails | Reinitialize from snapshot or backup |
| WAL no longer available | `pg_stat_replication` shows slot behind | Reinitialize |
| Data corruption detected | Conflict resolution finds impossible state | Reinitialize affected tables |
| Node offline too long | Configurable threshold (e.g., 7 days) | Reinitialize |

#### Partial Reinitialization

For large databases, reinitialize only affected tables:

```bash
# Reinitialize specific tables
steep-repl reinit --node node_b --tables orders,line_items

# Reinitialize entire schema
steep-repl reinit --node node_b --schema sales

# Full reinitialization
steep-repl reinit --node node_b --full
```

#### Reinitialization Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    Reinitialization Flow                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  1. Pause replication to affected node                          │
│  2. Disable identity range constraints on target tables         │
│  3. TRUNCATE affected tables on target (with CASCADE option)    │
│  4. Copy data from source (snapshot or streaming)               │
│  5. Re-establish identity ranges                                │
│  6. Apply WAL changes accumulated during reinit                 │
│  7. Resume normal replication                                   │
│                                                                 │
│  Note: Other tables continue replicating during partial reinit  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 6.5 Schema Synchronization

Before data can flow, schemas must match:

```sql
-- steep_repl.schema_fingerprint()
-- Returns hash of table structure for comparison

SELECT steep_repl.compare_schemas('node_a', 'node_b');

-- Returns:
-- table_name | status    | difference
-- -----------+-----------+------------------------------------------
-- orders     | MATCH     |
-- customers  | MISMATCH  | Column 'loyalty_tier' missing on node_b
-- products   | MATCH     |
```

#### Schema Sync Options

```yaml
replication:
  initialization:
    schema_sync:
      mode: strict              # strict | auto | manual

      # strict: Fail if schemas don't match
      # auto:   Apply DDL to make schemas match (requires DDL replication)
      # manual: Warn but allow user to fix
```

### 6.6 Initialization States

```
┌─────────────────────────────────────────────────────────────────┐
│                    Node Initialization States                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  UNINITIALIZED ──► PREPARING ──► COPYING ──► CATCHING_UP       │
│        │               │            │             │             │
│        │               │            │             ▼             │
│        │               │            │         SYNCHRONIZED      │
│        │               │            │             │             │
│        ▼               ▼            ▼             ▼             │
│      FAILED ◄──────────────────────────────── DIVERGED         │
│        │                                          │             │
│        └──────────► REINITIALIZING ◄──────────────┘             │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘

States:
  UNINITIALIZED  - Node registered but no data
  PREPARING      - Creating slots, validating schemas
  COPYING        - Snapshot/backup restore in progress
  CATCHING_UP    - Applying WAL changes since snapshot
  SYNCHRONIZED   - Normal replication active
  DIVERGED       - Node detected as out of sync
  FAILED         - Initialization failed (human intervention needed)
  REINITIALIZING - Recovery in progress
```

### 6.7 Monitoring in Steep UI

```
┌─ Nodes ───────────────────────────────────────────────────────────┐
│                                                                   │
│  Node        State          Lag         Init Progress   Health   │
│  ──────────────────────────────────────────────────────────────── │
│  node_a      SYNCHRONIZED   -           -               ● OK     │
│  node_b      SYNCHRONIZED   1.2s        -               ● OK     │
│  node_c      COPYING        -           67% (ETA 14m)   ◐ INIT   │
│  node_d      DIVERGED       -           -               ● ERROR  │
│                                                                   │
│  [I]nitialize  [R]einitialize  [P]ause  [D]etails               │
└───────────────────────────────────────────────────────────────────┘
```

### 6.8 Initial Sync with Existing Data on Both Nodes

When setting up bidirectional replication between nodes that **both already contain data**, special handling is required to avoid conflicts during initial sync.

#### Scenario

```
┌──────────────────┐         ┌──────────────────┐
│      Node A      │         │      Node B      │
│  (10,000 rows)   │◄───────►│  (8,000 rows)    │
│                  │         │  (some overlap)  │
└──────────────────┘         └──────────────────┘
```

Both nodes have existing data, potentially with overlapping primary keys or conflicting row values.

#### Setup Procedure

**Step 1: Quiesce Writes**

Stop application writes on both nodes before setup:

```bash
steep-repl init --mode=bidirectional-merge --quiesce-writes
[INFO] Pausing application writes on Node A...
[INFO] Pausing application writes on Node B...
[INFO] Waiting for in-flight transactions to complete...
```

**Step 2: Analyze Data Overlap**

steep-repl analyzes both nodes to identify:
- Matching rows (same PK, same data) - no action needed
- Conflicting rows (same PK, different data) - need resolution
- Unique rows (PK exists on one node only) - need replication

```bash
steep-repl analyze-overlap --tables=orders,customers

[INFO] Analyzing data overlap between nodes...

Table: orders
├── Matching rows:        5,234
├── Conflicting rows:       847  ⚠
└── Unique to Node A:     4,766
└── Unique to Node B:     2,153

Table: customers
├── Matching rows:        2,891
├── Conflicting rows:       156  ⚠
└── Unique to Node A:     1,109
└── Unique to Node B:       544
```

**Step 3: Resolve Pre-Existing Conflicts**

Before enabling bidirectional replication, resolve conflicts using one of:

```bash
# Option 1: Node A wins (use Node A's data for conflicts)
steep-repl resolve-overlap --strategy=prefer-node-a

# Option 2: Node B wins (use Node B's data for conflicts)
steep-repl resolve-overlap --strategy=prefer-node-b

# Option 3: Timestamp-based (use most recently modified)
steep-repl resolve-overlap --strategy=last-modified

# Option 4: Manual review (interactive)
steep-repl resolve-overlap --strategy=manual
```

**Step 4: Assign Identity Ranges**

After conflict resolution, assign non-overlapping ranges based on existing data:

```bash
steep-repl assign-ranges --analyze-existing

[INFO] Analyzing existing ID distribution...
[INFO] orders: Node A max_id=15234, Node B max_id=12847
[INFO] Assigning ranges:
       Node A: 1-20000 (existing), 40001-50000 (new inserts)
       Node B: 20001-40000 (new inserts)
```

**Step 5: Enable Bidirectional Replication**

```bash
steep-repl enable --skip-initial-sync

[INFO] Skipping initial data sync (data already reconciled)
[INFO] Creating subscriptions with copy_data = false...
[INFO] Bidirectional replication enabled
[INFO] Resuming application writes...
```

#### Data Reconciliation Report

steep-repl generates a reconciliation report for audit:

```bash
steep-repl reconciliation-report --output=reconciliation_2024-01-15.json
```

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "strategy": "prefer-node-a",
  "tables": {
    "orders": {
      "conflicts_resolved": 847,
      "resolution_breakdown": {
        "node_a_selected": 847,
        "node_b_selected": 0
      }
    }
  }
}
```

---

## 7. Filtering and Selective Replication

Leverage PostgreSQL's native publication filtering as much as possible.

**Note**: Row-level and column-level filtering require **PostgreSQL 15+**. Table-level filtering works on all supported versions.

### 7.1 Table-Level Filtering

```sql
-- PostgreSQL native: Specify tables in publication
CREATE PUBLICATION steep_pub FOR TABLE orders, customers, products;

-- Or exclude specific tables
CREATE PUBLICATION steep_pub FOR ALL TABLES
    WHERE (schemaname NOT IN ('audit', 'temp', 'staging'));
```

#### Configuration

```yaml
replication:
  filtering:
    # Include only these tables (whitelist)
    include_tables:
      - public.orders
      - public.customers
      - sales.*              # All tables in sales schema

    # Or exclude these tables (blacklist)
    exclude_tables:
      - public.audit_log
      - public.temp_*        # Wildcard support
      - staging.*

    # Exclude entire schemas
    exclude_schemas:
      - pg_temp
      - information_schema
```

### 6.2 Row-Level Filtering

PostgreSQL 15+ supports row filters on publications:

```sql
-- Only replicate orders from specific regions
CREATE PUBLICATION steep_pub FOR TABLE orders
    WHERE (region IN ('US', 'EU'));

-- Only replicate active customers
CREATE PUBLICATION steep_pub FOR TABLE customers
    WHERE (status = 'active');
```

#### Configuration

```yaml
replication:
  filtering:
    row_filters:
      orders:
        where: "region IN ('US', 'EU')"
      customers:
        where: "status = 'active'"
```

**Caution**: Row filters can cause conflicts if a row is updated to no longer match the filter on one node but still matches on another.

#### Row Filter Limitations

| Limitation | Details |
|------------|---------|
| **Filter on replica key columns** | If you filter on a column that gets updated, the row may "disappear" from the subscriber |
| **No filter on partitioned table parent** | Must apply filters to individual partitions |
| **Initial sync respects filter** | Only matching rows copied during `copy_data = true` |
| **UPDATE changes filter match** | If an UPDATE causes a row to no longer match the filter, it's effectively deleted on subscriber |
| **Bidirectional complexity** | Row filtered on A but not B can cause INSERT-INSERT conflicts if created on B |

### 6.3 Column Filtering

PostgreSQL 15+ supports column lists:

```sql
-- Exclude sensitive columns
CREATE PUBLICATION steep_pub FOR TABLE customers (id, name, email, created_at);
-- Excludes: ssn, credit_card, password_hash
```

#### Configuration

```yaml
replication:
  filtering:
    column_filters:
      customers:
        include: [id, name, email, created_at]
        # OR
        exclude: [ssn, credit_card, password_hash]
```

#### Column Filter Limitations

| Limitation | Details |
|------------|---------|
| **Must include replica identity columns** | Primary key or REPLICA IDENTITY columns cannot be excluded |
| **TOAST columns** | Large values may behave unexpectedly if column is filtered |
| **Schema changes** | Adding a column requires updating the publication column list |
| **Generated columns** | Cannot be included in column lists |

### 6.4 Filtering in Steep TUI

```
┌─ Replication Filters ─────────────────────────────────────────────┐
│                                                                   │
│  Tables: 23 included, 5 excluded                                 │
│                                                                   │
│  Table              Filter              Columns                   │
│  ──────────────────────────────────────────────────────────────── │
│  orders             region IN (US,EU)   all                      │
│  customers          status = 'active'   4 of 12 (excl: ssn,...)  │
│  products           -                   all                      │
│  audit_log          EXCLUDED            -                        │
│                                                                   │
│  [E]dit filters  [V]alidate  [A]pply changes                    │
└───────────────────────────────────────────────────────────────────┘
```

---

## 8. Monitoring and Health Checks

### 8.1 Replication Health Metrics

Extend Steep's existing replication monitoring for bidirectional:

| Metric | Source | Alert Threshold |
|--------|--------|-----------------|
| Replication lag (bytes) | `pg_stat_replication` | Configurable (default: 100MB) |
| Replication lag (time) | `pg_stat_replication` | Configurable (default: 60s) |
| Conflict rate | `steep_repl.conflict_log` | > 10/minute |
| Pending conflicts | `steep_repl.conflict_log` | > 0 (for manual strategy) |
| DDL queue depth | `steep_repl.ddl_queue` | > 5 pending |
| Range utilization | `steep_repl.identity_ranges` | > 80% consumed |
| Node health | steep-repl daemon | Any node not SYNCHRONIZED |
| Slot lag | `pg_replication_slots` | > wal_keep_size |

### 7.2 Health Check Endpoints

steep-repl daemon exposes health checks:

```bash
# CLI
steep-repl health
steep-repl health --json

# HTTP endpoint (optional, for load balancers)
curl http://localhost:5434/health
```

```json
{
  "status": "healthy",
  "node": "node_a",
  "checks": {
    "postgresql": "ok",
    "extension": "ok",
    "peer_connectivity": "ok",
    "replication_lag": {"status": "ok", "lag_bytes": 1024},
    "conflict_rate": {"status": "ok", "per_minute": 0.5},
    "range_utilization": {"status": "warning", "tables_above_80pct": ["orders"]}
  },
  "peers": {
    "node_b": {"status": "healthy", "last_seen": "2025-12-03T14:32:00Z"}
  }
}
```

### 7.3 Alerts Integration

Leverage Steep's existing alert system (Feature 012):

```yaml
alerts:
  rules:
    # Bidirectional replication alerts
    - name: replication_lag_critical
      metric: replication_lag_bytes
      operator: ">"
      warning: 52428800      # 50MB
      critical: 104857600    # 100MB
      enabled: true

    - name: high_conflict_rate
      metric: steep_repl_conflicts_per_minute
      operator: ">"
      warning: 5
      critical: 20
      enabled: true

    - name: pending_manual_conflicts
      metric: steep_repl_pending_conflicts
      operator: ">"
      warning: 1
      critical: 10
      enabled: true

    - name: range_exhaustion_warning
      metric: steep_repl_range_utilization_pct
      operator: ">"
      warning: 80
      critical: 95
      enabled: true

    - name: node_not_synchronized
      metric: steep_repl_nodes_not_synced
      operator: ">"
      warning: 0
      critical: 0
      enabled: true
```

### 7.4 Dashboard Integration

Add bidirectional replication panel to Steep dashboard:

```
┌─ Bidirectional Replication ───────────────────────────────────────┐
│                                                                   │
│  Topology: node_a ◄──────► node_b                                │
│  Status: SYNCHRONIZED                                            │
│                                                                   │
│  Lag (A→B): 1.2s  ████████░░ 1.2MB                              │
│  Lag (B→A): 0.8s  ██████░░░░ 0.8MB                              │
│                                                                   │
│  Conflicts (24h):  3 resolved, 0 pending                        │
│  DDL Queue:        0 pending                                     │
│  Ranges:           2 tables > 80%  ⚠                            │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

### 7.5 steep-repl Daemon Metrics

The daemon collects and exposes metrics for Steep:

```go
// internal/repl/metrics/collector.go
type ReplMetrics struct {
    // Lag
    LagBytesAtoB      int64
    LagBytesBtoA      int64
    LagSecondsAtoB    float64
    LagSecondsBtoA    float64

    // Conflicts
    ConflictsTotal    int64
    ConflictsPending  int64
    ConflictsPerMin   float64

    // DDL
    DDLQueueDepth     int64
    DDLAppliedTotal   int64

    // Ranges
    RangesAbove80Pct  []string
    RangesAbove95Pct  []string

    // Nodes
    NodesTotal        int
    NodesSynchronized int
    NodesInitializing int
    NodesDiverged     int
}
```

### 7.6 Logging

Structured logging for troubleshooting:

```json
{"level":"info","ts":"2025-12-03T14:32:00Z","msg":"conflict detected","table":"orders","pk":"50432","type":"UPDATE_UPDATE","resolution":"last_write_wins","winner":"node_b"}
{"level":"warn","ts":"2025-12-03T14:32:01Z","msg":"range threshold exceeded","table":"line_items","utilization":87.5,"action":"pre_allocating_next_range"}
{"level":"error","ts":"2025-12-03T14:32:02Z","msg":"peer unreachable","peer":"node_b","last_seen":"2025-12-03T14:30:00Z","action":"marking_unhealthy"}
```

---

## 9. Conflict Detection and Resolution

### 9.1 Conflict Types

| Type | Description | Example |
|------|-------------|---------|
| INSERT-INSERT | Same PK inserted on both nodes | Order #1000 created on A and B simultaneously |
| UPDATE-UPDATE | Same row updated on both nodes | Customer address changed on A and B |
| UPDATE-DELETE | Row updated on one node, deleted on other | Order modified on A, cancelled on B |
| DELETE-DELETE | Same row deleted on both nodes | Not really a conflict, both agree |

### 6.2 Detection

PostgreSQL 18 provides conflict logging:

```sql
-- Built-in conflict stats
SELECT subname, conflict_count, last_conflict_time
FROM pg_stat_subscription_stats;
```

The steep_repl extension adds detailed conflict metadata:

```sql
-- steep_repl.conflicts table
CREATE TABLE steep_repl.conflict_log (
    id              BIGSERIAL PRIMARY KEY,
    detected_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    subscription    TEXT NOT NULL,
    table_schema    TEXT NOT NULL,
    table_name      TEXT NOT NULL,
    conflict_type   TEXT NOT NULL,  -- INSERT_INSERT, UPDATE_UPDATE, etc.
    local_tuple     JSONB,
    remote_tuple    JSONB,
    local_origin    TEXT,
    remote_origin   TEXT,
    local_xact_ts   TIMESTAMPTZ,
    remote_xact_ts  TIMESTAMPTZ,
    resolution      TEXT,           -- PENDING, APPLIED_REMOTE, KEPT_LOCAL, MERGED
    resolved_at     TIMESTAMPTZ,
    resolved_by     TEXT            -- 'policy:last_write_wins', 'manual:user@host'
);
```

### 6.3 Resolution Strategies

#### Built-in Strategies

| Strategy | Description | Use Case |
|----------|-------------|----------|
| `last_write_wins` | Higher timestamp wins | General default |
| `first_write_wins` | Lower timestamp wins | Rare, "first come first served" |
| `node_priority` | Designated node always wins | Primary/secondary with writes allowed |
| `keep_local` | Local always wins | Edge nodes that are authoritative |
| `apply_remote` | Remote always wins | Central node is authoritative |
| `manual` | Queue for human resolution | Critical data requiring review |

#### Configuration

```yaml
replication:
  conflicts:
    default_strategy: last_write_wins

    # Per-table overrides
    tables:
      orders:
        strategy: manual           # Orders require human review

      inventory:
        strategy: last_write_wins

      customer_preferences:
        strategy: node_priority
        priority:
          - node_a                 # Primary wins
          - node_b
```

### 6.4 Resolution Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    Conflict Resolution Flow                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────┐                                              │
│  │ PG18 Detects │                                              │
│  │ Conflict     │                                              │
│  └──────┬───────┘                                              │
│         │                                                       │
│         ▼                                                       │
│  ┌──────────────┐     ┌──────────────────────────────────┐    │
│  │ steep_repl   │────►│ Log to steep_repl.conflict_log   │    │
│  │ Extension    │     └──────────────────────────────────┘    │
│  └──────┬───────┘                                              │
│         │ Notify                                                │
│         ▼                                                       │
│  ┌──────────────┐     ┌──────────────────────────────────┐    │
│  │ steep-repl   │────►│ Look up resolution policy        │    │
│  │ Daemon       │     └──────────────────────────────────┘    │
│  └──────┬───────┘                                              │
│         │                                                       │
│         ▼                                                       │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ Strategy = manual?                                        │  │
│  │   YES → Queue for UI, mark PENDING                       │  │
│  │   NO  → Apply strategy, mark resolved                    │  │
│  └──────────────────────────────────────────────────────────┘  │
│         │                                                       │
│         ▼                                                       │
│  ┌──────────────┐                                              │
│  │ Apply winner │                                              │
│  │ to local DB  │                                              │
│  └──────────────┘                                              │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 6.5 Manual Resolution UI

```
┌─ Pending Conflicts ───────────────────────────────────────────────┐
│                                                                   │
│  #1 UPDATE-UPDATE on orders.order_id = 50432                     │
│  ────────────────────────────────────────────────────────────────│
│  Detected: 2025-12-03 14:32:15                                   │
│                                                                   │
│  LOCAL (node_a, 14:32:10)        REMOTE (node_b, 14:32:12)      │
│  ┌─────────────────────────┐    ┌─────────────────────────┐     │
│  │ status: processing      │    │ status: shipped         │     │
│  │ updated_by: alice       │    │ updated_by: bob         │     │
│  │ notes: "checking..."    │    │ notes: "out for deliv"  │     │
│  └─────────────────────────┘    └─────────────────────────┘     │
│                                                                   │
│  [L]ocal wins  [R]emote wins  [M]erge  [S]kip                   │
│                                                                   │
│  ▼ 3 more conflicts pending                                      │
└───────────────────────────────────────────────────────────────────┘
```

### 9.6 Application Trigger Behavior

Tables with application-defined triggers require special consideration in bidirectional replication.

#### Trigger Firing on Replicated Changes

By default, PostgreSQL **does not fire triggers** for changes applied via logical replication. This is intentional to prevent:
- Cascading trigger effects across nodes
- Duplicate side effects (e.g., audit log entries on both nodes)
- Infinite loops (trigger on A → replicate to B → trigger on B → replicate to A)

```sql
-- This trigger fires for local INSERTs but NOT for replicated rows
CREATE TRIGGER audit_orders
    AFTER INSERT ON orders
    FOR EACH ROW EXECUTE FUNCTION log_order_change();
```

#### When Triggers Need to Fire

Some triggers must execute on replicated data:

| Trigger Type | Should Fire? | Reason |
|--------------|--------------|--------|
| Audit logging | ❌ No | Would duplicate entries |
| Materialized view refresh | ✓ Yes | Local view needs updating |
| Derived column calculation | ✓ Yes | Computed fields need values |
| Cache invalidation | ✓ Yes | Local cache must be cleared |
| External notification | ❌ No | Would send duplicate alerts |

#### Enabling Triggers for Replicated Data

Use `ALTER TABLE ... ENABLE ALWAYS TRIGGER` for triggers that must fire on replicated changes:

```sql
-- Trigger that SHOULD fire on replicated data
CREATE TRIGGER update_order_total
    AFTER INSERT OR UPDATE ON order_lines
    FOR EACH ROW EXECUTE FUNCTION recalculate_order_total();

-- Enable for ALL changes including replication
ALTER TABLE order_lines ENABLE ALWAYS TRIGGER update_order_total;
```

#### Configuration in steep-repl

steep-repl provides commands to manage trigger behavior:

```bash
# List triggers and their replica status
steep-repl triggers --table=orders

Table: orders
├── audit_orders         REPLICA (fires on replicated: NO)
├── update_totals        ALWAYS  (fires on replicated: YES)
└── send_notification    ORIGIN  (fires on replicated: NO)

# Enable a trigger for replicated data
steep-repl trigger enable-always --table=orders --trigger=update_cache

# Revert to default behavior
steep-repl trigger enable-replica --table=orders --trigger=update_cache
```

#### Trigger Audit Report

steep-repl can audit triggers before enabling bidirectional replication:

```bash
steep-repl audit-triggers

[WARN] Found 3 triggers that may need review:

orders.audit_orders (AFTER INSERT/UPDATE/DELETE)
  Status: REPLICA (default - won't fire on replicated data)
  Recommendation: Keep as REPLICA to avoid duplicate audit entries

order_lines.recalc_total (AFTER INSERT/UPDATE)
  Status: REPLICA (default - won't fire on replicated data)
  Recommendation: Change to ALWAYS if order totals need updating

inventory.send_low_stock_alert (AFTER UPDATE)
  Status: REPLICA (default - won't fire on replicated data)
  Recommendation: Keep as REPLICA to avoid duplicate alerts
```

---

## 10. DDL Replication

### 10.1 Overview

DDL (Data Definition Language) changes must be coordinated across nodes to prevent schema drift. The steep_repl extension captures DDL via PostgreSQL's ProcessUtility hook.

### 9.2 Captured DDL

| DDL Type | Captured | Notes |
|----------|----------|-------|
| CREATE TABLE | Yes | Including constraints, defaults |
| DROP TABLE | Yes | With confirmation option |
| ALTER TABLE ADD COLUMN | Yes | |
| ALTER TABLE DROP COLUMN | Yes | Requires quorum or confirmation |
| ALTER TABLE ALTER COLUMN | Yes | Type changes need special handling |
| CREATE INDEX | Yes | CONCURRENTLY supported |
| DROP INDEX | Yes | |
| CREATE/DROP FUNCTION | Configurable | May have node-specific code |
| CREATE/DROP TRIGGER | Configurable | Replication triggers excluded |
| TRUNCATE | Yes | Via existing queue mechanism |

### 9.3 DDL Queue

```sql
-- steep_repl.ddl_queue table
CREATE TABLE steep_repl.ddl_queue (
    id              BIGSERIAL PRIMARY KEY,
    captured_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    origin_node     TEXT NOT NULL,
    ddl_command     TEXT NOT NULL,
    object_type     TEXT NOT NULL,       -- TABLE, INDEX, FUNCTION, etc.
    object_schema   TEXT,
    object_name     TEXT,
    status          TEXT NOT NULL,       -- PENDING, APPROVED, APPLIED, REJECTED
    applied_at      TIMESTAMPTZ,
    applied_by      TEXT,
    error_message   TEXT
);
```

### 9.4 ProcessUtility Hook (pgrx)

```rust
use pgrx::prelude::*;

static mut PREV_PROCESS_UTILITY_HOOK: pg_sys::ProcessUtility_hook_type = None;

#[pg_guard]
pub unsafe extern "C" fn steep_process_utility_hook(
    pstmt: *mut pg_sys::PlannedStmt,
    query_string: *const std::os::raw::c_char,
    read_only_tree: bool,
    context: pg_sys::ProcessUtilityContext,
    params: pg_sys::ParamListInfo,
    query_env: *mut pg_sys::QueryEnvironment,
    dest: *mut pg_sys::DestReceiver,
    qc: *mut pg_sys::QueryCompletion,
) {
    // Skip if this is replicated DDL (prevent loops)
    if is_replicated_ddl_context() {
        call_prev_hook(...);
        return;
    }

    // Capture DDL before execution
    let ddl_info = capture_ddl_info(pstmt, query_string);

    // Execute original DDL
    call_prev_hook(...);

    // If successful, queue for replication
    if let Some(info) = ddl_info {
        queue_ddl_for_replication(info);
        notify_daemon();
    }
}
```

### 9.5 DDL Approval Workflow

For destructive DDL (DROP, ALTER DROP COLUMN), optional approval:

```yaml
replication:
  ddl:
    auto_apply:
      - CREATE TABLE
      - ALTER TABLE ADD COLUMN
      - CREATE INDEX
      - DROP INDEX

    require_approval:
      - DROP TABLE
      - ALTER TABLE DROP COLUMN
      - TRUNCATE

    exclude:
      - CREATE FUNCTION    # Node-specific functions
      - CREATE TRIGGER     # Managed by replication
```

### 9.6 DDL Queue UI

```
┌─ DDL Queue ───────────────────────────────────────────────────────┐
│                                                                   │
│  Status: 2 pending, 15 applied today, 0 rejected                 │
│                                                                   │
│  PENDING                                                          │
│  ──────────────────────────────────────────────────────────────── │
│  #42 DROP TABLE legacy_audit (from node_b, 14:20:05)             │
│      ⚠ Destructive operation - requires approval                 │
│                                                                   │
│  #41 ALTER TABLE orders DROP COLUMN deprecated_field (node_a)    │
│      ⚠ Destructive operation - requires approval                 │
│                                                                   │
│  RECENTLY APPLIED                                                 │
│  ──────────────────────────────────────────────────────────────── │
│  #40 CREATE INDEX idx_orders_date ON orders(created_at) ✓        │
│  #39 ALTER TABLE customers ADD COLUMN loyalty_tier TEXT ✓        │
│                                                                   │
│  [A]pprove  [R]eject  [V]iew full DDL  [D]iff schemas            │
└───────────────────────────────────────────────────────────────────┘
```

---

## 11. Topology Management

### 11.1 Supported Topologies

```
STAR (Hub-Spoke)              MESH (Peer-to-Peer)

      ┌───┐                   ┌───┐───────┌───┐
      │ A │ (Hub)             │ A │       │ B │
      └─┬─┘                   └─┬─┘───────└─┬─┘
   ┌────┼────┐                  │    ╲ ╱    │
   │    │    │                  │     ╳     │
 ┌─┴─┐┌─┴─┐┌─┴─┐              ┌─┴─┐  ╱ ╲  ┌─┴─┐
 │ B ││ C ││ D │ (Spokes)     │ C │───────│ D │
 └───┘└───┘└───┘              └───┘       └───┘

- Central coordinator         - Any node can coordinate
- Simpler conflict resolution - More complex conflicts
- Single point of failure     - More resilient
```

### 10.2 Node Configuration

```yaml
replication:
  topology:
    mode: mesh                 # star | mesh
    this_node:
      name: node_a
      priority: 100            # Higher = wins more conflicts

    nodes:
      - name: node_b
        host: node-b.example.com
        port: 5432
        priority: 90

      - name: node_c
        host: node-c.example.com
        port: 5432
        priority: 80
```

### 10.3 Coordinator Election (Mesh Mode)

In mesh mode, a coordinator must be elected for:
- Range allocation
- DDL approval aggregation
- Conflict arbitration when timestamps are equal

Election uses node priority as tie-breaker:

```
1. All nodes start as followers
2. Highest priority node with quorum becomes coordinator
3. If coordinator fails, next highest priority takes over
4. Coordinator state stored in steep_repl.cluster_state
```

---

## 12. steep_repl Extension Schema

### 12.1 Tables

```sql
-- Extension schema
CREATE SCHEMA steep_repl;

-- Cluster and node state
CREATE TABLE steep_repl.nodes (
    node_id         TEXT PRIMARY KEY,
    node_name       TEXT NOT NULL,
    host            TEXT NOT NULL,
    port            INTEGER NOT NULL DEFAULT 5432,
    priority        INTEGER NOT NULL DEFAULT 50,
    is_coordinator  BOOLEAN NOT NULL DEFAULT false,
    last_seen       TIMESTAMPTZ,
    status          TEXT NOT NULL DEFAULT 'unknown'
);

-- Identity range tracking
CREATE TABLE steep_repl.identity_ranges (
    id              BIGSERIAL PRIMARY KEY,
    table_schema    TEXT NOT NULL,
    table_name      TEXT NOT NULL,
    node_id         TEXT NOT NULL REFERENCES steep_repl.nodes(node_id),
    range_start     BIGINT NOT NULL,
    range_end       BIGINT NOT NULL,
    allocated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    status          TEXT NOT NULL DEFAULT 'active',  -- active, exhausted, released
    UNIQUE (table_schema, table_name, node_id, range_start)
);

-- Conflict log (see section 4.2)
CREATE TABLE steep_repl.conflict_log (...);

-- DDL queue (see section 5.3)
CREATE TABLE steep_repl.ddl_queue (...);

-- Coordinator state
CREATE TABLE steep_repl.coordinator_state (
    key             TEXT PRIMARY KEY,
    value           JSONB NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 11.2 Functions

```sql
-- Range allocation
CREATE FUNCTION steep_repl.allocate_range(
    p_table_schema TEXT,
    p_table_name TEXT,
    p_range_size BIGINT DEFAULT 10000
) RETURNS TABLE (range_start BIGINT, range_end BIGINT);

-- Check range consumption
CREATE FUNCTION steep_repl.check_range_threshold(
    p_table_schema TEXT,
    p_table_name TEXT,
    p_threshold_percent INTEGER DEFAULT 80
) RETURNS TABLE (
    current_value BIGINT,
    range_end BIGINT,
    percent_used NUMERIC,
    needs_expansion BOOLEAN
);

-- Apply range constraint
CREATE FUNCTION steep_repl.apply_range_constraint(
    p_table_schema TEXT,
    p_table_name TEXT,
    p_range_start BIGINT,
    p_range_end BIGINT
) RETURNS BOOLEAN;
```

---

## 13. steep-repl Daemon

### 13.1 Overview

The steep-repl daemon is a **separate** Go service dedicated to bidirectional replication coordination. It is distinct from steep-agent and has its own lifecycle.

| Aspect | steep-agent | steep-repl |
|--------|-------------|------------|
| Purpose | Monitoring & metrics | Replication coordination |
| Required for | Background data collection | Bidirectional replication only |
| Runs on | All monitored nodes | Only replication participants |
| Binary | `bin/steep-agent` | `bin/steep-repl` |
| Service name | `steep-agent` | `steep-repl` |
| Default port | N/A (local only) | 5433 (gRPC, configurable) |

### 12.2 Installation

```bash
# Build
make build-repl

# Install as service (separate from steep-agent)
./bin/steep-repl install --user

# Start
./bin/steep-repl start

# Check status
./bin/steep-repl status
```

### 12.3 Components

```go
// cmd/steep-repl/main.go
package main

import (
    "steep/internal/repl/coordinator"
    "steep/internal/repl/conflict"
    "steep/internal/repl/ddl"
    "steep/internal/repl/ranges"
    "steep/internal/repl/topology"
)

func main() {
    cfg := config.Load()

    daemon := &Daemon{
        Topology:    topology.NewManager(cfg),
        Ranges:      ranges.NewCoordinator(cfg),
        Conflicts:   conflict.NewArbitrator(cfg),
        DDL:         ddl.NewCoordinator(cfg),
    }

    daemon.Run()
}
```

### 12.4 Communication

```
┌─────────────────────────────────────────────────────────────────┐
│                    Communication Architecture                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────┐      gRPC (5433)      ┌──────────┐              │
│  │steep-repl│◄────────────────────►│steep-repl│              │
│  │ (node_a) │                       │ (node_b) │              │
│  └────┬─────┘                       └────┬─────┘              │
│       │                                  │                     │
│       │ pgx                              │ pgx                 │
│       ▼                                  ▼                     │
│  ┌──────────┐                       ┌──────────┐              │
│  │PostgreSQL│◄─────Logical Rep─────►│PostgreSQL│              │
│  └──────────┘                       └──────────┘              │
│                                                                 │
│  ┌──────────┐   Named Pipe (Win)    ┌──────────┐              │
│  │Steep TUI │◄──Unix Socket (Lin)──►│steep-repl│              │
│  └──────────┘                       └──────────┘              │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

| Channel | Protocol | Purpose |
|---------|----------|---------|
| Node-to-Node | gRPC (TLS) | Coordinator election, range requests, health checks |
| Daemon-to-PostgreSQL | pgx | Range management, conflict resolution, DDL queue |
| Daemon-to-TUI (Windows) | Named Pipe (`\\.\pipe\steep-repl`) | Status, manual resolution, configuration |
| Daemon-to-TUI (Linux/macOS) | Unix Socket | Status, manual resolution, configuration |
| PostgreSQL-to-PostgreSQL | Native logical replication | Data replication (managed by PostgreSQL) |

---

## 14. Steep TUI Integration

### 14.1 New Views/Tabs

| Location | Addition |
|----------|----------|
| Replication View | New "Bidirectional" tab |
| Replication View | "Conflicts" subtab |
| Replication View | "DDL Queue" subtab |
| Replication View | "Ranges" subtab |
| Dashboard | Conflict count in alert panel |
| Status Bar | Replication health indicator |

### 13.2 Key Bindings (Bidirectional Tab)

| Key | Action |
|-----|--------|
| `Tab` | Cycle subtabs (Overview, Conflicts, DDL, Ranges) |
| `c` | View conflicts |
| `d` | View DDL queue |
| `r` | View identity ranges |
| `L/R` | Local wins / Remote wins (in conflict view) |
| `A` | Approve DDL (in DDL queue) |
| `X` | Reject DDL (in DDL queue) |

---

## 15. Implementation Phases

### Phase 1: Foundation (Weeks 1-2)
- [ ] Set up pgrx development environment
- [ ] Create steep_repl extension skeleton
- [ ] Implement schema (tables, basic functions)
- [ ] Build ProcessUtility hook for DDL capture
- [ ] Create steep-repl daemon skeleton

### Phase 2: Identity Ranges (Weeks 3-4)
- [ ] Implement range allocation logic
- [ ] CHECK constraint management
- [ ] Threshold monitoring
- [ ] Steep UI: Ranges view

### Phase 3: Conflict Handling (Weeks 5-6)
- [ ] Integrate with PG18 conflict logging
- [ ] Implement resolution strategies
- [ ] Manual resolution queue
- [ ] Steep UI: Conflicts view

### Phase 4: DDL Replication (Weeks 7-8)
- [ ] Complete ProcessUtility hook
- [ ] DDL queue and apply mechanism
- [ ] Approval workflow
- [ ] Steep UI: DDL Queue view

### Phase 5: Topology & Coordination (Weeks 9-10)
- [ ] Node discovery and health
- [ ] Coordinator election
- [ ] Multi-node testing
- [ ] Steep UI: Topology visualization

### Phase 6: Hardening (Weeks 11-12)
- [ ] Failure scenario testing
- [ ] Performance optimization
- [ ] Documentation
- [ ] Production readiness checklist

---

## 16. Design Decisions

This section documents decisions on previously open questions.

### 16.1 Coordinator Availability

**Question**: Single coordinator is SPOF. Implement Raft consensus?

**Decision**: **No Raft for MVP. Simple failover with state in PostgreSQL.**

**Rationale**:
- Raft requires minimum 3 nodes; MVP targets 2-node deployments
- DBAs expect simple failover, not distributed consensus complexity
- Coordinator state is already stored in `steep_repl.coordinator_state` table
- Any node can become coordinator by reading state from database

**Implementation**:

```
┌─────────────────────────────────────────────────────────────────┐
│                    Coordinator Failover                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  2-Node Topology:                                               │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ • One node is coordinator (highest priority)            │   │
│  │ • If coordinator unreachable, other node self-promotes  │   │
│  │ • State read from local steep_repl tables               │   │
│  │ • No split-brain possible (only 2 nodes)               │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  N-Node Topology (future):                                      │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ • Priority-based election (highest available wins)      │   │
│  │ • Quorum required: (N/2)+1 nodes must agree             │   │
│  │ • Raft optional for users requiring stronger guarantees │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**What coordinators actually do** (and why failover is simple):

| Function | State Location | Failover Impact |
|----------|----------------|-----------------|
| Range allocation | `steep_repl.identity_ranges` | New coordinator reads max, continues |
| DDL queue | `steep_repl.ddl_queue` | Pending DDL still in table |
| Conflict arbitration | `steep_repl.conflict_log` | Pending conflicts still queued |
| Node health | `steep_repl.nodes` | Re-evaluated by new coordinator |

### 15.2 Clock Synchronization

**Question**: Last-write-wins requires synchronized clocks. Require NTP? Use logical clocks?

**Decision**: **Require NTP. Use commit timestamps as fallback.**

**Rationale**:
- NTP is standard practice; every production system should have it
- 1-second accuracy is sufficient for conflict resolution (conflicts within 1s are rare)
- PostgreSQL's `track_commit_timestamp` provides reliable ordering when enabled
- Logical clocks (vector clocks, HLC) add complexity without proportional benefit

**Implementation**:

```yaml
replication:
  clock_sync:
    # Primary: Require NTP
    require_ntp: true
    max_drift_ms: 1000

    # Fallback: PostgreSQL commit timestamps
    use_commit_timestamp: true  # Requires track_commit_timestamp = on

    # Tie-breaker when timestamps equal
    tie_breaker: node_priority  # Higher priority node wins
```

```sql
-- When track_commit_timestamp is enabled
SELECT pg_xact_commit_timestamp(xmin) as commit_time
FROM orders WHERE order_id = 12345;

-- Conflict resolution uses this when available
-- Falls back to transaction timestamp (xact_start) otherwise
```

**Startup check** (already in Section 17.2):
```
steep-repl start
[INFO] Checking clock synchronization...
[INFO] NTP source: time.windows.com, offset: +23ms
[INFO] PostgreSQL track_commit_timestamp: enabled
[INFO] Clock check: PASSED
```

### 15.3 Large Transactions

**Question**: How to handle transactions that span many rows? Batch conflict detection?

**Decision**: **Per-row conflict detection. Batch UI for resolution.**

**Rationale**:
- PostgreSQL logical replication already handles large transactions via streaming
- Conflicts are inherently per-row (same PK modified on both nodes)
- DBAs expect to see individual conflicts but resolve them efficiently
- Grouping by time window or transaction makes bulk resolution practical

**Implementation**:

```
┌─────────────────────────────────────────────────────────────────┐
│                    Large Transaction Handling                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Detection (per-row):                                           │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ • Each row conflict logged individually                 │   │
│  │ • Transaction ID (xid) preserved for grouping          │   │
│  │ • Timestamp window captured                             │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Resolution (bulk options):                                     │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ • Resolve all conflicts in transaction                  │   │
│  │ • Resolve all conflicts in time window                  │   │
│  │ • Resolve all conflicts for table                       │   │
│  │ • Apply same resolution to similar conflicts           │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**Conflict log schema addition**:

```sql
-- Group conflicts for bulk resolution
ALTER TABLE steep_repl.conflict_log ADD COLUMN
    origin_xid BIGINT;  -- Transaction ID from origin node

-- Bulk resolution function
CREATE FUNCTION steep_repl.resolve_conflicts_bulk(
    p_resolution TEXT,
    p_filter_xid BIGINT DEFAULT NULL,
    p_filter_table TEXT DEFAULT NULL,
    p_filter_time_start TIMESTAMPTZ DEFAULT NULL,
    p_filter_time_end TIMESTAMPTZ DEFAULT NULL
) RETURNS INTEGER AS $$
    -- Resolves all matching conflicts with same resolution
    -- Returns count of resolved conflicts
$$ LANGUAGE plpgsql;
```

**UI for bulk resolution**:

```
┌─ Pending Conflicts ───────────────────────────────────────────────┐
│                                                                   │
│  Showing: 47 conflicts (grouped by transaction)                  │
│                                                                   │
│  ▶ Transaction 1234567 (2025-12-03 14:32:00) - 23 conflicts      │
│    └─ orders: 15 UPDATE-UPDATE                                   │
│    └─ line_items: 8 UPDATE-UPDATE                                │
│                                                                   │
│  ▶ Transaction 1234590 (2025-12-03 14:32:05) - 12 conflicts      │
│    └─ inventory: 12 UPDATE-UPDATE                                │
│                                                                   │
│  ▼ Transaction 1234601 (2025-12-03 14:32:10) - 12 conflicts      │
│    │                                                             │
│    │  #1 orders.order_id = 50432  [L]ocal [R]emote              │
│    │  #2 orders.order_id = 50433  [L]ocal [R]emote              │
│    │  ...                                                        │
│                                                                   │
│  [A]ll local  [Z]All remote  [E]xpand  [C]ollapse               │
│  [T]ime range resolution  [Tab]le resolution                    │
└───────────────────────────────────────────────────────────────────┘
```

### 15.4 Schema Versioning

**Question**: Track schema versions for compatibility checking during DDL?

**Decision**: **Yes. Schema fingerprint per table, validated before DDL apply.**

**Rationale**:
- DDL applied to mismatched schema causes errors or silent corruption
- DBAs expect drift detection before it causes problems
- Simple hash comparison is fast and reliable
- Enables "diff" view in UI

**Implementation**:

```sql
-- Schema fingerprint table
CREATE TABLE steep_repl.schema_fingerprints (
    table_schema    TEXT NOT NULL,
    table_name      TEXT NOT NULL,
    fingerprint     TEXT NOT NULL,      -- SHA256 of column definitions
    column_count    INTEGER NOT NULL,
    captured_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (table_schema, table_name)
);

-- Fingerprint function
CREATE FUNCTION steep_repl.compute_fingerprint(
    p_schema TEXT,
    p_table TEXT
) RETURNS TEXT AS $$
    SELECT encode(sha256(string_agg(
        column_name || ':' || data_type || ':' || coalesce(column_default, '') || ':' || is_nullable,
        '|' ORDER BY ordinal_position
    )::bytea), 'hex')
    FROM information_schema.columns
    WHERE table_schema = p_schema AND table_name = p_table;
$$ LANGUAGE sql STABLE;

-- Compare fingerprints across nodes
CREATE FUNCTION steep_repl.compare_fingerprints(
    p_peer_node TEXT
) RETURNS TABLE (
    table_schema TEXT,
    table_name TEXT,
    local_fingerprint TEXT,
    remote_fingerprint TEXT,
    status TEXT  -- MATCH, MISMATCH, LOCAL_ONLY, REMOTE_ONLY
);
```

**DDL queue integration**:

```sql
-- DDL queue includes expected schema state
ALTER TABLE steep_repl.ddl_queue ADD COLUMN
    pre_fingerprint TEXT,   -- Schema before DDL
    post_fingerprint TEXT;  -- Expected schema after DDL

-- Before applying DDL on target:
-- 1. Compute current fingerprint
-- 2. Compare to pre_fingerprint
-- 3. If mismatch: FAIL with "schema drift detected"
-- 4. Apply DDL
-- 5. Verify post_fingerprint matches
```

**UI integration**:

```
┌─ Schema Status ───────────────────────────────────────────────────┐
│                                                                   │
│  Last sync: 2025-12-03 14:30:00                                  │
│  Status: ⚠ 1 table with drift                                   │
│                                                                   │
│  Table              HQ              Cloud           Status       │
│  ──────────────────────────────────────────────────────────────── │
│  orders             a1b2c3d4        a1b2c3d4        ● MATCH      │
│  customers          e5f6g7h8        e5f6g7h8        ● MATCH      │
│  inventory          i9j0k1l2        m3n4o5p6        ⚠ DRIFT      │
│                                                                   │
│  [D]iff inventory  [S]ync schema  [R]efresh                     │
└───────────────────────────────────────────────────────────────────┘

┌─ Schema Diff: inventory ──────────────────────────────────────────┐
│                                                                   │
│  HQ                              Cloud                           │
│  ──────────────────────────────────────────────────────────────── │
│  + reserved_qty INTEGER          (missing)                       │
│  last_updated TIMESTAMPTZ        last_updated TIMESTAMPTZ        │
│                                                                   │
│  Recommendation: Apply pending DDL #42 on Cloud                  │
│                                                                   │
│  [A]pply DDL  [I]gnore  [M]anual sync                           │
└───────────────────────────────────────────────────────────────────┘
```

### 15.5 Conflict Resolution Rollback

**Question**: Can we roll back a conflict resolution if it was wrong?

**Decision**: **Yes, via "revert" that creates a corrective change. Not true rollback.**

**Rationale**:
- True rollback in distributed systems is complex (other nodes may have acted on the data)
- DBAs expect an "undo" capability for mistakes
- Revert = apply the opposite change, creating an audit trail
- Time-limited by retention policy (can't revert ancient conflicts)

**Implementation**:

```sql
-- Conflict log stores both tuples for revert capability
-- (already in schema: local_tuple, remote_tuple JSONB)

-- Revert function
CREATE FUNCTION steep_repl.revert_resolution(
    p_conflict_id BIGINT,
    p_reason TEXT DEFAULT 'Manual revert'
) RETURNS BIGINT AS $$  -- Returns new conflict_id for the revert
DECLARE
    v_conflict steep_repl.conflict_log%ROWTYPE;
    v_revert_id BIGINT;
BEGIN
    -- Get original conflict
    SELECT * INTO v_conflict
    FROM steep_repl.conflict_log
    WHERE id = p_conflict_id;

    IF v_conflict.resolution = 'PENDING' THEN
        RAISE EXCEPTION 'Cannot revert unresolved conflict';
    END IF;

    -- Determine what to restore
    -- If we kept local, restore remote (and vice versa)
    CASE v_conflict.resolution
        WHEN 'KEPT_LOCAL' THEN
            -- Apply remote tuple now
            PERFORM steep_repl.apply_tuple(
                v_conflict.table_schema,
                v_conflict.table_name,
                v_conflict.remote_tuple
            );
        WHEN 'APPLIED_REMOTE' THEN
            -- Apply local tuple now
            PERFORM steep_repl.apply_tuple(
                v_conflict.table_schema,
                v_conflict.table_name,
                v_conflict.local_tuple
            );
    END CASE;

    -- Log the revert as a new entry
    INSERT INTO steep_repl.conflict_log (
        table_schema, table_name, conflict_type,
        local_tuple, remote_tuple, resolution, resolved_by
    ) VALUES (
        v_conflict.table_schema, v_conflict.table_name, 'REVERT',
        v_conflict.remote_tuple, v_conflict.local_tuple,
        'REVERTED', format('revert:%s (original #%s)', current_user, p_conflict_id)
    ) RETURNING id INTO v_revert_id;

    -- Mark original as reverted
    UPDATE steep_repl.conflict_log
    SET resolution = 'REVERTED',
        resolved_by = resolved_by || format(' [reverted by #%s]', v_revert_id)
    WHERE id = p_conflict_id;

    RETURN v_revert_id;
END;
$$ LANGUAGE plpgsql;
```

**Limitations**:

| Limitation | Reason |
|------------|--------|
| Time-limited | Conflict data pruned per retention policy (default 30 days) |
| Cascading effects | Reverting may cause new conflicts if other changes depend on it |
| Not atomic across nodes | Revert is a new change that replicates normally |
| Merged resolutions | If resolution was MERGED (custom), original values may not fully restore |

**UI for revert**:

```
┌─ Conflict History ────────────────────────────────────────────────┐
│                                                                   │
│  #1042 orders.order_id = 50432                                   │
│  ────────────────────────────────────────────────────────────────│
│  Detected:  2025-12-03 14:32:15                                  │
│  Resolved:  2025-12-03 14:35:00 by alice@hq                      │
│  Resolution: KEPT_LOCAL                                          │
│                                                                   │
│  LOCAL (kept):              REMOTE (discarded):                  │
│  ┌─────────────────────┐    ┌─────────────────────┐              │
│  │ status: processing  │    │ status: shipped     │              │
│  │ updated_by: alice   │    │ updated_by: bob     │              │
│  └─────────────────────┘    └─────────────────────┘              │
│                                                                   │
│  ⚠ Revert will apply the REMOTE values (bob's changes)          │
│                                                                   │
│  [R]evert  [C]ancel                                              │
│                                                                   │
│  Revert reason: ________________________________________         │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

**CLI support**:

```bash
# View conflict details
steep-repl conflict show 1042

# Revert with reason
steep-repl conflict revert 1042 --reason "Alice confirmed Bob's update was correct"

# Dry-run to see what would change
steep-repl conflict revert 1042 --dry-run
```

---

## 17. References

- [PostgreSQL 18 Logical Replication](https://www.postgresql.org/docs/18/logical-replication.html)
- [SQL Server Merge Replication Identity Ranges](https://docs.microsoft.com/en-us/sql/relational-databases/replication/merge/parameterized-filters-optimize-for-precomputed-partitions)
- [pgrx Documentation](https://github.com/pgcentralfoundation/pgrx)
- [pglogical Source Code](https://github.com/2ndQuadrant/pglogical)
- Steep Replication View (Feature 006)

---

## 18. Production Readiness

This section addresses requirements for running bidirectional replication in production environments, including mixed-platform deployments (e.g., Windows on-premises ↔ Linux cloud).

### 18.1 Data Validation

Replication can silently diverge due to bugs, network issues, or operational errors. Periodic validation is **mandatory** for production.

#### Validation Levels

```
┌─────────────────────────────────────────────────────────────────┐
│                    Data Validation Hierarchy                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Level 1: ROW COUNT (fast, frequent)                           │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ • Compare row counts per table across nodes             │   │
│  │ • Run every 5 minutes (configurable)                    │   │
│  │ • Alert on >0.1% difference or absolute threshold       │   │
│  │ • Negligible performance impact                         │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Level 2: CHECKSUM (slower, periodic)                          │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ • Hash of (PK, updated_at, key_columns) per row        │   │
│  │ • Run nightly on quiet tables, weekly on busy          │   │
│  │ • Identifies specific divergent rows                    │   │
│  │ • Supports sampling for large tables (1-10%)           │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Level 3: FULL COMPARE (slowest, on-demand)                    │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ • Row-by-row column comparison                          │   │
│  │ • Used after suspected divergence                       │   │
│  │ • Generates repair SQL script                           │   │
│  │ • Can run on subset of rows (WHERE clause)             │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Validation Functions

```sql
-- Row count validation (Level 1)
CREATE FUNCTION steep_repl.validate_row_counts(
    p_peer_node TEXT DEFAULT NULL  -- NULL = all peers
) RETURNS TABLE (
    table_schema    TEXT,
    table_name      TEXT,
    local_count     BIGINT,
    remote_count    BIGINT,
    difference      BIGINT,
    diff_percent    NUMERIC,
    status          TEXT    -- OK, WARNING, CRITICAL
) AS $$
    -- Queries local count and requests remote count via dblink/fdw
$$ LANGUAGE plpgsql;

-- Checksum validation (Level 2)
CREATE FUNCTION steep_repl.validate_checksums(
    p_table_schema  TEXT,
    p_table_name    TEXT,
    p_peer_node     TEXT,
    p_sample_pct    NUMERIC DEFAULT 100,
    p_key_columns   TEXT[] DEFAULT NULL  -- NULL = all columns
) RETURNS TABLE (
    pk_value        TEXT,
    local_hash      TEXT,
    remote_hash     TEXT,
    divergence_type TEXT    -- MISSING_LOCAL, MISSING_REMOTE, DATA_MISMATCH
);

-- Full compare with repair script (Level 3)
CREATE FUNCTION steep_repl.compare_and_repair(
    p_table_schema  TEXT,
    p_table_name    TEXT,
    p_peer_node     TEXT,
    p_where_clause  TEXT DEFAULT NULL,
    p_dry_run       BOOLEAN DEFAULT true
) RETURNS TABLE (
    pk_value        TEXT,
    divergence_type TEXT,
    repair_sql      TEXT,
    applied         BOOLEAN
);
```

#### Validation Configuration

```yaml
replication:
  validation:
    enabled: true

    row_count:
      interval: 5m
      alert_threshold_percent: 0.1
      alert_threshold_absolute: 100

    checksum:
      schedule: "0 3 * * *"    # 3 AM daily
      sample_percent: 10        # 10% sample for large tables
      large_table_threshold: 1GB

      # Per-table overrides
      tables:
        orders:
          sample_percent: 100   # Full check for critical tables
        audit_log:
          enabled: false        # Skip audit tables

    on_divergence:
      action: alert             # alert | auto_repair | pause_replication
      notify: [webhook, email]
```

#### Validation in Steep UI

```
┌─ Data Validation ─────────────────────────────────────────────────┐
│                                                                   │
│  Last Validation: 2025-12-03 03:15:00 (8 hours ago)              │
│  Status: ● OK (all tables match)                                 │
│                                                                   │
│  Row Counts (5m ago):                                            │
│  ──────────────────────────────────────────────────────────────── │
│  Table           Local       Remote      Diff      Status        │
│  orders          1,245,892   1,245,892   0         ● OK          │
│  customers       89,234      89,234      0         ● OK          │
│  line_items      4,892,103   4,892,101   2         ⚠ WARN        │
│                                                                   │
│  Checksums (last night):                                         │
│  ──────────────────────────────────────────────────────────────── │
│  orders:     ✓ 100% validated, 0 mismatches                      │
│  customers:  ✓ 100% validated, 0 mismatches                      │
│  products:   ✓ 10% sample, 0 mismatches                          │
│                                                                   │
│  [V]alidate now  [R]epair divergence  [H]istory                 │
└───────────────────────────────────────────────────────────────────┘
```

### 17.2 Clock Synchronization

**Last-write-wins requires synchronized clocks. This is mandatory, not optional.**

#### Requirements

| Environment | NTP Configuration | Monitoring |
|-------------|-------------------|------------|
| Windows HQ | Windows Time Service (w32time) | `w32tm /query /status` |
| Cloud Linux | chrony or systemd-timesyncd | `chronyc tracking` |
| Both | Max drift: 1 second | Alert if exceeded |

#### steep-repl Clock Checks

```go
// internal/repl/clock/sync.go
type ClockStatus struct {
    Synchronized bool
    Offset       time.Duration
    Source       string    // NTP server
    LastSync     time.Time
}

func CheckClockSync() (*ClockStatus, error) {
    switch runtime.GOOS {
    case "windows":
        return checkWindowsTime()  // w32tm /query /status
    default:
        return checkChrony()       // chronyc tracking -n
    }
}

func (d *Daemon) validateClocks() error {
    local := CheckClockSync()
    if !local.Synchronized {
        return fmt.Errorf("local clock not synchronized to NTP")
    }
    if local.Offset.Abs() > d.config.MaxClockDrift {
        return fmt.Errorf("clock drift %v exceeds max %v",
            local.Offset, d.config.MaxClockDrift)
    }
    return nil
}
```

#### Configuration

```yaml
replication:
  clock_sync:
    require_ntp: true             # Fail to start if NTP not synced
    max_drift_ms: 1000            # Alert if drift > 1 second
    check_interval: 60s           # How often to verify

    # Fallback when clocks unreliable
    fallback_to_logical_clock: false  # Use pg_xact_commit_timestamp()

    # Windows-specific
    windows:
      ntp_server: "time.windows.com"
      sync_command: "w32tm /resync"

    # Linux-specific
    linux:
      ntp_service: "chrony"       # chrony | systemd-timesyncd | ntpd
```

#### Startup Validation

```
┌─────────────────────────────────────────────────────────────────┐
│              steep-repl Startup Clock Check                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  $ steep-repl start                                             │
│                                                                 │
│  [INFO] Checking clock synchronization...                       │
│  [INFO] NTP source: time.windows.com                            │
│  [INFO] Clock offset: +23ms (within 1000ms limit)              │
│  [INFO] Clock check: PASSED                                     │
│                                                                 │
│  -- OR --                                                       │
│                                                                 │
│  [ERROR] Clock synchronization check failed                     │
│  [ERROR] NTP not synchronized (stratum 16)                      │
│  [ERROR] Run: w32tm /resync                                     │
│  [ERROR] steep-repl cannot start with unsynchronized clocks    │
│  [ERROR] Override with --skip-clock-check (NOT RECOMMENDED)    │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 17.3 Failover and Failback

#### Failover: HQ → Cloud

When HQ becomes unreachable, Cloud must be able to operate independently.

```
┌─────────────────────────────────────────────────────────────────┐
│                    Failover: HQ → Cloud                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  TRIGGER CONDITIONS (any of):                                   │
│  • HQ unreachable for > failover_timeout (default: 5 minutes)  │
│  • Manual: steep-repl failover --promote cloud               │
│  • steep-repl daemon on HQ reports fatal error                 │
│                                                                 │
│  AUTOMATIC FAILOVER STEPS:                                      │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ 1. DETECT                                               │   │
│  │    • Peer health checks fail for failover_timeout       │   │
│  │    • gRPC connection times out                          │   │
│  │    • PostgreSQL connection fails                        │   │
│  │                                                         │   │
│  │ 2. WAIT                                                 │   │
│  │    • Drain pending replication (grace_period: 30s)     │   │
│  │    • Apply any buffered changes                         │   │
│  │                                                         │   │
│  │ 3. PROMOTE                                              │   │
│  │    • Cloud steep-repl becomes coordinator            │   │
│  │    • Update steep_repl.nodes (HQ → UNREACHABLE)        │   │
│  │    • Expand identity ranges to include HQ's range      │   │
│  │                                                         │   │
│  │ 4. NOTIFY                                               │   │
│  │    • Webhook: FAILOVER_STARTED, FAILOVER_COMPLETE      │   │
│  │    • Email to configured addresses                      │   │
│  │    • Log to steep_repl.failover_history                │   │
│  │                                                         │   │
│  │ 5. OPERATE                                              │   │
│  │    • Cloud accepts all writes                         │   │
│  │    • Application should switch connection string        │   │
│  │    • Conflicts queued for when HQ returns              │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Identity Range During Failover

```sql
-- Before failover
-- HQ:      orders range 1-100000
-- Cloud: orders range 100001-200000

-- During failover, Cloud expands to cover both
-- This prevents range exhaustion during extended outage

ALTER TABLE orders DROP CONSTRAINT steep_range_orders;
ALTER TABLE orders ADD CONSTRAINT steep_range_orders
    CHECK (order_id >= 1 AND order_id <= 200000);

-- Sequence continues from Cloud's position
-- New orders: 150001, 150002, ... (no collision with HQ's 1-100000)

-- When HQ returns, ranges are re-split:
-- HQ:      orders range 200001-300000 (new range)
-- Cloud: orders range 100001-200000 (keep current)
```

#### Failback: Cloud → HQ

```
┌─────────────────────────────────────────────────────────────────┐
│                    Failback: Cloud → HQ                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  PRECONDITIONS:                                                 │
│  • HQ PostgreSQL is running and accessible                      │
│  • HQ steep-repl daemon is healthy                             │
│  • Network connectivity verified                                │
│                                                                 │
│  FAILBACK STEPS:                                                │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ 1. RECONNECT                                            │   │
│  │    steep-repl failback --prepare                        │   │
│  │    • Re-establish gRPC connection                       │   │
│  │    • Verify schemas match                               │   │
│  │    • Calculate sync delta (what HQ missed)             │   │
│  │                                                         │   │
│  │ 2. SYNC                                                 │   │
│  │    steep-repl failback --sync                          │   │
│  │    • HQ catches up from Cloud via replication        │   │
│  │    • May take hours for large deltas                    │   │
│  │    • Progress shown in UI                               │   │
│  │                                                         │   │
│  │ 3. VALIDATE                                             │   │
│  │    steep-repl failback --validate                      │   │
│  │    • Row count comparison                               │   │
│  │    • Checksum critical tables                           │   │
│  │    • Resolve any conflicts from failover period        │   │
│  │                                                         │   │
│  │ 4. SWITCH                                               │   │
│  │    steep-repl failback --complete                      │   │
│  │    • HQ regains coordinator role                        │   │
│  │    • HQ gets new identity range (post-Cloud)         │   │
│  │    • Bidirectional replication resumes                  │   │
│  │                                                         │   │
│  │ 5. NOTIFY                                               │   │
│  │    • Webhook: FAILBACK_COMPLETE                        │   │
│  │    • Application can switch back to HQ                  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Configuration

```yaml
replication:
  failover:
    enabled: true
    mode: manual              # manual | automatic

    # Automatic failover settings
    automatic:
      timeout: 5m             # How long before declaring peer dead
      grace_period: 30s       # Wait for pending changes to apply
      require_quorum: false   # For 2-node, quorum not possible

    # Identity range handling
    range_expansion: true     # Expand to cover failed node's range

    # Notifications
    notify:
      webhook_url: "https://..."
      email: ["dba@company.com"]
      events: [failover_started, failover_complete, failback_complete]
```

#### Failover UI

```
┌─ Failover Status ─────────────────────────────────────────────────┐
│                                                                   │
│  ⚠ FAILOVER ACTIVE                                               │
│                                                                   │
│  HQ (node_a):      UNREACHABLE since 2025-12-03 14:30:00        │
│  Cloud (node_b): PROMOTED (coordinator)                        │
│                                                                   │
│  Failover initiated: 2025-12-03 14:35:00 (automatic)            │
│  Duration: 2h 15m                                                │
│                                                                   │
│  Changes since failover:                                         │
│  • 1,234 orders created on Cloud                              │
│  • 5,678 updates applied                                         │
│  • Identity ranges expanded                                      │
│                                                                   │
│  When HQ returns:                                                │
│  [P]repare failback  [V]alidate  [C]omplete failback            │
│                                                                   │
└───────────────────────────────────────────────────────────────────┘
```

### 17.4 Backup Coordination

Consistent backups across replicated nodes require coordination.

#### Backup Strategies

| Strategy | Description | When to Use |
|----------|-------------|-------------|
| **Independent** | Each node backs up independently | Default, simplest |
| **Coordinated** | Pause replication, backup both, resume | Exact consistency needed |
| **Primary-only** | Only backup coordinator, restore to both | Simplest recovery |

#### Coordinated Backup

```bash
# Step 1: Pause replication on all nodes
steep-repl backup prepare --all-nodes

# Step 2: Record consistent LSN across nodes
steep-repl backup snapshot
# Returns: backup_id: bk_20251203_143000
#          hq_lsn: 0/1234ABCD
#          cloud_lsn: 0/1234ABCD

# Step 3: Perform backups (user's tooling)
pg_basebackup -D /backup/hq -h hq ...
pg_basebackup -D /backup/cloud -h cloud ...

# Step 4: Resume replication
steep-repl backup complete --backup-id bk_20251203_143000

# Backup metadata stored for recovery
steep-repl backup list
steep-repl backup show bk_20251203_143000
```

#### Point-in-Time Recovery Considerations

```
┌─────────────────────────────────────────────────────────────────┐
│                    PITR with Bidirectional                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Challenge: Recovering to a point-in-time when nodes had       │
│  different data (pre-replication, or during conflict)          │
│                                                                 │
│  Recommendation:                                                │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ 1. Always recover BOTH nodes to same point             │   │
│  │ 2. Use coordinated backup LSN as recovery target       │   │
│  │ 3. Re-establish replication after recovery             │   │
│  │ 4. Validate data matches before resuming               │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  steep-repl recovery plan:                                      │
│  • steep-repl backup recover --backup-id bk_... --both-nodes  │
│  • Guides user through pg_restore on each node                 │
│  • Validates schemas match post-restore                        │
│  • Re-creates replication subscriptions                        │
│  • Re-allocates identity ranges from current max values        │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 17.5 Business Notifications

E-commerce replication events must reach the right people.

#### Notification Channels

```yaml
replication:
  notifications:
    channels:
      slack:
        webhook_url: "https://hooks.slack.com/services/..."
        channel: "#db-alerts"

      email:
        smtp_host: "smtp.company.com"
        smtp_port: 587
        from: "steep-repl@company.com"
        to: ["dba@company.com", "oncall@company.com"]

      pagerduty:
        routing_key: "..."

      teams:
        webhook_url: "https://outlook.office.com/webhook/..."

    # Which events trigger which channels
    routing:
      conflict_detected:
        channels: [slack]
        tables: [orders, payments, inventory]  # Critical tables only

      conflict_pending_manual:
        channels: [slack, email]
        escalation:
          after: 30m
          to: [pagerduty]

      node_unreachable:
        channels: [slack, pagerduty]

      failover_started:
        channels: [slack, email, pagerduty]

      failover_complete:
        channels: [slack, email]

      validation_failed:
        channels: [slack, email, pagerduty]

      range_threshold_exceeded:
        channels: [slack]
```

#### Notification Templates

```yaml
replication:
  notifications:
    templates:
      conflict_detected: |
        🔴 *Replication Conflict Detected*

        Table: {{ .TableSchema }}.{{ .TableName }}
        PK: {{ .PrimaryKey }}
        Type: {{ .ConflictType }}

        Local ({{ .LocalNode }}): {{ .LocalValue | truncate 100 }}
        Remote ({{ .RemoteNode }}): {{ .RemoteValue | truncate 100 }}

        Resolution: {{ .Resolution }}

      failover_started: |
        🚨 *FAILOVER INITIATED*

        Failed node: {{ .FailedNode }}
        Promoted node: {{ .PromotedNode }}
        Trigger: {{ .Trigger }}

        Action required: Update application connection strings
```

### 17.6 WAL Retention Sizing

For WAN replication, calculate WAL retention based on expected outage duration.

#### Sizing Calculator

```
┌─────────────────────────────────────────────────────────────────┐
│                    WAL Retention Calculator                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Inputs:                                                        │
│  • Expected max outage: 48 hours (weekend)                     │
│  • Write rate: 100 MB/hour (estimate from pg_stat_wal)         │
│  • Safety margin: 2x                                            │
│                                                                 │
│  Calculation:                                                   │
│  WAL needed = 48h × 100 MB/h × 2 = 9.6 GB                      │
│                                                                 │
│  Recommendation:                                                │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ wal_keep_size = 10GB                                    │   │
│  │                                                         │   │
│  │ -- OR use replication slots (auto-retention) --        │   │
│  │ max_slot_wal_keep_size = 20GB  (cap to prevent fill)   │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Monitor: pg_replication_slots.wal_status                       │
│  Alert if: wal_status = 'lost' or slot_lag > 50% of limit     │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Configuration

```yaml
replication:
  wal_retention:
    # Sizing guidance
    expected_max_outage: 48h

    # Monitoring
    alert_threshold_percent: 50   # Alert when slot uses 50% of limit

    # Recommended PostgreSQL settings (informational)
    recommended_settings:
      wal_keep_size: "10GB"
      max_slot_wal_keep_size: "20GB"
```

### 18.7 Extension Upgrade Strategy

The steep_repl PostgreSQL extension requires careful upgrade planning to maintain replication continuity.

#### Version Compatibility

| Extension Version | PostgreSQL 18 | steep-repl (Go) | Key Features |
|-------------------|---------------|-----------------|--------------|
| 1.0.x             | ✓             | 1.0.x - 1.2.x   | Core replication, conflict detection |
| 1.1.x             | ✓             | 1.1.x - 1.3.x   | DDL replication integration |
| 1.2.x             | ✓             | 1.2.x+          | Sequence sync, parallel COPY |

The extension and Go daemon maintain backward compatibility within minor versions.

#### Rolling Upgrade Procedure

Upgrade one node at a time to maintain replication availability:

```bash
# Step 1: Check current versions on all nodes
steep-repl version --all-nodes

Node       Extension    Daemon     PostgreSQL
────────────────────────────────────────────
node_a     1.0.2        1.0.5      18.0
node_b     1.0.2        1.0.5      18.1
node_c     1.0.2        1.0.5      18.0

# Step 2: Verify upgrade compatibility
steep-repl upgrade-check --target-version=1.1.0

[OK] Extension 1.1.0 compatible with all nodes
[OK] Daemon 1.1.0 compatible with extension 1.0.2 (mixed-version support)
[INFO] Recommended upgrade order: node_c, node_a, node_b

# Step 3: Upgrade first node (pause replication to this node)
steep-repl pause --node=node_c
steep-repl upgrade --node=node_c --version=1.1.0

[INFO] Stopping steep-repl daemon on node_c...
[INFO] Upgrading extension: ALTER EXTENSION steep_repl UPDATE TO '1.1.0';
[INFO] Restarting steep-repl daemon...
[INFO] Resuming replication...

# Step 4: Verify and continue
steep-repl health --node=node_c
[OK] node_c: healthy (extension 1.1.0, daemon 1.1.0)

# Step 5: Repeat for remaining nodes
steep-repl upgrade --node=node_a --version=1.1.0
steep-repl upgrade --node=node_b --version=1.1.0
```

#### Extension Schema Migrations

Major extension versions may include schema migrations:

```sql
-- Extension handles migrations automatically via ALTER EXTENSION UPDATE
-- Migration history tracked in steep_repl.schema_version

SELECT * FROM steep_repl.schema_version ORDER BY applied_at DESC;

version | applied_at           | description
────────┼──────────────────────┼────────────────────────────
1.1.0   | 2025-01-15 10:30:00  | Add conflict_metadata JSONB column
1.0.0   | 2024-12-01 08:00:00  | Initial schema
```

#### Rollback Procedure

If issues occur during upgrade:

```bash
steep-repl rollback --node=node_c --version=1.0.2

[INFO] Stopping steep-repl daemon...
[INFO] Extension rollback: ALTER EXTENSION steep_repl UPDATE TO '1.0.2';
[WARN] Some features may be unavailable after rollback
[INFO] Resuming replication...
```

#### Pre-Upgrade Checklist

```bash
steep-repl upgrade-preflight --target=1.1.0

✓ All nodes healthy
✓ No pending conflicts
✓ Replication lag < 1 minute
✓ Backup completed within last 24 hours
✓ Extension update path exists: 1.0.2 → 1.1.0
✓ Disk space sufficient for migration
✓ Maintenance window scheduled

Ready for upgrade. Estimated duration: 15 minutes per node
```

---

## 19. Networking

### 19.1 Overview

Bidirectional replication requires reliable network connectivity between nodes. For cross-site and mixed-platform deployments, **Tailscale** is the recommended networking solution.

### 18.2 Network Requirements

| Requirement | Port | Protocol | Purpose |
|-------------|------|----------|---------|
| PostgreSQL | 5432 | TCP | Logical replication streams |
| steep-repl gRPC | 5433 | TCP | Daemon-to-daemon coordination |
| steep-repl HTTP | 5434 | TCP | Health checks (optional) |
| Tailscale | 41641 | UDP | WireGuard tunnel (if using Tailscale) |

### 18.3 Tailscale Integration

Tailscale provides zero-config mesh networking ideal for this use case.

#### Why Tailscale?

| Feature | Benefit for Steep |
|---------|-------------------|
| **Zero-config mesh** | Windows + Linux + macOS nodes just work together |
| **NAT traversal** | No firewall holes needed at HQ |
| **MagicDNS** | `hq.tailnet.ts.net` instead of managing IPs |
| **ACLs** | Restrict PostgreSQL access to steep-repl only |
| **Key rotation** | Automatic, no manual certificate management |
| **Cross-platform** | Windows, Linux, macOS all supported |
| **Subnet routing** | Expose remote datacenter network to other sites if needed |
| **Exit nodes** | Route traffic through specific nodes if needed |

#### Tailscale Setup

```
┌─────────────────────────────────────────────────────────────────┐
│                    Tailscale Architecture                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│     HQ (Windows)                    Cloud (Linux)             │
│  ┌──────────────────┐           ┌──────────────────┐           │
│  │ Tailscale        │           │ Tailscale        │           │
│  │ 100.64.1.1       │◄─────────►│ 100.64.1.2       │           │
│  │ hq.net.ts.net    │  WireGuard│ cloud.net.ts   │           │
│  └────────┬─────────┘   Tunnel  └────────┬─────────┘           │
│           │                              │                      │
│  ┌────────▼─────────┐           ┌────────▼─────────┐           │
│  │ PostgreSQL       │           │ PostgreSQL       │           │
│  │ localhost:5432   │◄─────────►│ localhost:5432   │           │
│  └────────┬─────────┘  Logical  └────────┬─────────┘           │
│           │            Repl             │                      │
│  ┌────────▼─────────┐           ┌────────▼─────────┐           │
│  │ steep-repl       │           │ steep-repl       │           │
│  │ localhost:5433   │◄─────────►│ localhost:5433   │           │
│  └──────────────────┘   gRPC    └──────────────────┘           │
│                                                                 │
│  Connection strings use Tailscale hostnames:                   │
│  • hq.mynet.ts.net:5432                                        │
│  • cloud.mynet.ts.net:5432                                   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Installation

```bash
# Windows HQ (PowerShell as Administrator)
winget install Tailscale.Tailscale
tailscale up --authkey tskey-auth-...

# Cloud Linux
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up --authkey tskey-auth-...
```

#### Tailscale ACL Configuration

```jsonc
// tailscale ACL policy (admin console or GitOps)
{
  "acls": [
    // Allow steep-repl daemons to communicate
    {
      "action": "accept",
      "src": ["tag:steep-repl"],
      "dst": ["tag:steep-repl:5432,5433,5434"]
    },

    // Allow steep TUI to connect to daemons
    {
      "action": "accept",
      "src": ["tag:steep-admin"],
      "dst": ["tag:steep-repl:5433,5434"]
    }
  ],

  "tagOwners": {
    "tag:steep-repl": ["autogroup:admin"],
    "tag:steep-admin": ["autogroup:admin"]
  }
}
```

#### steep-repl Tailscale Integration

```yaml
# ~/.config/steep/config.yaml
replication:
  networking:
    provider: tailscale         # tailscale | manual | wireguard

    tailscale:
      # Option 1: Expect Tailscale already running
      expect_connected: true

      # Option 2: steep-repl manages Tailscale (advanced)
      auto_connect: false
      auth_key_env: TAILSCALE_AUTHKEY

      # Health check via Tailscale status
      check_peer_status: true

    # Node addressing uses Tailscale hostnames
    nodes:
      - name: hq
        host: hq.mynet.ts.net        # Tailscale MagicDNS
        port: 5432
        grpc_port: 5433

      - name: cloud
        host: cloud.mynet.ts.net
        port: 5432
        grpc_port: 5433
```

#### Tailscale Health Checks

```go
// internal/repl/network/tailscale.go
package network

import (
    "encoding/json"
    "os/exec"
)

type TailscaleStatus struct {
    Self struct {
        Online   bool   `json:"Online"`
        HostName string `json:"HostName"`
        TailAddr string `json:"TailscaleIPs"`
    } `json:"Self"`
    Peer map[string]struct {
        HostName      string `json:"HostName"`
        Online        bool   `json:"Online"`
        LastHandshake string `json:"LastHandshake"`
        RxBytes       int64  `json:"RxBytes"`
        TxBytes       int64  `json:"TxBytes"`
    } `json:"Peer"`
}

func GetTailscaleStatus() (*TailscaleStatus, error) {
    cmd := exec.Command("tailscale", "status", "--json")
    out, err := cmd.Output()
    if err != nil {
        return nil, err
    }
    var status TailscaleStatus
    json.Unmarshal(out, &status)
    return &status, nil
}

func (d *Daemon) checkTailscalePeers() error {
    status, err := GetTailscaleStatus()
    if err != nil {
        return fmt.Errorf("tailscale not running: %w", err)
    }

    for _, node := range d.config.Nodes {
        peer, ok := status.Peer[node.Host]
        if !ok || !peer.Online {
            d.markNodeUnreachable(node.Name, "tailscale peer offline")
        }
    }
    return nil
}
```

### 18.4 Manual Networking (Without Tailscale)

For environments where Tailscale isn't suitable:

```yaml
replication:
  networking:
    provider: manual

    # Direct connections (requires firewall config)
    nodes:
      - name: hq
        host: hq.company.com          # Public DNS or IP
        port: 5432
        grpc_port: 5433

      - name: cloud
        host: 203.0.113.50            # Cloud public IP
        port: 5432
        grpc_port: 5433

    # TLS is mandatory for manual networking
    tls:
      enabled: true
      cert_file: /etc/steep/certs/server.crt
      key_file: /etc/steep/certs/server.key
      ca_file: /etc/steep/certs/ca.crt
      verify_peer: true
```

### 18.5 WAN Considerations

| Consideration | Recommendation |
|---------------|----------------|
| **Latency** | Expect 20-100ms for cross-site. Replication is async; this is fine. |
| **Bandwidth** | Monitor with `pg_stat_wal`. E-commerce typically 10-100 MB/hour. |
| **Packet loss** | TCP handles this. Tailscale's WireGuard is resilient. |
| **MTU issues** | Tailscale handles automatically. Manual: ensure 1280+ MTU. |
| **Reconnection** | steep-repl implements exponential backoff on connection loss. |

### 18.6 Network Monitoring in Steep UI

```
┌─ Network Status ──────────────────────────────────────────────────┐
│                                                                   │
│  Provider: Tailscale                                             │
│  This node: hq.mynet.ts.net (100.64.1.1)                        │
│                                                                   │
│  Peer Connectivity:                                              │
│  ──────────────────────────────────────────────────────────────── │
│  Node        Tailscale IP    Latency    Last Handshake   Status  │
│  cloud     100.64.1.2      45ms       12s ago          ● OK    │
│                                                                   │
│  Bandwidth (last hour):                                          │
│  HQ → Cloud: 45.2 MB  ████████░░░░░░░░░░░░                    │
│  Cloud → HQ: 12.8 MB  ██░░░░░░░░░░░░░░░░░░                    │
│                                                                   │
│  [T]ailscale status  [P]ing test  [R]econnect                   │
└───────────────────────────────────────────────────────────────────┘
```

---

## 20. Security

### 20.1 Security Model

```
┌─────────────────────────────────────────────────────────────────┐
│                    Security Layers                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Layer 1: NETWORK                                               │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ • Tailscale/WireGuard encryption (mandatory)            │   │
│  │ • No public exposure of PostgreSQL ports                │   │
│  │ • ACLs restrict access to replication nodes only        │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Layer 2: TRANSPORT                                             │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ • TLS 1.3 for gRPC (when not using Tailscale)          │   │
│  │ • PostgreSQL sslmode=require                            │   │
│  │ • Certificate verification enabled                      │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Layer 3: AUTHENTICATION                                        │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ • PostgreSQL roles with minimal privileges              │   │
│  │ • steep-repl daemon uses dedicated replication role     │   │
│  │ • No password storage in config files                   │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Layer 4: AUTHORIZATION                                         │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ • RBAC for manual conflict resolution                   │   │
│  │ • DDL approval requires specific role                   │   │
│  │ • Bypass mode restricted to authorized roles            │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│  Layer 5: AUDIT                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │ • All conflict resolutions logged with user/time       │   │
│  │ • DDL changes tracked with origin and approver          │   │
│  │ • Bypass mode usage logged and alerted                  │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 19.2 Credential Management

**Rule: No plaintext passwords in configuration files.**

#### Supported Credential Sources

```yaml
replication:
  credentials:
    # Option 1: Environment variables (recommended for services)
    hq:
      host: hq.mynet.ts.net
      user_env: STEEP_HQ_USER           # Read from env
      password_env: STEEP_HQ_PASSWORD   # Read from env

    # Option 2: Password command (recommended for interactive)
    cloud:
      host: cloud.mynet.ts.net
      user: steep_repl
      password_command: "pass show postgres/cloud"  # Or 1password, vault, etc.

    # Option 3: .pgpass file (PostgreSQL standard)
    # Just omit password, pgx will use .pgpass

    # Option 4: Certificate authentication (most secure)
    secure_node:
      host: secure.mynet.ts.net
      user: steep_repl
      sslcert: /etc/steep/certs/client.crt
      sslkey: /etc/steep/certs/client.key
```

#### Windows Credential Manager Integration

```go
// internal/config/credentials_windows.go
// +build windows

import (
    "github.com/danieljoos/wincred"
)

func GetCredential(target string) (string, string, error) {
    cred, err := wincred.GetGenericCredential(target)
    if err != nil {
        return "", "", err
    }
    return cred.UserName, string(cred.CredentialBlob), nil
}
```

```yaml
replication:
  credentials:
    hq:
      host: hq.mynet.ts.net
      credential_manager: "steep/hq"  # Windows Credential Manager target
```

### 19.3 PostgreSQL Role Configuration

```sql
-- Dedicated replication role with minimal privileges
CREATE ROLE steep_repl WITH
    LOGIN
    REPLICATION
    PASSWORD NULL;  -- Use certificate auth or .pgpass

-- Grant necessary permissions
GRANT USAGE ON SCHEMA steep_repl TO steep_repl;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA steep_repl TO steep_repl;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA steep_repl TO steep_repl;

-- For each replicated table
GRANT SELECT, INSERT, UPDATE, DELETE ON public.orders TO steep_repl;
GRANT SELECT, INSERT, UPDATE, DELETE ON public.customers TO steep_repl;
-- ... etc

-- For DDL replication (if enabled)
-- Note: Requires superuser or specific privileges
GRANT CREATE ON DATABASE mydb TO steep_repl;

-- pg_hba.conf entry
-- hostssl  replication  steep_repl  100.64.0.0/16  scram-sha-256
-- (Tailscale IP range)
```

### 19.4 Role-Based Access Control (RBAC)

```yaml
replication:
  rbac:
    enabled: true

    roles:
      viewer:
        # Can view replication status, conflicts, ranges
        permissions:
          - view_status
          - view_conflicts
          - view_ranges
          - view_ddl_queue

      operator:
        # Can resolve conflicts, approve DDL
        inherits: viewer
        permissions:
          - resolve_conflicts
          - approve_ddl
          - reject_ddl

      admin:
        # Full control including bypass mode
        inherits: operator
        permissions:
          - enable_bypass
          - manage_ranges
          - failover
          - failback

    # Map PostgreSQL roles to steep_repl roles
    role_mapping:
      steep_admin: admin
      steep_ops: operator
      steep_viewer: viewer
```

#### RBAC Enforcement

```sql
-- steep_repl checks calling role before sensitive operations
CREATE FUNCTION steep_repl.resolve_conflict(
    p_conflict_id BIGINT,
    p_resolution TEXT
) RETURNS BOOLEAN AS $$
BEGIN
    -- Check RBAC
    IF NOT steep_repl.has_permission('resolve_conflicts') THEN
        RAISE EXCEPTION 'Permission denied: resolve_conflicts required';
    END IF;

    -- Log who resolved it
    UPDATE steep_repl.conflict_log
    SET resolution = p_resolution,
        resolved_at = now(),
        resolved_by = format('manual:%s@%s', current_user, inet_client_addr())
    WHERE id = p_conflict_id;

    RETURN true;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;
```

### 19.5 Audit Logging

All security-relevant actions are logged for compliance (SOX, PCI, etc.).

```sql
-- Audit log table
CREATE TABLE steep_repl.audit_log (
    id              BIGSERIAL PRIMARY KEY,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    action          TEXT NOT NULL,
    actor           TEXT NOT NULL,       -- role@host
    target_type     TEXT,                -- conflict, ddl, range, bypass
    target_id       TEXT,
    old_value       JSONB,
    new_value       JSONB,
    client_ip       INET,
    success         BOOLEAN NOT NULL DEFAULT true,
    error_message   TEXT
);

-- Index for compliance queries
CREATE INDEX idx_audit_log_time ON steep_repl.audit_log(occurred_at);
CREATE INDEX idx_audit_log_actor ON steep_repl.audit_log(actor);
CREATE INDEX idx_audit_log_action ON steep_repl.audit_log(action);

-- Audit log retention (configurable)
-- Default: 2 years for compliance
```

#### Audited Actions

| Action | Target | Details Logged |
|--------|--------|----------------|
| `conflict_resolved` | conflict_id | Old resolution, new resolution, who |
| `ddl_approved` | ddl_id | DDL command, approver |
| `ddl_rejected` | ddl_id | DDL command, reason, rejecter |
| `bypass_enabled` | table or global | Duration, reason, who |
| `bypass_disabled` | table or global | Who |
| `failover_initiated` | node | Failed node, promoted node, trigger |
| `range_allocated` | table | Node, range start/end |
| `node_added` | node | Node config |
| `node_removed` | node | Reason |

### 19.6 Certificate Management

For environments not using Tailscale:

```yaml
replication:
  tls:
    enabled: true

    # Server certificate (for gRPC server)
    server:
      cert_file: /etc/steep/certs/server.crt
      key_file: /etc/steep/certs/server.key

    # Client certificate (for connecting to peers)
    client:
      cert_file: /etc/steep/certs/client.crt
      key_file: /etc/steep/certs/client.key

    # CA for verification
    ca_file: /etc/steep/certs/ca.crt

    # Certificate rotation
    rotation:
      check_interval: 24h
      warn_before_expiry: 30d
      critical_before_expiry: 7d
```

#### Certificate Generation Helper

```bash
# steep-repl can generate self-signed certs for testing
steep-repl tls generate --node hq --output /etc/steep/certs/

# For production, use proper PKI (Let's Encrypt, internal CA, etc.)
```

---

## 21. Operations Runbook

### 21.1 Common Scenarios and Resolutions

#### Scenario: High Conflict Rate

```
┌─────────────────────────────────────────────────────────────────┐
│  SCENARIO: Conflict Rate Spike                                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  SYMPTOMS:                                                      │
│  • Alert: "high_conflict_rate" triggered                       │
│  • steep_repl_conflicts_per_minute > threshold                 │
│  • Specific tables showing repeated conflicts                   │
│                                                                 │
│  DIAGNOSIS:                                                     │
│  1. Check which tables are affected:                           │
│     SELECT table_name, count(*), max(detected_at)              │
│     FROM steep_repl.conflict_log                               │
│     WHERE detected_at > now() - interval '1 hour'              │
│     GROUP BY table_name ORDER BY count(*) DESC;                │
│                                                                 │
│  2. Identify conflicting operations:                           │
│     SELECT conflict_type, count(*)                             │
│     FROM steep_repl.conflict_log                               │
│     WHERE detected_at > now() - interval '1 hour'              │
│     GROUP BY conflict_type;                                    │
│                                                                 │
│  3. Check for application issues:                              │
│     - Same record being updated by both nodes?                 │
│     - Batch job running on both nodes simultaneously?          │
│     - Identity range exhaustion causing INSERT-INSERT?         │
│                                                                 │
│  RESOLUTION:                                                    │
│  • INSERT-INSERT: Check identity ranges, allocate new ranges  │
│  • UPDATE-UPDATE: Review application logic, consider node     │
│                   affinity for specific records                │
│  • Batch jobs: Coordinate scheduling, run on one node only    │
│  • If legitimate: Switch to manual resolution for table       │
│                                                                 │
│  COMMANDS:                                                      │
│  steep-repl conflicts show --last 1h                           │
│  steep-repl conflicts summary --group-by table                 │
│  steep-repl range status                                       │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Scenario: Node Unreachable

```
┌─────────────────────────────────────────────────────────────────┐
│  SCENARIO: Peer Node Unreachable                                 │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  SYMPTOMS:                                                      │
│  • Alert: "node_unreachable"                                   │
│  • Replication lag increasing on healthy node                  │
│  • gRPC connection failures in logs                            │
│                                                                 │
│  DIAGNOSIS:                                                     │
│  1. Check network connectivity:                                │
│     tailscale status                        # Is peer online?  │
│     tailscale ping cloud.mynet.ts.net    # Can we reach it? │
│                                                                 │
│  2. Check PostgreSQL on unreachable node (if accessible):      │
│     pg_isready -h cloud.mynet.ts.net -p 5432                │
│                                                                 │
│  3. Check steep-repl daemon on unreachable node:               │
│     ssh cloud steep-repl status                              │
│     ssh cloud journalctl -u steep-repl -n 50                 │
│                                                                 │
│  4. Check replication slot status:                             │
│     SELECT slot_name, active, restart_lsn,                     │
│            pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)  │
│            AS lag_bytes                                        │
│     FROM pg_replication_slots;                                 │
│                                                                 │
│  RESOLUTION:                                                    │
│  • Network issue: Fix Tailscale/VPN, check firewalls          │
│  • PostgreSQL down: Restart PostgreSQL, check logs            │
│  • steep-repl down: Restart daemon, check logs                │
│  • Extended outage: Consider manual failover                   │
│                                                                 │
│  COMMANDS:                                                      │
│  steep-repl node status                                        │
│  steep-repl node check cloud                                 │
│  steep-repl failover --dry-run                                 │
│                                                                 │
│  IF FAILOVER NEEDED:                                            │
│  steep-repl failover --promote hq                              │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Scenario: Identity Range Exhaustion

```
┌─────────────────────────────────────────────────────────────────┐
│  SCENARIO: Identity Range Exhaustion                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  SYMPTOMS:                                                      │
│  • Alert: "range_exhaustion_warning" at 80%, 95%              │
│  • INSERTs failing with constraint violation                   │
│  • Error: "new row violates check constraint steep_range_..."  │
│                                                                 │
│  DIAGNOSIS:                                                     │
│  1. Check current range usage:                                 │
│     SELECT table_name, range_start, range_end,                 │
│            currval(seq_name) as current_value,                 │
│            (currval(seq_name) - range_start)::float /          │
│            (range_end - range_start) * 100 as pct_used         │
│     FROM steep_repl.identity_ranges                            │
│     WHERE status = 'active';                                   │
│                                                                 │
│  2. Check if pre-allocation is working:                        │
│     SELECT * FROM steep_repl.identity_ranges                   │
│     WHERE status = 'pending';                                  │
│                                                                 │
│  RESOLUTION (automatic):                                        │
│  • steep-repl should auto-allocate at 80% threshold           │
│  • If not happening, check daemon logs                         │
│                                                                 │
│  RESOLUTION (manual):                                           │
│  steep-repl range allocate --table orders --size 100000       │
│                                                                 │
│  EMERGENCY (if INSERT blocking):                                │
│  1. Enable bypass temporarily:                                 │
│     steep-repl bypass --enable --table orders --duration 30m  │
│  2. Allocate new range:                                        │
│     steep-repl range allocate --table orders                  │
│  3. Disable bypass:                                            │
│     steep-repl bypass --disable --table orders                │
│                                                                 │
│  PREVENTION:                                                    │
│  • Increase range_size for high-volume tables                  │
│  • Lower threshold_percent (e.g., 70%)                        │
│  • Monitor range_utilization metric                            │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Scenario: Replication Lag Growing

```
┌─────────────────────────────────────────────────────────────────┐
│  SCENARIO: Replication Lag Growing                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  SYMPTOMS:                                                      │
│  • Alert: "replication_lag_critical"                           │
│  • Lag bytes/time increasing over time                         │
│  • Subscriber apply workers busy                               │
│                                                                 │
│  DIAGNOSIS:                                                     │
│  1. Check replication status:                                  │
│     SELECT application_name, state,                            │
│            pg_wal_lsn_diff(sent_lsn, replay_lsn) as lag_bytes,│
│            replay_lag                                          │
│     FROM pg_stat_replication;                                  │
│                                                                 │
│  2. Check apply worker status:                                 │
│     SELECT * FROM pg_stat_subscription;                        │
│                                                                 │
│  3. Identify slow queries on subscriber:                       │
│     SELECT query, state, wait_event                            │
│     FROM pg_stat_activity                                      │
│     WHERE backend_type = 'logical replication worker';         │
│                                                                 │
│  4. Check for lock contention:                                 │
│     SELECT * FROM pg_locks                                     │
│     WHERE NOT granted;                                         │
│                                                                 │
│  RESOLUTION:                                                    │
│  • Slow apply: Add indexes on subscriber, tune apply workers  │
│  • Lock contention: Identify blocking queries, kill if needed │
│  • High write volume: Temporary, wait for catch-up            │
│  • Network issue: Check Tailscale/connectivity                │
│                                                                 │
│  TUNING:                                                        │
│  -- On subscriber                                               │
│  ALTER SUBSCRIPTION steep_sub                                  │
│      SET (streaming = parallel, parallel_apply = 4);          │
│                                                                 │
│  COMMANDS:                                                      │
│  steep-repl lag status                                         │
│  steep-repl lag history --table orders                        │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Scenario: DDL Stuck in Queue

```
┌─────────────────────────────────────────────────────────────────┐
│  SCENARIO: DDL Not Replicating                                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  SYMPTOMS:                                                      │
│  • Schema mismatch between nodes                               │
│  • DDL queue shows PENDING items                               │
│  • Replication errors on missing columns/tables                │
│                                                                 │
│  DIAGNOSIS:                                                     │
│  1. Check DDL queue:                                           │
│     SELECT * FROM steep_repl.ddl_queue                         │
│     WHERE status IN ('PENDING', 'FAILED')                      │
│     ORDER BY captured_at;                                      │
│                                                                 │
│  2. Check for approval requirements:                           │
│     SELECT * FROM steep_repl.ddl_queue                         │
│     WHERE status = 'PENDING'                                   │
│       AND object_type IN ('DROP TABLE', 'DROP COLUMN');        │
│                                                                 │
│  3. Compare schemas:                                           │
│     SELECT * FROM steep_repl.compare_schemas('hq', 'cloud');│
│                                                                 │
│  RESOLUTION:                                                    │
│  • Pending approval: Approve or reject via UI/CLI             │
│     steep-repl ddl approve 42                                  │
│     steep-repl ddl reject 42 --reason "Not needed"            │
│                                                                 │
│  • Failed DDL: Check error, fix, retry                        │
│     steep-repl ddl retry 42                                    │
│                                                                 │
│  • Manual sync: Apply DDL manually on target node             │
│     psql -h cloud -c "ALTER TABLE orders ADD COLUMN ..."    │
│     steep-repl ddl mark-applied 42                            │
│                                                                 │
│  COMMANDS:                                                      │
│  steep-repl ddl list --status pending                          │
│  steep-repl ddl show 42                                        │
│  steep-repl schema compare                                     │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Scenario: Data Validation Failed

```
┌─────────────────────────────────────────────────────────────────┐
│  SCENARIO: Data Validation Detects Divergence                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  SYMPTOMS:                                                      │
│  • Alert: "validation_failed"                                  │
│  • Row count mismatch                                          │
│  • Checksum mismatches on specific rows                        │
│                                                                 │
│  DIAGNOSIS:                                                     │
│  1. Check validation results:                                  │
│     steep-repl validate results --last                         │
│                                                                 │
│  2. If row count mismatch:                                     │
│     steep-repl validate row-counts --table orders             │
│                                                                 │
│  3. If checksum mismatch, get specific rows:                   │
│     steep-repl validate checksums --table orders \            │
│         --output-divergent /tmp/divergent.csv                  │
│                                                                 │
│  4. Examine divergent rows:                                    │
│     -- On HQ                                                    │
│     SELECT * FROM orders WHERE order_id = 12345;              │
│     -- On Cloud                                               │
│     SELECT * FROM orders WHERE order_id = 12345;              │
│                                                                 │
│  RESOLUTION:                                                    │
│  • Single rows: Manually determine correct value, UPDATE      │
│  • Many rows: Use repair script                                │
│     steep-repl validate repair --table orders \               │
│         --source hq --target cloud --dry-run                │
│                                                                 │
│  • If source unclear: Flag for manual review                  │
│     steep-repl validate flag --rows 12345,12346 --reason "..."│
│                                                                 │
│  INVESTIGATION:                                                 │
│  • Check conflict log around divergent row timestamps          │
│  • Check if bypass was enabled                                 │
│  • Check for bugs in application code                          │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 20.2 Maintenance Procedures

#### Weekly Maintenance Checklist

```
┌─────────────────────────────────────────────────────────────────┐
│                    Weekly Maintenance Checklist                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  □ Review conflict log                                          │
│    steep-repl conflicts summary --last 7d                       │
│                                                                 │
│  □ Check identity range utilization                             │
│    steep-repl range status                                      │
│                                                                 │
│  □ Verify validation jobs ran                                   │
│    steep-repl validate history --last 7d                        │
│                                                                 │
│  □ Review DDL queue (clear old entries)                        │
│    steep-repl ddl list --status applied --older-than 30d      │
│    steep-repl ddl prune --older-than 30d                       │
│                                                                 │
│  □ Check replication slot health                               │
│    SELECT slot_name, active,                                   │
│           pg_size_pretty(pg_wal_lsn_diff(                     │
│               pg_current_wal_lsn(), restart_lsn))             │
│    FROM pg_replication_slots;                                  │
│                                                                 │
│  □ Verify backup coordination                                   │
│    steep-repl backup list --last 7d                            │
│                                                                 │
│  □ Check clock synchronization                                  │
│    steep-repl health --check clock                             │
│                                                                 │
│  □ Review audit log for anomalies                              │
│    SELECT action, count(*) FROM steep_repl.audit_log          │
│    WHERE occurred_at > now() - interval '7 days'              │
│    GROUP BY action;                                            │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Adding a New Node

```bash
# 1. Install PostgreSQL and steep_repl extension on new node
sudo apt install postgresql-18
psql -c "CREATE EXTENSION steep_repl;"

# 2. Install and configure steep-repl daemon
./steep-repl install --user
vim ~/.config/steep/config.yaml  # Configure node

# 3. Register node with existing cluster
steep-repl node add --name node_c \
    --host node-c.mynet.ts.net \
    --from-node hq

# 4. Initialize from existing node
steep-repl init node_c --from hq --method snapshot
# (Or use manual backup method for large DBs)

# 5. Verify replication
steep-repl node status
steep-repl validate row-counts

# 6. Update application config to include new node (if needed)
```

#### Removing a Node

```bash
# 1. Pause replication to/from the node
steep-repl node pause node_c

# 2. Wait for queues to drain
steep-repl node drain node_c --timeout 5m

# 3. Remove node from cluster
steep-repl node remove node_c

# 4. Clean up on removed node (optional)
psql -c "DROP EXTENSION steep_repl CASCADE;"
steep-repl uninstall
```

#### Upgrading steep-repl

```bash
# 1. Check current version
steep-repl version

# 2. Stop daemon
steep-repl stop

# 3. Backup config
cp ~/.config/steep/config.yaml ~/.config/steep/config.yaml.bak

# 4. Install new version
# (download or build new binary)
mv steep-repl.new /usr/local/bin/steep-repl

# 5. Run migrations if needed
steep-repl upgrade --check
steep-repl upgrade --apply

# 6. Restart daemon
steep-repl start

# 7. Verify
steep-repl version
steep-repl health
```

### 20.3 Emergency Procedures

#### Emergency: Stop All Replication

```bash
# If replication is causing data corruption or other critical issues

# Option 1: Pause (resumable)
steep-repl pause --all-nodes

# Option 2: Stop daemon on all nodes
steep-repl stop  # On each node

# Option 3: Disable subscriptions in PostgreSQL
psql -c "ALTER SUBSCRIPTION steep_sub DISABLE;"
```

#### Emergency: Force Failover

```bash
# When automatic failover isn't triggering but HQ is clearly down

steep-repl failover --force --promote cloud

# This bypasses the normal timeout and quorum checks
# Use only when you're certain HQ won't come back soon
```

#### Emergency: Bypass Range Constraints

```bash
# When you need to insert data that violates range constraints

# Enable bypass (logged, alerted)
steep-repl bypass --enable --table orders --duration 1h --reason "Emergency bulk insert"

# Do your work
psql -c "COPY orders FROM '/path/to/emergency_data.csv';"

# Disable bypass
steep-repl bypass --disable --table orders

# Allocate new ranges to cover inserted data
steep-repl range reallocate --table orders
```

### 21.4 Slot Cleanup on Node Removal

When permanently removing a node from the replication topology, replication slots must be cleaned up to prevent WAL accumulation.

#### Why Cleanup Matters

Orphaned replication slots cause:
- WAL files accumulating indefinitely (disk full)
- `pg_wal` directory growing unbounded
- Eventually: PostgreSQL refusing writes when disk is full

```sql
-- Check for inactive slots (potential orphans)
SELECT slot_name, active, restart_lsn,
       pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS lag
FROM pg_replication_slots
WHERE active = false;
```

#### Removal Procedure

**Step 1: Verify node is truly being removed**

```bash
steep-repl status --node=node_c

[INFO] Node: node_c
[INFO] Status: DISCONNECTED (last seen: 2 hours ago)
[WARN] Replication slot steep_sub_node_c is inactive

Confirm permanent removal? [y/N]: y
```

**Step 2: Remove node from topology**

```bash
steep-repl remove-node node_c

[INFO] Disabling subscription on node_c (if reachable)...
[INFO] Dropping replication slot steep_sub_node_c on node_a...
[INFO] Dropping replication slot steep_sub_node_c on node_b...
[INFO] Removing node_c from topology metadata...
[INFO] Node removed successfully

Remaining nodes:
• node_a (active)
• node_b (active)
```

**Step 3: Clean up if remove-node fails**

If the node is unreachable and slots can't be cleaned automatically:

```bash
# On each remaining node, manually drop the orphaned slot
steep-repl slot drop --name steep_sub_node_c --force

# Or directly via SQL
psql -c "SELECT pg_drop_replication_slot('steep_sub_node_c');"
```

#### Automatic Slot Monitoring

steep-repl monitors for orphaned slots:

```yaml
# config.yaml
replication:
  slot_cleanup:
    enabled: true
    inactive_threshold: 24h       # Alert if inactive for 24 hours
    auto_drop_threshold: 168h     # Auto-drop after 7 days inactive
    wal_threshold_gb: 50          # Alert if slot holds >50GB WAL
```

```bash
steep-repl slot status

Slot Name              Node      Active   Lag        Age
───────────────────────────────────────────────────────────
steep_sub_node_a       node_a    ✓ yes    12 MB      -
steep_sub_node_b       node_b    ✓ yes    8 MB       -
steep_sub_node_c       node_c    ✗ no     2.3 GB ⚠   47h ⚠

[WARN] steep_sub_node_c inactive for 47 hours
[WARN] steep_sub_node_c holding 2.3 GB of WAL

Recommended action:
  steep-repl remove-node node_c   # If node_c is permanently gone
  steep-repl wake --node=node_c   # If node_c should reconnect
```

#### Pre-Removal Checklist

```bash
steep-repl remove-node --preflight node_c

✓ Node node_c not in active topology
✓ No pending data to sync from node_c
✓ Other nodes don't depend on node_c for coordinator
✓ Identity ranges can be reclaimed

[WARN] node_c has 3 unique rows not yet replicated to other nodes
       These rows will be lost if you proceed.

Proceed anyway? [y/N]:
```

---

## 22. Testing Requirements

### 22.1 Testing Philosophy

**Core Principles:**

1. **No Mocks, Fakes, or Test Doubles** - All tests run against real implementations
2. **Real PostgreSQL Instances** - Integration tests use testcontainers with actual PostgreSQL
3. **Full Topology Testing** - Replication tests set up complete multi-node topologies
4. **70% Code Coverage Target** - Measured across all packages

```
┌─────────────────────────────────────────────────────────────────┐
│                    Testing Philosophy                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ❌ PROHIBITED:                                                 │
│  • Mocks (gomock, mockery, etc.)                               │
│  • Fakes (in-memory implementations)                           │
│  • Test doubles (stubs, spies)                                 │
│  • Interface-based dependency injection for testing            │
│                                                                 │
│  ✓ REQUIRED:                                                   │
│  • Real PostgreSQL via testcontainers                          │
│  • Real steep_repl extension installed                         │
│  • Real steep-repl daemon processes                            │
│  • Real network connections (localhost/Docker network)         │
│  • Real file system operations                                 │
│  • Real IPC (named pipes on Windows, Unix sockets on Linux)   │
│                                                                 │
│  WHY:                                                          │
│  • Mocks test assumptions, not behavior                        │
│  • PostgreSQL behavior cannot be accurately mocked             │
│  • Replication edge cases only surface with real databases    │
│  • Confidence in production comes from testing production-like │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 21.2 Test Categories

#### Unit Tests

Unit tests for pure functions and business logic only. No external dependencies.

```go
// ✓ Good: Pure function, no dependencies
func TestComputeRangeUtilization(t *testing.T) {
    pct := ComputeRangeUtilization(8500, 1, 10000)
    assert.Equal(t, 85.0, pct)
}

// ✓ Good: Parsing logic
func TestParseConflictType(t *testing.T) {
    ct := ParseConflictType("UPDATE_UPDATE")
    assert.Equal(t, ConflictTypeUpdateUpdate, ct)
}

// ❌ Bad: Mocking database
func TestRangeAllocation(t *testing.T) {
    mockDB := new(MockDB)  // PROHIBITED
    mockDB.On("Query", ...).Return(...)
}
```

**Coverage target**: 80% for pure logic packages (`internal/repl/ranges/calc.go`, etc.)

#### Integration Tests

Integration tests run against real PostgreSQL instances using testcontainers.

```go
// internal/repl/ranges/integration_test.go
func TestRangeAllocation_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    ctx := context.Background()

    // Start real PostgreSQL container
    pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image:        "postgres:18",
            ExposedPorts: []string{"5432/tcp"},
            Env: map[string]string{
                "POSTGRES_PASSWORD": "test",
                "POSTGRES_DB":       "steep_test",
            },
            WaitingFor: wait.ForLog("database system is ready to accept connections"),
        },
        Started: true,
    })
    require.NoError(t, err)
    defer pg.Terminate(ctx)

    // Install real extension
    connStr := getConnectionString(pg)
    db, _ := pgx.Connect(ctx, connStr)
    _, err = db.Exec(ctx, "CREATE EXTENSION steep_repl")
    require.NoError(t, err)

    // Test real range allocation
    coordinator := ranges.NewCoordinator(db)
    allocated, err := coordinator.AllocateRange(ctx, "public", "orders", 10000)
    require.NoError(t, err)
    assert.Equal(t, int64(1), allocated.RangeStart)
    assert.Equal(t, int64(10000), allocated.RangeEnd)

    // Verify constraint was created
    var constraintExists bool
    db.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM pg_constraint
            WHERE conname = 'steep_range_orders'
        )
    `).Scan(&constraintExists)
    assert.True(t, constraintExists)
}
```

**Coverage target**: 70% for integration packages

### 21.3 Replication Topology Tests

Full topology tests verify the system works as designed with multiple PostgreSQL nodes.

#### Two-Node Topology Test Setup

```go
// internal/repl/topology/integration_test.go

type TwoNodeTopology struct {
    NodeA       testcontainers.Container
    NodeB       testcontainers.Container
    DaemonA     *exec.Cmd
    DaemonB     *exec.Cmd
    ConnStrA    string
    ConnStrB    string
    GRPCAddrA   string
    GRPCAddrB   string
    network     testcontainers.Network
}

func SetupTwoNodeTopology(t *testing.T) *TwoNodeTopology {
    ctx := context.Background()

    // Create Docker network for node communication
    network, err := testcontainers.GenericNetwork(ctx, testcontainers.GenericNetworkRequest{
        NetworkRequest: testcontainers.NetworkRequest{
            Name:   "steep-test-net",
            Driver: "bridge",
        },
    })
    require.NoError(t, err)

    // Start Node A (PostgreSQL 18)
    nodeA, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image:        "postgres:18",
            ExposedPorts: []string{"5432/tcp"},
            Networks:     []string{"steep-test-net"},
            NetworkAliases: map[string][]string{
                "steep-test-net": {"node-a"},
            },
            Env: map[string]string{
                "POSTGRES_PASSWORD": "test",
                "POSTGRES_DB":       "steep_test",
            },
            Cmd: []string{
                "-c", "wal_level=logical",
                "-c", "max_replication_slots=10",
                "-c", "max_wal_senders=10",
            },
            WaitingFor: wait.ForLog("database system is ready to accept connections"),
        },
        Started: true,
    })
    require.NoError(t, err)

    // Start Node B (PostgreSQL 18)
    nodeB, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image:        "postgres:18",
            ExposedPorts: []string{"5432/tcp"},
            Networks:     []string{"steep-test-net"},
            NetworkAliases: map[string][]string{
                "steep-test-net": {"node-b"},
            },
            Env: map[string]string{
                "POSTGRES_PASSWORD": "test",
                "POSTGRES_DB":       "steep_test",
            },
            Cmd: []string{
                "-c", "wal_level=logical",
                "-c", "max_replication_slots=10",
                "-c", "max_wal_senders=10",
            },
            WaitingFor: wait.ForLog("database system is ready to accept connections"),
        },
        Started: true,
    })
    require.NoError(t, err)

    topo := &TwoNodeTopology{
        NodeA:   nodeA,
        NodeB:   nodeB,
        network: network,
    }

    // Install extension on both nodes
    topo.installExtension(t, nodeA)
    topo.installExtension(t, nodeB)

    // Create test schema on both nodes
    topo.createTestSchema(t)

    // Start steep-repl daemons
    topo.startDaemons(t)

    // Setup bidirectional replication
    topo.setupReplication(t)

    return topo
}

func (topo *TwoNodeTopology) Teardown(t *testing.T) {
    ctx := context.Background()

    // Stop daemons
    if topo.DaemonA != nil {
        topo.DaemonA.Process.Kill()
    }
    if topo.DaemonB != nil {
        topo.DaemonB.Process.Kill()
    }

    // Terminate containers
    topo.NodeA.Terminate(ctx)
    topo.NodeB.Terminate(ctx)
    topo.network.Remove(ctx)
}
```

#### Replication Flow Tests

```go
func TestBidirectionalReplication_InsertFromNodeA(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping topology test")
    }

    topo := SetupTwoNodeTopology(t)
    defer topo.Teardown(t)

    ctx := context.Background()
    dbA, _ := pgx.Connect(ctx, topo.ConnStrA)
    dbB, _ := pgx.Connect(ctx, topo.ConnStrB)

    // Insert on Node A
    _, err := dbA.Exec(ctx, "INSERT INTO orders (order_id, customer_id, status) VALUES (1, 100, 'new')")
    require.NoError(t, err)

    // Wait for replication (with timeout)
    require.Eventually(t, func() bool {
        var count int
        dbB.QueryRow(ctx, "SELECT count(*) FROM orders WHERE order_id = 1").Scan(&count)
        return count == 1
    }, 10*time.Second, 100*time.Millisecond, "row not replicated to Node B")

    // Verify data matches
    var status string
    dbB.QueryRow(ctx, "SELECT status FROM orders WHERE order_id = 1").Scan(&status)
    assert.Equal(t, "new", status)
}

func TestBidirectionalReplication_InsertFromNodeB(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping topology test")
    }

    topo := SetupTwoNodeTopology(t)
    defer topo.Teardown(t)

    ctx := context.Background()
    dbA, _ := pgx.Connect(ctx, topo.ConnStrA)
    dbB, _ := pgx.Connect(ctx, topo.ConnStrB)

    // Insert on Node B (different range)
    _, err := dbB.Exec(ctx, "INSERT INTO orders (order_id, customer_id, status) VALUES (10001, 200, 'pending')")
    require.NoError(t, err)

    // Wait for replication to Node A
    require.Eventually(t, func() bool {
        var count int
        dbA.QueryRow(ctx, "SELECT count(*) FROM orders WHERE order_id = 10001").Scan(&count)
        return count == 1
    }, 10*time.Second, 100*time.Millisecond, "row not replicated to Node A")
}
```

#### Conflict Resolution Tests

```go
func TestConflictDetection_UpdateUpdate(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping topology test")
    }

    topo := SetupTwoNodeTopology(t)
    defer topo.Teardown(t)

    ctx := context.Background()
    dbA, _ := pgx.Connect(ctx, topo.ConnStrA)
    dbB, _ := pgx.Connect(ctx, topo.ConnStrB)

    // Setup: Insert a row and let it replicate
    _, err := dbA.Exec(ctx, "INSERT INTO orders (order_id, customer_id, status) VALUES (1, 100, 'new')")
    require.NoError(t, err)
    waitForReplication(t, dbB, "SELECT count(*) FROM orders WHERE order_id = 1", 1)

    // Pause replication temporarily to create conflict
    pauseReplication(t, topo)

    // Update on Node A
    _, err = dbA.Exec(ctx, "UPDATE orders SET status = 'processing' WHERE order_id = 1")
    require.NoError(t, err)

    // Update same row on Node B (different value)
    _, err = dbB.Exec(ctx, "UPDATE orders SET status = 'shipped' WHERE order_id = 1")
    require.NoError(t, err)

    // Resume replication - conflict should be detected
    resumeReplication(t, topo)

    // Verify conflict was logged
    require.Eventually(t, func() bool {
        var count int
        dbA.QueryRow(ctx, `
            SELECT count(*) FROM steep_repl.conflict_log
            WHERE table_name = 'orders'
              AND conflict_type = 'UPDATE_UPDATE'
        `).Scan(&count)
        return count >= 1
    }, 10*time.Second, 100*time.Millisecond, "conflict not detected")

    // Verify last-write-wins resolution (default)
    var statusA, statusB string
    dbA.QueryRow(ctx, "SELECT status FROM orders WHERE order_id = 1").Scan(&statusA)
    dbB.QueryRow(ctx, "SELECT status FROM orders WHERE order_id = 1").Scan(&statusB)
    assert.Equal(t, statusA, statusB, "nodes should have same value after resolution")
}

func TestConflictDetection_InsertInsert(t *testing.T) {
    // Test INSERT-INSERT conflict when both nodes insert same PK
    // (This should be prevented by identity ranges, test the constraint)
}

func TestConflictResolution_ManualStrategy(t *testing.T) {
    // Test that manual strategy queues conflict for resolution
}

func TestConflictResolution_NodePriority(t *testing.T) {
    // Test that higher priority node wins
}
```

#### Identity Range Tests

```go
func TestIdentityRange_Enforcement(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping topology test")
    }

    topo := SetupTwoNodeTopology(t)
    defer topo.Teardown(t)

    ctx := context.Background()
    dbA, _ := pgx.Connect(ctx, topo.ConnStrA)

    // Node A has range 1-10000
    // Try to insert outside range - should fail
    _, err := dbA.Exec(ctx, "INSERT INTO orders (order_id, customer_id, status) VALUES (10001, 100, 'new')")
    require.Error(t, err)
    assert.Contains(t, err.Error(), "violates check constraint")
}

func TestIdentityRange_AutoExpansion(t *testing.T) {
    // Test that ranges auto-expand at 80% threshold
}

func TestIdentityRange_BypassMode(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping topology test")
    }

    topo := SetupTwoNodeTopology(t)
    defer topo.Teardown(t)

    ctx := context.Background()
    dbA, _ := pgx.Connect(ctx, topo.ConnStrA)

    // Enable bypass
    _, err := dbA.Exec(ctx, "SET steep_repl.bypass_range_check = 'on'")
    require.NoError(t, err)

    // Now insert outside range should work
    _, err = dbA.Exec(ctx, "INSERT INTO orders (order_id, customer_id, status) VALUES (99999, 100, 'bulk')")
    require.NoError(t, err)

    // Verify audit log
    var logCount int
    dbA.QueryRow(ctx, `
        SELECT count(*) FROM steep_repl.audit_log
        WHERE action = 'bypass_enabled'
    `).Scan(&logCount)
    assert.GreaterOrEqual(t, logCount, 1)
}
```

#### DDL Replication Tests

```go
func TestDDLReplication_CreateTable(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping topology test")
    }

    topo := SetupTwoNodeTopology(t)
    defer topo.Teardown(t)

    ctx := context.Background()
    dbA, _ := pgx.Connect(ctx, topo.ConnStrA)
    dbB, _ := pgx.Connect(ctx, topo.ConnStrB)

    // Create table on Node A
    _, err := dbA.Exec(ctx, "CREATE TABLE new_table (id SERIAL PRIMARY KEY, name TEXT)")
    require.NoError(t, err)

    // Wait for DDL to replicate
    require.Eventually(t, func() bool {
        var exists bool
        dbB.QueryRow(ctx, `
            SELECT EXISTS (
                SELECT 1 FROM information_schema.tables
                WHERE table_name = 'new_table'
            )
        `).Scan(&exists)
        return exists
    }, 30*time.Second, 500*time.Millisecond, "table not replicated to Node B")
}

func TestDDLReplication_AlterTableAddColumn(t *testing.T) {
    // Test ALTER TABLE ADD COLUMN replicates
}

func TestDDLReplication_DropTableRequiresApproval(t *testing.T) {
    // Test DROP TABLE enters PENDING state
}
```

#### Failover Tests

```go
func TestFailover_NodeADown(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping topology test")
    }

    topo := SetupTwoNodeTopology(t)
    defer topo.Teardown(t)

    ctx := context.Background()

    // Kill Node A
    topo.NodeA.Terminate(ctx)

    // Wait for Node B to detect and self-promote
    require.Eventually(t, func() bool {
        dbB, _ := pgx.Connect(ctx, topo.ConnStrB)
        var isCoordinator bool
        dbB.QueryRow(ctx, `
            SELECT is_coordinator FROM steep_repl.nodes
            WHERE node_name = 'node_b'
        `).Scan(&isCoordinator)
        return isCoordinator
    }, 60*time.Second, 1*time.Second, "Node B did not become coordinator")

    // Verify Node B can still accept writes
    dbB, _ := pgx.Connect(ctx, topo.ConnStrB)
    _, err := dbB.Exec(ctx, "INSERT INTO orders (order_id, customer_id, status) VALUES (10001, 100, 'failover')")
    require.NoError(t, err)
}

func TestFailback_NodeAReturns(t *testing.T) {
    // Test failback procedure when Node A returns
}
```

### 21.4 Cross-Platform Tests

```go
// Run on Windows
func TestIPC_NamedPipes(t *testing.T) {
    if runtime.GOOS != "windows" {
        t.Skip("Windows-only test")
    }

    // Test named pipe communication
    listener, err := winio.ListenPipe(`\\.\pipe\steep-repl-test`, nil)
    require.NoError(t, err)
    defer listener.Close()

    // ... test IPC communication
}

// Run on Linux/macOS
func TestIPC_UnixSockets(t *testing.T) {
    if runtime.GOOS == "windows" {
        t.Skip("Unix-only test")
    }

    // Test Unix socket communication
    sockPath := filepath.Join(os.TempDir(), "steep-repl-test.sock")
    listener, err := net.Listen("unix", sockPath)
    require.NoError(t, err)
    defer listener.Close()

    // ... test IPC communication
}
```

### 21.5 Extension Tests (Rust/pgrx)

```rust
// extensions/steep_repl/src/tests.rs

#[cfg(any(test, feature = "pg_test"))]
#[pgrx::pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_range_allocation() {
        // Test runs in a real PostgreSQL instance via pgrx
        Spi::run("CREATE TABLE test_orders (id SERIAL PRIMARY KEY)").unwrap();

        let result = Spi::get_one::<i64>(
            "SELECT range_end FROM steep_repl.allocate_range('public', 'test_orders', 10000)"
        ).unwrap();

        assert_eq!(result, Some(10000));
    }

    #[pg_test]
    fn test_check_constraint_created() {
        Spi::run("CREATE TABLE test_orders (id SERIAL PRIMARY KEY)").unwrap();
        Spi::run("SELECT steep_repl.apply_range_constraint('public', 'test_orders', 1, 10000)").unwrap();

        let exists = Spi::get_one::<bool>(
            "SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'steep_range_test_orders')"
        ).unwrap();

        assert_eq!(exists, Some(true));
    }

    #[pg_test]
    fn test_conflict_log_insert() {
        // Test conflict logging
    }

    #[pg_test]
    fn test_ddl_capture() {
        // Test ProcessUtility hook captures DDL
    }
}
```

### 21.6 Test Configuration

```yaml
# .github/workflows/test.yml
name: Tests

on: [push, pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - run: go test -short -coverprofile=coverage.out ./...
      - run: go tool cover -func=coverage.out | grep total | awk '{print $3}'

  integration-tests:
    strategy:
      matrix:
        os: [ubuntu-latest, windows-latest, macos-latest]
        pg: [16, 17, 18]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - name: Run integration tests
        run: go test -v -timeout 30m ./internal/repl/...
        env:
          POSTGRES_VERSION: ${{ matrix.pg }}

  topology-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - name: Run topology tests
        run: go test -v -timeout 60m -run 'Topology|Replication|Failover' ./...

  extension-tests:
    strategy:
      matrix:
        pg: [16, 17, 18]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: dtolnay/rust-toolchain@stable
      - run: cargo install cargo-pgrx
      - run: cargo pgrx init --pg${{ matrix.pg }} download
      - run: cargo pgrx test pg${{ matrix.pg }}
        working-directory: extensions/steep_repl

  coverage-check:
    needs: [unit-tests, integration-tests]
    runs-on: ubuntu-latest
    steps:
      - name: Check coverage threshold
        run: |
          COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
          if (( $(echo "$COVERAGE < 70" | bc -l) )); then
            echo "Coverage $COVERAGE% is below 70% threshold"
            exit 1
          fi
```

### 21.7 Test Helpers

```go
// internal/testutil/topology.go
package testutil

import (
    "context"
    "testing"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/stretchr/testify/require"
)

// WaitForReplication waits for a query to return expected count
func WaitForReplication(t *testing.T, db *pgx.Conn, query string, expected int) {
    t.Helper()
    ctx := context.Background()

    require.Eventually(t, func() bool {
        var count int
        db.QueryRow(ctx, query).Scan(&count)
        return count == expected
    }, 30*time.Second, 100*time.Millisecond, "replication timeout")
}

// WaitForConflict waits for a conflict to be logged
func WaitForConflict(t *testing.T, db *pgx.Conn, tableName, conflictType string) {
    t.Helper()
    ctx := context.Background()

    require.Eventually(t, func() bool {
        var count int
        db.QueryRow(ctx, `
            SELECT count(*) FROM steep_repl.conflict_log
            WHERE table_name = $1 AND conflict_type = $2
        `, tableName, conflictType).Scan(&count)
        return count >= 1
    }, 30*time.Second, 100*time.Millisecond, "conflict not detected")
}

// AssertNodesInSync verifies both nodes have identical data
func AssertNodesInSync(t *testing.T, dbA, dbB *pgx.Conn, table string) {
    t.Helper()
    ctx := context.Background()

    var countA, countB int
    dbA.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&countA)
    dbB.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&countB)

    require.Equal(t, countA, countB, "row counts differ between nodes")

    // Checksum comparison
    var hashA, hashB string
    query := "SELECT md5(string_agg(t::text, '' ORDER BY 1)) FROM " + table + " t"
    dbA.QueryRow(ctx, query).Scan(&hashA)
    dbB.QueryRow(ctx, query).Scan(&hashB)

    require.Equal(t, hashA, hashB, "data checksums differ between nodes")
}
```

### 21.8 Coverage Requirements by Package

| Package | Coverage Target | Test Type |
|---------|-----------------|-----------|
| `internal/repl/ranges` | 75% | Unit + Integration |
| `internal/repl/conflicts` | 75% | Unit + Integration |
| `internal/repl/ddl` | 70% | Integration |
| `internal/repl/topology` | 70% | Topology |
| `internal/repl/ipc` | 70% | Integration |
| `internal/repl/grpc` | 70% | Integration |
| `internal/repl/daemon` | 65% | Integration |
| `extensions/steep_repl` | 70% | pgrx pg_test |
| **Overall** | **70%** | All |

### 21.9 Makefile Targets

```makefile
# Test targets
.PHONY: test test-short test-integration test-topology test-extension test-coverage

test: test-short test-integration test-topology test-extension

test-short:
	go test -short -v ./...

test-integration:
	go test -v -timeout 30m ./internal/repl/...

test-topology:
	go test -v -timeout 60m -run 'Topology|Replication|Failover' ./...

test-extension:
	cd extensions/steep_repl && cargo pgrx test pg18

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | grep total
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	if [ $$(echo "$$COVERAGE < 70" | bc) -eq 1 ]; then \
		echo "Coverage $$COVERAGE% is below 70% threshold"; \
		exit 1; \
	fi

test-coverage-html:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
```

---

## Changelog

| Version | Date | Changes |
|---------|------|---------|
| 0.9 | 2025-12-03 | Added Section 21: Testing Requirements (no mocks/fakes/doubles, real PostgreSQL testcontainers, full topology tests, 70% coverage target) |
| 0.8 | 2025-12-03 | Resolved all open questions in Section 15: coordinator failover (no Raft, state in PG), clock sync (NTP + commit timestamps), large transactions (bulk resolution UI), schema versioning (fingerprints), conflict rollback (revert function) |
| 0.7 | 2025-12-03 | Added PG15+ requirement and limitations for row/column filtering; removed vendor-specific references for public consumption |
| 0.6 | 2025-12-03 | Added Sections 17-20: Production Readiness (validation, clock sync, failover, backup, notifications, WAL sizing), Networking (Tailscale integration), Security (credentials, RBAC, audit), Operations Runbook |
| 0.5 | 2025-12-03 | Added Section 6: Filtering (table/row/column via PG native); Section 7: Monitoring and Health Checks; 10-week MVP scope defined |
| 0.4 | 2025-12-03 | Added Section 5: Node Initialization and Snapshots (automatic snapshot, manual backup, reinitialization, schema sync, state machine) |
| 0.3 | 2025-12-03 | Added cross-platform compatibility section (Windows first); named pipes vs Unix sockets; pgrx Windows build; fixed section numbering |
| 0.2 | 2025-12-03 | Clarified steep-repl/steep-agent separation; added constraint bypass mechanism |
| 0.1 | 2025-12-03 | Initial draft |
