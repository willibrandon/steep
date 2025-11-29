package logs

import (
	"testing"
)

func TestParseStderrLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantNil  bool
		severity string
		message  string
		pid      int
		user     string
		app      string
	}{
		{
			name:     "nested brackets [brandon@[local]]",
			input:    "2025-11-28 17:07:56.999 PST [75142] [psql] [brandon@[local]]LOG:  statement: BEGIN;",
			severity: "INFO", // LOG normalizes to INFO
			message:  "statement: BEGIN;",
			pid:      75142,
			user:     "brandon",
			app:      "psql",
		},
		{
			name:     "simple format",
			input:    "2025-11-28 12:34:56.123 PST [12345] LOG:  simple message",
			severity: "INFO",
			message:  "simple message",
			pid:      12345,
		},
		{
			name:     "error severity",
			input:    "2025-11-28 12:34:56 PST [99999] ERROR:  deadlock detected",
			severity: "ERROR",
			message:  "deadlock detected",
			pid:      99999,
		},
		{
			name:     "warning severity",
			input:    "2025-11-28 12:34:56 PST [11111] WARNING:  some warning",
			severity: "WARN",
			message:  "some warning",
			pid:      11111,
		},
		{
			name:     "no severity marker",
			input:    "This is just a random line",
			wantNil:  true,
		},
		{
			name:     "continuation line",
			input:    "DETAIL:  Process 123 waits for lock",
			wantNil:  true, // No severity marker at expected position
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := parseStderrLine(tt.input)

			if tt.wantNil {
				if entry != nil {
					t.Errorf("expected nil, got entry with message: %s", entry.Message)
				}
				return
			}

			if entry == nil {
				t.Fatalf("expected entry, got nil")
			}

			if entry.Severity != tt.severity {
				t.Errorf("severity: got %q, want %q", entry.Severity, tt.severity)
			}

			if entry.Message != tt.message {
				t.Errorf("message: got %q, want %q", entry.Message, tt.message)
			}

			if entry.PID != tt.pid {
				t.Errorf("pid: got %d, want %d", entry.PID, tt.pid)
			}

			if tt.user != "" && entry.User != tt.user {
				t.Errorf("user: got %q, want %q", entry.User, tt.user)
			}

			if tt.app != "" && entry.Application != tt.app {
				t.Errorf("app: got %q, want %q", entry.Application, tt.app)
			}
		})
	}
}
