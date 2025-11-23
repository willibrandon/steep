package queries

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/charmbracelet/lipgloss"
	"github.com/willibrandon/steep/internal/ui/styles"
	"github.com/willibrandon/steep/internal/ui/views/queries/pev"
)

// ansiRegex matches ANSI escape sequences
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// ExplainView renders an EXPLAIN plan result.
type ExplainView struct {
	query            string
	plan             string
	formattedQuery   string
	formattedPlan    string // just the plan visualization
	formattedContent string // query + plan combined for display
	err              string
	scrollOffset     int
	width            int
	height           int
	pgFormatMissing  bool
	pgFormatChecked  bool
	analyze          bool
}

// NewExplainView creates a new EXPLAIN view.
func NewExplainView() *ExplainView {
	return &ExplainView{}
}

// SetPlan sets the EXPLAIN plan to display.
func (v *ExplainView) SetPlan(query, plan string, analyze bool) {
	v.query = query
	v.plan = plan
	v.analyze = analyze
	v.err = ""
	v.scrollOffset = 0
	// Store formatted query without highlighting for clipboard
	v.formattedQuery = v.formatSQLPlain(query)
	// Combine formatted query and plan for unified scrolling
	formattedQuery := v.formatSQL(query)

	if analyze {
		// Use pev visualization for EXPLAIN ANALYZE (has actual timing data)
		v.formattedPlan = v.formatPev(plan)
	} else {
		// Use JSON format for regular EXPLAIN
		v.formattedPlan = v.formatJSON(plan)
	}

	v.formattedContent = formattedQuery + "\n\n" + v.formattedPlan
}

// Query returns the formatted query string (for clipboard).
func (v *ExplainView) Query() string {
	if v.formattedQuery != "" {
		return v.formattedQuery
	}
	return v.query
}

// Plan returns the raw plan JSON string.
func (v *ExplainView) Plan() string {
	return v.plan
}

// FormattedPlan returns the formatted plan output (visual or JSON) with ANSI codes stripped.
func (v *ExplainView) FormattedPlan() string {
	return ansiRegex.ReplaceAllString(v.formattedPlan, "")
}

// SetError sets an error message to display.
func (v *ExplainView) SetError(query string, err error) {
	v.query = query
	v.plan = ""
	v.formattedPlan = ""
	v.formattedContent = ""
	v.err = err.Error()
	v.scrollOffset = 0
}

// SetSize sets the viewport dimensions.
func (v *ExplainView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

// ScrollDown scrolls down by n lines.
func (v *ExplainView) ScrollDown(n int) {
	lines := v.getLines()
	maxOffset := max(0, len(lines)-v.contentHeight())
	v.scrollOffset = min(v.scrollOffset+n, maxOffset)
}

// ScrollUp scrolls up by n lines.
func (v *ExplainView) ScrollUp(n int) {
	v.scrollOffset = max(0, v.scrollOffset-n)
}

// ScrollToTop scrolls to the top.
func (v *ExplainView) ScrollToTop() {
	v.scrollOffset = 0
}

// ScrollToBottom scrolls to the bottom.
func (v *ExplainView) ScrollToBottom() {
	lines := v.getLines()
	v.scrollOffset = max(0, len(lines)-v.contentHeight())
}

// PageDown scrolls down by a page.
func (v *ExplainView) PageDown() {
	v.ScrollDown(v.contentHeight())
}

// PageUp scrolls up by a page.
func (v *ExplainView) PageUp() {
	v.ScrollUp(v.contentHeight())
}

// View renders the EXPLAIN view.
func (v *ExplainView) View() string {
	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.ColorAccent).
		MarginBottom(1)

	titleText := "EXPLAIN Plan"
	if v.analyze {
		titleText = "EXPLAIN ANALYZE Plan"
	}
	title := titleStyle.Render(titleText)

	// Content (query + plan combined, scrollable)
	var content string
	if v.err != "" {
		errorStyle := lipgloss.NewStyle().
			Foreground(styles.ColorError)
		content = errorStyle.Render("Error: " + v.err)
	} else {
		content = v.renderContent()
	}

	// Footer with navigation hints
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		MarginTop(1)

	lines := v.getLines()
	scrollInfo := ""
	if len(lines) > v.contentHeight() {
		scrollInfo = fmt.Sprintf(" (%d/%d)", v.scrollOffset+1, len(lines))
	}

	footer := footerStyle.Render("[j/k]scroll [g/G]top/bottom [y]copy query [Y]copy plan [esc/q]back" + scrollInfo)

	// Warning if pgFormatter is missing
	if v.pgFormatMissing {
		warningStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")). // Yellow
			Bold(true)
		warning := warningStyle.Render("⚠ pgFormatter not available - SQL not formatted (docker pull backplane/pgformatter)")
		footer = warning + "\n" + footer
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		content,
		footer,
	)
}

// renderContent renders the scrollable plan content.
func (v *ExplainView) renderContent() string {
	lines := v.getLines()
	if len(lines) == 0 {
		return styles.InfoStyle.Render("No plan available")
	}

	// Get visible lines
	height := v.contentHeight()
	endIdx := min(v.scrollOffset+height, len(lines))
	visibleLines := lines[v.scrollOffset:endIdx]

	// Pad to fill height
	for len(visibleLines) < height {
		visibleLines = append(visibleLines, "")
	}

	return strings.Join(visibleLines, "\n")
}

// getLines returns the formatted content split into lines.
func (v *ExplainView) getLines() []string {
	if v.formattedContent == "" {
		return nil
	}
	return strings.Split(v.formattedContent, "\n")
}

// contentHeight returns the height available for plan content.
func (v *ExplainView) contentHeight() int {
	// height - title(1) - footer(1) - margins(2)
	return max(1, v.height-4)
}

// formatJSON formats the EXPLAIN JSON for readability.
func (v *ExplainView) formatJSON(jsonStr string) string {
	if jsonStr == "" {
		return ""
	}

	// Pretty print the JSON
	var prettyJSON bytes.Buffer
	err := json.Indent(&prettyJSON, []byte(jsonStr), "", "  ")
	if err != nil {
		// If formatting fails, return as-is
		return jsonStr
	}

	// Apply syntax highlighting
	return v.highlightJSON(prettyJSON.String())
}

// formatSQLPlain formats a SQL query without syntax highlighting (for clipboard).
func (v *ExplainView) formatSQLPlain(sql string) string {
	if sql == "" {
		return ""
	}

	// Workaround for pgFormatter bug: empty strings ('') prevent proper column wrapping
	// Replace with placeholder before formatting, then restore after
	const placeholder = "'__EMPTY_STRING_PLACEHOLDER__'"
	sqlToFormat := strings.ReplaceAll(sql, "''", placeholder)

	// Try pg_format via Docker with -W 1 for one column per line
	cmd := exec.Command("docker", "run", "--rm", "-i", "backplane/pgformatter", "-s", "2", "-W", "1")
	cmd.Stdin = strings.NewReader(sqlToFormat)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		v.pgFormatChecked = true
		formatted := strings.TrimSpace(out.String())
		// Restore empty strings
		return strings.ReplaceAll(formatted, placeholder, "''")
	}

	// Check if Docker/image is missing
	if !v.pgFormatChecked {
		v.pgFormatChecked = true
		v.pgFormatMissing = true
	}

	return sql
}

// formatSQL formats a SQL query with syntax highlighting.
func (v *ExplainView) formatSQL(sql string) string {
	if sql == "" {
		return ""
	}

	// Workaround for pgFormatter bug: empty strings ('') prevent proper column wrapping
	// Replace with placeholder before formatting, then restore after
	const placeholder = "'__EMPTY_STRING_PLACEHOLDER__'"
	sqlToFormat := strings.ReplaceAll(sql, "''", placeholder)

	// Try pg_format via Docker with -W 1 for one column per line
	formatted := sql
	cmd := exec.Command("docker", "run", "--rm", "-i", "backplane/pgformatter", "-s", "2", "-W", "1")
	cmd.Stdin = strings.NewReader(sqlToFormat)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		formatted = strings.TrimSpace(out.String())
		// Restore empty strings
		formatted = strings.ReplaceAll(formatted, placeholder, "''")
	}

	// Apply syntax highlighting
	var buf bytes.Buffer
	if err := quick.Highlight(&buf, formatted, "postgresql", "terminal256", "monokai"); err != nil {
		return formatted
	}

	return buf.String()
}

// highlightJSON applies simple syntax highlighting to JSON.
func (v *ExplainView) highlightJSON(jsonStr string) string {
	// Simple highlighting: keys in cyan, strings in green, numbers in yellow
	lines := strings.Split(jsonStr, "\n")
	var highlighted []string

	keyStyle := lipgloss.NewStyle().Foreground(styles.ColorAccent)
	stringStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	numberStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	boolStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("13"))

	for _, line := range lines {
		// Highlight keys (before colon)
		if colonIdx := strings.Index(line, ":"); colonIdx > 0 {
			key := line[:colonIdx]
			value := line[colonIdx:]

			// Style the key
			key = keyStyle.Render(key)

			// Style the value based on type
			valuePart := strings.TrimPrefix(value, ": ")
			valuePart = strings.TrimSuffix(valuePart, ",")
			valuePart = strings.TrimSpace(valuePart)

			if strings.HasPrefix(valuePart, "\"") {
				// String value
				value = ": " + stringStyle.Render(strings.TrimSuffix(line[colonIdx+2:], ","))
				if strings.HasSuffix(line, ",") {
					value += ","
				}
			} else if valuePart == "true" || valuePart == "false" || valuePart == "null" {
				// Boolean/null
				value = ": " + boolStyle.Render(valuePart)
				if strings.HasSuffix(line, ",") {
					value += ","
				}
			} else if len(valuePart) > 0 && (valuePart[0] >= '0' && valuePart[0] <= '9' || valuePart[0] == '-') {
				// Number
				value = ": " + numberStyle.Render(valuePart)
				if strings.HasSuffix(line, ",") {
					value += ","
				}
			}

			line = key + value
		}

		highlighted = append(highlighted, line)
	}

	return strings.Join(highlighted, "\n")
}

// formatPev formats the EXPLAIN plan using pev visualization.
func (v *ExplainView) formatPev(planJSON string) string {
	if planJSON == "" {
		return ""
	}

	// Use pev to visualize the plan
	var buf bytes.Buffer
	reader := strings.NewReader(planJSON)

	// Use terminal width for wrapping, default to 80
	width := uint(80)
	if v.width > 0 {
		width = uint(v.width)
	}

	err := pev.Visualize(&buf, reader, width)
	if err != nil {
		// Show error and fall back to JSON format
		errorMsg := fmt.Sprintf("PEV visualization failed: %v\n\n", err)
		return errorMsg + v.formatJSON(planJSON)
	}

	return buf.String()
}
