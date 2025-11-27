// Package highlight provides SQL syntax highlighting and formatting utilities.
package highlight

import (
	"bytes"
	"os/exec"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
)

// SQL applies syntax highlighting to SQL using Chroma.
// Uses PostgreSQL lexer with Monokai style, outputs ANSI terminal codes.
// Returns original string if highlighting fails.
func SQL(sql string) string {
	if sql == "" {
		return ""
	}

	var buf bytes.Buffer
	if err := quick.Highlight(&buf, sql, "postgresql", "terminal256", "monokai"); err != nil {
		return sql
	}

	return buf.String()
}

// SQLWithStyle applies syntax highlighting with a custom style.
// Available styles: monokai, dracula, github, native, etc.
// See: https://xyproto.github.io/splash/docs/all.html
func SQLWithStyle(sql, style string) string {
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

// FormatSQL formats SQL using pgFormatter via Docker.
// Returns the original SQL if formatting fails.
func FormatSQL(sql string) string {
	if sql == "" {
		return ""
	}

	// Try to format with pgFormatter
	cmd := exec.Command("docker", "run", "--rm", "-i", "backplane/pgformatter", "-s", "2", "-W", "1")
	cmd.Stdin = strings.NewReader(sql)

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return sql // Return original if formatting fails
	}

	formatted := strings.TrimSpace(out.String())
	if formatted == "" {
		return sql
	}

	return formatted
}

// FormatAndHighlightSQL formats SQL with pgFormatter and applies syntax highlighting.
// If formatting fails, applies highlighting to the original SQL.
func FormatAndHighlightSQL(sql string) string {
	formatted := FormatSQL(sql)
	return SQL(formatted)
}

// FormatAndHighlightSQLWithStyle formats SQL and applies highlighting with a custom style.
func FormatAndHighlightSQLWithStyle(sql, style string) string {
	formatted := FormatSQL(sql)
	return SQLWithStyle(formatted, style)
}
