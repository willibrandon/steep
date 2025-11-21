# Reference Tool Study Notes

## pg_top - Activity View Layout

**Source**: `/Users/brandon/src/pg_top`

### Screen Layout (from layout.h)
```c
#define Y_LOADAVE    0    // Row 0: Load average, time
#define Y_PROCSTATE  1    // Row 1: Process states summary
#define Y_CPUSTATES  2    // Row 2: CPU states
#define Y_MEM        3    // Row 3: Memory usage
#define Y_DB         4    // Row 4: DB activity (TPS, hit%, rows)
#define Y_IO         5    // Row 5: DB I/O
#define Y_HEADER     7    // Row 7: Table header
#define Y_PROCS      8    // Row 8+: Process list
```

### Key Design Patterns
1. **Fixed row positions** - Each metric type has a dedicated row
2. **Compact metrics** - All DB stats on single line: `3 tps, 0 rollbs/s, 3 buffer r/s, 100 hit%, 6 row r/s, 0 row w/s`
3. **Table header** - Clear column separation with spaces
4. **Process list** - Starts at row 8, fills rest of screen

### Applicable to Steep
- Use fixed layout positions for metrics (rows 1-4)
- Keep activity table starting position consistent
- Format metrics compactly but readably

---

## htop - Metrics Panel Design

**Source**: `/Users/brandon/src/htop`

### Meter Modes (from MeterMode.h)
```c
enum MeterModeId_ {
   BAR_METERMODE = 1,    // Visual bar (e.g., CPU usage)
   TEXT_METERMODE,       // Text only
   GRAPH_METERMODE,      // Historical graph
   LED_METERMODE,        // LED-style display
};
```

### Header Layout (from HeaderLayout.h)
Supports flexible column arrangements:
- `HF_ONE_100` - 1 column, full width
- `HF_TWO_50_50` - 2 columns, 50/50 (default)
- `HF_FOUR_25_25_25_25` - 4 columns, 25% each

**For Steep**: Use 4-column layout (25% each) for TPS, Cache Hit, Connections, DB Size

### Meter Structure (from Meter.h)
```c
struct Meter_ {
   char* caption;           // Panel title
   MeterModeId mode;        // Display mode
   char txtBuffer[256];     // Formatted value
   double* values;          // Raw values
   double total;            // Maximum for percentage
};
```

### Key Design Patterns
1. **Caption + Value layout** - Label on top, value below
2. **Mode switching** - Same data, different visualizations
3. **Color-coded states** - Normal/Warning/Critical thresholds
4. **Percentage charts** - Visual representation of ratios

### Applicable to Steep
- Text mode for metrics (no bars needed for now)
- Caption/Value layout in each panel
- Warning highlighting for cache hit ratio < 90%
- 4-panel equal-width layout

---

## k9s - Keyboard Navigation Patterns

**Source**: `/Users/brandon/src/k9s`

### Key Definitions (from internal/ui/key.go)
- Vim-style: `j`, `k`, `h`, `l` for navigation
- Special keys: `/` (search), `?` (help), `:` (command)
- Shift variants: `G` (go to bottom), shift-numbers

### Navigation Philosophy
1. **Vim-first** - hjkl movement primary
2. **Arrow fallback** - Arrows work too
3. **Mnemonic keys** - `d` for describe/detail, `x` for delete
4. **Discoverable** - `?` always shows help

### Key Mapping for Steep
| Key | Action | Mnemonic |
|-----|--------|----------|
| j/Down | Move down | vim down |
| k/Up | Move up | vim up |
| g | Go to top | vim go |
| G | Go to bottom | vim Go |
| / | Filter | vim search |
| d | Detail | describe |
| c | Cancel query | cancel |
| x | Kill connection | terminate/x-out |
| r | Refresh | reload |
| s | Sort | sort |
| ? | Help | question |
| q | Quit | quit |

### Applicable to Steep
- Full vim-style navigation
- Mnemonic action keys
- Help overlay always accessible
- Filter mode with `/`

---

## Design Decisions for Steep

### Layout Structure
```
Row 0:     Status bar (connection + time)
Rows 1-4: Metrics panels (4 columns)
Row 5:    Separator line
Row 6:    Table header
Rows 7-N: Activity table
Row N+1:  Footer (hints + pagination)
```

### Metrics Panel Design
- 4 equal-width panels
- TEXT_METERMODE only (no bars)
- Caption above, value below
- Color states: normal, warning (yellow), critical (red)

### Color Scheme
Based on htop color patterns:
- Green: active/good
- Gray: idle/inactive
- Yellow: warning/attention
- Red: error/critical
- Orange: aborted/error state

### Keyboard Navigation
Full vim-style based on k9s patterns with PostgreSQL-specific actions (cancel/terminate).
