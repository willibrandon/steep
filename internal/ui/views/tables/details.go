package tables

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mattn/go-runewidth"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// buildDetailsLines builds the content lines for the details panel.
func (v *TablesView) buildDetailsLines() []string {
	if v.details == nil {
		return nil
	}

	t := &v.details.Table
	var lines []string
	contentWidth := max(76, v.width-4) // Min 80 cols - 4 margin

	// Size and Statistics in a compact 2-column layout
	lines = append(lines, styles.HeaderStyle.Render("Overview"))
	bloatStr := fmt.Sprintf("%.1f%%", t.BloatPct)
	if t.BloatEstimated {
		bloatStr = "~" + bloatStr
	}
	lines = append(lines, fmt.Sprintf("  Total Size:      %-12s  Rows:               %d", models.FormatBytes(t.TotalSize), t.RowCount))
	lines = append(lines, fmt.Sprintf("  Heap:            %-12s  Dead:               %d", models.FormatBytes(t.TableSize), t.DeadRows))
	lines = append(lines, fmt.Sprintf("  Indexes:         %-12s  Bloat:              %s", models.FormatBytes(t.IndexesSize), bloatStr))
	lines = append(lines, fmt.Sprintf("  TOAST:           %-12s  Cache:              %.1f%%", models.FormatBytes(t.ToastSize), t.CacheHitRatio))
	lines = append(lines, fmt.Sprintf("  Seq Scans:       %-12d  Index Scans:        %d", t.SeqScans, t.IndexScans))
	lines = append(lines, "")

	// Maintenance section - vacuum/analyze status with color coding
	lines = append(lines, styles.HeaderStyle.Render("Maintenance"))
	config := queries.DefaultStaleVacuumConfig()

	// Color code vacuum timestamp
	lastVacuum := queries.FormatVacuumTimestamp(t.LastVacuum)
	vacuumIndicator := queries.GetVacuumStatusIndicator(t.LastVacuum, nil, config)
	switch vacuumIndicator {
	case queries.VacuumIndicatorCritical:
		lastVacuum = lipgloss.NewStyle().Foreground(styles.ColorError).Render(lastVacuum)
	case queries.VacuumIndicatorWarning:
		lastVacuum = lipgloss.NewStyle().Foreground(styles.ColorIdleTxn).Render(lastVacuum)
	}

	// Color code autovacuum timestamp
	lastAutovacuum := queries.FormatVacuumTimestamp(t.LastAutovacuum)
	autovacuumIndicator := queries.GetVacuumStatusIndicator(nil, t.LastAutovacuum, config)
	switch autovacuumIndicator {
	case queries.VacuumIndicatorCritical:
		lastAutovacuum = lipgloss.NewStyle().Foreground(styles.ColorError).Render(lastAutovacuum)
	case queries.VacuumIndicatorWarning:
		lastAutovacuum = lipgloss.NewStyle().Foreground(styles.ColorIdleTxn).Render(lastAutovacuum)
	}

	// Color code analyze timestamp (use same thresholds)
	lastAnalyze := queries.FormatVacuumTimestamp(t.LastAnalyze)
	analyzeIndicator := queries.GetVacuumStatusIndicator(t.LastAnalyze, nil, config)
	switch analyzeIndicator {
	case queries.VacuumIndicatorCritical:
		lastAnalyze = lipgloss.NewStyle().Foreground(styles.ColorError).Render(lastAnalyze)
	case queries.VacuumIndicatorWarning:
		lastAnalyze = lipgloss.NewStyle().Foreground(styles.ColorIdleTxn).Render(lastAnalyze)
	}

	// Color code autoanalyze timestamp
	lastAutoanalyze := queries.FormatVacuumTimestamp(t.LastAutoanalyze)
	autoanalyzeIndicator := queries.GetVacuumStatusIndicator(nil, t.LastAutoanalyze, config)
	switch autoanalyzeIndicator {
	case queries.VacuumIndicatorCritical:
		lastAutoanalyze = lipgloss.NewStyle().Foreground(styles.ColorError).Render(lastAutoanalyze)
	case queries.VacuumIndicatorWarning:
		lastAutoanalyze = lipgloss.NewStyle().Foreground(styles.ColorIdleTxn).Render(lastAutoanalyze)
	}

	autovacuumStatus := "Enabled"
	if !t.AutovacuumEnabled {
		autovacuumStatus = styles.WarningStyle.Render("Disabled")
	}
	lines = append(lines, fmt.Sprintf("  Last Vacuum:     %-12s  Last Autovacuum:    %s", lastVacuum, lastAutovacuum))
	lines = append(lines, fmt.Sprintf("  Last Analyze:    %-12s  Last Autoanalyze:   %s", lastAnalyze, lastAutoanalyze))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  Vacuum Count:    %-12d  Autovacuum Count:   %d", t.VacuumCount, t.AutovacuumCount))
	lines = append(lines, fmt.Sprintf("  Autovacuum:      %s", autovacuumStatus))
	lines = append(lines, "")

	// Columns section - optimized for readability
	if len(v.details.Columns) > 0 {
		lines = append(lines, styles.HeaderStyle.Render(fmt.Sprintf("Columns (%d)", len(v.details.Columns))))

		// Dynamic widths for columns
		// Name(20) + Type + Null(4) + Default (full value, scrollable)
		nameWidth := 20
		nullWidth := 4
		remaining := contentWidth - nameWidth - nullWidth - 8
		typeWidth := max(20, remaining*55/100)

		header := fmt.Sprintf("  %-*s %-*s %-*s %s", nameWidth, "Name", typeWidth, "Type", nullWidth, "Null", "Default")
		lines = append(lines, styles.TableHeaderStyle.Width(contentWidth).Render(header))

		for _, col := range v.details.Columns {
			nullable := "YES"
			if !col.IsNullable {
				nullable = "NO"
			}

			colName := truncateString(col.Name, nameWidth)
			// Don't truncate type if it fits, only truncate if really long
			dataType := col.DataType
			if len(dataType) > typeWidth {
				dataType = dataType[:typeWidth-3] + "..."
			}

			defaultVal := ""
			if col.DefaultValue != nil {
				defaultVal = *col.DefaultValue
			}

			lines = append(lines, fmt.Sprintf("  %-*s %-*s %-*s %s",
				nameWidth, colName, typeWidth, dataType, nullWidth, nullable, defaultVal))
		}
		lines = append(lines, "")
	}

	// Constraints section - cleaner single-line format where possible
	if len(v.details.Constraints) > 0 {
		lines = append(lines, styles.HeaderStyle.Render(fmt.Sprintf("Constraints (%d)", len(v.details.Constraints))))

		for _, con := range v.details.Constraints {
			// Format: [TYPE] name: definition
			typeStr := string(con.Type)
			if typeStr == "" {
				typeStr = "??"
			}

			// Try to fit on one line if possible
			prefix := fmt.Sprintf("  [%s] %s: ", typeStr, con.Name)
			remainingWidth := contentWidth - len(prefix)

			if len(con.Definition) <= remainingWidth {
				lines = append(lines, prefix+con.Definition)
			} else {
				// Two lines: name on first, definition on second (indented)
				lines = append(lines, fmt.Sprintf("  [%s] %s", typeStr, con.Name))
				def := con.Definition
				if len(def) > contentWidth-6 {
					def = def[:contentWidth-9] + "..."
				}
				lines = append(lines, "      "+def)
			}
		}
		lines = append(lines, "")
	}

	// Indexes section
	if len(v.details.Indexes) > 0 {
		lines = append(lines, styles.HeaderStyle.Render(fmt.Sprintf("Indexes (%d)", len(v.details.Indexes))))

		// Fixed widths: size(10) + scans(10) + cache(8) = 28, rest for name
		idxNameWidth := max(30, contentWidth-32)

		header := fmt.Sprintf("  %-*s %10s %10s %7s", idxNameWidth, "Name", "Size", "Scans", "Cache")
		lines = append(lines, styles.TableHeaderStyle.Width(contentWidth).Render(header))

		for _, idx := range v.details.Indexes {
			// Build display name with type prefix
			var displayName string
			if idx.IsPrimary {
				displayName = "[PK] " + idx.Name
			} else if idx.IsUnique {
				displayName = "[UQ] " + idx.Name
			} else {
				displayName = idx.Name
			}
			displayName = truncateString(displayName, idxNameWidth)

			unusedMark := ""
			if idx.IsUnused {
				unusedMark = " *"
			}

			lines = append(lines, fmt.Sprintf("  %-*s %10s %10d %6.1f%%%s",
				idxNameWidth, displayName, models.FormatBytes(idx.Size), idx.ScanCount, idx.CacheHitRatio, unusedMark))
		}
		if v.hasUnusedIndexes() {
			lines = append(lines, "")
			lines = append(lines, styles.WarningStyle.Render("  * = unused (0 scans since stats reset)"))
		}
	}

	// Track max line width for horizontal scrolling (ANSI-aware)
	maxWidth := 0
	for _, line := range lines {
		w := ansi.StringWidth(line)
		if w > maxWidth {
			maxWidth = w
		}
	}
	v.detailsMaxLineWidth = maxWidth

	return lines
}

// hasUnusedIndexes checks if there are any unused indexes in details.
func (v *TablesView) hasUnusedIndexes() bool {
	if v.details == nil {
		return false
	}
	for _, idx := range v.details.Indexes {
		if idx.IsUnused {
			return true
		}
	}
	return false
}

// detailsContentHeight returns the visible content height for details panel.
func (v *TablesView) detailsContentHeight() int {
	// Full screen minus: status bar(3) + title(2) + footer with border(3) = 8
	// Subtract additional 2 for alignment with other views
	return max(5, v.height-10)
}

// scrollDetailsUp scrolls the details panel up by n lines.
func (v *TablesView) scrollDetailsUp(n int) {
	v.detailsScrollOffset = max(0, v.detailsScrollOffset-n)
}

// scrollDetailsDown scrolls the details panel down by n lines.
func (v *TablesView) scrollDetailsDown(n int) {
	maxOffset := max(0, len(v.detailsLines)-v.detailsContentHeight())
	v.detailsScrollOffset = min(v.detailsScrollOffset+n, maxOffset)
}

// scrollDetailsToBottom scrolls to the bottom of details.
func (v *TablesView) scrollDetailsToBottom() {
	maxOffset := max(0, len(v.detailsLines)-v.detailsContentHeight())
	v.detailsScrollOffset = maxOffset
}

// scrollDetailsLeft scrolls the details panel left by n columns.
func (v *TablesView) scrollDetailsLeft(n int) {
	v.detailsHScrollOffset = max(0, v.detailsHScrollOffset-n)
}

// scrollDetailsRight scrolls the details panel right by n columns.
func (v *TablesView) scrollDetailsRight(n int) {
	// Content area is v.width - 2, so max offset is maxLineWidth - (width - 2)
	contentWidth := v.width - 2
	maxOffset := max(0, v.detailsMaxLineWidth-contentWidth)
	v.detailsHScrollOffset = min(v.detailsHScrollOffset+n, maxOffset)
}

// scrollDetailsToRight scrolls to the right edge of details.
func (v *TablesView) scrollDetailsToRight() {
	contentWidth := v.width - 2
	maxOffset := max(0, v.detailsMaxLineWidth-contentWidth)
	v.detailsHScrollOffset = maxOffset
}

// renderDetails renders the table details as a full-screen view.
func (v *TablesView) renderDetails() string {
	if v.details == nil {
		return styles.InfoStyle.Render("No table selected")
	}

	t := &v.details.Table

	// Status bar (same as main view)
	statusBar := v.renderStatusBar()

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent).
		MarginBottom(1)
	title := titleStyle.Render(fmt.Sprintf("Table Details: %s.%s", t.SchemaName, t.Name))

	// Content with vertical and horizontal scrolling
	contentHeight := v.detailsContentHeight()
	contentWidth := v.width - 2
	lines := v.detailsLines
	if len(lines) == 0 {
		lines = []string{"No details available"}
	}

	endIdx := min(v.detailsScrollOffset+contentHeight, len(lines))
	visibleLines := lines[v.detailsScrollOffset:endIdx]

	// Apply horizontal scroll to each line (ANSI-aware)
	scrolledLines := make([]string, len(visibleLines))
	for i, line := range visibleLines {
		lineWidth := ansi.StringWidth(line)
		if v.detailsHScrollOffset >= lineWidth {
			scrolledLines[i] = ""
		} else {
			// Skip first N characters based on horizontal scroll (ANSI-aware)
			scrolledLines[i] = ansi.TruncateLeft(line, v.detailsHScrollOffset, "")
		}
	}

	// Pad to fill height
	for len(scrolledLines) < contentHeight {
		scrolledLines = append(scrolledLines, "")
	}
	content := strings.Join(scrolledLines, "\n")

	// Footer with scroll indicators (boxed like other views)
	scrollInfo := ""
	if len(lines) > contentHeight {
		scrollInfo = fmt.Sprintf(" %d/%d", v.detailsScrollOffset+1, len(lines))
	}
	hScrollInfo := ""
	if v.detailsHScrollOffset > 0 || v.detailsMaxLineWidth > contentWidth {
		hScrollInfo = fmt.Sprintf(" col:%d", v.detailsHScrollOffset+1)
	}

	hints := styles.FooterHintStyle.Render(fmt.Sprintf("[j/k]↕ [h/l]↔ [g/G]top/btm [y]copy [Esc]back%s%s", scrollInfo, hScrollInfo))

	// Calculate gap to fill footer width
	gap := v.width - lipgloss.Width(hints) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	footer := styles.FooterStyle.
		Width(v.width - 2).
		Render(hints + spaces)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		statusBar,
		title,
		content,
		footer,
	)
}

// truncateString truncates a string to maxLen, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// SetSize sets the dimensions of the view.
func (v *TablesView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

// SetConnected sets the connection status.
func (v *TablesView) SetConnected(connected bool) {
	v.connected = connected
}

// SetConnectionInfo sets the connection info string.
func (v *TablesView) SetConnectionInfo(info string) {
	v.connectionInfo = info
}

// SetReadOnly sets the read-only mode.
func (v *TablesView) SetReadOnly(readOnly bool) {
	v.readonlyMode = readOnly
}

// SetPool sets the database connection pool.
func (v *TablesView) SetPool(pool *pgxpool.Pool) {
	v.pool = pool
}

// IsInputMode returns true if in an input mode (overlays that should capture 'q').
func (v *TablesView) IsInputMode() bool {
	return v.mode == ModeDetails || v.mode == ModeHelp || v.mode == ModeOperationHistory
}

// showToast displays a toast message.
func (v *TablesView) showToast(message string, isError bool) {
	v.toastMessage = message
	v.toastError = isError
	v.toastTime = time.Now()
}

// Helper functions

func truncateWithWidth(s string, maxWidth int) string {
	w := runewidth.StringWidth(s)
	if w <= maxWidth {
		return s
	}
	return runewidth.Truncate(s, maxWidth-3, "...")
}

func padRight(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w >= width {
		return runewidth.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", width-w)
}

func formatNumber(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	if n < 1000000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	return fmt.Sprintf("%.1fB", float64(n)/1000000000)
}

