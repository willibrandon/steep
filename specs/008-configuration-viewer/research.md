# Research: Configuration Viewer

**Feature**: 008-configuration-viewer
**Date**: 2025-11-27

## Research Summary

This document consolidates research findings for implementing the PostgreSQL Configuration Viewer in Steep.

---

## 1. pg_settings Schema

### Decision: Use all 17 columns from pg_settings view

**Rationale**: The pg_settings view provides comprehensive parameter information. All columns are useful for either display or filtering.

**Columns Used**:

| Column | Type | Use in Viewer |
|--------|------|---------------|
| `name` | text | Primary identifier, table column |
| `setting` | text | Current value, table column |
| `unit` | text | Display unit after value |
| `category` | text | Filter/group criteria |
| `short_desc` | text | Table column (truncated) |
| `extra_desc` | text | Detail view only |
| `context` | text | Detail view, restart indicator |
| `vartype` | text | Detail view type info |
| `source` | text | Detail view, where value was set |
| `min_val` | text | Detail view constraints |
| `max_val` | text | Detail view constraints |
| `enumvals` | text[] | Detail view for enum params |
| `boot_val` | text | Compare for modified highlight |
| `reset_val` | text | Detail view |
| `sourcefile` | text | Detail view (config file location) |
| `sourceline` | int | Detail view (line number) |
| `pending_restart` | bool | Warning indicator |

**Alternatives Considered**:
- Subset of columns: Rejected because users need full context for understanding parameters
- Custom aggregated query: Rejected because pg_settings already provides optimal structure

---

## 2. Context Types

### Decision: Display context with color-coded restart requirements

**Context Values and Display Strategy**:

| Context | Display | Color | Meaning |
|---------|---------|-------|---------|
| `internal` | "Read-only" | Gray | Cannot be changed |
| `postmaster` | "Restart Required" | Red | Server restart needed |
| `sighup` | "Reload Required" | Yellow | `pg_reload_conf()` needed |
| `backend` | "New Connections" | Cyan | Takes effect on new sessions |
| `superuser` | "Superuser Session" | Blue | Immediate, superuser only |
| `user` | "User Session" | Green | Immediate, any user |

**Rationale**: Color coding helps DBAs quickly understand the impact of changing a parameter.

---

## 3. Variable Types (vartype)

### Decision: Parse and validate based on vartype for detail view

| vartype | Parsing | Display | Constraints |
|---------|---------|---------|-------------|
| `bool` | Accept on/off | "On"/"Off" | None |
| `integer` | Parse as int | Number with unit | min_val, max_val |
| `real` | Parse as float | Decimal with unit | min_val, max_val |
| `string` | As-is | Text | None in pg_settings |
| `enum` | Match enumvals | Current value | enumvals array |

**Rationale**: Different types need different presentation in detail view.

---

## 4. Parameter Categories

### Decision: Use flat category list with sub-category support

PostgreSQL organizes ~350 parameters into these categories (from pg_settings):

1. Autovacuum
2. Client Connection Defaults / Locale and Formatting
3. Client Connection Defaults / Other Defaults
4. Client Connection Defaults / Statement Behavior
5. Connections and Authentication / Authentication
6. Connections and Authentication / Connection Settings
7. Connections and Authentication / SSL
8. Customized Options
9. Developer Options
10. Error Handling
11. File Locations
12. Lock Management
13. Preset Options
14. Query Tuning / Genetic Query Optimizer
15. Query Tuning / Other Planner Options
16. Query Tuning / Planner Cost Constants
17. Query Tuning / Planner Method Configuration
18. Replication / Primary Server
19. Replication / Sending Servers
20. Replication / Standby Servers
21. Replication / Subscribers
22. Reporting and Logging / Process Title
23. Reporting and Logging / What to Log
24. Reporting and Logging / When to Log
25. Reporting and Logging / Where to Log
26. Resource Usage / Asynchronous Behavior
27. Resource Usage / Background Writer
28. Resource Usage / Cost-Based Vacuum Delay
29. Resource Usage / Disk
30. Resource Usage / Kernel Resources
31. Resource Usage / Memory
32. Statistics / Cumulative Query and Index Statistics
33. Statistics / Monitoring
34. Version and Platform Compatibility / Other Platforms and Clients
35. Version and Platform Compatibility / Previous PostgreSQL Versions
36. Write-Ahead Log / Archive Recovery
37. Write-Ahead Log / Archiving
38. Write-Ahead Log / Checkpoints
39. Write-Ahead Log / Recovery
40. Write-Ahead Log / Recovery Target
41. Write-Ahead Log / Settings
42. Write-Ahead Log / Summarization

**Filter Implementation**:
- Primary filter: Top-level category (before " / ")
- Secondary filter: Full category string (optional)

**Rationale**: ~40+ categories need simplification for filter dropdown; top-level grouping reduces to ~15 categories.

---

## 5. Modified Parameter Detection

### Decision: Compare `setting` != `boot_val` for yellow highlighting

```sql
SELECT name, setting, boot_val,
       CASE WHEN setting != boot_val THEN true ELSE false END AS is_modified
FROM pg_settings
ORDER BY name;
```

**Rationale**: boot_val represents the compiled-in default value. Any difference indicates customization.

**Edge Cases**:
- NULL boot_val: Some internal parameters have NULL boot_val; treat as not modified
- Type coercion: Both are text, so string comparison is safe

---

## 6. Visual Design Reference

### Reference Tools Studied

1. **pg_top** (`/Users/brandon/src/pg_top`): Process list with sort columns, highlighting
2. **k9s** (`/Users/brandon/src/k9s`): Resource browser with search, detail panels
3. **htop** (`/Users/brandon/src/htop`): Color-coded status, scrollable lists

### Proposed Layout (ASCII Mockup)

```
┌─ Configuration Viewer ─────────────────────────────────────────────────────┐
│ Filter: [All Categories ▼]  Search: /shared                   350 params   │
├────────────────────────────────────────────────────────────────────────────┤
│ NAME                     VALUE          UNIT   CATEGORY            DESC    │
│────────────────────────────────────────────────────────────────────────────│
│▶shared_buffers          128MB          8kB    Resource Usage / M… Sets n…│
│ shared_memory_type      mmap                  Preset Options       Select…│
│ shared_preload_librar…  (empty)              Client Connection…   Lists s…│
│ ssl                     off                   Connections and A…   Enable…│
│ ssl_ca_file             (empty)              Connections and A…   Locati…│
│ ssl_cert_file           server.crt           Connections and A…   Locati…│
│ ...                                                                        │
├────────────────────────────────────────────────────────────────────────────┤
│ [↑↓] Navigate  [/] Search  [c] Category  [d] Details  [s] Sort  [?] Help  │
└────────────────────────────────────────────────────────────────────────────┘
```

**Detail View (d key)**:

```
┌─ Parameter Details ────────────────────────────────────────────────────────┐
│                                                                            │
│  Name:        shared_buffers                                               │
│  Value:       128MB                                                        │
│  Unit:        8kB blocks                                                   │
│  Type:        integer                                                      │
│  Context:     postmaster (⚠ Restart Required)                              │
│                                                                            │
│  Default:     128MB                                                        │
│  Range:       16 - 1073741823                                              │
│  Source:      configuration file                                           │
│  File:        /var/lib/postgresql/data/postgresql.conf:121                 │
│                                                                            │
│  Description:                                                              │
│  Sets the number of shared memory buffers used by the server.              │
│                                                                            │
│  This controls the amount of memory the database server uses for           │
│  shared memory buffers. The default is typically 128 megabytes (128MB),    │
│  but might be less if your kernel settings will not support it.            │
│                                                                            │
├────────────────────────────────────────────────────────────────────────────┤
│ [Esc/q] Back  [y] Copy name  [Y] Copy value                                │
└────────────────────────────────────────────────────────────────────────────┘
```

**Visual Acceptance Criteria**:
- Modified parameters (setting != boot_val) highlighted in yellow
- Parameters with pending_restart=true show warning icon/color
- Clean column alignment with truncation for long values
- Smooth scrolling through ~350 parameters
- Search highlights matching text

---

## 7. Query Performance

### Decision: Single query with client-side filtering

```sql
SELECT
    name, setting, unit, category, short_desc, extra_desc,
    context, vartype, source, min_val, max_val, enumvals,
    boot_val, reset_val, sourcefile, sourceline, pending_restart
FROM pg_settings
ORDER BY name;
```

**Performance Analysis**:
- Row count: ~350 parameters (fixed, not data-dependent)
- Query time: < 10ms (no joins, no aggregations)
- Memory: ~100KB for full result set
- Refresh: Every 60 seconds (config rarely changes)

**Rationale**: pg_settings is a virtual view reading from shared memory; extremely fast. Client-side filtering avoids repeated queries.

**Alternatives Considered**:
- Server-side filtering: Rejected; overhead of query compilation exceeds filtering cost
- Prepared statements: Not needed; single simple query

---

## 8. Export Format

### Decision: Plain text key=value format (PostgreSQL conf style)

**Export Format**:
```
# PostgreSQL Configuration Export
# Generated by Steep on 2025-11-27 14:30:00
# Server: postgresql://localhost:5432/mydb
# Total parameters: 350 (42 modified from defaults)

# Modified parameters only (use :export config all <file> for all)

shared_buffers = '128MB'          # Sets the number of shared memory buffers
work_mem = '4MB'                  # Sets the maximum memory for query operations
maintenance_work_mem = '64MB'     # Sets the maximum memory for maintenance ops
...
```

**Alternatives Considered**:
- JSON: More machine-readable but less familiar to DBAs
- YAML: Good structure but overkill for flat key-value
- CSV: Good for import to spreadsheet but loses comments

**Rationale**: DBAs are familiar with postgresql.conf format. Easy to copy into config files.

---

## 9. Keyboard Bindings

### Decision: Follow existing Steep conventions

| Key | Action | Notes |
|-----|--------|-------|
| `8` | Open Configuration view | From main navigation |
| `j/k` or `↓/↑` | Navigate list | Vim-style |
| `g/G` | Go to top/bottom | Vim-style |
| `/` | Enter search mode | Filter by name/description |
| `Esc` | Clear search/exit mode | Standard |
| `c` | Open category filter | Dropdown selection |
| `d` | Open detail view | Show full parameter info |
| `s` | Cycle sort column | Name → Category → Modified |
| `S` | Toggle sort direction | Asc/Desc |
| `y` | Copy parameter name | To clipboard |
| `Y` | Copy parameter value | To clipboard |
| `:export config <file>` | Export configuration | Command mode |
| `?` | Show help | Standard |
| `q` | Close view/return | Standard |

**Rationale**: Consistent with existing Steep views (locks, tables, queries).

---

## 10. Version Compatibility

### Decision: Support PostgreSQL 11+ (per constitution)

**Version-specific handling**:
- `pending_restart` column: Available since PostgreSQL 9.5
- All other columns: Stable since PostgreSQL 8.3

**No version branching needed**: All required columns exist in PostgreSQL 11+.

---

## Unresolved Items

None. All technical decisions made based on research.
