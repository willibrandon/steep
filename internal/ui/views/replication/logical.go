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
		// Publication table headers
		pubHeaders := []struct {
			name  string
			width int
		}{
			{"Name", 22},
			{"Tables", 8},
			{"All", 5},
			{"Operations", 20},
			{"Subscribers", 12},
		}
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
		var headerRow strings.Builder
		for _, h := range pubHeaders {
			headerRow.WriteString(headerStyle.Render(padRight(h.name, h.width)))
		}
		b.WriteString(headerRow.String())
		b.WriteString("\n")

		for i, pub := range v.data.Publications {
			if i >= halfHeight-3 {
				break
			}
			selected := v.logicalFocusPubs && i == v.pubSelectedIdx
			b.WriteString(v.renderPubRow(pub, selected, pubHeaders))
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
		// Subscription table headers
		subHeaders := []struct {
			name  string
			width int
		}{
			{"Name", 22},
			{"Enabled", 9},
			{"Publications", 20},
			{"Lag", 12},
		}
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
		var headerRow strings.Builder
		for _, h := range subHeaders {
			headerRow.WriteString(headerStyle.Render(padRight(h.name, h.width)))
		}
		b.WriteString(headerRow.String())
		b.WriteString("\n")

		for i, sub := range v.data.Subscriptions {
			if i >= halfHeight-3 {
				break
			}
			selected := !v.logicalFocusPubs && i == v.subSelectedIdx
			b.WriteString(v.renderSubRow(sub, selected, subHeaders))
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

// renderPubRow renders a publication row with styling.
func (v *ReplicationView) renderPubRow(p models.Publication, selected bool, headers []struct {
	name  string
	width int
}) string {
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

	var row strings.Builder
	row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(p.Name, headers[0].width), headers[0].width)))
	row.WriteString(baseStyle.Render(padRight(fmt.Sprintf("%d", p.TableCount), headers[1].width)))
	row.WriteString(allTablesStyle.Render(padRight(allTablesStr, headers[2].width)))
	row.WriteString(opsStyle.Render(padRight(ops, headers[3].width)))
	row.WriteString(subStyle.Render(padRight(subStr, headers[4].width)))

	return row.String()
}

// renderSubRow renders a subscription row with styling.
func (v *ReplicationView) renderSubRow(s models.Subscription, selected bool, headers []struct {
	name  string
	width int
}) string {
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

	// Publications list
	pubsStr := strings.Join(s.Publications, ", ")
	if len(pubsStr) > headers[2].width {
		pubsStr = truncateWithEllipsis(pubsStr, headers[2].width)
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

	var row strings.Builder
	row.WriteString(baseStyle.Render(padRight(truncateWithEllipsis(s.Name, headers[0].width), headers[0].width)))
	row.WriteString(enabledStyle.Render(padRight(enabledStr, headers[1].width)))
	row.WriteString(baseStyle.Render(padRight(pubsStr, headers[2].width)))
	row.WriteString(lagStyle.Render(padRight(lagStr, headers[3].width)))

	return row.String()
}
