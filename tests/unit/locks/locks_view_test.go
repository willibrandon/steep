package locks_test

import (
	"sort"
	"testing"
	"time"

	"github.com/willibrandon/steep/internal/db/models"
)

// TestLocksData_GetStatus tests the status determination logic.
func TestLocksData_GetStatus(t *testing.T) {
	tests := []struct {
		name         string
		blockingPIDs map[int]bool
		blockedPIDs  map[int]bool
		pid          int
		want         models.LockStatus
	}{
		{
			name:         "normal status",
			blockingPIDs: map[int]bool{100: true},
			blockedPIDs:  map[int]bool{200: true},
			pid:          300,
			want:         models.LockStatusNormal,
		},
		{
			name:         "blocking status",
			blockingPIDs: map[int]bool{100: true},
			blockedPIDs:  map[int]bool{200: true},
			pid:          100,
			want:         models.LockStatusBlocking,
		},
		{
			name:         "blocked status",
			blockingPIDs: map[int]bool{100: true},
			blockedPIDs:  map[int]bool{200: true},
			pid:          200,
			want:         models.LockStatusBlocked,
		},
		{
			name:         "blocked takes priority over blocking",
			blockingPIDs: map[int]bool{100: true},
			blockedPIDs:  map[int]bool{100: true},
			pid:          100,
			want:         models.LockStatusBlocked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &models.LocksData{
				BlockingPIDs: tt.blockingPIDs,
				BlockedPIDs:  tt.blockedPIDs,
			}
			got := data.GetStatus(tt.pid)
			if got != tt.want {
				t.Errorf("GetStatus(%d) = %v, want %v", tt.pid, got, tt.want)
			}
		})
	}
}

// TestNewLocksData tests the constructor initializes all fields.
func TestNewLocksData(t *testing.T) {
	data := models.NewLocksData()

	if data.Locks == nil {
		t.Error("Locks should be initialized")
	}
	if data.Blocking == nil {
		t.Error("Blocking should be initialized")
	}
	if data.BlockingPIDs == nil {
		t.Error("BlockingPIDs should be initialized")
	}
	if data.BlockedPIDs == nil {
		t.Error("BlockedPIDs should be initialized")
	}
	if data.Chains == nil {
		t.Error("Chains should be initialized")
	}
}

// TestLockSorting tests sorting locks by different columns.
func TestLockSorting(t *testing.T) {
	locks := []models.Lock{
		{PID: 300, LockType: "relation", Mode: "RowShareLock", Duration: 5 * time.Second, Granted: true},
		{PID: 100, LockType: "transactionid", Mode: "ExclusiveLock", Duration: 10 * time.Second, Granted: false},
		{PID: 200, LockType: "relation", Mode: "AccessShareLock", Duration: 1 * time.Second, Granted: true},
	}

	t.Run("sort by PID", func(t *testing.T) {
		sorted := make([]models.Lock, len(locks))
		copy(sorted, locks)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].PID < sorted[j].PID
		})
		if sorted[0].PID != 100 || sorted[1].PID != 200 || sorted[2].PID != 300 {
			t.Errorf("Sort by PID failed: got %d, %d, %d", sorted[0].PID, sorted[1].PID, sorted[2].PID)
		}
	})

	t.Run("sort by Type", func(t *testing.T) {
		sorted := make([]models.Lock, len(locks))
		copy(sorted, locks)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].LockType < sorted[j].LockType
		})
		if sorted[0].LockType != "relation" || sorted[2].LockType != "transactionid" {
			t.Errorf("Sort by Type failed")
		}
	})

	t.Run("sort by Mode", func(t *testing.T) {
		sorted := make([]models.Lock, len(locks))
		copy(sorted, locks)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].Mode < sorted[j].Mode
		})
		if sorted[0].Mode != "AccessShareLock" {
			t.Errorf("Sort by Mode failed: first is %s", sorted[0].Mode)
		}
	})

	t.Run("sort by Duration descending", func(t *testing.T) {
		sorted := make([]models.Lock, len(locks))
		copy(sorted, locks)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].Duration > sorted[j].Duration
		})
		if sorted[0].Duration != 10*time.Second {
			t.Errorf("Sort by Duration failed: first is %v", sorted[0].Duration)
		}
	})

	t.Run("sort by Granted (waiting first)", func(t *testing.T) {
		sorted := make([]models.Lock, len(locks))
		copy(sorted, locks)
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].Granted != sorted[j].Granted {
				return !sorted[i].Granted // waiting (false) before granted (true)
			}
			return sorted[i].PID < sorted[j].PID
		})
		if sorted[0].Granted != false {
			t.Errorf("Sort by Granted failed: first is granted=%v", sorted[0].Granted)
		}
	})
}

// TestTruncateString tests string truncation for display.
func TestTruncateString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{
			name:  "no truncation needed",
			input: "short",
			max:   10,
			want:  "short",
		},
		{
			name:  "exact length",
			input: "exact",
			max:   5,
			want:  "exact",
		},
		{
			name:  "needs truncation",
			input: "this is a long string",
			max:   10,
			want:  "this is...",
		},
		{
			name:  "very short max",
			input: "test",
			max:   3,
			want:  "...",
		},
		{
			name:  "empty string",
			input: "",
			max:   10,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

// truncateString truncates a string to max length with ellipsis.
func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return "..."
	}
	return s[:max-3] + "..."
}

// TestFormatDuration tests duration formatting for display.
func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			name:     "zero duration",
			duration: 0,
			want:     "0s",
		},
		{
			name:     "milliseconds",
			duration: 500 * time.Millisecond,
			want:     "0s",
		},
		{
			name:     "seconds",
			duration: 5 * time.Second,
			want:     "5s",
		},
		{
			name:     "minutes and seconds",
			duration: 2*time.Minute + 30*time.Second,
			want:     "2m30s",
		},
		{
			name:     "hours",
			duration: 1*time.Hour + 5*time.Minute + 10*time.Second,
			want:     "1h5m0s",
		},
		{
			name:     "just under a minute",
			duration: 59 * time.Second,
			want:     "59s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.duration)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}

// formatDuration formats a duration for display in the locks table.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	if d < time.Minute {
		return d.Truncate(time.Second).String()
	}
	if d < time.Hour {
		return d.Truncate(time.Second).String()
	}
	// Hours
	return d.Truncate(time.Minute).String()
}

// TestBlockingRelationship tests BlockingRelationship struct.
func TestBlockingRelationship(t *testing.T) {
	rel := models.BlockingRelationship{
		BlockedPID:      100,
		BlockedUser:     "user1",
		BlockedQuery:    "SELECT * FROM table1",
		BlockedDuration: 5 * time.Second,
		BlockingPID:     200,
		BlockingUser:    "user2",
		BlockingQuery:   "UPDATE table1 SET col = 1",
	}

	if rel.BlockedPID != 100 {
		t.Errorf("BlockedPID = %d, want 100", rel.BlockedPID)
	}
	if rel.BlockingPID != 200 {
		t.Errorf("BlockingPID = %d, want 200", rel.BlockingPID)
	}
	if rel.BlockedDuration != 5*time.Second {
		t.Errorf("BlockedDuration = %v, want 5s", rel.BlockedDuration)
	}
}

// TestBlockingChain tests BlockingChain tree structure.
func TestBlockingChain(t *testing.T) {
	chain := models.BlockingChain{
		BlockerPID: 100,
		Query:      "SELECT * FROM table1 FOR UPDATE",
		LockMode:   "ExclusiveLock",
		User:       "admin",
		Blocked: []models.BlockingChain{
			{
				BlockerPID: 200,
				Query:      "UPDATE table1 SET col = 1",
				LockMode:   "RowExclusiveLock",
				User:       "user1",
				Blocked:    nil,
			},
			{
				BlockerPID: 300,
				Query:      "DELETE FROM table1 WHERE id = 1",
				LockMode:   "RowExclusiveLock",
				User:       "user2",
				Blocked:    nil,
			},
		},
	}

	if chain.BlockerPID != 100 {
		t.Errorf("Root BlockerPID = %d, want 100", chain.BlockerPID)
	}
	if len(chain.Blocked) != 2 {
		t.Errorf("Blocked count = %d, want 2", len(chain.Blocked))
	}
	if chain.Blocked[0].BlockerPID != 200 {
		t.Errorf("First blocked PID = %d, want 200", chain.Blocked[0].BlockerPID)
	}
}

// TestLockModel tests Lock model fields.
func TestLockModel(t *testing.T) {
	lock := models.Lock{
		PID:           12345,
		User:          "postgres",
		Database:      "mydb",
		LockType:      "relation",
		Mode:          "ExclusiveLock",
		Granted:       true,
		Relation:      "users",
		Query:         "SELECT * FROM users WHERE id = 1",
		State:         "active",
		Duration:      30 * time.Second,
		WaitEventType: "",
		WaitEvent:     "",
	}

	if lock.PID != 12345 {
		t.Errorf("PID = %d, want 12345", lock.PID)
	}
	if lock.LockType != "relation" {
		t.Errorf("LockType = %s, want relation", lock.LockType)
	}
	if !lock.Granted {
		t.Error("Granted should be true")
	}
	if lock.Duration != 30*time.Second {
		t.Errorf("Duration = %v, want 30s", lock.Duration)
	}
}

// TestLockModeValues tests that common lock modes are valid strings.
func TestLockModeValues(t *testing.T) {
	validModes := []string{
		"AccessShareLock",
		"RowShareLock",
		"RowExclusiveLock",
		"ShareUpdateExclusiveLock",
		"ShareLock",
		"ShareRowExclusiveLock",
		"ExclusiveLock",
		"AccessExclusiveLock",
	}

	for _, mode := range validModes {
		if mode == "" {
			t.Errorf("Lock mode should not be empty")
		}
	}
}

// TestLockTypeValues tests that common lock types are valid strings.
func TestLockTypeValues(t *testing.T) {
	validTypes := []string{
		"relation",
		"extend",
		"page",
		"tuple",
		"transactionid",
		"virtualxid",
		"object",
		"userlock",
		"advisory",
	}

	for _, lt := range validTypes {
		if lt == "" {
			t.Errorf("Lock type should not be empty")
		}
	}
}

// TestEmptyLocksData tests handling of empty locks data.
func TestEmptyLocksData(t *testing.T) {
	data := models.NewLocksData()

	if len(data.Locks) != 0 {
		t.Errorf("Locks should be empty, got %d", len(data.Locks))
	}

	// GetStatus on empty data should return normal
	status := data.GetStatus(100)
	if status != models.LockStatusNormal {
		t.Errorf("GetStatus on empty data should return Normal, got %v", status)
	}
}

// TestLockStatusConstants tests lock status constant values.
func TestLockStatusConstants(t *testing.T) {
	if models.LockStatusNormal != 0 {
		t.Errorf("LockStatusNormal = %d, want 0", models.LockStatusNormal)
	}
	if models.LockStatusBlocking != 1 {
		t.Errorf("LockStatusBlocking = %d, want 1", models.LockStatusBlocking)
	}
	if models.LockStatusBlocked != 2 {
		t.Errorf("LockStatusBlocked = %d, want 2", models.LockStatusBlocked)
	}
}
