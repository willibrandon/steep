package components

import (
	"strings"
	"testing"
)

func TestBarChart_EmptyData(t *testing.T) {
	config := DefaultBarChartConfig()
	chart := NewBarChart(config)

	result := chart.View()

	if !strings.Contains(result, "No data available") {
		t.Errorf("Empty chart should show 'No data available', got: %s", result)
	}
}

func TestBarChart_WithTitle(t *testing.T) {
	config := DefaultBarChartConfig()
	config.Title = "Test Chart"
	chart := NewBarChart(config)

	chart.SetItems([]BarChartItem{
		{Label: "Item 1", Value: 100, Rank: 1},
	})

	result := chart.View()

	if !strings.Contains(result, "Test Chart") {
		t.Errorf("Chart should contain title, got: %s", result)
	}
}

func TestBarChart_SetSize(t *testing.T) {
	config := DefaultBarChartConfig()
	chart := NewBarChart(config)

	chart.SetSize(100, 15)

	if chart.config.Width != 100 {
		t.Errorf("Width should be 100, got: %d", chart.config.Width)
	}
	if chart.config.Height != 15 {
		t.Errorf("Height should be 15, got: %d", chart.config.Height)
	}
}

func TestBarChart_SetSizeMinimums(t *testing.T) {
	config := DefaultBarChartConfig()
	chart := NewBarChart(config)

	// Try to set too small
	chart.SetSize(10, 0)

	// Should maintain minimums
	if chart.config.Width < 40 {
		t.Errorf("Width should be at least 40, got: %d", chart.config.Width)
	}
	if chart.config.Height < 1 {
		t.Errorf("Height should be at least 1, got: %d", chart.config.Height)
	}
}

func TestTruncateLabel(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long label", 10, "this is..."},
		{"ab", 3, "ab"},
		{"abcd", 3, "abc"},
	}

	for _, tc := range tests {
		result := truncateLabel(tc.input, tc.maxLen)
		if result != tc.expected {
			t.Errorf("truncateLabel(%q, %d) = %q, want %q",
				tc.input, tc.maxLen, result, tc.expected)
		}
	}
}

func TestGetRankStyleStatic(t *testing.T) {
	// Test that rank 1-3 returns red
	style1 := getRankStyleStatic(1)
	style2 := getRankStyleStatic(2)
	style3 := getRankStyleStatic(3)

	// We can't easily test the actual color, but we can verify no panic
	_ = style1.Render("test")
	_ = style2.Render("test")
	_ = style3.Render("test")

	// Test that rank 4-6 returns yellow
	style4 := getRankStyleStatic(4)
	style5 := getRankStyleStatic(5)
	style6 := getRankStyleStatic(6)

	_ = style4.Render("test")
	_ = style5.Render("test")
	_ = style6.Render("test")

	// Test that rank 7+ returns green
	style7 := getRankStyleStatic(7)
	style10 := getRankStyleStatic(10)

	_ = style7.Render("test")
	_ = style10.Render("test")
}

func TestRenderSimpleBarChart(t *testing.T) {
	items := []BarChartItem{
		{Label: "Query 1", Value: 100, Rank: 1},
		{Label: "Query 2", Value: 75, Rank: 2},
		{Label: "Query 3", Value: 50, Rank: 3},
		{Label: "Query 4", Value: 25, Rank: 4},
	}

	config := DefaultBarChartConfig()
	config.Title = "Top Queries"
	config.ValueFormatter = func(v float64) string {
		return strings.TrimSuffix(strings.TrimSuffix(
			strings.ReplaceAll(
				strings.ReplaceAll(string(rune(int(v)))+"ms", "\x00", ""),
				"\x00", ""),
			"ms"), "\x00") + "ms"
	}

	result := RenderSimpleBarChart(items, config)

	// Should contain title
	if !strings.Contains(result, "Top Queries") {
		t.Errorf("Result should contain title, got: %s", result)
	}

	// Should contain all labels
	if !strings.Contains(result, "Query 1") {
		t.Errorf("Result should contain 'Query 1', got: %s", result)
	}
	if !strings.Contains(result, "Query 4") {
		t.Errorf("Result should contain 'Query 4', got: %s", result)
	}

	// Should contain bar characters
	if !strings.Contains(result, "█") {
		t.Errorf("Result should contain bar characters, got: %s", result)
	}
}

func TestRenderSimpleBarChart_Empty(t *testing.T) {
	result := RenderSimpleBarChart(nil, DefaultBarChartConfig())

	if result != "" {
		t.Errorf("Empty items should return empty string, got: %s", result)
	}
}

func TestRenderSimpleBarChart_MaxItems(t *testing.T) {
	items := make([]BarChartItem, 20)
	for i := range items {
		items[i] = BarChartItem{
			Label: "Item",
			Value: float64(20 - i),
			Rank:  i + 1,
		}
	}

	config := DefaultBarChartConfig()
	config.Height = 5 // Only show 5 items

	result := RenderSimpleBarChart(items, config)

	lines := strings.Split(result, "\n")
	// Count non-empty lines (excluding title)
	barLines := 0
	for _, line := range lines {
		if strings.Contains(line, "█") {
			barLines++
		}
	}

	if barLines > 5 {
		t.Errorf("Should only render %d bars, got %d", 5, barLines)
	}
}

func TestBarChartPanel_Visibility(t *testing.T) {
	config := DefaultBarChartConfig()
	panel := NewBarChartPanel(config)

	// Initially visible
	if !panel.IsVisible() {
		t.Error("Panel should be visible by default")
	}

	// Set invisible
	panel.SetVisible(false)
	result := panel.View()

	if result != "" {
		t.Errorf("Hidden panel should return empty string, got: %s", result)
	}
}

func TestBarChartPanel_WithData(t *testing.T) {
	config := DefaultBarChartConfig()
	config.Title = "Panel Title"
	panel := NewBarChartPanel(config)

	panel.SetItems([]BarChartItem{
		{Label: "Test", Value: 100, Rank: 1},
	})
	panel.SetSize(80, 12)

	result := panel.View()

	// Should contain border characters (rounded border)
	if !strings.Contains(result, "╭") && !strings.Contains(result, "┌") {
		t.Errorf("Panel should have border, got: %s", result)
	}
}

func TestColorBarInLine(t *testing.T) {
	style := getRankStyleStatic(1) // Red

	tests := []struct {
		input   string
		hasBars bool
	}{
		{"Label █████ 100", true},
		{"No bars here", false},
		{"Mixed ████ text ████", true},
	}

	for _, tc := range tests {
		result := colorBarInLine(tc.input, style)

		// The result should be non-empty
		if result == "" {
			t.Errorf("colorBarInLine returned empty for input: %s", tc.input)
		}

		// Result should still contain the label text
		if tc.hasBars && !strings.Contains(result, "Label") && !strings.Contains(result, "Mixed") {
			// Just verify it didn't corrupt the line
			t.Errorf("colorBarInLine corrupted the line: %s", result)
		}
	}
}

func TestDefaultBarChartConfig(t *testing.T) {
	config := DefaultBarChartConfig()

	if config.Width != 80 {
		t.Errorf("Default width should be 80, got: %d", config.Width)
	}
	if config.Height != 10 {
		t.Errorf("Default height should be 10, got: %d", config.Height)
	}
	if config.MaxLabelWidth != 25 {
		t.Errorf("Default max label width should be 25, got: %d", config.MaxLabelWidth)
	}
	if !config.ShowValues {
		t.Error("Default ShowValues should be true")
	}
	if !config.Horizontal {
		t.Error("Default Horizontal should be true")
	}
	if config.ValueFormatter == nil {
		t.Error("Default ValueFormatter should not be nil")
	}
}

func TestBarChart_SetTitle(t *testing.T) {
	config := DefaultBarChartConfig()
	chart := NewBarChart(config)

	chart.SetTitle("New Title")

	if chart.config.Title != "New Title" {
		t.Errorf("Title should be 'New Title', got: %s", chart.config.Title)
	}
}

func BenchmarkRenderSimpleBarChart(b *testing.B) {
	items := []BarChartItem{
		{Label: "SELECT * FROM orders WHERE customer_id = $1", Value: 45200, Rank: 1},
		{Label: "INSERT INTO log_entries", Value: 38100, Rank: 2},
		{Label: "UPDATE users SET last_login", Value: 22400, Rank: 3},
		{Label: "SELECT COUNT(*) FROM orders", Value: 18900, Rank: 4},
		{Label: "SELECT u.*, o.* FROM users", Value: 15200, Rank: 5},
		{Label: "DELETE FROM sessions", Value: 12800, Rank: 6},
		{Label: "SELECT * FROM products", Value: 8400, Rank: 7},
		{Label: "INSERT INTO metrics", Value: 6200, Rank: 8},
		{Label: "SELECT AVG(amount)", Value: 4800, Rank: 9},
		{Label: "UPDATE inventory", Value: 2100, Rank: 10},
	}

	config := DefaultBarChartConfig()
	config.Title = "Top 10 Queries by Time"
	config.ValueFormatter = func(v float64) string {
		if v < 1000 {
			return "<1s"
		}
		return strings.TrimSuffix(strings.TrimSuffix(
			strings.ReplaceAll(string(rune(int(v/1000)))+"s", "\x00", ""),
			"s"), "\x00") + "s"
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RenderSimpleBarChart(items, config)
	}
}

func BenchmarkBarChart_View(b *testing.B) {
	config := DefaultBarChartConfig()
	config.Title = "Benchmark Chart"
	chart := NewBarChart(config)

	items := []BarChartItem{
		{Label: "Item 1", Value: 100, Rank: 1},
		{Label: "Item 2", Value: 80, Rank: 2},
		{Label: "Item 3", Value: 60, Rank: 3},
		{Label: "Item 4", Value: 40, Rank: 4},
		{Label: "Item 5", Value: 20, Rank: 5},
	}
	chart.SetItems(items)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = chart.View()
	}
}
