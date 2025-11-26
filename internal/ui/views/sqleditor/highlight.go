// Package sqleditor provides the SQL Editor view for Steep.
package sqleditor

import (
	"bytes"

	"github.com/alecthomas/chroma/v2/quick"
)

// Syntax highlighting is applied to:
// 1. Executed query header in results pane (renderResultsTable)
// 2. History display when browsing past queries (US6 - history.go, to be implemented)
//
// Usage for history display (when implementing US6):
//   highlightedSQL := HighlightSQL(historyEntry.SQL)
//   // or for multi-line display with custom style:
//   highlightedSQL := HighlightSQLWithStyle(historyEntry.SQL, "dracula")

// HighlightSQL applies syntax highlighting to SQL using Chroma.
// Uses PostgreSQL lexer with Monokai style, outputs ANSI terminal codes.
// Returns original string if highlighting fails.
func HighlightSQL(sql string) string {
	if sql == "" {
		return ""
	}

	var buf bytes.Buffer
	if err := quick.Highlight(&buf, sql, "postgresql", "terminal256", "monokai"); err != nil {
		return sql
	}

	return buf.String()
}

// HighlightSQLWithStyle applies syntax highlighting with a custom style.
// Available styles: monokai, dracula, github, native, etc.
// See: https://xyproto.github.io/splash/docs/all.html
func HighlightSQLWithStyle(sql, style string) string {
	if sql == "" {
		return ""
	}
	if style == "" {
		style = "monokai"
	}

	var buf bytes.Buffer
	if err := quick.Highlight(&buf, sql, "postgresql", "terminal256", style); err != nil {
		return sql
	}

	return buf.String()
}
