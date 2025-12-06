package replication

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// renderSlots renders the Slots tab content.
func (v *ReplicationView) renderSlots() string {
	if len(v.data.Slots) == 0 {
		msg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("No replication slots configured.")
		return lipgloss.Place(v.width, v.height-5, lipgloss.Center, lipgloss.Center, msg)
	}

	var b strings.Builder

	// Column headers - adaptive for terminal width
	headers := v.getSlotHeaders()

	// Header row
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	var headerRow strings.Builder
	for _, h := range headers {
		headerRow.WriteString(headerStyle.Render(padRight(h.Name, h.Width)))
	}
	b.WriteString(headerRow.String())
	b.WriteString("\n")

	// Table height
	// Reserve: status(3) + title(1) + tabs(1) + footer(3) = 8
	tableHeight := v.height - 8

	// Data rows
	visibleRows := min(tableHeight, len(v.data.Slots)-v.slotScrollOffset)
	for i := 0; i < visibleRows; i++ {
		idx := v.slotScrollOffset + i
		if idx >= len(v.data.Slots) {
			break
		}
		slot := v.data.Slots[idx]
		b.WriteString(v.renderSlotRow(slot, idx == v.slotSelectedIdx, headers))
		b.WriteString("\n")
	}

	// Wrap content in height container to push footer to bottom
	// Reserve: status(3) + title(1) + tabs(1) + footer(3) = 8
	contentHeight := v.height - 8
	content := lipgloss.NewStyle().
		Height(contentHeight).
		Render(b.String())

	return content + "\n" + v.renderFooter()
}

// getSlotHeaders returns headers adapted to terminal width.
// Wide mode (>140): Shows LSN columns
// Normal mode (85-140): Standard columns with flex Name
// Narrow mode (<85): Essential columns only
func (v *ReplicationView) getSlotHeaders() []ColumnConfig {
	// Fixed width columns
	const (
		typeWidth      = 10
		activeWidth    = 8
		retainedWidth  = 12
		walStatusWidth = 12
		lsnWidth       = 14
		databaseWidth  = 15
	)

	// Wide mode (>140): Add LSN and Database columns
	if v.width >= 140 {
		fixedWidth := typeWidth + activeWidth + retainedWidth + walStatusWidth + lsnWidth*2 + databaseWidth
		nameWidth := max(20, (v.width-fixedWidth)/2)
		if nameWidth > 35 {
			nameWidth = 35
		}
		return []ColumnConfig{
			{"Name", nameWidth, "name"},
			{"Type", typeWidth, "type"},
			{"Database", databaseWidth, "database"},
			{"Active", activeWidth, "active"},
			{"Restart LSN", lsnWidth, "restart_lsn"},
			{"Confirmed LSN", lsnWidth, "confirmed_lsn"},
			{"Retained", retainedWidth, "retained"},
			{"WAL Status", walStatusWidth, "wal_status"},
		}
	}

	// Normal mode (85-140): Standard columns with flex Name
	if v.width >= 85 {
		fixedWidth := typeWidth + activeWidth + retainedWidth + walStatusWidth
		nameWidth := max(25, v.width-fixedWidth-2)
		if nameWidth > 40 {
			nameWidth = 40
		}
		return []ColumnConfig{
			{"Name", nameWidth, "name"},
			{"Type", typeWidth, "type"},
			{"Active", activeWidth, "active"},
			{"Retained", retainedWidth, "retained"},
			{"WAL Status", walStatusWidth, "wal_status"},
		}
	}

	// Narrow mode (<85): Essential columns only
	return []ColumnConfig{
		{"Name", 25, "name"},
		{"Type", typeWidth, "type"},
		{"Active", activeWidth, "active"},
		{"Retained", retainedWidth, "retained"},
		{"WAL Status", walStatusWidth, "wal_status"},
	}
}

// renderSlotRow renders a single slot row.
func (v *ReplicationView) renderSlotRow(s models.ReplicationSlot, selected bool, headers []ColumnConfig) string {
	baseStyle := lipgloss.NewStyle()
	if selected {
		baseStyle = baseStyle.Background(lipgloss.Color("236"))
	}

	// Check for orphaned slot (inactive for >24 hours) - T034
	isOrphaned := s.IsOrphaned(24 * time.Hour)

	// Name with orphaned indicator
	nameStyle := baseStyle
	slotName := s.SlotName
	if isOrphaned {
		nameStyle = nameStyle.Foreground(lipgloss.Color("214")) // Yellow for orphaned
		slotName = "!" + s.SlotName                             // Prefix with warning
	}

	// Active status style
	activeStyle := baseStyle
	activeStr := "No"
	if s.Active {
		activeStyle = activeStyle.Foreground(lipgloss.Color("42"))
		activeStr = "Yes"
	} else {
		activeStyle = activeStyle.Foreground(lipgloss.Color("214"))
		if isOrphaned {
			activeStr = "No*" // Asterisk indicates orphaned
		}
	}

	// Retained WAL with warning indicator - T033
	// Use 80% of 1GB as threshold for significant retention warning
	retainedStyle := baseStyle
	retainedStr := s.FormatRetainedBytes()
	if !s.Active && s.RetainedBytes > 800*1024*1024 { // >800MB and inactive
		retainedStyle = retainedStyle.Foreground(lipgloss.Color("196")) // Red for high retention
		retainedStr = "!" + retainedStr
	} else if !s.Active && s.RetainedBytes > 100*1024*1024 { // >100MB and inactive
		retainedStyle = retainedStyle.Foreground(lipgloss.Color("214")) // Yellow for moderate retention
	}

	// WAL status color
	walStyle := baseStyle
	switch s.WALStatus {
	case "lost":
		walStyle = walStyle.Foreground(lipgloss.Color("196"))
	case "unreserved":
		walStyle = walStyle.Foreground(lipgloss.Color("214"))
	}

	// Build row dynamically based on available columns
	var row strings.Builder
	for _, h := range headers {
		switch h.Key {
		case "name":
			row.WriteString(nameStyle.Render(padRight(truncateWithEllipsis(slotName, h.Width), h.Width)))
		case "type":
			row.WriteString(baseStyle.Render(padRight(s.SlotType.String(), h.Width)))
		case "database":
			db := s.Database
			if db == "" {
				db = "-"
			}
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(db, h.Width), h.Width)))
		case "active":
			row.WriteString(activeStyle.Render(padRight(activeStr, h.Width)))
		case "restart_lsn":
			lsn := s.RestartLSN
			if lsn == "" {
				lsn = "-"
			}
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(lsn, h.Width), h.Width)))
		case "confirmed_lsn":
			lsn := s.ConfirmedFlushLSN
			if lsn == "" {
				lsn = "-"
			}
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(lsn, h.Width), h.Width)))
		case "retained":
			row.WriteString(retainedStyle.Render(padRight(retainedStr, h.Width)))
		case "wal_status":
			row.WriteString(walStyle.Render(padRight(s.WALStatus, h.Width)))
		}
	}

	return row.String()
}
