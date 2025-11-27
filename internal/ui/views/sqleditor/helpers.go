package sqleditor

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// padOrTruncate pads or truncates a string to the given display width.
// Uses lipgloss.Width for proper unicode handling.
func padOrTruncate(s string, width int) string {
	displayWidth := lipgloss.Width(s)
	if displayWidth > width {
		// Truncate by runes, not bytes
		if width > 3 {
			return truncateRunes(s, width-3) + "..."
		}
		return truncateRunes(s, width)
	}
	return s + strings.Repeat(" ", width-displayWidth)
}

// truncateRunes truncates a string to n display characters.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	width := 0
	for i, r := range runes {
		w := lipgloss.Width(string(r))
		if width+w > n {
			return string(runes[:i])
		}
		width += w
	}
	return s
}

// isSelectQuery checks if SQL is a SELECT-type query that returns rows.
func isSelectQuery(sql string) bool {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	return strings.HasPrefix(upper, "SELECT") ||
		strings.HasPrefix(upper, "WITH") ||
		strings.HasPrefix(upper, "TABLE") ||
		strings.HasPrefix(upper, "(SELECT") ||
		strings.HasPrefix(upper, "VALUES")
}

// hasLimitOrOffset checks if SQL already contains LIMIT or OFFSET clause.
func hasLimitOrOffset(sql string) bool {
	upper := strings.ToUpper(sql)
	// Check for LIMIT or OFFSET keywords (with word boundaries)
	// This is a simple check - won't catch LIMIT in string literals, but good enough
	return strings.Contains(upper, " LIMIT ") ||
		strings.Contains(upper, " OFFSET ") ||
		strings.Contains(upper, "\nLIMIT ") ||
		strings.Contains(upper, "\tLIMIT ") ||
		strings.Contains(upper, "\nOFFSET ") ||
		strings.Contains(upper, "\tOFFSET ") ||
		strings.HasSuffix(upper, " LIMIT") ||
		strings.HasSuffix(upper, " OFFSET")
}
