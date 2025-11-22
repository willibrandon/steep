# Visual Research: Query Performance View

## Reference Tools Studied

### pg_top

**Layout Pattern:**
- Fixed header rows: load average, process states, CPU states, memory, DB stats, IO stats
- Column header row with fixed-width columns
- Process list takes remaining vertical space
- Footer with command hints

**Key Visual Elements:**
- Right-aligned numeric columns (PID, CPU%, MEM%, etc.)
- Left-aligned text columns (COMMAND, USER)
- Column headers uppercase
- Active row highlighted with reverse video
- Color coding for different process states

**Table Columns (relevant to queries):**
- PID, USERNAME, SIZE, RES, STATE, XTIME, QTIME, %CPU, LOCKS, COMMAND

### htop

**Layout Pattern:**
- Top: CPU/Memory meters as bar graphs
- Middle: Process table (scrollable)
- Bottom: Function key hints (F1-F10)

**Key Visual Elements:**
- Colored bar graphs for resource usage
- Tree view option for process hierarchy
- Sortable columns with indicator on active sort column
- Search/filter with incremental highlight
- Selected row with distinct background color

**Navigation:**
- Arrow keys / vim keys (j/k) for row navigation
- F6 for sort column selection
- / for search
- Space for tag/mark

### Common Patterns

1. **Table Layout**: Fixed-width columns, right-align numbers, left-align text
2. **Status Bar**: Top row with connection/system info
3. **Footer**: Bottom row with keyboard hints
4. **Sort Indicator**: Arrow or highlight on sorted column
5. **Selection**: Distinct background for selected row
6. **Numeric Formatting**: Aligned decimal points, abbreviated large numbers (1.2K, 5.3M)

## Design Decisions for Steep Queries View

### Layout (Top to Bottom)
1. **Status Bar** (1 line): Connection info, last update time
2. **Column Headers** (1 line): Query, Calls, Total Time, Mean Time, Rows
3. **Query Table** (variable): Scrollable list of queries
4. **Footer** (1 line): Key hints, sort indicator, total count

### Column Widths (80-char minimum)
- Query: Flexible (remaining space, truncate with ...)
- Calls: 8 chars (right-aligned)
- Total Time: 12 chars (right-aligned, ms/s/m format)
- Mean Time: 12 chars (right-aligned, ms/s/m format)
- Rows: 10 chars (right-aligned)

### Navigation Keys
- j/k or down/up: Move selection
- g/G: Go to top/bottom
- Ctrl+u/Ctrl+d: Page up/down
- s: Cycle sort column
- /: Search/filter
- Enter: Show full query
- y: Copy to clipboard (future)
- r: Refresh
- q: Quit/back

### Color Scheme (from styles/colors.go)
- Selected row: ColorHighlight background
- Column headers: Bold
- High values: ColorWarningFg or ColorCriticalFg for slow queries
- Sort indicator: ColorActive

### Time Formatting
- < 1000ms: "123ms"
- 1-60s: "1.23s"
- 60s+: "1m23s"
- 60m+: "1h23m"

### Row Count Formatting
- < 1000: "123"
- 1K-1M: "1.2K"
- 1M+: "1.2M"
