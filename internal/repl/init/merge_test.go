package init_test

import (
	"testing"
	"time"

	replinit "github.com/willibrandon/steep/internal/repl/init"
)

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		wantErr bool
	}{
		{"time.Time", time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), false},
		{"RFC3339", "2024-01-15T10:30:00Z", false},
		{"RFC3339Nano", "2024-01-15T10:30:00.123456789Z", false},
		{"datetime space", "2024-01-15 10:30:00", false},
		{"datetime T", "2024-01-15T10:30:00", false},
		{"datetime microsec", "2024-01-15 10:30:00.123456", false},
		{"invalid string", "not-a-timestamp", true},
		{"invalid type int", 12345, true},
		{"invalid type float", 123.45, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := replinit.ParseTimestamp(tc.input)
			if (err != nil) != tc.wantErr {
				t.Errorf("ParseTimestamp(%v) error = %v; wantErr = %v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestParseTimestampValues(t *testing.T) {
	input := "2024-06-15T14:30:45Z"
	got, err := replinit.ParseTimestamp(input)
	if err != nil {
		t.Fatalf("ParseTimestamp(%q) unexpected error: %v", input, err)
	}

	want := time.Date(2024, 6, 15, 14, 30, 45, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("ParseTimestamp(%q) = %v; want %v", input, got, want)
	}
}

func TestTopologicalSort_NoDeps(t *testing.T) {
	tables := []replinit.MergeTableInfo{
		{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		{Schema: "public", Name: "products", PKColumns: []string{"id"}},
		{Schema: "public", Name: "categories", PKColumns: []string{"id"}},
	}

	m := replinit.NewMerger(nil, nil, nil)
	sorted, err := m.TopologicalSort(tables, nil)
	if err != nil {
		t.Fatalf("TopologicalSort() error = %v", err)
	}

	if len(sorted) != len(tables) {
		t.Errorf("TopologicalSort() returned %d tables; want %d", len(sorted), len(tables))
	}

	// With no deps, should be sorted alphabetically by schema.table
	names := make([]string, len(sorted))
	for i, tbl := range sorted {
		names[i] = tbl.Schema + "." + tbl.Name
	}

	// Verify deterministic order (alphabetical when no deps)
	expected := []string{"public.categories", "public.products", "public.users"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("sorted[%d] = %q; want %q", i, name, expected[i])
		}
	}
}

func TestTopologicalSort_WithDeps(t *testing.T) {
	// orders depends on users and products
	// order_items depends on orders and products
	tables := []replinit.MergeTableInfo{
		{Schema: "public", Name: "users", PKColumns: []string{"id"}},
		{Schema: "public", Name: "products", PKColumns: []string{"id"}},
		{Schema: "public", Name: "orders", PKColumns: []string{"id"}},
		{Schema: "public", Name: "order_items", PKColumns: []string{"id"}},
	}

	deps := []replinit.FKDependency{
		{ChildSchema: "public", ChildTable: "orders", ParentSchema: "public", ParentTable: "users"},
		{ChildSchema: "public", ChildTable: "orders", ParentSchema: "public", ParentTable: "products"},
		{ChildSchema: "public", ChildTable: "order_items", ParentSchema: "public", ParentTable: "orders"},
		{ChildSchema: "public", ChildTable: "order_items", ParentSchema: "public", ParentTable: "products"},
	}

	m := replinit.NewMerger(nil, nil, nil)
	sorted, err := m.TopologicalSort(tables, deps)
	if err != nil {
		t.Fatalf("TopologicalSort() error = %v", err)
	}

	if len(sorted) != len(tables) {
		t.Errorf("TopologicalSort() returned %d tables; want %d", len(sorted), len(tables))
	}

	// Build position map
	pos := make(map[string]int)
	for i, tbl := range sorted {
		pos[tbl.Schema+"."+tbl.Name] = i
	}

	// Verify constraints: parents must come before children
	assertBefore := func(parent, child string) {
		if pos[parent] >= pos[child] {
			t.Errorf("%s (pos %d) should come before %s (pos %d)", parent, pos[parent], child, pos[child])
		}
	}

	assertBefore("public.users", "public.orders")
	assertBefore("public.products", "public.orders")
	assertBefore("public.orders", "public.order_items")
	assertBefore("public.products", "public.order_items")
}

func TestTopologicalSort_CycleDetection(t *testing.T) {
	tables := []replinit.MergeTableInfo{
		{Schema: "public", Name: "a", PKColumns: []string{"id"}},
		{Schema: "public", Name: "b", PKColumns: []string{"id"}},
		{Schema: "public", Name: "c", PKColumns: []string{"id"}},
	}

	// a -> b -> c -> a (cycle)
	deps := []replinit.FKDependency{
		{ChildSchema: "public", ChildTable: "b", ParentSchema: "public", ParentTable: "a"},
		{ChildSchema: "public", ChildTable: "c", ParentSchema: "public", ParentTable: "b"},
		{ChildSchema: "public", ChildTable: "a", ParentSchema: "public", ParentTable: "c"},
	}

	m := replinit.NewMerger(nil, nil, nil)
	_, err := m.TopologicalSort(tables, deps)
	if err == nil {
		t.Error("TopologicalSort() should return error for cyclic dependencies")
	}
}

func TestTopologicalSort_ExternalDepsIgnored(t *testing.T) {
	// Only include "orders" table, but dep references "users" which is not in our set
	tables := []replinit.MergeTableInfo{
		{Schema: "public", Name: "orders", PKColumns: []string{"id"}},
	}

	deps := []replinit.FKDependency{
		{ChildSchema: "public", ChildTable: "orders", ParentSchema: "public", ParentTable: "users"},
	}

	m := replinit.NewMerger(nil, nil, nil)
	sorted, err := m.TopologicalSort(tables, deps)
	if err != nil {
		t.Fatalf("TopologicalSort() error = %v", err)
	}

	if len(sorted) != 1 {
		t.Errorf("TopologicalSort() returned %d tables; want 1", len(sorted))
	}
}

func TestResolveByLastModified_BothHaveTimestamps(t *testing.T) {
	m := replinit.NewMerger(nil, nil, nil)

	conflict := replinit.ConflictDetail{
		PKValue: map[string]any{"id": 1},
		NodeAValue: map[string]any{
			"id":         1,
			"name":       "old",
			"updated_at": "2024-01-01T10:00:00Z",
		},
		NodeBValue: map[string]any{
			"id":         1,
			"name":       "new",
			"updated_at": "2024-01-02T10:00:00Z", // B is newer
		},
	}

	resolution, keepValue := m.ResolveByLastModified(conflict)

	if resolution != "kept_b" {
		t.Errorf("resolution = %q; want %q", resolution, "kept_b")
	}
	if keepValue["name"] != "new" {
		t.Errorf("keepValue[name] = %v; want %q", keepValue["name"], "new")
	}
}

func TestResolveByLastModified_AIsNewer(t *testing.T) {
	m := replinit.NewMerger(nil, nil, nil)

	conflict := replinit.ConflictDetail{
		PKValue: map[string]any{"id": 1},
		NodeAValue: map[string]any{
			"id":         1,
			"name":       "newer",
			"updated_at": "2024-01-15T10:00:00Z", // A is newer
		},
		NodeBValue: map[string]any{
			"id":         1,
			"name":       "older",
			"updated_at": "2024-01-01T10:00:00Z",
		},
	}

	resolution, keepValue := m.ResolveByLastModified(conflict)

	if resolution != "kept_a" {
		t.Errorf("resolution = %q; want %q", resolution, "kept_a")
	}
	if keepValue["name"] != "newer" {
		t.Errorf("keepValue[name] = %v; want %q", keepValue["name"], "newer")
	}
}

func TestResolveByLastModified_OnlyAHasTimestamp(t *testing.T) {
	m := replinit.NewMerger(nil, nil, nil)

	conflict := replinit.ConflictDetail{
		PKValue: map[string]any{"id": 1},
		NodeAValue: map[string]any{
			"id":         1,
			"name":       "with_ts",
			"updated_at": "2024-01-01T10:00:00Z",
		},
		NodeBValue: map[string]interface{}{
			"id":   1,
			"name": "no_ts",
		},
	}

	resolution, keepValue := m.ResolveByLastModified(conflict)

	if resolution != "kept_a" {
		t.Errorf("resolution = %q; want %q (A has timestamp)", resolution, "kept_a")
	}
	if keepValue["name"] != "with_ts" {
		t.Errorf("keepValue[name] = %v; want %q", keepValue["name"], "with_ts")
	}
}

func TestResolveByLastModified_OnlyBHasTimestamp(t *testing.T) {
	m := replinit.NewMerger(nil, nil, nil)

	conflict := replinit.ConflictDetail{
		PKValue: map[string]any{"id": 1},
		NodeAValue: map[string]any{
			"id":   1,
			"name": "no_ts",
		},
		NodeBValue: map[string]any{
			"id":          1,
			"name":        "with_ts",
			"modified_at": "2024-01-01T10:00:00Z", // different column name
		},
	}

	resolution, keepValue := m.ResolveByLastModified(conflict)

	if resolution != "kept_b" {
		t.Errorf("resolution = %q; want %q (B has timestamp)", resolution, "kept_b")
	}
	if keepValue["name"] != "with_ts" {
		t.Errorf("keepValue[name] = %v; want %q", keepValue["name"], "with_ts")
	}
}

func TestResolveByLastModified_NeitherHasTimestamp(t *testing.T) {
	m := replinit.NewMerger(nil, nil, nil)

	conflict := replinit.ConflictDetail{
		PKValue: map[string]any{"id": 1},
		NodeAValue: map[string]any{
			"id":   1,
			"name": "a_value",
		},
		NodeBValue: map[string]any{
			"id":   1,
			"name": "b_value",
		},
	}

	resolution, keepValue := m.ResolveByLastModified(conflict)

	// Should default to A as tiebreaker
	if resolution != "kept_a" {
		t.Errorf("resolution = %q; want %q (fallback to A)", resolution, "kept_a")
	}
	if keepValue["name"] != "a_value" {
		t.Errorf("keepValue[name] = %v; want %q", keepValue["name"], "a_value")
	}
}

func TestResolveByLastModified_AlternateTimestampColumns(t *testing.T) {
	m := replinit.NewMerger(nil, nil, nil)

	// Test with "last_modified" column
	conflict := replinit.ConflictDetail{
		PKValue: map[string]any{"id": 1},
		NodeAValue: map[string]any{
			"id":            1,
			"last_modified": "2024-01-01T10:00:00Z",
		},
		NodeBValue: map[string]any{
			"id":            1,
			"last_modified": "2024-01-02T10:00:00Z",
		},
	}

	resolution, _ := m.ResolveByLastModified(conflict)
	if resolution != "kept_b" {
		t.Errorf("resolution = %q; want %q for last_modified column", resolution, "kept_b")
	}

	// Test with "timestamp" column
	conflict2 := replinit.ConflictDetail{
		PKValue: map[string]any{"id": 1},
		NodeAValue: map[string]any{
			"id":        1,
			"timestamp": "2024-01-02T10:00:00Z",
		},
		NodeBValue: map[string]any{
			"id":        1,
			"timestamp": "2024-01-01T10:00:00Z",
		},
	}

	resolution2, _ := m.ResolveByLastModified(conflict2)
	if resolution2 != "kept_a" {
		t.Errorf("resolution = %q; want %q for timestamp column", resolution2, "kept_a")
	}
}
