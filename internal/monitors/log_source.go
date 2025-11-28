// Package monitors provides background goroutines for fetching PostgreSQL data.
package monitors

// AccessMethod represents how to read PostgreSQL log files.
type AccessMethod int

const (
	// AccessFileSystem reads logs directly from the file system.
	AccessFileSystem AccessMethod = iota
	// AccessPgReadFile reads logs via pg_read_file() function.
	AccessPgReadFile
)

// String returns the display string for an access method.
func (m AccessMethod) String() string {
	switch m {
	case AccessFileSystem:
		return "filesystem"
	case AccessPgReadFile:
		return "pg_read_file"
	default:
		return "unknown"
	}
}

// LogSource holds configuration for log data source.
type LogSource struct {
	// LogDir is the directory containing log files.
	LogDir string
	// LogPattern is the glob pattern for log files (e.g., "postgresql-*.log").
	LogPattern string
	// Format is the detected format (CSV, JSON, Stderr).
	Format LogFormat
	// AccessMethod is how to read logs (FileSystem, PgReadFile).
	AccessMethod AccessMethod
	// Enabled indicates whether logging is enabled on server.
	Enabled bool
	// CurrentFile is the currently active log file.
	CurrentFile string
}

// DetectFormatFromFilename detects log format from file extension.
func DetectFormatFromFilename(filename string) LogFormat {
	if len(filename) < 4 {
		return LogFormatUnknown
	}

	switch {
	case len(filename) >= 4 && filename[len(filename)-4:] == ".csv":
		return LogFormatCSV
	case len(filename) >= 5 && filename[len(filename)-5:] == ".json":
		return LogFormatJSON
	case len(filename) >= 4 && filename[len(filename)-4:] == ".log":
		return LogFormatStderr
	default:
		return LogFormatUnknown
	}
}
