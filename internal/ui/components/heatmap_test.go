package components

import (
	"strings"
	"testing"
)

func TestNewHeatmap(t *testing.T) {
	config := DefaultHeatmapConfig()
	h := NewHeatmap(config)

	if h == nil {
		t.Fatal("NewHeatmap returned nil")
	}

	// Check initial state
	if h.hasData {
		t.Error("New heatmap should not have data")
	}

	// All cells should be -1 (no data)
	for d := 0; d < 7; d++ {
		for hr := 0; hr < 24; hr++ {
			if h.data[d][hr] != -1 {
				t.Errorf("Cell [%d][%d] should be -1, got %f", d, hr, h.data[d][hr])
			}
		}
	}
}

func TestHeatmap_SetData(t *testing.T) {
	h := NewHeatmap(DefaultHeatmapConfig())

	// Create test data
	var data [7][24]float64
	for d := 0; d < 7; d++ {
		for hr := 0; hr < 24; hr++ {
			data[d][hr] = float64(d*24 + hr)
		}
	}

	h.SetData(data, 0, 167)

	if !h.hasData {
		t.Error("Heatmap should have data after SetData")
	}

	if h.min != 0 {
		t.Errorf("Expected min 0, got %f", h.min)
	}

	if h.max != 167 {
		t.Errorf("Expected max 167, got %f", h.max)
	}
}

func TestHeatmap_EmptyData(t *testing.T) {
	h := NewHeatmap(DefaultHeatmapConfig())
	h.config.Title = "Test Heatmap"

	view := h.View()

	if !strings.Contains(view, "Collecting data") {
		t.Error("Empty heatmap should show 'Collecting data' message")
	}

	if !strings.Contains(view, "Test Heatmap") {
		t.Error("Empty heatmap should show title")
	}
}

func TestHeatmap_ViewFull(t *testing.T) {
	config := DefaultHeatmapConfig()
	config.Title = "TPS Heatmap"
	config.Condensed = false
	h := NewHeatmap(config)

	// Create test data with varying values
	var data [7][24]float64
	for d := 0; d < 7; d++ {
		for hr := 0; hr < 24; hr++ {
			// Simulate higher activity during business hours
			if hr >= 9 && hr <= 17 && d >= 1 && d <= 5 {
				data[d][hr] = 1000
			} else {
				data[d][hr] = 100
			}
		}
	}

	h.SetData(data, 100, 1000)
	view := h.View()

	// Check title present
	if !strings.Contains(view, "TPS Heatmap") {
		t.Error("Full heatmap should show title")
	}

	// Check day labels present
	for _, day := range []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"} {
		if !strings.Contains(view, day) {
			t.Errorf("Full heatmap should show day label %s", day)
		}
	}

	// Check legend present
	if !strings.Contains(view, "Low") || !strings.Contains(view, "Peak") {
		t.Error("Full heatmap should show legend")
	}
}

func TestHeatmap_ViewCondensed(t *testing.T) {
	config := DefaultHeatmapConfig()
	config.Title = "TPS Heatmap"
	config.Condensed = true
	config.Width = 80
	h := NewHeatmap(config)

	// Create test data
	var data [7][24]float64
	for d := 0; d < 7; d++ {
		for hr := 0; hr < 24; hr++ {
			data[d][hr] = float64(hr * 100)
		}
	}

	h.SetData(data, 0, 2300)
	view := h.View()

	// Check period labels present
	for _, period := range []string{"Night", "Morning", "Afternoon", "Evening"} {
		if !strings.Contains(view, period) {
			t.Errorf("Condensed heatmap should show period label %s", period)
		}
	}

	// Check day labels present
	if !strings.Contains(view, "Mon") || !strings.Contains(view, "Sat") {
		t.Error("Condensed heatmap should show day labels")
	}
}

func TestHeatmap_SetSize(t *testing.T) {
	h := NewHeatmap(DefaultHeatmapConfig())

	// Wide terminal - should use full mode
	h.SetSize(120)
	if h.config.Condensed {
		t.Error("Wide terminal should not use condensed mode")
	}

	// Narrow terminal - should use condensed mode
	h.SetSize(80)
	if !h.config.Condensed {
		t.Error("Narrow terminal should use condensed mode")
	}
}

func TestHeatmap_GetIntensityLevel(t *testing.T) {
	h := NewHeatmap(DefaultHeatmapConfig())
	h.min = 0
	h.max = 100

	tests := []struct {
		value    float64
		expected int
	}{
		{0, 0},   // 0% -> Low
		{10, 0},  // 10% -> Low
		{24, 0},  // 24% -> Low
		{25, 1},  // 25% -> Medium
		{49, 1},  // 49% -> Medium
		{50, 2},  // 50% -> High
		{74, 2},  // 74% -> High
		{75, 3},  // 75% -> Peak
		{100, 3}, // 100% -> Peak
	}

	for _, tt := range tests {
		level := h.getIntensityLevel(tt.value)
		if level != tt.expected {
			t.Errorf("getIntensityLevel(%f) = %d, expected %d", tt.value, level, tt.expected)
		}
	}
}

func TestHeatmap_GetIntensityLevel_SameMinMax(t *testing.T) {
	h := NewHeatmap(DefaultHeatmapConfig())
	h.min = 50
	h.max = 50 // Same min and max

	// Should return medium level (2) when all values are the same
	level := h.getIntensityLevel(50)
	if level != 2 {
		t.Errorf("Expected level 2 for same min/max, got %d", level)
	}
}

func TestHeatmap_AggregatePeriod(t *testing.T) {
	h := NewHeatmap(DefaultHeatmapConfig())

	// Set up test data for Monday (day 1)
	for hr := 0; hr < 24; hr++ {
		h.data[1][hr] = float64(hr * 10)
	}

	// Test morning period (6-11)
	avg := h.aggregatePeriod(1, 6, 11)
	// Average of 60, 70, 80, 90, 100, 110 = 510/6 = 85
	expected := 85.0
	if avg != expected {
		t.Errorf("Expected average %f, got %f", expected, avg)
	}

	// Test with no data
	for hr := 0; hr < 24; hr++ {
		h.data[0][hr] = -1 // Sunday has no data
	}
	avg = h.aggregatePeriod(0, 6, 11)
	if avg != -1 {
		t.Errorf("Expected -1 for no data, got %f", avg)
	}
}

func TestHeatmap_RenderCell(t *testing.T) {
	h := NewHeatmap(DefaultHeatmapConfig())
	h.min = 0
	h.max = 100

	// Test no data cell
	cell := h.renderCell(-1)
	if !strings.Contains(cell, "··") {
		t.Error("No data cell should render as dots")
	}

	// Test low value
	h.hasData = true
	cell = h.renderCell(10)
	if !strings.Contains(cell, "░░") {
		t.Errorf("Low value cell should render as light shade, got %q", cell)
	}

	// Test peak value
	cell = h.renderCell(90)
	if !strings.Contains(cell, "██") {
		t.Errorf("Peak value cell should render as full block, got %q", cell)
	}
}

func TestHeatmapPanel_Visibility(t *testing.T) {
	panel := NewHeatmapPanel(DefaultHeatmapConfig())

	// Default should be hidden
	if panel.IsVisible() {
		t.Error("Panel should be hidden by default")
	}

	// View should be empty when hidden
	view := panel.View()
	if view != "" {
		t.Error("Hidden panel should return empty string")
	}

	// Toggle visibility
	panel.Toggle()
	if !panel.IsVisible() {
		t.Error("Panel should be visible after toggle")
	}

	// Toggle back
	panel.Toggle()
	if panel.IsVisible() {
		t.Error("Panel should be hidden after second toggle")
	}
}

func TestHeatmapPanel_SetVisible(t *testing.T) {
	panel := NewHeatmapPanel(DefaultHeatmapConfig())

	panel.SetVisible(true)
	if !panel.IsVisible() {
		t.Error("Panel should be visible after SetVisible(true)")
	}

	panel.SetVisible(false)
	if panel.IsVisible() {
		t.Error("Panel should be hidden after SetVisible(false)")
	}
}

func TestHeatmapPanel_Height(t *testing.T) {
	panel := NewHeatmapPanel(DefaultHeatmapConfig())

	// Hidden panel should have zero height
	if panel.Height() != 0 {
		t.Error("Hidden panel should have zero height")
	}

	// Visible panel should have non-zero height
	panel.SetVisible(true)
	if panel.Height() == 0 {
		t.Error("Visible panel should have non-zero height")
	}
}

func TestHeatmapPanel_WithData(t *testing.T) {
	panel := NewHeatmapPanel(DefaultHeatmapConfig())
	panel.SetVisible(true)
	panel.SetTitle("Test Heatmap")

	// Create test data
	var data [7][24]float64
	for d := 0; d < 7; d++ {
		for hr := 0; hr < 24; hr++ {
			data[d][hr] = float64(d*24 + hr)
		}
	}

	panel.SetData(data, 0, 167)

	if !panel.HasData() {
		t.Error("Panel should have data")
	}

	view := panel.View()
	if view == "" {
		t.Error("Visible panel with data should not return empty string")
	}

	// Check that it has a border (rounded corners)
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╯") {
		t.Error("Panel should have rounded border")
	}
}

func TestRenderSimpleHeatmap(t *testing.T) {
	var data [7][24]float64
	for d := 0; d < 7; d++ {
		for hr := 0; hr < 24; hr++ {
			data[d][hr] = float64(hr)
		}
	}

	config := DefaultHeatmapConfig()
	config.Title = "Simple Heatmap"

	view := RenderSimpleHeatmap(data, 0, 23, config)

	if !strings.Contains(view, "Simple Heatmap") {
		t.Error("RenderSimpleHeatmap should include title")
	}

	if !strings.Contains(view, "Mon") {
		t.Error("RenderSimpleHeatmap should include day labels")
	}
}

func TestDefaultHeatmapConfig(t *testing.T) {
	config := DefaultHeatmapConfig()

	if config.Width != 100 {
		t.Errorf("Default width should be 100, got %d", config.Width)
	}

	if !config.ShowLegend {
		t.Error("Default should show legend")
	}

	if !config.ShowHourAxis {
		t.Error("Default should show hour axis")
	}

	if !config.ShowDayAxis {
		t.Error("Default should show day axis")
	}

	if config.Condensed {
		t.Error("Default should not be condensed")
	}
}

func TestHeatmap_PartialData(t *testing.T) {
	h := NewHeatmap(DefaultHeatmapConfig())

	// Only set data for weekdays during business hours
	var data [7][24]float64
	for d := 0; d < 7; d++ {
		for hr := 0; hr < 24; hr++ {
			data[d][hr] = -1 // Initialize with no data
		}
	}

	// Only Monday-Friday, 9-17
	for d := 1; d <= 5; d++ {
		for hr := 9; hr <= 17; hr++ {
			data[d][hr] = 500.0
		}
	}

	h.SetData(data, 500, 500)

	if !h.hasData {
		t.Error("Heatmap should have data")
	}

	view := h.View()

	// Should render with dots for missing data
	if !strings.Contains(view, "··") {
		t.Error("Partial data heatmap should show dots for missing cells")
	}
}
