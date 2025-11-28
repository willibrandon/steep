package logs

import (
	"regexp"
	"strings"

	"github.com/willibrandon/steep/internal/db/models"
)

// LogFilter holds filter criteria for log entries.
type LogFilter struct {
	// Severity filters to a specific severity level (nil = show all)
	Severity *models.LogSeverity
	// MinSeverity is the minimum severity to show (default: SeverityDebug)
	MinSeverity models.LogSeverity
	// SearchPattern is the compiled regex for text search (nil = no search)
	SearchPattern *regexp.Regexp
	// SearchText is the original search text (for display)
	SearchText string
}

// NewLogFilter creates a new filter with default settings.
func NewLogFilter() *LogFilter {
	return &LogFilter{
		MinSeverity: models.SeverityDebug,
	}
}

// Matches returns true if the entry passes all filter criteria.
func (f *LogFilter) Matches(entry models.LogEntry) bool {
	// Check severity filter
	if f.Severity != nil && entry.Severity != *f.Severity {
		return false
	}

	// Check minimum severity
	if entry.Severity < f.MinSeverity {
		return false
	}

	// Check search pattern
	if f.SearchPattern != nil {
		if !f.SearchPattern.MatchString(entry.Message) &&
			!f.SearchPattern.MatchString(entry.Detail) &&
			!f.SearchPattern.MatchString(entry.Database) &&
			!f.SearchPattern.MatchString(entry.User) {
			return false
		}
	}

	return true
}

// SetLevel sets the severity filter from a string.
// Valid values: "error", "warning", "warn", "info", "debug", "all", "clear"
// Add "+" suffix for minimum level (e.g., "warn+" shows warnings AND errors)
func (f *LogFilter) SetLevel(level string) error {
	level = strings.ToLower(strings.TrimSpace(level))

	// Check for "+" suffix (minimum level mode)
	minMode := strings.HasSuffix(level, "+")
	if minMode {
		level = strings.TrimSuffix(level, "+")
	}

	var sev models.LogSeverity
	switch level {
	case "error", "fatal", "panic":
		sev = models.SeverityError
	case "warning", "warn":
		sev = models.SeverityWarning
	case "info", "log", "notice":
		sev = models.SeverityInfo
	case "debug":
		sev = models.SeverityDebug
	case "all", "clear", "":
		f.Severity = nil
		f.MinSeverity = models.SeverityDebug
		return nil
	default:
		sev = models.ParseSeverity(strings.ToUpper(level))
	}

	if minMode {
		// Minimum level mode: show this level and above
		f.Severity = nil
		f.MinSeverity = sev
	} else {
		// Exact match mode: show only this level
		f.Severity = &sev
		f.MinSeverity = models.SeverityDebug
	}

	return nil
}

// SetSearch sets the search pattern from a string.
// Returns an error if the pattern is invalid regex.
func (f *LogFilter) SetSearch(pattern string) error {
	if pattern == "" {
		f.SearchPattern = nil
		f.SearchText = ""
		return nil
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}

	f.SearchPattern = re
	f.SearchText = pattern
	return nil
}

// ClearSearch clears the search filter.
func (f *LogFilter) ClearSearch() {
	f.SearchPattern = nil
	f.SearchText = ""
}

// ClearLevel clears the severity filter.
func (f *LogFilter) ClearLevel() {
	f.Severity = nil
}

// Clear resets all filters to defaults.
func (f *LogFilter) Clear() {
	f.Severity = nil
	f.MinSeverity = models.SeverityDebug
	f.SearchPattern = nil
	f.SearchText = ""
}

// HasFilters returns true if any filters are active.
func (f *LogFilter) HasFilters() bool {
	return f.Severity != nil || f.MinSeverity > models.SeverityDebug || f.SearchPattern != nil
}

// LevelString returns the current level filter as a string.
func (f *LogFilter) LevelString() string {
	if f.Severity != nil {
		return f.Severity.String()
	}
	if f.MinSeverity > models.SeverityDebug {
		return f.MinSeverity.String() + "+"
	}
	return ""
}

// FilterEntries filters a slice of entries and returns only matching ones.
func (f *LogFilter) FilterEntries(entries []models.LogEntry) []models.LogEntry {
	if !f.HasFilters() {
		return entries
	}

	result := make([]models.LogEntry, 0, len(entries))
	for _, entry := range entries {
		if f.Matches(entry) {
			result = append(result, entry)
		}
	}
	return result
}
