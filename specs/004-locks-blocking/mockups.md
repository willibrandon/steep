# Visual Mockups: Locks & Blocking Detection

**Feature Branch**: `004-locks-blocking`
**Date**: 2025-11-22

## Reference Study Summary

### pg_top Lock Display
- Shows locks per process with columns: database, schema, table, index, type, granted
- Simple tabular format with pipe separators
- No color coding for blocking relationships

### htop Process Tree
- Tree hierarchy using box-drawing characters (├──, └──, │)
- Indentation shows parent-child relationships
- Color coding for different process states

### k9s Kubernetes TUI
- Color coding: red for errors, yellow for warnings, green for healthy
- Split views with table on top
- Confirmation dialogs for destructive actions

## ASCII Mockup: Locks View (Primary Design)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Steep - Locks                                                    [?] Help   │
├─────────────────────────────────────────────────────────────────────────────┤
│ Locks: 12  |  Blocking: 2  |  Blocked: 3  |  Last Update: 14:32:05          │
├─────────────────────────────────────────────────────────────────────────────┤
│  PID   │ Type       │ Mode               │ Grant │ DB       │ Relation     │
├────────┼────────────┼────────────────────┼───────┼──────────┼──────────────┤
│  1234  │ relation   │ AccessExclusiveLo… │  Yes  │ mydb     │ users        │ ← Yellow (blocking)
│  5678  │ relation   │ RowExclusiveLock   │  No   │ mydb     │ users        │ ← Red (blocked)
│  9012  │ relation   │ RowExclusiveLock   │  No   │ mydb     │ users        │ ← Red (blocked)
│  3456  │ relation   │ RowExclusiveLock   │  Yes  │ mydb     │ orders       │
│  7890  │ transacti… │ ExclusiveLock      │  Yes  │ mydb     │              │
│  2345  │ relation   │ AccessShareLock    │  Yes  │ mydb     │ products     │
├─────────────────────────────────────────────────────────────────────────────┤
│ Lock Dependency Tree                                                         │
├─────────────────────────────────────────────────────────────────────────────┤
│ ├── [PID:1234 AccessExclusiveLock] ALTER TABLE users ADD COLUMN email...    │
│ │   ├── [PID:5678 waiting] UPDATE users SET status = 'active' WHERE...      │
│ │   └── [PID:9012 waiting] DELETE FROM users WHERE id = 123                 │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
│ [s]ort [d]etail [x]kill [j/k]nav [?]help [1-6]views                          │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Color Scheme

| Element | Color | Hex | Description |
|---------|-------|-----|-------------|
| Blocked row | Red foreground | #FF5555 | Query waiting for lock |
| Blocking row | Yellow foreground | #FFB86C | Query holding lock that blocks others |
| Normal row | Default | - | Query with lock, not involved in blocking |
| Selected row | Inverse | - | Currently selected row |
| Header | Bold | - | Column headers |
| Tree metadata | Cyan | #8BE9FD | PID and lock mode in brackets |

## Layout Specifications

### Table Section (Top 60% of available height)

| Column | Min Width | Max Width | Alignment | Notes |
|--------|-----------|-----------|-----------|-------|
| PID | 6 | 8 | Right | Process ID |
| Type | 10 | 12 | Left | Lock type (truncate) |
| Mode | 18 | 22 | Left | Lock mode (truncate) |
| Grant | 5 | 7 | Center | Yes/No |
| DB | 8 | 12 | Left | Database name |
| Relation | Flex | Remaining | Left | Table/index name |

### Tree Section (Bottom 40% of available height)

- Uses treeprint library for rendering
- Each node shows: `[PID:XXXX LockMode] Query (truncated)`
- Indentation: 2 spaces per level
- Box-drawing characters: ├── └── │

### Minimum Terminal Size

- Width: 80 characters
- Height: 24 lines
- At 80 chars: Query column in table truncates to ~20 chars
- At 80 chars: Tree shows up to 3 levels comfortably

## Alternative Layouts Considered

### Layout B: Tabbed View
- Tab 1: Lock table only
- Tab 2: Dependency tree only
- Pro: More space for each view
- Con: Requires switching to see relationships

### Layout C: Side-by-Side
- Left: Lock table (60%)
- Right: Tree view (40%)
- Pro: Both views always visible
- Con: Table too narrow at 80 columns

**Decision**: Split view (Primary Design) provides best balance of information density and readability.

## Detail Modal

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Lock Details - PID 1234                                              [Esc]  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│ Process ID:    1234                                                          │
│ User:          postgres                                                      │
│ Database:      mydb                                                          │
│ Lock Type:     relation                                                      │
│ Lock Mode:     AccessExclusiveLock                                          │
│ Granted:       Yes                                                           │
│ Relation:      users                                                         │
│ Duration:      5m 32s                                                        │
│ State:         active                                                        │
│                                                                              │
│ Query:                                                                       │
│ ┌─────────────────────────────────────────────────────────────────────────┐ │
│ │ ALTER TABLE users ADD COLUMN email_verified BOOLEAN DEFAULT FALSE;     │ │
│ └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                              │
│ Blocking: 2 queries                                                          │
│   - PID 5678: UPDATE users SET status = 'active' WHERE...                   │
│   - PID 9012: DELETE FROM users WHERE id = 123                               │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Kill Confirmation Dialog

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Confirm Kill Query                                                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│ Are you sure you want to terminate this query?                               │
│                                                                              │
│ PID: 1234                                                                    │
│ User: postgres                                                               │
│ Query: ALTER TABLE users ADD COLUMN email_verified...                        │
│                                                                              │
│ This will unblock 2 waiting queries.                                         │
│                                                                              │
│                     [Enter] Confirm    [Esc] Cancel                          │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```
