package sqleditor

import (
	"strings"
	"unicode/utf8"
)

// padOrTruncate pads or truncates a string to the given display width.
// Uses rune count for width calculation (accurate for most Unicode).
func padOrTruncate(s string, width int) string {
	// Fast path: ASCII-only strings can use byte length
	if len(s) == utf8.RuneCountInString(s) {
		// Pure ASCII
		if len(s) > width {
			if width > 3 {
				return s[:width-3] + "..."
			}
			if width > 0 {
				return s[:width]
			}
			return ""
		}
		if len(s) < width {
			return s + strings.Repeat(" ", width-len(s))
		}
		return s
	}

	// Unicode path: use rune count
	runeCount := utf8.RuneCountInString(s)
	if runeCount > width {
		// Truncate by runes
		if width > 3 {
			return truncateToRunes(s, width-3) + "..."
		}
		if width > 0 {
			return truncateToRunes(s, width)
		}
		return ""
	}
	if runeCount < width {
		return s + strings.Repeat(" ", width-runeCount)
	}
	return s
}

// truncateToRunes truncates a string to n runes.
func truncateToRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
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
