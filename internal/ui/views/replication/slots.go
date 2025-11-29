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

	// Column headers
	headers := []struct {
		name  string
		width int
	}{
		{"Name", 25},
		{"Type", 10},
		{"Active", 8},
		{"Retained", 12},
		{"WAL Status", 12},
	}

	// Header row
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	var headerRow strings.Builder
	for _, h := range headers {
		headerRow.WriteString(headerStyle.Render(padRight(h.name, h.width)))
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

// renderSlotRow renders a single slot row.
func (v *ReplicationView) renderSlotRow(s models.ReplicationSlot, selected bool, headers []struct {
	name  string
	width int
}) string {
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

	var row strings.Builder
	row.WriteString(nameStyle.Render(padRight(truncateWithEllipsis(slotName, headers[0].width), headers[0].width)))
	row.WriteString(baseStyle.Render(padRight(s.SlotType.String(), headers[1].width)))
	row.WriteString(activeStyle.Render(padRight(activeStr, headers[2].width)))
	row.WriteString(retainedStyle.Render(padRight(retainedStr, headers[3].width)))
	row.WriteString(walStyle.Render(padRight(s.WALStatus, headers[4].width)))

	return row.String()
}
