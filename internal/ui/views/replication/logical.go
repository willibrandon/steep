package replication

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// renderLogical renders the Logical tab content.
func (v *ReplicationView) renderLogical() string {
	// Check if wal_level is not 'logical' - can't use logical replication
	if v.data.Config != nil && v.data.Config.WALLevel.CurrentValue != "logical" {
		walLevel := v.data.Config.WALLevel.CurrentValue
		if walLevel == "" {
			walLevel = "unknown"
		}

		title := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("214")). // Yellow warning
			Render("⚠ Logical Replication Not Available")

		details := fmt.Sprintf("Current wal_level: %s\n"+
			"Required: logical\n\n"+
			"To enable logical replication:\n"+
			"  1. Set wal_level = 'logical' in postgresql.conf\n"+
			"     or: ALTER SYSTEM SET wal_level = 'logical';\n"+
			"  2. Restart PostgreSQL (requires full restart)\n"+
			"  3. Return here to create publications/subscriptions",
			walLevel)

		msg := lipgloss.JoinVertical(lipgloss.Center,
			title,
			"",
			lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(details),
		)
		return lipgloss.Place(v.width, v.height-5, lipgloss.Center, lipgloss.Center, msg)
	}

	// wal_level is 'logical' but no pubs/subs exist
	if !v.data.HasLogicalReplication() {
		title := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("42")). // Green - ready
			Render("✓ Ready for Logical Replication")

		details := "No publications or subscriptions configured.\n\n" +
			"To set up logical replication:\n" +
			"  1. Press Tab to go to Setup tab\n" +
			"  2. Use logical replication wizard [o]"

		msg := lipgloss.JoinVertical(lipgloss.Center,
			title,
			"",
			lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(details),
		)
		return lipgloss.Place(v.width, v.height-5, lipgloss.Center, lipgloss.Center, msg)
	}

	// Wide mode (>140): Side-by-side layout
	if v.width >= 140 {
		return v.renderLogicalWide()
	}

	// Normal/narrow mode: Stacked layout
	return v.renderLogicalStacked()
}

// renderLogicalWide renders publications and subscriptions side-by-side.
func (v *ReplicationView) renderLogicalWide() string {
	// Reserve: status(3) + title(1) + tabs(1) + footer(3) = 8
	contentHeight := v.height - 8

	// Split width between publications and subscriptions
	halfWidth := (v.width - 3) / 2 // -3 for separator

	// Build publications panel
	pubContent := v.renderPublicationsPanel(halfWidth, contentHeight)

	// Build subscriptions panel
	subContent := v.renderSubscriptionsPanel(halfWidth, contentHeight)

	// Join horizontally with a vertical separator
	separator := lipgloss.NewStyle().
		Foreground(styles.ColorMuted).
		Render(strings.Repeat("│\n", contentHeight-1) + "│")

	content := lipgloss.JoinHorizontal(lipgloss.Top,
		pubContent,
		separator,
		subContent,
	)

	return content + "\n" + v.renderFooter()
}

// renderPublicationsPanel renders the publications section for wide mode.
func (v *ReplicationView) renderPublicationsPanel(width, height int) string {
	var b strings.Builder

	// Header
	pubHeader := "Publications"
	if v.logicalFocusPubs {
		pubHeader = "▶ " + pubHeader
	} else {
		pubHeader = "  " + pubHeader
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render(pubHeader))
	b.WriteString("\n")

	if len(v.data.Publications) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("  No publications"))
		b.WriteString("\n")
	} else {
		// Adaptive headers for wide mode
		headers := v.getPubHeaders(width)

		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
		var headerRow strings.Builder
		for _, h := range headers {
			headerRow.WriteString(headerStyle.Render(padRight(h.Name, h.Width)))
		}
		b.WriteString(headerRow.String())
		b.WriteString("\n")

		maxRows := height - 3
		for i, pub := range v.data.Publications {
			if i >= maxRows {
				break
			}
			selected := v.logicalFocusPubs && i == v.pubSelectedIdx
			b.WriteString(v.renderPubRowAdaptive(pub, selected, headers))
			b.WriteString("\n")
		}
	}

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Render(b.String())
}

// renderSubscriptionsPanel renders the subscriptions section for wide mode.
func (v *ReplicationView) renderSubscriptionsPanel(width, height int) string {
	var b strings.Builder

	// Header
	subHeader := "Subscriptions"
	if !v.logicalFocusPubs {
		subHeader = "▶ " + subHeader
	} else {
		subHeader = "  " + subHeader
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render(subHeader))
	b.WriteString("\n")

	if len(v.data.Subscriptions) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("  No subscriptions"))
		b.WriteString("\n")
	} else {
		// Adaptive headers for wide mode
		headers := v.getSubHeaders(width)

		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
		var headerRow strings.Builder
		for _, h := range headers {
			headerRow.WriteString(headerStyle.Render(padRight(h.Name, h.Width)))
		}
		b.WriteString(headerRow.String())
		b.WriteString("\n")

		maxRows := height - 3
		for i, sub := range v.data.Subscriptions {
			if i >= maxRows {
				break
			}
			selected := !v.logicalFocusPubs && i == v.subSelectedIdx
			b.WriteString(v.renderSubRowAdaptive(sub, selected, headers))
			b.WriteString("\n")
		}
	}

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Render(b.String())
}

// renderLogicalStacked renders publications and subscriptions stacked vertically.
func (v *ReplicationView) renderLogicalStacked() string {
	var b strings.Builder

	// Split view: publications on top, subscriptions on bottom
	// Reserve: status(3) + title(1) + tabs(1) + footer(3) = 8
	halfHeight := (v.height - 8) / 2

	// Publications section
	pubHeader := "Publications"
	if v.logicalFocusPubs {
		pubHeader = "▶ " + pubHeader
	} else {
		pubHeader = "  " + pubHeader
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render(pubHeader))
	b.WriteString("\n")

	if len(v.data.Publications) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("  No publications"))
		b.WriteString("\n")
	} else {
		// Adaptive headers
		pubHeaders := v.getPubHeaders(v.width)

		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
		var headerRow strings.Builder
		for _, h := range pubHeaders {
			headerRow.WriteString(headerStyle.Render(padRight(h.Name, h.Width)))
		}
		b.WriteString(headerRow.String())
		b.WriteString("\n")

		for i, pub := range v.data.Publications {
			if i >= halfHeight-3 {
				break
			}
			selected := v.logicalFocusPubs && i == v.pubSelectedIdx
			b.WriteString(v.renderPubRowAdaptive(pub, selected, pubHeaders))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	// Subscriptions section
	subHeader := "Subscriptions"
	if !v.logicalFocusPubs {
		subHeader = "▶ " + subHeader
	} else {
		subHeader = "  " + subHeader
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent).Render(subHeader))
	b.WriteString("\n")

	if len(v.data.Subscriptions) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("  No subscriptions"))
		b.WriteString("\n")
	} else {
		// Adaptive headers
		subHeaders := v.getSubHeaders(v.width)

		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
		var headerRow strings.Builder
		for _, h := range subHeaders {
			headerRow.WriteString(headerStyle.Render(padRight(h.Name, h.Width)))
		}
		b.WriteString(headerRow.String())
		b.WriteString("\n")

		for i, sub := range v.data.Subscriptions {
			if i >= halfHeight-3 {
				break
			}
			selected := !v.logicalFocusPubs && i == v.subSelectedIdx
			b.WriteString(v.renderSubRowAdaptive(sub, selected, subHeaders))
			b.WriteString("\n")
		}
	}

	// Wrap content in height container to push footer to bottom
	// Reserve: status(3) + title(1) + tabs(1) + footer(3) = 8
	contentHeight := v.height - 8
	content := lipgloss.NewStyle().
		Height(contentHeight).
		Render(b.String())

	return content + "\n" + v.renderFooter()
}

// Shared column widths for visual alignment between Publications and Subscriptions tables.
// Layout alignment:
//   Publications: Name | Tables+All | Operations | Subscribers
//   Subscriptions: Name | Enabled    | Pubs       | Lag
// The Name columns match, middle columns roughly align, trailing columns match.
const (
	logicalNameMinWidth = 25  // Minimum Name column width
	logicalNameMaxWidth = 40  // Maximum Name column width
	logicalTrailWidth   = 12  // Trailing column (Subscribers/Lag)
	logicalMidWidth     = 20  // Middle flex column (Operations/Publications)
)

// getPubHeaders returns adaptive headers for publication table.
func (v *ReplicationView) getPubHeaders(availWidth int) []ColumnConfig {
	const (
		tablesWidth = 8
		allWidth    = 5
	)

	// Wide mode: Expand Name column
	if availWidth >= 85 {
		fixedWidth := tablesWidth + allWidth + logicalMidWidth + logicalTrailWidth
		nameWidth := max(logicalNameMinWidth, availWidth-fixedWidth-2)
		if nameWidth > logicalNameMaxWidth {
			nameWidth = logicalNameMaxWidth
		}
		return []ColumnConfig{
			{"Name", nameWidth, "name"},
			{"Tables", tablesWidth, "tables"},
			{"All", allWidth, "all"},
			{"Operations", logicalMidWidth, "operations"},
			{"Subscribers", logicalTrailWidth, "subscribers"},
		}
	}

	// Narrow: Hide Subscribers column
	if availWidth >= 60 {
		return []ColumnConfig{
			{"Name", logicalNameMinWidth, "name"},
			{"Tables", tablesWidth, "tables"},
			{"All", allWidth, "all"},
			{"Operations", logicalMidWidth, "operations"},
		}
	}

	// Minimum
	return []ColumnConfig{
		{"Name", 22, "name"},
		{"Tables", tablesWidth, "tables"},
		{"Ops", 12, "operations"},
	}
}

// getSubHeaders returns adaptive headers for subscription table.
func (v *ReplicationView) getSubHeaders(availWidth int) []ColumnConfig {
	const (
		enabledWidth = 9
	)

	// Wide mode: Match Name width with Publications, flex Publications column
	if availWidth >= 85 {
		// Calculate Name width to match Publications table
		fixedWidth := enabledWidth + logicalTrailWidth
		remaining := availWidth - fixedWidth - 2

		// Give Name the same proportion as in Publications
		// Pubs fixed: 8+5+20+12 = 45, so Name gets availWidth - 45 - 2
		pubFixedWidth := 8 + 5 + logicalMidWidth + logicalTrailWidth
		nameWidth := max(logicalNameMinWidth, availWidth-pubFixedWidth-2)
		if nameWidth > logicalNameMaxWidth {
			nameWidth = logicalNameMaxWidth
		}

		// Publications column gets the rest
		pubsWidth := remaining - nameWidth
		if pubsWidth < logicalMidWidth {
			pubsWidth = logicalMidWidth
		}

		return []ColumnConfig{
			{"Name", nameWidth, "name"},
			{"Enabled", enabledWidth, "enabled"},
			{"Publications", pubsWidth, "publications"},
			{"Lag", logicalTrailWidth, "lag"},
		}
	}

	// Narrow: Fixed widths matching Publications
	return []ColumnConfig{
		{"Name", logicalNameMinWidth, "name"},
		{"Enabled", enabledWidth, "enabled"},
		{"Publications", logicalMidWidth, "publications"},
		{"Lag", logicalTrailWidth, "lag"},
	}
}

// renderPubRowAdaptive renders a publication row with adaptive columns.
func (v *ReplicationView) renderPubRowAdaptive(p models.Publication, selected bool, headers []ColumnConfig) string {
	baseStyle := lipgloss.NewStyle()
	if selected {
		baseStyle = baseStyle.Background(lipgloss.Color("236"))
	}

	// All tables indicator styling
	allTablesStyle := baseStyle
	allTablesStr := "No"
	if p.AllTables {
		allTablesStyle = allTablesStyle.Foreground(lipgloss.Color("42")) // Green
		allTablesStr = "Yes"
	}

	// Operations styling - green for all enabled, yellow for partial
	opsStyle := baseStyle
	ops := p.OperationFlags()
	allOps := p.Insert && p.Update && p.Delete && p.Truncate
	if allOps {
		opsStyle = opsStyle.Foreground(lipgloss.Color("42")) // Green for full
	} else if p.Insert || p.Update || p.Delete {
		opsStyle = opsStyle.Foreground(lipgloss.Color("214")) // Yellow for partial
	}

	// Subscriber count styling
	subStyle := baseStyle
	subStr := fmt.Sprintf("%d", p.SubscriberCount)
	if p.SubscriberCount > 0 {
		subStyle = subStyle.Foreground(lipgloss.Color("42")) // Green
	} else {
		subStyle = subStyle.Foreground(lipgloss.Color("241")) // Dim
		subStr = "0"
	}

	// Build row dynamically based on available columns
	var row strings.Builder
	for _, h := range headers {
		switch h.Key {
		case "name":
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(p.Name, h.Width), h.Width)))
		case "tables":
			row.WriteString(baseStyle.Render(padRight(fmt.Sprintf("%d", p.TableCount), h.Width)))
		case "all":
			row.WriteString(allTablesStyle.Render(padRight(allTablesStr, h.Width)))
		case "operations":
			row.WriteString(opsStyle.Render(padRight(truncateWithEllipsis(ops, h.Width), h.Width)))
		case "subscribers":
			row.WriteString(subStyle.Render(padRight(subStr, h.Width)))
		}
	}

	return row.String()
}

// renderSubRowAdaptive renders a subscription row with adaptive columns.
func (v *ReplicationView) renderSubRowAdaptive(s models.Subscription, selected bool, headers []ColumnConfig) string {
	baseStyle := lipgloss.NewStyle()
	if selected {
		baseStyle = baseStyle.Background(lipgloss.Color("236"))
	}

	// Enabled status styling
	enabledStyle := baseStyle
	enabledStr := "No"
	if s.Enabled {
		enabledStyle = enabledStyle.Foreground(lipgloss.Color("42")) // Green
		enabledStr = "Yes"
	} else {
		enabledStyle = enabledStyle.Foreground(lipgloss.Color("214")) // Yellow
	}

	// Lag styling
	lagStyle := baseStyle
	lagStr := s.FormatByteLag()
	if s.ByteLag > 10*1024*1024 { // > 10MB
		lagStyle = lagStyle.Foreground(lipgloss.Color("196")) // Red
	} else if s.ByteLag > 1024*1024 { // > 1MB
		lagStyle = lagStyle.Foreground(lipgloss.Color("214")) // Yellow
	} else if s.ByteLag > 0 {
		lagStyle = lagStyle.Foreground(lipgloss.Color("42")) // Green
	}

	// Build row dynamically based on available columns
	var row strings.Builder
	for _, h := range headers {
		switch h.Key {
		case "name":
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(s.Name, h.Width), h.Width)))
		case "enabled":
			row.WriteString(enabledStyle.Render(padRight(enabledStr, h.Width)))
		case "publications":
			pubsStr := strings.Join(s.Publications, ", ")
			row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(pubsStr, h.Width), h.Width)))
		case "lag":
			row.WriteString(lagStyle.Render(padRight(lagStr, h.Width)))
		}
	}

	return row.String()
}
