# Visual Design: Locks & Blocking Detection

**Feature Branch**: `004-locks-blocking`
**Date**: 2025-11-22

## Design Decision

**Chosen Layout**: Demo 2 - Split View (table top, tree bottom)

### Rationale

After building and evaluating three prototype demos:

1. **Demo 1 (Simple Table)**: Shows locks in tabular format but lacks visual representation of blocking relationships. Users must mentally construct the dependency chains.

2. **Demo 2 (Split View)** CHOSEN: Provides both detailed lock information in a sortable table AND visual blocking chain hierarchy in the tree. Best balance of information density and relationship clarity.

3. **Demo 3 (Tree-Focused)**: Prioritizes blocking chains but loses detailed lock attributes. Better for "at a glance" scenarios but insufficient for investigating specific lock details.

**Why Demo 2**: DBAs need both capabilities - quickly identifying blocking chains AND examining specific lock details (mode, granted status, relation). The split view provides this without requiring mode switching.

## Visual Acceptance Criteria

### Must Have (Blocking for Implementation)

1. **Table displays all required columns**: PID, Type, Mode, Granted, Database, Relation
2. **Blocked rows render in red foreground** (#FF5555)
3. **Blocking rows render in yellow foreground** (#FFB86C)
4. **Tree shows blocking hierarchy** with correct parent-child indentation
5. **View renders correctly at 80x24** minimum terminal size
6. **Color coding is immediately distinguishable** - red/yellow must be visually distinct
7. **Tree metadata shows PID and lock mode** in brackets format: [PID:1234 LockMode]

### Should Have (Important but not blocking)

1. **Query text truncates cleanly** with ellipsis (…) character
2. **Selected row has visible highlight** (inverse colors)
3. **Table columns align consistently** without overflow
4. **Tree renders without wrapping** at 80 columns
5. **Stats bar shows lock counts**: total, blocking, blocked

### Nice to Have (Future improvements)

1. **Customizable color scheme** via config
2. **Adjustable table/tree split ratio**
3. **Tree collapse/expand controls**

## UI Consistency Requirements (NON-NEGOTIABLE)

The locks view MUST exactly follow the queries view patterns for visual and behavioral consistency:

### Detail View (Query Display)

1. **Layout**: Use `lipgloss.JoinVertical` with title, scrollable content, footer - exactly like explain view
2. **Scrolling**: Manual `scrollOffset` integer, NOT bubbles viewport component (viewport causes Esc delay)
3. **Key handling**: Use `msg.String() == "esc"` or `msg.String() == "q"` - NOT `msg.Type == tea.KeyEsc`
4. **SQL Formatting**: Format via Docker pgFormatter: `docker run --rm -i ghcr.io/darold/pgformatter:latest pg_format`
5. **Syntax Highlighting**: Chroma with monokai theme (`github.com/alecthomas/chroma/v2/quick`)

### Footer Key Hints

Must match queries view format:
```
[↑/↓]scroll [Esc]close [c]copy
```

### Table Behavior

- Call `table.Blur()` when entering detail mode
- Call `table.Focus()` when exiting detail mode

### Reference Implementation

Study these files before implementing locks view:
- `internal/ui/views/queries/view.go` - footer pattern, table integration
- `internal/ui/views/queries/explain.go` - detail view with SQL formatting, scroll handling (PRIMARY REFERENCE)
- `internal/ui/views/queries/detail.go` - modal pattern

## Implementation Specifications

### Colors (Dracula Theme)

| Element | Color | Hex |
|---------|-------|-----|
| Blocked text | Red | #FF5555 |
| Blocking text | Yellow/Orange | #FFB86C |
| Normal text | Foreground | #F8F8F2 |
| Header text | Cyan | #8BE9FD |
| Border | Purple/Gray | #6272A4 |
| Selected bg | Dark | #44475A |

### Layout Proportions

- **Table section**: 50-60% of available height
- **Tree section**: 40-50% of available height
- **Minimum table height**: 4 rows
- **Minimum tree height**: 3 levels

### Column Widths (80 column terminal)

| Column | Width | Notes |
|--------|-------|-------|
| PID | 6 | Right-aligned |
| Type | 10 | Left-aligned, truncate |
| Mode | 18 | Left-aligned, truncate |
| Grant | 5 | Center (Yes/No) |
| DB | 6 | Left-aligned, truncate |
| Relation | Remaining | Left-aligned, truncate |

### Tree Node Format

```
├── [PID:1234 AccessExclusiveLock] ALTER TABLE users ADD...
│   ├── [PID:5678 waiting] UPDATE users SET status...
│   └── [PID:9012 waiting] DELETE FROM users WHERE...
```

- Metadata in brackets: PID and lock mode (or "waiting" for blocked)
- Query truncated to ~45 characters
- Box-drawing characters from treeprint library

## Demo Commands

Run the demos to see each layout approach:

```bash
# Demo 1: Simple table
go run ./internal/ui/demos/demo_locks_table

# Demo 2: Split view (CHOSEN)
go run ./internal/ui/demos/demo_locks_split

# Demo 3: Tree-focused
go run ./internal/ui/demos/demo_locks_alt
```

## Sign-off

- [ ] Visual design reviewed and approved
- [ ] Color scheme validated for accessibility (color blindness)
- [ ] 80x24 rendering verified
- [ ] Tree depth tested (3+ levels)

**Status**: Ready for implementation with Demo 2 split view layout
