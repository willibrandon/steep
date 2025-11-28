// Package logs provides the Log Viewer view.
package logs

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SearchDirection indicates how to find entries relative to a timestamp.
type SearchDirection int

const (
	SearchClosest SearchDirection = iota // Find closest entry (default)
	SearchAfter                          // Find first entry at or after timestamp (>)
	SearchBefore                         // Find last entry at or before timestamp (<)
)

// ParsedTimestamp contains the result of parsing a user-provided timestamp.
type ParsedTimestamp struct {
	Time      time.Time       // The parsed timestamp
	Format    string          // Description of the format that matched
	Relative  bool            // True if this was a relative timestamp (-1h, -30m, etc.)
	Direction SearchDirection // Search direction (closest, after, before)
}

// ParseTimestampInput parses a user-provided timestamp string.
// Supported formats (FR-024):
//   - ISO 8601: 2025-11-27T14:30:00
//   - Date-time: 2025-11-27 14:30 or 2025-11-27 14:30:00
//   - Time-only: 14:30 (assumes today)
//   - Relative: -1h, -30m, -2d, -1w
//   - Direction prefix: >14:30 (after), <14:30 (before)
func ParseTimestampInput(input string) (*ParsedTimestamp, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("empty timestamp")
	}

	// Check for direction prefix (> or <)
	direction := SearchClosest
	if strings.HasPrefix(input, ">") {
		direction = SearchAfter
		input = strings.TrimSpace(input[1:])
	} else if strings.HasPrefix(input, "<") {
		direction = SearchBefore
		input = strings.TrimSpace(input[1:])
	}

	if input == "" {
		return nil, fmt.Errorf("empty timestamp after direction prefix")
	}

	// Try relative format first (-1h, -30m, -2d, -1w)
	if strings.HasPrefix(input, "-") {
		result, err := parseRelativeTimestamp(input)
		if err != nil {
			return nil, err
		}
		result.Direction = direction
		return result, nil
	}

	// Try absolute formats
	result, err := parseAbsoluteTimestamp(input)
	if err != nil {
		return nil, err
	}
	result.Direction = direction
	return result, nil
}

// parseRelativeTimestamp parses relative timestamps like -1h, -30m, -2d.
func parseRelativeTimestamp(input string) (*ParsedTimestamp, error) {
	// Match pattern: -<number><unit>
	pattern := regexp.MustCompile(`^-(\d+)([smhdw])$`)
	matches := pattern.FindStringSubmatch(input)
	if matches == nil {
		return nil, fmt.Errorf("invalid relative format: %s (use -1h, -30m, -2d, -1w)", input)
	}

	value, _ := strconv.Atoi(matches[1])
	unit := matches[2]

	var duration time.Duration
	switch unit {
	case "s":
		duration = time.Duration(value) * time.Second
	case "m":
		duration = time.Duration(value) * time.Minute
	case "h":
		duration = time.Duration(value) * time.Hour
	case "d":
		duration = time.Duration(value) * 24 * time.Hour
	case "w":
		duration = time.Duration(value) * 7 * 24 * time.Hour
	}

	ts := time.Now().Add(-duration)
	return &ParsedTimestamp{
		Time:     ts,
		Format:   fmt.Sprintf("relative (%s ago)", input[1:]),
		Relative: true,
	}, nil
}

// parseAbsoluteTimestamp parses absolute timestamps in various formats.
func parseAbsoluteTimestamp(input string) (*ParsedTimestamp, error) {
	now := time.Now()
	loc := now.Location()

	// Define formats to try, in order of specificity
	formats := []struct {
		layout string
		desc   string
		adjust func(time.Time) time.Time
	}{
		// ISO 8601 formats
		{time.RFC3339, "ISO 8601", nil},
		{time.RFC3339Nano, "ISO 8601 nano", nil},
		{"2006-01-02T15:04:05", "ISO 8601 local", nil},
		{"2006-01-02T15:04", "ISO 8601 minute", nil},

		// Date-time formats
		{"2006-01-02 15:04:05", "date-time", nil},
		{"2006-01-02 15:04", "date-time minute", nil},
		{"2006-1-2 15:04:05", "date-time short", nil},
		{"2006-1-2 15:04", "date-time short minute", nil},

		// Date only (assume start of day)
		{"2006-01-02", "date only", nil},
		{"2006-1-2", "date only short", nil},

		// Time only (assume today)
		{"15:04:05", "time only", func(t time.Time) time.Time {
			return time.Date(now.Year(), now.Month(), now.Day(),
				t.Hour(), t.Minute(), t.Second(), 0, loc)
		}},
		{"15:04", "time only minute", func(t time.Time) time.Time {
			return time.Date(now.Year(), now.Month(), now.Day(),
				t.Hour(), t.Minute(), 0, 0, loc)
		}},
		{"3:04pm", "time 12h", func(t time.Time) time.Time {
			return time.Date(now.Year(), now.Month(), now.Day(),
				t.Hour(), t.Minute(), 0, 0, loc)
		}},
		{"3:04PM", "time 12h upper", func(t time.Time) time.Time {
			return time.Date(now.Year(), now.Month(), now.Day(),
				t.Hour(), t.Minute(), 0, 0, loc)
		}},
	}

	// Try each format
	for _, f := range formats {
		t, err := time.ParseInLocation(f.layout, input, loc)
		if err == nil {
			if f.adjust != nil {
				t = f.adjust(t)
			}
			return &ParsedTimestamp{
				Time:     t,
				Format:   f.desc,
				Relative: false,
			}, nil
		}
	}

	return nil, fmt.Errorf("unrecognized timestamp format: %s\nSupported: ISO 8601, date-time (YYYY-MM-DD HH:MM), time-only (HH:MM), relative (-1h, -30m, -2d)", input)
}

// FormatTimestampHint returns a hint string showing supported timestamp formats.
func FormatTimestampHint() string {
	return `Supported formats:
  ISO 8601:    2025-11-27T14:30:00
  Date-time:   2025-11-27 14:30
  Time only:   14:30 (today)
  Relative:    -1h, -30m, -2d, -1w`
}
