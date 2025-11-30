package roles

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/db/queries"
	"github.com/willibrandon/steep/internal/ui/styles"
)

// buildDetailsLines builds the content lines for the details panel.
func (v *RolesView) buildDetailsLines() []string {
	if v.details == nil {
		return nil
	}

	r := &v.details.Role
	var lines []string
	contentWidth := max(76, v.width-4)

	// Role Overview
	lines = append(lines, styles.HeaderStyle.Render("Overview"))
	lines = append(lines, fmt.Sprintf("  Name:            %s", r.Name))
	lines = append(lines, fmt.Sprintf("  OID:             %d", r.OID))
	lines = append(lines, "")

	// Attributes
	lines = append(lines, styles.HeaderStyle.Render("Attributes"))
	attrs := queries.FormatRoleAttributes(queries.RoleAttributeInfo{
		IsSuperuser:   r.IsSuperuser,
		CanLogin:      r.CanLogin,
		CanCreateRole: r.CanCreateRole,
		CanCreateDB:   r.CanCreateDB,
		CanBypassRLS:  r.CanBypassRLS,
	})
	lines = append(lines, fmt.Sprintf("  Attributes:      %s", attrs))

	// Individual attribute flags
	if r.IsSuperuser {
		lines = append(lines, fmt.Sprintf("  - Superuser:     %s", styles.WarningStyle.Render("Yes")))
	} else {
		lines = append(lines, "  - Superuser:     No")
	}
	if r.CanLogin {
		lines = append(lines, "  - Can Login:     Yes")
	} else {
		lines = append(lines, "  - Can Login:     No")
	}
	if r.CanCreateRole {
		lines = append(lines, "  - Create Role:   Yes")
	} else {
		lines = append(lines, "  - Create Role:   No")
	}
	if r.CanCreateDB {
		lines = append(lines, "  - Create DB:     Yes")
	} else {
		lines = append(lines, "  - Create DB:     No")
	}
	if r.CanBypassRLS {
		lines = append(lines, fmt.Sprintf("  - Bypass RLS:    %s", styles.WarningStyle.Render("Yes")))
	} else {
		lines = append(lines, "  - Bypass RLS:    No")
	}
	if r.Inherit {
		lines = append(lines, "  - Inherit:       Yes")
	} else {
		lines = append(lines, "  - Inherit:       No")
	}
	if r.Replication {
		lines = append(lines, "  - Replication:   Yes")
	} else {
		lines = append(lines, "  - Replication:   No")
	}
	lines = append(lines, "")

	// Connection settings
	lines = append(lines, styles.HeaderStyle.Render("Connection"))
	lines = append(lines, fmt.Sprintf("  Connection Limit: %s", queries.FormatConnectionLimit(r.ConnectionLimit)))
	lines = append(lines, fmt.Sprintf("  Valid Until:      %s", queries.FormatValidUntil(r.ValidUntil)))
	lines = append(lines, "")

	// Memberships (roles this role belongs to)
	if len(v.details.Memberships) > 0 {
		lines = append(lines, styles.HeaderStyle.Render(fmt.Sprintf("Member Of (%d)", len(v.details.Memberships))))
		for _, m := range v.details.Memberships {
			opts := ""
			if m.AdminOption {
				opts += " [admin]"
			}
			if !m.InheritOption {
				opts += " [noinherit]"
			}
			lines = append(lines, fmt.Sprintf("  - %s%s", m.RoleName, opts))
		}
		lines = append(lines, "")
	}

	// Members (roles that are members of this role)
	if len(v.details.Members) > 0 {
		lines = append(lines, styles.HeaderStyle.Render(fmt.Sprintf("Members (%d)", len(v.details.Members))))
		for _, m := range v.details.Members {
			opts := ""
			if m.AdminOption {
				opts += " [admin]"
			}
			lines = append(lines, fmt.Sprintf("  - %s%s", m.MemberName, opts))
		}
		lines = append(lines, "")
	}

	// Owned objects
	if len(v.details.OwnedTables) > 0 {
		lines = append(lines, styles.HeaderStyle.Render(fmt.Sprintf("Owned Objects (%d)", len(v.details.OwnedTables))))

		// Group by type
		byType := make(map[string][]string)
		for _, obj := range v.details.OwnedTables {
			byType[obj.ObjectType] = append(byType[obj.ObjectType], obj.ObjectName)
		}

		for objType, names := range byType {
			lines = append(lines, fmt.Sprintf("  %s:", objType))
			for _, name := range names {
				if len(name) > contentWidth-6 {
					name = name[:contentWidth-9] + "..."
				}
				lines = append(lines, fmt.Sprintf("    - %s", name))
			}
		}
		lines = append(lines, "")
	}

	// Configuration parameters
	if len(r.Config) > 0 {
		lines = append(lines, styles.HeaderStyle.Render(fmt.Sprintf("Configuration (%d)", len(r.Config))))
		for _, cfg := range r.Config {
			lines = append(lines, fmt.Sprintf("  - %s", cfg))
		}
	}

	return lines
}

// detailsContentHeight returns the visible content height for details panel.
func (v *RolesView) detailsContentHeight() int {
	// status bar(3 with border) + title(2 with margin) + footer(3 with border) = 8
	return max(5, v.height-8)
}

// scrollDetailsUp scrolls the details panel up by n lines.
func (v *RolesView) scrollDetailsUp(n int) {
	v.detailsScrollOffset = max(0, v.detailsScrollOffset-n)
}

// scrollDetailsDown scrolls the details panel down by n lines.
func (v *RolesView) scrollDetailsDown(n int) {
	maxOffset := max(0, len(v.detailsLines)-v.detailsContentHeight())
	v.detailsScrollOffset = min(v.detailsScrollOffset+n, maxOffset)
}

// scrollDetailsToBottom scrolls to the bottom of details.
func (v *RolesView) scrollDetailsToBottom() {
	maxOffset := max(0, len(v.detailsLines)-v.detailsContentHeight())
	v.detailsScrollOffset = maxOffset
}

// renderDetails renders the role details as a full-screen view.
func (v *RolesView) renderDetails() string {
	if v.details == nil {
		return styles.InfoStyle.Render("No role selected")
	}

	var b strings.Builder

	// Status bar
	b.WriteString(v.renderStatusBar())
	b.WriteString("\n")

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent).
		MarginBottom(1)
	b.WriteString(titleStyle.Render(fmt.Sprintf("Role Details: %s", v.details.Name)))
	b.WriteString("\n")

	// Content with scrolling
	contentHeight := v.detailsContentHeight()
	lines := v.detailsLines
	if len(lines) == 0 {
		lines = []string{"No details available"}
	}

	endIdx := min(v.detailsScrollOffset+contentHeight, len(lines))
	visibleLines := lines[v.detailsScrollOffset:endIdx]

	// Pad to fill height
	for len(visibleLines) < contentHeight {
		visibleLines = append(visibleLines, "")
	}
	b.WriteString(strings.Join(visibleLines, "\n"))
	b.WriteString("\n")

	// Footer with scroll indicators
	scrollInfo := ""
	if len(lines) > contentHeight {
		scrollInfo = fmt.Sprintf(" %d/%d", v.detailsScrollOffset+1, len(lines))
	}

	hints := styles.FooterHintStyle.Render(fmt.Sprintf("[j/k]↕ [g/G]top/btm [y]copy [Esc]back%s", scrollInfo))

	gap := v.width - lipgloss.Width(hints) - 4
	if gap < 1 {
		gap = 1
	}
	spaces := lipgloss.NewStyle().Width(gap).Render("")

	b.WriteString(styles.FooterStyle.Width(v.width - 2).Render(hints + spaces))

	return b.String()
}
