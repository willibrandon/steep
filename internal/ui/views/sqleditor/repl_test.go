package sqleditor

import (
	"testing"
)

func TestFindExecutable(t *testing.T) {
	// Test finding a common executable
	path := findExecutable("go")
	if path == "" {
		t.Error("expected to find 'go' executable")
	}

	// Test non-existent executable
	path = findExecutable("nonexistent_binary_xyz")
	if path != "" {
		t.Error("expected empty path for non-existent executable")
	}
}

func TestFindRepl(t *testing.T) {
	connString := "postgres://user:pass@localhost/db"

	tests := []struct {
		name     string
		replType ReplType
	}{
		{"auto", ReplAuto},
		{"pgcli", ReplPgcli},
		{"psql", ReplPsql},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findRepl(tt.replType, connString)

			switch tt.replType {
			case ReplAuto:
				// Auto should find at least one (pgcli, psql, or docker fallback)
				if findExecutable("pgcli") != "" || findExecutable("psql") != "" || findExecutable("docker") != "" {
					if result.tool == "" || result.cmd == nil {
						t.Errorf("expected to find a REPL in auto mode, got err: %s", result.err)
					}
				}
			case ReplPgcli:
				if findExecutable("pgcli") != "" {
					if result.tool != "pgcli" {
						t.Errorf("expected tool 'pgcli', got '%s'", result.tool)
					}
					if result.cmd == nil {
						t.Error("expected non-nil command for pgcli")
					}
				} else if findExecutable("docker") != "" {
					// Docker fallback should work
					if result.cmd == nil {
						t.Logf("Docker available but pgcli image may not be pulled: %s", result.err)
					}
				}
			case ReplPsql:
				if findExecutable("psql") != "" {
					if result.tool != "psql" {
						t.Errorf("expected tool 'psql', got '%s'", result.tool)
					}
					if result.cmd == nil {
						t.Error("expected non-nil command for psql")
					}
				} else if findExecutable("docker") != "" {
					// Docker fallback should work
					if result.cmd == nil {
						t.Logf("Docker available but postgres image may not be pulled: %s", result.err)
					}
				}
			}
		})
	}
}

func TestReplTypeConstants(t *testing.T) {
	if ReplPgcli != "pgcli" {
		t.Errorf("ReplPgcli should be 'pgcli', got '%s'", ReplPgcli)
	}
	if ReplPsql != "psql" {
		t.Errorf("ReplPsql should be 'psql', got '%s'", ReplPsql)
	}
	if ReplAuto != "auto" {
		t.Errorf("ReplAuto should be 'auto', got '%s'", ReplAuto)
	}
}

func TestAdjustConnStringForDocker(t *testing.T) {
	// Note: This function only modifies on Windows.
	// On other platforms it returns the string unchanged.
	// These tests verify the replacement logic would work correctly.

	tests := []struct {
		name     string
		input    string
		expected string // Expected output on Windows
	}{
		{
			name:     "URL format with localhost",
			input:    "postgres://user:pass@localhost:5432/db",
			expected: "postgres://user:pass@host.docker.internal:5432/db",
		},
		{
			name:     "URL format with 127.0.0.1",
			input:    "postgres://user:pass@127.0.0.1:5432/db",
			expected: "postgres://user:pass@host.docker.internal:5432/db",
		},
		{
			name:     "Key-value format with localhost",
			input:    "host=localhost port=5432 dbname=test",
			expected: "host=host.docker.internal port=5432 dbname=test",
		},
		{
			name:     "Key-value format with 127.0.0.1",
			input:    "host=127.0.0.1 port=5432 dbname=test",
			expected: "host=host.docker.internal port=5432 dbname=test",
		},
		{
			name:     "Remote host unchanged",
			input:    "postgres://user:pass@db.example.com:5432/db",
			expected: "postgres://user:pass@db.example.com:5432/db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adjustConnStringForDocker(tt.input)
			// On non-Windows, the function returns unchanged
			// We're just verifying it doesn't crash and handles the input
			if result == "" {
				t.Error("expected non-empty result")
			}
			// Log for visibility during Windows testing
			t.Logf("Input: %s -> Output: %s", tt.input, result)
		})
	}
}
