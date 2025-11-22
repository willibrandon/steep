# ASCII Mockup: Queries View

## Main View (80x24 minimum)

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ postgres://localhost:5432/mydb                           2024-01-15 14:32:05 │
├──────────────────────────────────────────────────────────────────────────────┤
│ Query                                          Calls    Total    Mean   Rows │
├──────────────────────────────────────────────────────────────────────────────┤
│>SELECT * FROM users WHERE id = $1                 1.2K    5.3s   4.4ms   1.2K │
│ SELECT * FROM orders WHERE user_id = $1 AND...     892   12.1s  13.6ms  45.2K │
│ INSERT INTO events (type, data) VALUES ($1...      456    2.1s   4.6ms    456 │
│ UPDATE users SET last_login = $1 WHERE id =...     234    1.8s   7.7ms    234 │
│ SELECT COUNT(*) FROM sessions WHERE expire...      123  892ms   7.3ms    123 │
│ DELETE FROM cache WHERE expires_at < $1             89  156ms   1.8ms     89 │
│ SELECT u.*, p.* FROM users u JOIN profiles...       45    3.2s  71.1ms   2.1K │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
├──────────────────────────────────────────────────────────────────────────────┤
│ [j/k]nav [s]ort [/]filter [r]efresh [q]uit          Sort: Total ↓     7 / 7 │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Column Layout Details

### Responsive Widths (80-char terminal)

```
| Query (40 flex) | Calls (8) | Total (8) | Mean (8) | Rows (8) |
```

### Wider Terminal (120-char)

```
| Query (72 flex) | Calls (10) | Total (10) | Mean (10) | Rows (10) |
```

## Visual States

### Selected Row (highlighted)
```
│ SELECT * FROM users WHERE id = $1                 1.2K    5.3s   4.4ms   1.2K │
```
- Uses inverse/highlight background color
- Leading `>` indicator

### Filter Active
```
├──────────────────────────────────────────────────────────────────────────────┤
│ [FILTERED] /users                                  Sort: Total ↓     3 / 7 │
└──────────────────────────────────────────────────────────────────────────────┘
```

### Filter Input Mode
```
├──────────────────────────────────────────────────────────────────────────────┤
│ Filter: users_                                                               │
└──────────────────────────────────────────────────────────────────────────────┘
```

### Sort Column Indicators
- Current sort column shows arrow: ↓ (desc) or ↑ (asc)
- Press `s` to cycle: Total → Calls → Mean → Rows → Total

### Empty State
```
│                                                                              │
│                         No queries recorded yet                              │
│                                                                              │
│              Queries will appear as they are executed                        │
│                                                                              │
```

### Error State
```
│                                                                              │
│                      Error loading query statistics                          │
│                                                                              │
│                   failed to read from SQLite database                        │
│                                                                              │
```

## Time Formatting Examples

| Value (ms) | Display |
|------------|---------|
| 0.5        | <1ms    |
| 12.3       | 12ms    |
| 123.4      | 123ms   |
| 1234       | 1.2s    |
| 12345      | 12.3s   |
| 65000      | 1m5s    |
| 3665000    | 1h1m    |

## Row Count Formatting Examples

| Value      | Display |
|------------|---------|
| 0          | 0       |
| 123        | 123     |
| 1234       | 1.2K    |
| 12345      | 12.3K   |
| 123456     | 123K    |
| 1234567    | 1.2M    |

## Keyboard Navigation

| Key           | Action                        |
|---------------|-------------------------------|
| j / ↓         | Move down one row             |
| k / ↑         | Move up one row               |
| g / Home      | Go to first row               |
| G / End       | Go to last row                |
| Ctrl+d/PgDn   | Page down                     |
| Ctrl+u/PgUp   | Page up                       |
| s             | Cycle sort column             |
| /             | Enter filter mode             |
| Esc           | Clear filter / cancel         |
| r             | Force refresh                 |
| q             | Quit / back to dashboard      |
| Enter         | View full query (future)      |
| y             | Copy query to clipboard (US6) |

## Acceptance Criteria

1. Table fills available height (minus status bar, header, footer)
2. Query column truncates with "..." when too long
3. Numeric columns are right-aligned
4. Selected row is visually distinct
5. Sort indicator shows current sort column and direction
6. Count shows "displayed / total" queries
7. Renders correctly at 80x24 minimum
8. Auto-refresh every 5 seconds (no visible flicker)
