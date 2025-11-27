// Package config provides the Configuration Viewer view.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/willibrandon/steep/internal/db/models"
)

// ExportCommand represents a parsed export command.
type ExportCommand struct {
	Filename string
}

// parseExportCommand parses ":export config <filename>" syntax.
// Returns nil if the command is not a valid export command.
func parseExportCommand(input string) *ExportCommand {
	// Trim leading colon if present
	input = strings.TrimPrefix(input, ":")
	input = strings.TrimSpace(input)

	// Check for "export config" prefix
	if !strings.HasPrefix(strings.ToLower(input), "export config ") {
		return nil
	}

	// Extract filename
	parts := strings.SplitN(input, " ", 3)
	if len(parts) < 3 {
		return nil
	}

	filename := strings.TrimSpace(parts[2])
	if filename == "" {
		return nil
	}

	// Expand tilde for home directory
	if strings.HasPrefix(filename, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			filename = filepath.Join(home, filename[2:])
		}
	}

	return &ExportCommand{Filename: filename}
}

// exportConfig writes parameters to a file in PostgreSQL conf format.
// Returns the number of parameters written and any error.
func exportConfig(filename string, params []models.Parameter, connectionInfo string) (int, error) {
	// Create parent directories if needed
	dir := filepath.Dir(filename)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return 0, fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Create or truncate the file
	f, err := os.Create(filename)
	if err != nil {
		return 0, fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	// Write header
	fmt.Fprintf(f, "# PostgreSQL Configuration Export\n")
	fmt.Fprintf(f, "# Exported: %s\n", time.Now().Format(time.RFC3339))
	if connectionInfo != "" {
		fmt.Fprintf(f, "# Source: %s\n", connectionInfo)
	}
	fmt.Fprintf(f, "# Parameters: %d\n", len(params))
	fmt.Fprintf(f, "#\n")
	fmt.Fprintf(f, "# Format: name = 'value' # description\n")
	fmt.Fprintf(f, "\n")

	// Write parameters
	for _, p := range params {
		// Format value based on type
		var valueStr string
		switch p.VarType {
		case "string":
			// Strings need quotes and escaping
			valueStr = fmt.Sprintf("'%s'", escapeConfigValue(p.Setting))
		case "bool":
			// Booleans as on/off
			valueStr = p.Setting
		default:
			// Numbers and enums can be unquoted
			valueStr = p.Setting
		}

		// Add unit comment if present
		var comment string
		if p.ShortDesc != "" {
			comment = p.ShortDesc
			if p.Unit != "" {
				comment = fmt.Sprintf("[%s] %s", p.Unit, comment)
			}
		} else if p.Unit != "" {
			comment = fmt.Sprintf("[%s]", p.Unit)
		}

		// Write the line
		if comment != "" {
			fmt.Fprintf(f, "%s = %s  # %s\n", p.Name, valueStr, comment)
		} else {
			fmt.Fprintf(f, "%s = %s\n", p.Name, valueStr)
		}
	}

	return len(params), nil
}

// escapeConfigValue escapes special characters in PostgreSQL config values.
func escapeConfigValue(s string) string {
	// Escape single quotes by doubling them
	s = strings.ReplaceAll(s, "'", "''")
	// Escape backslashes
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return s
}

// SetCommand represents a parsed set command.
type SetCommand struct {
	Parameter string
	Value     string
}

// parseSetCommand parses ":set <parameter> <value>" syntax.
// Returns nil if the command is not a valid set command.
func parseSetCommand(input string) *SetCommand {
	// Trim leading colon if present
	input = strings.TrimPrefix(input, ":")
	input = strings.TrimSpace(input)

	// Check for "set " prefix
	if !strings.HasPrefix(strings.ToLower(input), "set ") {
		return nil
	}

	// Extract parameter and value
	rest := strings.TrimSpace(input[4:]) // Skip "set "

	// Split on first space or equals sign
	var parameter, value string

	// Handle "set param=value" or "set param = value" or "set param value"
	if idx := strings.Index(rest, "="); idx != -1 {
		parameter = strings.TrimSpace(rest[:idx])
		value = strings.TrimSpace(rest[idx+1:])
	} else {
		parts := strings.SplitN(rest, " ", 2)
		if len(parts) < 2 {
			return nil
		}
		parameter = strings.TrimSpace(parts[0])
		value = strings.TrimSpace(parts[1])
	}

	if parameter == "" || value == "" {
		return nil
	}

	// Remove quotes from value if present
	value = strings.Trim(value, "'\"")

	return &SetCommand{
		Parameter: parameter,
		Value:     value,
	}
}

// ResetCommand represents a parsed reset command.
type ResetCommand struct {
	Parameter string
}

// parseResetCommand parses ":reset <parameter>" syntax.
// Returns nil if the command is not a valid reset command.
func parseResetCommand(input string) *ResetCommand {
	// Trim leading colon if present
	input = strings.TrimPrefix(input, ":")
	input = strings.TrimSpace(input)

	// Check for "reset " prefix
	if !strings.HasPrefix(strings.ToLower(input), "reset ") {
		return nil
	}

	// Extract parameter
	parameter := strings.TrimSpace(input[6:]) // Skip "reset "

	if parameter == "" {
		return nil
	}

	return &ResetCommand{Parameter: parameter}
}

// parseReloadCommand checks if the input is a reload command.
// Returns true if the command is ":reload".
func parseReloadCommand(input string) bool {
	// Trim leading colon if present
	input = strings.TrimPrefix(input, ":")
	input = strings.TrimSpace(input)

	return strings.ToLower(input) == "reload"
}
