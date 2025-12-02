package sqleditor

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// NullDisplayValue is the string shown for NULL values.
const NullDisplayValue = "NULL"

// FormatValue converts a PostgreSQL value to a display string.
// Handles all common PostgreSQL types returned by pgx.
func FormatValue(val any) string {
	if val == nil {
		return NullDisplayValue
	}

	switch v := val.(type) {
	// Boolean
	case bool:
		if v {
			return "true"
		}
		return "false"

	// Integers
	case int:
		return strconv.Itoa(v)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)

	// Floating point
	case float32:
		return formatFloat(float64(v), 32)
	case float64:
		return formatFloat(v, 64)

	// Strings
	case string:
		// Sanitize control characters that would break table layout
		return sanitizeForDisplay(v)
	case []byte:
		// Check if it's printable text or binary
		if isPrintable(v) {
			return sanitizeForDisplay(string(v))
		}
		// Format as hex for binary data
		return fmt.Sprintf("\\x%x", v)

	// Time types
	case time.Time:
		return formatTime(v)
	case pgtype.Date:
		if v.Valid {
			return v.Time.Format("2006-01-02")
		}
		return NullDisplayValue
	case pgtype.Timestamp:
		if v.Valid {
			return v.Time.Format("2006-01-02 15:04:05")
		}
		return NullDisplayValue
	case pgtype.Timestamptz:
		if v.Valid {
			return v.Time.Format("2006-01-02 15:04:05 MST")
		}
		return NullDisplayValue
	case pgtype.Time:
		if v.Valid {
			// pgtype.Time stores microseconds since midnight
			h := v.Microseconds / 3600000000
			m := (v.Microseconds % 3600000000) / 60000000
			s := (v.Microseconds % 60000000) / 1000000
			us := v.Microseconds % 1000000
			if us > 0 {
				return fmt.Sprintf("%02d:%02d:%02d.%06d", h, m, s, us)
			}
			return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
		}
		return NullDisplayValue
	case pgtype.Interval:
		if v.Valid {
			return formatInterval(v)
		}
		return NullDisplayValue

	// UUID
	case [16]byte:
		return formatUUID(v)
	case pgtype.UUID:
		if v.Valid {
			return formatUUID(v.Bytes)
		}
		return NullDisplayValue

	// JSON/JSONB
	case map[string]any:
		return formatJSON(v)
	case []any:
		return formatJSON(v)

	// Numeric (arbitrary precision)
	case pgtype.Numeric:
		if v.Valid {
			if v.NaN {
				return "NaN"
			}
			if v.InfinityModifier == pgtype.Infinity {
				return "Infinity"
			}
			if v.InfinityModifier == pgtype.NegativeInfinity {
				return "-Infinity"
			}
			// Convert to float64 for display (may lose precision)
			f, _ := v.Float64Value()
			if f.Valid {
				return formatFloat(f.Float64, 64)
			}
			// Fall back to string representation
			return v.Int.String()
		}
		return NullDisplayValue

	// Network types
	case netip.Addr:
		return v.String()
	case netip.Prefix:
		return v.String()
	case net.HardwareAddr:
		return v.String()

	// Text types with explicit pgtype wrappers
	case pgtype.Text:
		if v.Valid {
			return sanitizeForDisplay(v.String)
		}
		return NullDisplayValue
	case pgtype.Int2:
		if v.Valid {
			return strconv.FormatInt(int64(v.Int16), 10)
		}
		return NullDisplayValue
	case pgtype.Int4:
		if v.Valid {
			return strconv.FormatInt(int64(v.Int32), 10)
		}
		return NullDisplayValue
	case pgtype.Int8:
		if v.Valid {
			return strconv.FormatInt(v.Int64, 10)
		}
		return NullDisplayValue
	case pgtype.Float4:
		if v.Valid {
			return formatFloat(float64(v.Float32), 32)
		}
		return NullDisplayValue
	case pgtype.Float8:
		if v.Valid {
			return formatFloat(v.Float64, 64)
		}
		return NullDisplayValue
	case pgtype.Bool:
		if v.Valid {
			if v.Bool {
				return "true"
			}
			return "false"
		}
		return NullDisplayValue

	// Arrays - format as PostgreSQL array syntax
	case []int32:
		return formatIntArray(v)
	case []int64:
		return formatInt64Array(v)
	case []float64:
		return formatFloat64Array(v)
	case []string:
		return formatStringArray(v)
	case []bool:
		return formatBoolArray(v)

	// Point type
	case pgtype.Point:
		if v.Valid {
			return fmt.Sprintf("(%g,%g)", v.P.X, v.P.Y)
		}
		return NullDisplayValue

	// Range types
	case pgtype.Range[int32]:
		return formatRange(v)
	case pgtype.Range[int64]:
		return formatRange(v)
	case pgtype.Range[pgtype.Numeric]:
		return formatRange(v)
	case pgtype.Range[time.Time]:
		return formatRange(v)
	case pgtype.Range[pgtype.Date]:
		return formatRange(v)

	default:
		// For unknown types, use fmt.Sprintf
		return fmt.Sprintf("%v", v)
	}
}

// formatFloat formats a float with appropriate precision.
func formatFloat(f float64, bitSize int) string {
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}

	// Use shortest representation that roundtrips
	s := strconv.FormatFloat(f, 'g', -1, bitSize)

	// Ensure at least one decimal place for floats
	if !strings.Contains(s, ".") && !strings.Contains(s, "e") {
		s += ".0"
	}

	return s
}

// formatTime formats a time.Time value based on its precision.
func formatTime(t time.Time) string {
	// Check if time has sub-second precision
	if t.Nanosecond() > 0 {
		// PostgreSQL uses microsecond precision
		return t.Format("2006-01-02 15:04:05.999999")
	}
	return t.Format("2006-01-02 15:04:05")
}

// formatInterval formats a pgtype.Interval.
func formatInterval(v pgtype.Interval) string {
	parts := []string{}

	if v.Months != 0 {
		years := v.Months / 12
		months := v.Months % 12
		if years != 0 {
			parts = append(parts, fmt.Sprintf("%d year", years))
			if years > 1 || years < -1 {
				parts[len(parts)-1] += "s"
			}
		}
		if months != 0 {
			parts = append(parts, fmt.Sprintf("%d mon", months))
		}
	}

	if v.Days != 0 {
		parts = append(parts, fmt.Sprintf("%d day", v.Days))
		if v.Days > 1 || v.Days < -1 {
			parts[len(parts)-1] += "s"
		}
	}

	if v.Microseconds != 0 {
		hours := v.Microseconds / 3600000000
		minutes := (v.Microseconds % 3600000000) / 60000000
		seconds := (v.Microseconds % 60000000) / 1000000
		usec := v.Microseconds % 1000000

		if usec != 0 {
			parts = append(parts, fmt.Sprintf("%02d:%02d:%02d.%06d", hours, minutes, seconds, usec))
		} else {
			parts = append(parts, fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds))
		}
	}

	if len(parts) == 0 {
		return "00:00:00"
	}

	return strings.Join(parts, " ")
}

// formatUUID formats a UUID byte array.
func formatUUID(b [16]byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// formatJSON formats a JSON value.
func formatJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// isPrintable checks if a byte slice contains printable UTF-8 text.
func isPrintable(b []byte) bool {
	for _, c := range b {
		if c < 32 && c != '\t' && c != '\n' && c != '\r' {
			return false
		}
	}
	return true
}

// sanitizeForDisplay replaces control characters that would break table layout.
// Newlines, tabs, and carriage returns are replaced with visible representations.
func sanitizeForDisplay(s string) string {
	// Fast path: if no control characters, return as-is
	needsSanitize := false
	for i := 0; i < len(s); i++ {
		if s[i] < 32 {
			needsSanitize = true
			break
		}
	}
	if !needsSanitize {
		return s
	}

	// Replace control characters
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString("↵")
		case '\r':
			// Skip carriage returns
		case '\t':
			b.WriteString(" ")
		default:
			if r < 32 {
				// Replace other control chars with space
				b.WriteString(" ")
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// formatIntArray formats an int32 slice as PostgreSQL array.
func formatIntArray(arr []int32) string {
	if arr == nil {
		return NullDisplayValue
	}
	parts := make([]string, len(arr))
	for i, v := range arr {
		parts[i] = strconv.FormatInt(int64(v), 10)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// formatInt64Array formats an int64 slice as PostgreSQL array.
func formatInt64Array(arr []int64) string {
	if arr == nil {
		return NullDisplayValue
	}
	parts := make([]string, len(arr))
	for i, v := range arr {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// formatFloat64Array formats a float64 slice as PostgreSQL array.
func formatFloat64Array(arr []float64) string {
	if arr == nil {
		return NullDisplayValue
	}
	parts := make([]string, len(arr))
	for i, v := range arr {
		parts[i] = formatFloat(v, 64)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// formatStringArray formats a string slice as PostgreSQL array.
func formatStringArray(arr []string) string {
	if arr == nil {
		return NullDisplayValue
	}
	parts := make([]string, len(arr))
	for i, v := range arr {
		// Escape quotes in strings
		escaped := strings.ReplaceAll(v, `"`, `\"`)
		if strings.ContainsAny(v, `, "{}\\`) || v == "" {
			parts[i] = `"` + escaped + `"`
		} else {
			parts[i] = v
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// formatBoolArray formats a bool slice as PostgreSQL array.
func formatBoolArray(arr []bool) string {
	if arr == nil {
		return NullDisplayValue
	}
	parts := make([]string, len(arr))
	for i, v := range arr {
		if v {
			parts[i] = "t"
		} else {
			parts[i] = "f"
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// formatRange formats a pgtype.Range value.
func formatRange[T any](r pgtype.Range[T]) string {
	if !r.Valid {
		return NullDisplayValue
	}

	var left, right string

	switch r.LowerType {
	case pgtype.Inclusive:
		left = "["
	case pgtype.Exclusive:
		left = "("
	default:
		left = "("
	}

	switch r.UpperType {
	case pgtype.Inclusive:
		right = "]"
	case pgtype.Exclusive:
		right = ")"
	default:
		right = ")"
	}

	lowerStr := ""
	if r.LowerType != pgtype.Unbounded {
		lowerStr = fmt.Sprintf("%v", r.Lower)
	}

	upperStr := ""
	if r.UpperType != pgtype.Unbounded {
		upperStr = fmt.Sprintf("%v", r.Upper)
	}

	return fmt.Sprintf("%s%s,%s%s", left, lowerStr, upperStr, right)
}

// FormatResultSetRow converts a row of raw values to formatted strings.
func FormatResultSetRow(values []any) []string {
	result := make([]string, len(values))
	for i, val := range values {
		result[i] = FormatValue(val)
	}
	return result
}

// FormatResultSet converts raw query results to formatted display strings.
func FormatResultSet(rows [][]any) [][]string {
	result := make([][]string, len(rows))
	for i, row := range rows {
		result[i] = FormatResultSetRow(row)
	}
	return result
}

// TruncateValue truncates a value to a maximum display width.
func TruncateValue(s string, maxWidth int) string {
	if maxWidth <= 0 || len(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		return s[:maxWidth]
	}
	return s[:maxWidth-3] + "..."
}

// CalculateColumnWidth determines optimal column width based on content.
func CalculateColumnWidth(name string, values []string, maxWidth int) int {
	width := len(name)
	for _, v := range values {
		if len(v) > width {
			width = len(v)
		}
	}
	if maxWidth > 0 && width > maxWidth {
		width = maxWidth
	}
	return width
}
