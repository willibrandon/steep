# Research: Tables & Statistics Viewer

**Feature**: 005-tables-statistics
**Date**: 2025-11-24
**Status**: Complete

## Visual Design Research

### Reference Tools Studied

#### 1. pg_top - PostgreSQL Activity Monitor
- **Location**: `/Users/brandon/src/pg_top`
- **Relevant Pattern**: Table-based display with sortable columns
- **Observation**: Uses simple tabular layout with header row, no tree hierarchy
- **Applicable Learning**: Column alignment, sort indicators, fixed-width formatting

#### 2. htop - Process Viewer
- **Location**: `/Users/brandon/src/htop`
- **Relevant Pattern**: Tree rendering for process hierarchy
- **Observation**: Uses ASCII tree characters (├──, └──, │) for parent-child relationships
- **Applicable Learning**: Tree indentation using `├──` and `└──` prefixes, expandable nodes with `+`/`-` indicators

#### 3. k9s - Kubernetes TUI
- **Location**: `/Users/brandon/src/k9s`
- **Relevant Pattern**: Resource browser with expand/collapse, details panel
- **Observation**: Two-panel layout (list + details), breadcrumb navigation
- **Applicable Learning**: Tab-based panels, modal details overlay, keyboard-driven navigation

### Visual Design Decision

**Decision**: Tabular layout with inline tree hierarchy (htop-style)
**Rationale**:
- Steep's existing views (Queries, Locks) use tabular layouts
- Tree hierarchy rendered inline with indentation prefixes
- Details panel as modal overlay (consistent with existing detail views)
- Avoids complexity of multi-column tree widgets

**Alternatives Rejected**:
- Full tree widget (bubbles doesn't have native tree support)
- Split pane layout (adds complexity, deviates from existing patterns)

### ASCII Mockup

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ steep - Tables [5]                            postgres@localhost  12:34:56  │
├─────────────────────────────────────────────────────────────────────────────┤
│ Schema/Table                      Size (MB)    Rows    Bloat %    Cache %   │
├─────────────────────────────────────────────────────────────────────────────┤
│ ▼ public                                                                    │
│   ├─ users                            12.5    1,234      5.2%      98.5%   │
│   ├─ orders                          156.8   45,678     12.3%      95.2%   │
│   │  └─ orders_2024_q1                45.2   12,345      8.1%      96.1%   │
│   │  └─ orders_2024_q2                52.1   14,567      9.2%      95.8%   │
│   └─ products                         34.2    5,678      2.1%      99.1%   │
│ ▶ inventory (3 tables)                                                      │
│ ▶ analytics (12 tables)                                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│ j/k:nav  Enter:expand/details  s:sort  S:direction  P:sys schemas  h:help  │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Layout Components**:
1. Status bar (top) - view name, connection, timestamp
2. Column headers - sortable columns with indicator
3. Tree content - schemas with expand/collapse, tables indented
4. Partitions - child partitions nested under parent
5. Footer - keyboard hints

**Color Coding**:
- Red: Bloat >20% (critical)
- Yellow: Bloat 10-20% (warning), Unused indexes
- Green: Healthy (<10% bloat)
- Muted: Collapsed schemas, partition children

---

## PostgreSQL Query Research

### 1. Table Statistics Query

**Decision**: Combined query using pg_stat_all_tables + pg_statio_all_tables + pg_total_relation_size()

**Query** (~250ms execution):
```sql
SELECT
  nsp.nspname as schema_name,
  t.relname as table_name,
  t.oid as table_oid,
  pg_total_relation_size(t.oid) as total_size_bytes,
  pg_relation_size(t.oid) as table_size_bytes,
  pg_indexes_size(t.oid) as indexes_size_bytes,
  COALESCE(pg_total_relation_size(t.reltoastrelid), 0) as toast_size_bytes,
  COALESCE(s.n_live_tup, 0) as row_count,
  COALESCE(s.n_dead_tup, 0) as dead_rows,
  COALESCE(
    ROUND(100.0 * io.heap_blks_hit /
      NULLIF(io.heap_blks_hit + io.heap_blks_read, 0), 2),
    0) as cache_hit_ratio,
  s.last_vacuum,
  s.last_analyze,
  t.relkind
FROM pg_class t
JOIN pg_namespace nsp ON nsp.oid = t.relnamespace
LEFT JOIN pg_stat_all_tables s ON s.relid = t.oid
LEFT JOIN pg_statio_all_tables io ON io.relid = t.oid
WHERE t.relkind IN ('r', 'p')  -- regular tables and partitioned tables
ORDER BY nsp.nspname, t.relname;
```

**Rationale**:
- `pg_total_relation_size()` gives total including indexes and TOAST (per spec)
- `COALESCE()` handles tables with no stats (newly created)
- `NULLIF()` prevents division by zero in cache hit calculation
- `relkind IN ('r', 'p')` includes both regular and partitioned tables
- No LIMIT needed for schema filtering (done in Go for toggle support)

**Alternatives Rejected**:
- Using pg_table_size() - doesn't include indexes
- Querying size functions separately - multiple round trips

### 2. Index Statistics Query

**Decision**: Query pg_stat_all_indexes with unused detection

**Query** (~150ms execution):
```sql
SELECT
  nsp.nspname as schema_name,
  t.relname as table_name,
  idx.relname as index_name,
  idx.oid as index_oid,
  pg_relation_size(idx.oid) as index_size_bytes,
  COALESCE(s.idx_scan, 0) as scan_count,
  COALESCE(s.idx_tup_read, 0) as rows_read,
  COALESCE(s.idx_tup_fetch, 0) as rows_fetched,
  COALESCE(
    ROUND(100.0 * io.idx_blks_hit /
      NULLIF(io.idx_blks_hit + io.idx_blks_read, 0), 2),
    0) as cache_hit_ratio,
  i.indisprimary as is_primary,
  i.indisunique as is_unique
FROM pg_index i
JOIN pg_class idx ON i.indexrelid = idx.oid
JOIN pg_class t ON i.indrelid = t.oid
JOIN pg_namespace nsp ON nsp.oid = t.relnamespace
LEFT JOIN pg_stat_all_indexes s ON s.indexrelid = idx.oid
LEFT JOIN pg_statio_all_indexes io ON io.indexrelid = idx.oid
WHERE t.relkind IN ('r', 'p')
ORDER BY nsp.nspname, t.relname, idx.relname;
```

**Rationale**:
- `idx_scan = 0` identifies unused indexes (highlighted yellow per spec)
- Include `indisprimary` and `indisunique` for context in details
- Join through pg_class for consistent schema filtering

### 3. Partition Hierarchy Detection

**Decision**: Use pg_inherits with relkind filtering

**Query** (~50ms execution):
```sql
SELECT
  parent.oid as parent_oid,
  child.oid as child_oid,
  parent_ns.nspname as parent_schema,
  parent.relname as parent_table,
  child_ns.nspname as child_schema,
  child.relname as child_table
FROM pg_inherits i
JOIN pg_class parent ON parent.oid = i.inhparent
JOIN pg_namespace parent_ns ON parent_ns.oid = parent.relnamespace
JOIN pg_class child ON child.oid = i.inhrelid
JOIN pg_namespace child_ns ON child_ns.oid = child.relnamespace
WHERE parent.relkind = 'p'  -- partitioned parent
ORDER BY parent_ns.nspname, parent.relname, child.relname;
```

**Rationale**:
- `relkind = 'p'` ensures we only get partitioned tables as parents
- Returns parent→child relationships for building tree structure
- Lightweight query suitable for frequent refresh

### 4. pgstattuple Bloat Detection

**Extension Check**:
```sql
SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'pgstattuple');
```

**Bloat Calculation** (per table, can be slow for large tables):
```sql
SELECT
  dead_tuple_percent as bloat_percentage
FROM pgstattuple($1);  -- $1 = table OID or 'schema.table'
```

**Decision**: Use estimated bloat from pg_stat_all_tables as primary, pgstattuple as optional enhancement

**Estimated Bloat Query** (fast fallback):
```sql
SELECT
  t.oid,
  ROUND(100.0 * COALESCE(s.n_dead_tup, 0) /
    NULLIF(COALESCE(s.n_live_tup, 0) + COALESCE(s.n_dead_tup, 0), 0),
    2) as estimated_bloat_pct
FROM pg_class t
LEFT JOIN pg_stat_all_tables s ON s.relid = t.oid
WHERE t.relkind IN ('r', 'p');
```

**Rationale**:
- pgstattuple can take minutes for large tables - not suitable for 30s refresh
- Estimated bloat from dead_tuple ratio is good enough for most use cases
- pgstattuple runs on-demand for selected table only (details panel)
- Extension auto-install with user confirmation per spec

**Alternatives Rejected**:
- Running pgstattuple on all tables - too slow
- Excluding bloat entirely - reduces feature value

### 5. System Schema Filtering

**Decision**: Filter in application layer for toggle support

**Pattern**: Store `showSystemSchemas` boolean in view state, filter results in Go:
```go
if !v.showSystemSchemas {
    filtered := schemas[:0]
    for _, s := range schemas {
        if s.Name != "pg_catalog" && s.Name != "information_schema" && !strings.HasPrefix(s.Name, "pg_toast") {
            filtered = append(filtered, s)
        }
    }
    schemas = filtered
}
```

**Rationale**:
- Filtering in Go allows instant toggle without re-query
- Fetch all schemas once, filter as needed
- Consistent with 30s refresh cycle

### 6. Table Details Queries

**Column Definitions**:
```sql
SELECT
  a.attnum as position,
  a.attname as column_name,
  pg_catalog.format_type(a.atttypid, a.atttypmod) as data_type,
  NOT a.attnotnull as is_nullable,
  pg_get_expr(d.adbin, d.adrelid) as default_value
FROM pg_attribute a
LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE a.attrelid = $1  -- table OID
  AND a.attnum > 0
  AND NOT a.attisdropped
ORDER BY a.attnum;
```

**Constraints**:
```sql
SELECT
  con.conname as constraint_name,
  CASE con.contype
    WHEN 'p' THEN 'PRIMARY KEY'
    WHEN 'u' THEN 'UNIQUE'
    WHEN 'f' THEN 'FOREIGN KEY'
    WHEN 'c' THEN 'CHECK'
  END as constraint_type,
  pg_get_constraintdef(con.oid) as definition
FROM pg_constraint con
WHERE con.conrelid = $1  -- table OID
ORDER BY con.contype, con.conname;
```

**Rationale**:
- Load on-demand when details panel opens
- <50ms execution, well within budget
- Uses table OID for efficient lookup

---

## Bubbletea Pattern Research

### Tree Rendering Approach

**Decision**: Custom tree rendering with string prefixes

**Pattern** (from locks/queries views):
```go
func (v *TablesView) renderTreeRow(item TreeItem, isLast bool) string {
    var prefix string
    if item.IsSchema {
        if item.Expanded {
            prefix = "▼ "
        } else {
            prefix = "▶ "
        }
    } else if item.IsPartition {
        prefix = "   └─ "  // child partition
    } else {
        if isLast {
            prefix = "   └─ "
        } else {
            prefix = "   ├─ "
        }
    }
    return prefix + item.Name
}
```

**Rationale**:
- No external tree library needed
- Consistent with Steep's simple rendering approach
- Unicode characters render well in modern terminals

### Multi-Panel Layout

**Decision**: Modal overlay for details (consistent with existing views)

**Pattern** (from locks view ModeDetail):
```go
func (v *TablesView) View() string {
    if v.mode == ModeDetails {
        return v.renderWithOverlay(v.renderDetailsPanel())
    }
    return v.renderMainView()
}
```

**Rationale**:
- Locks view already uses modal overlay for detail view
- Avoids split-pane complexity
- Full width for details panel content

### Focus Management

**Decision**: Tab key switches focus between table list and index list

**Pattern**:
```go
type FocusPanel int
const (
    FocusTables FocusPanel = iota
    FocusIndexes
)

func (v *TablesView) handleTab() {
    if v.focusPanel == FocusTables {
        v.focusPanel = FocusIndexes
    } else {
        v.focusPanel = FocusTables
    }
}
```

---

## Implementation Strategy

### Query Execution Timing

| Query | Estimated Time | When Run |
|-------|---------------|----------|
| Table statistics | ~250ms | Initial load, 30s refresh |
| Index statistics | ~150ms | Parallel with tables |
| Partition hierarchy | ~50ms | Parallel with tables |
| Extension check | ~10ms | Initial load only |
| Table details | ~50ms | On-demand (Enter/d) |
| Bloat (pgstattuple) | 1-60s per table | Background, selected table only |

### Total Initial Load: ~300ms (well under 500ms budget)

### Data Flow

1. **View Init**: Check pgstattuple extension, load tables/indexes/partitions in parallel
2. **30s Refresh**: Reload tables/indexes, update display
3. **User Selection**: Load details on-demand
4. **Bloat Request**: Background goroutine with spinner for pgstattuple

---

## Technologies Confirmed

- **Go 1.21+**: Already in use
- **pgx/pgxpool**: Already in use for database queries
- **bubbletea/bubbles/lipgloss**: Already in use for UI
- **golang.design/x/clipboard**: Already in use for copy functionality

No new dependencies required.
