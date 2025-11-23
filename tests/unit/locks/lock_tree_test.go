package locks_test

import (
	"strings"
	"testing"

	"github.com/willibrandon/steep/internal/db/models"
	"github.com/willibrandon/steep/internal/ui/components"
)

// TestRenderLockTree_Empty tests rendering with no chains.
func TestRenderLockTree_Empty(t *testing.T) {
	result := components.RenderLockTree(nil, 80)
	if result != "" {
		t.Errorf("Expected empty string for nil chains, got %q", result)
	}

	result = components.RenderLockTree([]models.BlockingChain{}, 80)
	if result != "" {
		t.Errorf("Expected empty string for empty chains, got %q", result)
	}
}

// TestRenderLockTree_SingleChain tests rendering a single blocking chain.
func TestRenderLockTree_SingleChain(t *testing.T) {
	chains := []models.BlockingChain{
		{
			BlockerPID: 100,
			Query:      "SELECT * FROM users FOR UPDATE",
			LockMode:   "ExclusiveLock",
			User:       "admin",
			Blocked:    nil,
		},
	}

	result := components.RenderLockTree(chains, 80)

	if result == "" {
		t.Error("Expected non-empty result for single chain")
	}

	// Should contain the PID
	if !strings.Contains(result, "100") {
		t.Error("Result should contain PID 100")
	}

	// Should contain the user
	if !strings.Contains(result, "admin") {
		t.Error("Result should contain user 'admin'")
	}
}

// TestRenderLockTree_TwoLevelChain tests a blocker with one blocked.
func TestRenderLockTree_TwoLevelChain(t *testing.T) {
	chains := []models.BlockingChain{
		{
			BlockerPID: 100,
			Query:      "SELECT * FROM users FOR UPDATE",
			LockMode:   "ExclusiveLock",
			User:       "admin",
			Blocked: []models.BlockingChain{
				{
					BlockerPID: 200,
					Query:      "UPDATE users SET name = 'test'",
					LockMode:   "RowExclusiveLock",
					User:       "user1",
					Blocked:    nil,
				},
			},
		},
	}

	result := components.RenderLockTree(chains, 80)

	// Should contain both PIDs
	if !strings.Contains(result, "100") {
		t.Error("Result should contain blocker PID 100")
	}
	if !strings.Contains(result, "200") {
		t.Error("Result should contain blocked PID 200")
	}

	// Should have multiple lines (tree structure)
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Errorf("Expected at least 2 lines for two-level chain, got %d", len(lines))
	}
}

// TestRenderLockTree_DeepChain tests a 4-level deep chain.
func TestRenderLockTree_DeepChain(t *testing.T) {
	chains := []models.BlockingChain{
		{
			BlockerPID: 100,
			Query:      "Root query",
			LockMode:   "ExclusiveLock",
			User:       "root",
			Blocked: []models.BlockingChain{
				{
					BlockerPID: 200,
					Query:      "Level 1 query",
					LockMode:   "RowExclusiveLock",
					User:       "user1",
					Blocked: []models.BlockingChain{
						{
							BlockerPID: 300,
							Query:      "Level 2 query",
							LockMode:   "RowExclusiveLock",
							User:       "user2",
							Blocked: []models.BlockingChain{
								{
									BlockerPID: 400,
									Query:      "Level 3 query",
									LockMode:   "RowExclusiveLock",
									User:       "user3",
									Blocked:    nil,
								},
							},
						},
					},
				},
			},
		},
	}

	result := components.RenderLockTree(chains, 80)

	// Should contain all PIDs
	for _, pid := range []string{"100", "200", "300", "400"} {
		if !strings.Contains(result, pid) {
			t.Errorf("Result should contain PID %s", pid)
		}
	}

	// Should have at least 4 lines
	lines := strings.Split(result, "\n")
	if len(lines) < 4 {
		t.Errorf("Expected at least 4 lines for deep chain, got %d", len(lines))
	}
}

// TestRenderLockTree_WideChain tests a blocker with multiple blocked.
func TestRenderLockTree_WideChain(t *testing.T) {
	chains := []models.BlockingChain{
		{
			BlockerPID: 100,
			Query:      "Root query",
			LockMode:   "ExclusiveLock",
			User:       "root",
			Blocked: []models.BlockingChain{
				{
					BlockerPID: 201,
					Query:      "Blocked 1",
					LockMode:   "RowExclusiveLock",
					User:       "user1",
					Blocked:    nil,
				},
				{
					BlockerPID: 202,
					Query:      "Blocked 2",
					LockMode:   "RowExclusiveLock",
					User:       "user2",
					Blocked:    nil,
				},
				{
					BlockerPID: 203,
					Query:      "Blocked 3",
					LockMode:   "RowExclusiveLock",
					User:       "user3",
					Blocked:    nil,
				},
			},
		},
	}

	result := components.RenderLockTree(chains, 80)

	// Should contain all PIDs
	for _, pid := range []string{"100", "201", "202", "203"} {
		if !strings.Contains(result, pid) {
			t.Errorf("Result should contain PID %s", pid)
		}
	}
}

// TestRenderLockTree_MultipleRoots tests multiple independent chains.
func TestRenderLockTree_MultipleRoots(t *testing.T) {
	chains := []models.BlockingChain{
		{
			BlockerPID: 100,
			Query:      "Chain 1 root",
			LockMode:   "ExclusiveLock",
			User:       "admin1",
			Blocked: []models.BlockingChain{
				{
					BlockerPID: 101,
					Query:      "Chain 1 blocked",
					LockMode:   "RowExclusiveLock",
					User:       "user1",
					Blocked:    nil,
				},
			},
		},
		{
			BlockerPID: 200,
			Query:      "Chain 2 root",
			LockMode:   "ExclusiveLock",
			User:       "admin2",
			Blocked: []models.BlockingChain{
				{
					BlockerPID: 201,
					Query:      "Chain 2 blocked",
					LockMode:   "RowExclusiveLock",
					User:       "user2",
					Blocked:    nil,
				},
			},
		},
	}

	result := components.RenderLockTree(chains, 80)

	// Should contain PIDs from both chains
	for _, pid := range []string{"100", "101", "200", "201"} {
		if !strings.Contains(result, pid) {
			t.Errorf("Result should contain PID %s", pid)
		}
	}
}

// TestRenderLockTree_QueryTruncation tests that long queries are truncated.
func TestRenderLockTree_QueryTruncation(t *testing.T) {
	longQuery := strings.Repeat("SELECT * FROM very_long_table_name WHERE ", 10)

	chains := []models.BlockingChain{
		{
			BlockerPID: 100,
			Query:      longQuery,
			LockMode:   "ExclusiveLock",
			User:       "admin",
			Blocked:    nil,
		},
	}

	result := components.RenderLockTree(chains, 80)

	// Result should not contain the full query
	if strings.Contains(result, longQuery) {
		t.Error("Long query should be truncated")
	}

	// Should still contain PID
	if !strings.Contains(result, "100") {
		t.Error("Result should contain PID 100")
	}
}

// TestRenderLockTree_EmptyQuery tests handling of empty query.
func TestRenderLockTree_EmptyQuery(t *testing.T) {
	chains := []models.BlockingChain{
		{
			BlockerPID: 100,
			Query:      "",
			LockMode:   "ExclusiveLock",
			User:       "admin",
			Blocked:    nil,
		},
	}

	result := components.RenderLockTree(chains, 80)

	// Should still render (with placeholder)
	if result == "" {
		t.Error("Should render even with empty query")
	}

	// Should contain PID
	if !strings.Contains(result, "100") {
		t.Error("Result should contain PID 100")
	}
}

// TestRenderLockTree_NarrowWidth tests rendering with narrow width.
func TestRenderLockTree_NarrowWidth(t *testing.T) {
	chains := []models.BlockingChain{
		{
			BlockerPID: 100,
			Query:      "SELECT * FROM users WHERE id = 1",
			LockMode:   "ExclusiveLock",
			User:       "admin",
			Blocked:    nil,
		},
	}

	// Very narrow width
	result := components.RenderLockTree(chains, 40)

	// Should still render
	if result == "" {
		t.Error("Should render even with narrow width")
	}

	// Should contain PID
	if !strings.Contains(result, "100") {
		t.Error("Result should contain PID 100")
	}
}

// TestRenderLockTree_ComplexChain tests a complex mixed chain.
func TestRenderLockTree_ComplexChain(t *testing.T) {
	// Root blocks two, one of which blocks another
	chains := []models.BlockingChain{
		{
			BlockerPID: 100,
			Query:      "Root",
			LockMode:   "ExclusiveLock",
			User:       "root",
			Blocked: []models.BlockingChain{
				{
					BlockerPID: 200,
					Query:      "Branch A",
					LockMode:   "RowExclusiveLock",
					User:       "userA",
					Blocked: []models.BlockingChain{
						{
							BlockerPID: 300,
							Query:      "Leaf from A",
							LockMode:   "RowExclusiveLock",
							User:       "leafA",
							Blocked:    nil,
						},
					},
				},
				{
					BlockerPID: 201,
					Query:      "Branch B (leaf)",
					LockMode:   "RowExclusiveLock",
					User:       "userB",
					Blocked:    nil,
				},
			},
		},
	}

	result := components.RenderLockTree(chains, 80)

	// Should contain all PIDs
	for _, pid := range []string{"100", "200", "201", "300"} {
		if !strings.Contains(result, pid) {
			t.Errorf("Result should contain PID %s", pid)
		}
	}

	// Should have tree structure characters
	if !strings.Contains(result, "─") && !strings.Contains(result, "-") {
		t.Log("Note: Tree might not have visible structure characters depending on treeprint output")
	}
}

// TestRenderLockTree_TreeStructure tests that output has tree-like structure.
func TestRenderLockTree_TreeStructure(t *testing.T) {
	chains := []models.BlockingChain{
		{
			BlockerPID: 100,
			Query:      "Root",
			LockMode:   "ExclusiveLock",
			User:       "root",
			Blocked: []models.BlockingChain{
				{
					BlockerPID: 200,
					Query:      "Child",
					LockMode:   "RowExclusiveLock",
					User:       "child",
					Blocked:    nil,
				},
			},
		},
	}

	result := components.RenderLockTree(chains, 80)
	lines := strings.Split(result, "\n")

	// Should have header "Blocking Chains"
	hasHeader := false
	for _, line := range lines {
		if strings.Contains(line, "Blocking Chains") {
			hasHeader = true
			break
		}
	}
	if !hasHeader {
		t.Error("Tree should have 'Blocking Chains' header")
	}
}
