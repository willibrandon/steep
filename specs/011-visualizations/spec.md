# Feature Specification: Advanced Visualizations

**Feature Branch**: `011-visualizations`
**Created**: 2025-11-29
**Status**: Draft
**Input**: Implement Advanced Visualizations using asciigraph library for ASCII-based charts. Add time-series line graphs for key metrics (TPS, connections, cache hit ratio) with configurable time windows (1m, 5m, 1h, 24h). Generate sparklines for inline trending in Activity and Tables views showing query duration and table growth. Create bar charts for comparative analysis (top tables by size, top queries by time). Support in-memory circular buffer for historical data with memory limits and sqlite disk persistence. Prioritize P1 story (time-series graphs) and P2 (sparklines, bar charts) and P3 (heatmaps). Provide chart toggle to show/hide visualizations.

## Clarifications

### Session 2025-11-29

- Q: What is the retention period for SQLite-persisted historical data? → A: 7 days (matches replication monitoring pattern from 006-replication-monitoring)

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Time-Series Graphs for Key Metrics (Priority: P1)

As a DBA, I want to see time-series graphs for key metrics (TPS, connections, cache hit ratio) so I can visualize trends over time and identify patterns or anomalies.

**Why this priority**: Trend visualization is fundamental for proactive database management. Point-in-time values don't reveal whether a metric is stable, improving, or degrading. Time-series graphs provide the context needed to anticipate problems before they become critical.

**Independent Test**: Can be fully tested by navigating to the Dashboard view and observing time-series graphs that update in real-time, delivering immediate trend visibility for key metrics.

**Acceptance Scenarios**:

1. **Given** a PostgreSQL database with active traffic, **When** the DBA opens the Dashboard view, **Then** they see time-series line graphs for TPS, active connections, and cache hit ratio
2. **Given** the Dashboard is displayed with time-series graphs, **When** 60 seconds elapse, **Then** the graphs show a rolling window of data points updated in real-time
3. **Given** a time-series graph is displayed, **When** the DBA changes the time window (1m, 5m, 15m, 1h), **Then** the graph rescales to show the selected time period
4. **Given** historical data exists for the selected time window, **When** viewing extended periods (15m+), **Then** the graph displays data from persistent storage seamlessly merged with in-memory data

---

### User Story 2 - Sparklines for Activity View (Priority: P2)

As a DBA, I want to see sparklines next to connections in the Activity view so I can quickly identify connections with unusual query duration patterns without switching views.

**Why this priority**: Inline trend indicators reduce context switching. DBAs can spot problematic connections at a glance without navigating to detail views, enabling faster triage during incidents.

**Independent Test**: Can be fully tested by viewing the Activity table and observing sparklines for each connection showing recent query duration trends, delivering quick visual triage capability.

**Acceptance Scenarios**:

1. **Given** the Activity view with active connections, **When** the DBA observes a connection row, **Then** they see a sparkline showing query duration trend over the last 5 minutes
2. **Given** a connection with consistently fast queries, **When** viewing its sparkline, **Then** the sparkline shows a flat low line
3. **Given** a connection with a recent spike in query duration, **When** viewing its sparkline, **Then** the spike is clearly visible as a peak in the sparkline

---

### User Story 3 - Sparklines for Tables View (Priority: P2)

As a DBA, I want to see sparklines in the Tables view showing table size growth trends so I can identify rapidly growing tables that may need attention.

**Why this priority**: Table growth trends help with capacity planning. A table that's grown 50% in a day requires different attention than one growing 1% per month.

**Independent Test**: Can be fully tested by viewing the Tables view and observing sparklines for each table showing size trend over time, delivering growth visibility without additional queries.

**Acceptance Scenarios**:

1. **Given** the Tables view with table statistics, **When** the DBA views a table row, **Then** they see a sparkline showing table size trend over the last 24 hours
2. **Given** a table with rapid growth, **When** viewing its sparkline, **Then** an upward trend is clearly visible
3. **Given** a table with stable size, **When** viewing its sparkline, **Then** a flat line is displayed

---

### User Story 4 - Bar Charts for Comparative Analysis (Priority: P2)

As a DBA, I want to see bar charts showing comparative metrics (top tables by size, top queries by execution time) so I can quickly identify outliers and prioritize optimization efforts.

**Why this priority**: Comparative visualization makes outliers immediately obvious. A bar chart showing one query taking 80% of total time is more impactful than a sorted table of numbers.

**Independent Test**: Can be fully tested by viewing bar chart panels showing top resource consumers, delivering visual identification of outliers.

**Acceptance Scenarios**:

1. **Given** the Queries view, **When** the DBA opens the comparative chart panel, **Then** they see a horizontal bar chart showing top 10 queries by total execution time
2. **Given** the Tables view, **When** the DBA opens the comparative chart panel, **Then** they see a horizontal bar chart showing top 10 tables by size
3. **Given** a bar chart with one dominant outlier, **When** viewing the chart, **Then** the outlier bar is visually prominent compared to others
4. **Given** a bar chart is displayed, **When** the DBA selects a bar, **Then** they can navigate to the corresponding detail view for that item

---

### User Story 5 - Chart Toggle (Priority: P2)

As a DBA, I want to toggle charts on/off so I can maximize screen space when I need to focus on tabular data.

**Why this priority**: Screen real estate in terminal UIs is limited. Users should control how much space is devoted to visualizations vs. detailed data.

**Independent Test**: Can be fully tested by pressing the toggle key and observing charts appear/disappear, delivering user control over visualization display.

**Acceptance Scenarios**:

1. **Given** the Dashboard with charts visible, **When** the DBA presses 'v', **Then** the charts are hidden and the view shows only tabular data/metrics
2. **Given** the Dashboard with charts hidden, **When** the DBA presses 'v', **Then** the charts are displayed again
3. **Given** charts are toggled off, **When** the DBA switches to another view and returns, **Then** the toggle state is preserved

---

### User Story 6 - Heatmaps for Time-Based Patterns (Priority: P3)

As a DBA, I want to see heatmaps showing query load by hour/day so I can identify peak usage times and plan maintenance windows.

**Why this priority**: Time-based pattern analysis is valuable for capacity planning and scheduling maintenance, but is less urgent than real-time monitoring capabilities.

**Independent Test**: Can be fully tested by viewing the heatmap panel showing historical activity distribution, delivering temporal pattern visibility.

**Acceptance Scenarios**:

1. **Given** sufficient historical data exists, **When** the DBA opens the heatmap panel, **Then** they see a grid showing query volume by hour (x-axis) and day (y-axis)
2. **Given** the heatmap is displayed, **When** observing peak hours, **Then** those cells are visually highlighted with warmer colors
3. **Given** the heatmap is displayed, **When** observing low-activity periods, **Then** those cells are visually subdued with cooler colors

---

### Edge Cases

- What happens when there is insufficient data to display a graph?
  - Display an empty graph frame with a message: "Collecting data..." until minimum data points are available
- What happens when the selected time window exceeds available historical data?
  - Display available data with a visual indicator showing where data begins
  - Message: "Historical data available from [timestamp]"
- What happens when memory limit for historical data is reached?
  - Oldest data points are automatically evicted (circular buffer behavior)
  - No user-facing error; data collection continues seamlessly
- What happens when the terminal is too narrow for sparklines?
  - Sparklines are hidden; column shows a "-" indicator
  - Full visualization available when terminal is resized
- What happens when charts are enabled but the terminal is too small?
  - Charts are auto-hidden with a notification
  - Toggle key still works but charts only display when space is sufficient
- What happens when SQLite persistence fails (disk full, permissions)?
  - System continues operating with in-memory data only
  - Warning displayed in status bar: "Persistence unavailable"
  - Automatic retry when conditions improve

## Requirements *(mandatory)*

### Functional Requirements

**Time-Series Graphs:**

- **FR-001**: System MUST display time-series line graphs for TPS metric on the Dashboard
- **FR-002**: System MUST display time-series line graphs for active connection count on the Dashboard
- **FR-003**: System MUST display time-series line graphs for cache hit ratio on the Dashboard
- **FR-004**: System MUST support configurable time windows: 1 minute, 5 minutes, 15 minutes, 1 hour, 24 hours
- **FR-005**: System MUST update graphs in real-time at the view's refresh interval
- **FR-006**: System MUST render graphs using ASCII characters compatible with standard terminals
- **FR-007**: System MUST auto-scale Y-axis based on data range with appropriate labels

**Sparklines:**

- **FR-008**: System MUST display sparklines in the Activity view showing query duration trend per connection
- **FR-009**: System MUST display sparklines in the Tables view showing table size trend
- **FR-010**: System MUST display sparklines in the Replication view showing lag trend per replica (leverage existing implementation)
- **FR-011**: Sparklines MUST fit within a single table column (8-15 characters width)
- **FR-012**: Sparklines MUST update with the view's refresh cycle

**Bar Charts:**

- **FR-013**: System MUST display horizontal bar charts showing top 10 queries by total execution time
- **FR-014**: System MUST display horizontal bar charts showing top 10 tables by size
- **FR-015**: Bar charts MUST include value labels showing actual values
- **FR-016**: Bar charts MUST support keyboard navigation to select individual bars

**Heatmaps:**

- **FR-017**: System MUST display heatmaps showing query volume by hour (24 columns) and day of week (7 rows)
- **FR-018**: Heatmaps MUST use color intensity to indicate volume (cooler=low, warmer=high)
- **FR-019**: Heatmaps MUST support at least 5 distinct intensity levels

**Data Management:**

- **FR-020**: System MUST store historical data in an in-memory circular buffer
- **FR-021**: System MUST enforce a maximum of 10,000 data points per metric in memory
- **FR-022**: System MUST persist historical data to SQLite for time windows exceeding 15 minutes
- **FR-022a**: System MUST retain persisted historical data for 7 days, automatically purging older entries
- **FR-023**: System MUST seamlessly merge in-memory and persisted data when rendering
- **FR-024**: System MUST collect data at 1-second granularity for short windows (1m, 5m)
- **FR-025**: System MUST aggregate data to appropriate granularity for longer windows (1h=10s, 24h=1m)

**Interaction:**

- **FR-026**: System MUST toggle chart visibility with the 'v' key
- **FR-027**: System MUST preserve toggle state across view switches within a session
- **FR-028**: System MUST support keyboard selection of time windows using number keys (1=1m, 2=5m, 3=15m, 4=1h, 5=24h)
- **FR-029**: System MUST integrate charts without breaking existing keyboard navigation
- **FR-030**: System MUST display help text explaining chart-related keybindings in help overlay

**Performance:**

- **FR-031**: Chart rendering MUST complete within 50ms to maintain 60 FPS UI responsiveness
- **FR-032**: Data collection MUST NOT impact view refresh intervals
- **FR-033**: Memory usage for visualization data MUST NOT exceed 10MB
- **FR-034**: Persistence writes MUST be asynchronous and NOT block the UI

### Key Entities

- **MetricSeries**: A named time-series of data points (timestamp, value) for a specific metric (TPS, connections, cache hit ratio)
- **DataPoint**: A single measurement with timestamp and numeric value
- **CircularBuffer**: An in-memory ring buffer storing recent data points with automatic eviction of oldest entries
- **ChartConfig**: Configuration for a chart including metric source, time window, dimensions, and display preferences
- **Heatmap Cell**: A single cell in the heatmap grid representing aggregated activity for a specific hour and day combination

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: DBAs can observe metric trends within 5 seconds of opening a visualization-enabled view
- **SC-002**: Time-series graphs update smoothly at the configured refresh interval with no visible stutter
- **SC-003**: Sparklines provide actionable trend information for 95% of rows in Activity and Tables views
- **SC-004**: DBAs can identify the top resource consumers within 3 seconds using bar charts
- **SC-005**: Chart toggle response time is under 100ms
- **SC-006**: System maintains smooth 60 FPS rendering with charts enabled
- **SC-007**: Memory usage for all visualization data stays under 10MB
- **SC-008**: Historical data for 24-hour windows is available within 500ms of request
- **SC-009**: 90% of users can successfully change time windows on first attempt
- **SC-010**: Heatmap patterns are visually distinguishable, enabling users to identify peak hours within 5 seconds

## Assumptions

- PostgreSQL version 11+ is used for metrics collection
- The asciigraph library is compatible with Bubbletea rendering (verified in 006-replication-monitoring)
- The pterm library provides bar charts and heatmaps compatible with Bubbletea via `Srender()`
- SQLite database for steep configuration already exists (from 003-query-performance feature)
- Terminal supports standard ASCII box-drawing characters for chart rendering
- Terminal supports 256 colors for heatmap intensity visualization (RGB gradients)
- Existing sparkline implementation from 006-replication-monitoring can be leveraged/extended
- Minimum terminal size of 80x24 is available; charts are hidden below this size

## Scope Boundaries

**In Scope:**
- Time-series line graphs for Dashboard metrics (TPS, connections, cache hit ratio)
- Sparklines for Activity view (query duration) and Tables view (size trend)
- Bar charts for Queries view (top queries) and Tables view (top tables)
- Heatmaps for temporal pattern analysis (query volume by hour/day)
- In-memory circular buffer with configurable limits
- SQLite persistence for extended time windows
- Chart toggle functionality
- Configurable time windows (1m, 5m, 15m, 1h, 24h)

**Out of Scope:**
- Custom chart creation by users
- Exporting charts as images or files
- Interactive pan/zoom within charts
- Real-time alerting based on chart thresholds (separate feature)
- Charts in SQL Editor view (focus on execution results, not trends)
- Multi-server metric aggregation (single-server focus)
- Chart annotations or markers
