package components

import (
	"strings"
	"testing"

	"github.com/guptarohit/asciigraph"

	"github.com/willibrandon/steep/internal/metrics"
)

func TestNewTimeSeriesChart(t *testing.T) {
	config := DefaultTimeSeriesConfig()
	chart := NewTimeSeriesChart(config)

	if chart == nil {
		t.Fatal("expected non-nil chart")
	}
	if chart.width != config.Width {
		t.Errorf("expected width %d, got %d", config.Width, chart.width)
	}
	if chart.height != config.Height {
		t.Errorf("expected height %d, got %d", config.Height, chart.height)
	}
}

func TestTimeSeriesChart_SetData(t *testing.T) {
	chart := NewTimeSeriesChart(DefaultTimeSeriesConfig())

	data := []float64{1, 2, 3, 4, 5}
	chart.SetData(data)

	if len(chart.data) != len(data) {
		t.Errorf("expected %d data points, got %d", len(data), len(chart.data))
	}
}

func TestTimeSeriesChart_HasSufficientData(t *testing.T) {
	config := DefaultTimeSeriesConfig()
	config.MinPoints = 3
	chart := NewTimeSeriesChart(config)

	// No data
	if chart.HasSufficientData() {
		t.Error("expected false with no data")
	}

	// Insufficient data
	chart.SetData([]float64{1, 2})
	if chart.HasSufficientData() {
		t.Error("expected false with 2 points")
	}

	// Just enough data
	chart.SetData([]float64{1, 2, 3})
	if !chart.HasSufficientData() {
		t.Error("expected true with 3 points")
	}

	// More than enough
	chart.SetData([]float64{1, 2, 3, 4, 5})
	if !chart.HasSufficientData() {
		t.Error("expected true with 5 points")
	}
}

func TestTimeSeriesChart_SetWindow(t *testing.T) {
	chart := NewTimeSeriesChart(DefaultTimeSeriesConfig())

	windows := []metrics.TimeWindow{
		metrics.TimeWindow1m,
		metrics.TimeWindow5m,
		metrics.TimeWindow15m,
		metrics.TimeWindow1h,
		metrics.TimeWindow24h,
	}

	for _, w := range windows {
		chart.SetWindow(w)
		if chart.window != w {
			t.Errorf("expected window %v, got %v", w, chart.window)
		}
	}
}

func TestTimeSeriesChart_SetSize(t *testing.T) {
	chart := NewTimeSeriesChart(DefaultTimeSeriesConfig())

	// Valid sizes
	chart.SetSize(100, 10)
	if chart.width != 100 {
		t.Errorf("expected width 100, got %d", chart.width)
	}
	if chart.height != 10 {
		t.Errorf("expected height 10, got %d", chart.height)
	}

	// Too small width - should keep previous
	chart.SetSize(10, 10)
	if chart.width != 100 {
		t.Errorf("expected width to remain 100 (too small), got %d", chart.width)
	}

	// Too small height - should keep previous
	chart.SetSize(100, 1)
	if chart.height != 10 {
		t.Errorf("expected height to remain 10 (too small), got %d", chart.height)
	}
}

func TestTimeSeriesChart_View_CollectingData(t *testing.T) {
	chart := NewTimeSeriesChart(DefaultTimeSeriesConfig())
	chart.SetTitle("TPS")
	chart.SetWindow(metrics.TimeWindow1m)
	chart.SetSize(80, 8)

	// No data - should show collecting message
	view := chart.View()

	if !strings.Contains(view, "Collecting data...") {
		t.Error("expected 'Collecting data...' message in view")
	}

	// With some data but not enough
	chart.SetData([]float64{1, 2})
	view = chart.View()

	if !strings.Contains(view, "Collecting data...") {
		t.Error("expected 'Collecting data...' message with insufficient data")
	}
	if !strings.Contains(view, "2/60") {
		t.Errorf("expected point count '2/60' in view, got: %s", view)
	}
}

func TestTimeSeriesChart_View_WithData(t *testing.T) {
	config := DefaultTimeSeriesConfig()
	config.MinPoints = 3
	chart := NewTimeSeriesChart(config)
	chart.SetTitle("TPS")
	chart.SetWindow(metrics.TimeWindow1h)
	chart.SetSize(80, 8)

	// Provide enough data
	data := make([]float64, 100)
	for i := range data {
		data[i] = float64(i%50 + 10)
	}
	chart.SetData(data)

	view := chart.View()

	// Should not show collecting message
	if strings.Contains(view, "Collecting data...") {
		t.Error("should not show 'Collecting data...' with sufficient data")
	}

	// Should have border characters
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╯") {
		t.Error("expected rounded border characters in view")
	}
}

func TestTimeSeriesChart_buildCaption(t *testing.T) {
	chart := NewTimeSeriesChart(DefaultTimeSeriesConfig())

	// With title
	chart.SetTitle("TPS")
	chart.SetWindow(metrics.TimeWindow1h)
	caption := chart.buildCaption()
	if caption != "TPS (1h)" {
		t.Errorf("expected 'TPS (1h)', got '%s'", caption)
	}

	// Without title
	chart.SetTitle("")
	caption = chart.buildCaption()
	if caption != "1h" {
		t.Errorf("expected '1h', got '%s'", caption)
	}
}

func TestTimeSeriesChart_expectedPoints(t *testing.T) {
	chart := NewTimeSeriesChart(DefaultTimeSeriesConfig())

	tests := []struct {
		window   metrics.TimeWindow
		expected int
	}{
		{metrics.TimeWindow1m, 60},
		{metrics.TimeWindow5m, 300},
		{metrics.TimeWindow15m, 90},
		{metrics.TimeWindow1h, 360},
		{metrics.TimeWindow24h, 1440},
	}

	for _, tt := range tests {
		chart.SetWindow(tt.window)
		got := chart.expectedPoints()
		if got != tt.expected {
			t.Errorf("window %s: expected %d points, got %d", tt.window.String(), tt.expected, got)
		}
	}
}

func TestTimeSeriesChart_resampleData(t *testing.T) {
	config := DefaultTimeSeriesConfig()
	config.Width = 50 // Graph width will be ~38 after accounting for Y-axis
	chart := NewTimeSeriesChart(config)

	// Small data - no resampling needed
	smallData := []float64{1, 2, 3, 4, 5}
	chart.SetData(smallData)
	resampled := chart.resampleData()
	if len(resampled) != len(smallData) {
		t.Errorf("small data should not be resampled: expected %d, got %d", len(smallData), len(resampled))
	}

	// Large data - should be downsampled
	largeData := make([]float64, 200)
	for i := range largeData {
		largeData[i] = float64(i)
	}
	chart.SetData(largeData)
	resampled = chart.resampleData()
	if len(resampled) >= len(largeData) {
		t.Error("large data should be downsampled")
	}
}

func TestNewTimeSeriesPanel(t *testing.T) {
	panel := NewTimeSeriesPanel()

	if panel == nil {
		t.Fatal("expected non-nil panel")
	}
	if panel.tpsChart == nil {
		t.Error("expected non-nil TPS chart")
	}
	if panel.connectionsChart == nil {
		t.Error("expected non-nil Connections chart")
	}
	if panel.cacheHitChart == nil {
		t.Error("expected non-nil Cache Hit chart")
	}
}

func TestTimeSeriesPanel_SetWindow(t *testing.T) {
	panel := NewTimeSeriesPanel()

	panel.SetWindow(metrics.TimeWindow5m)

	if panel.GetWindow() != metrics.TimeWindow5m {
		t.Error("expected window to be 5m")
	}
	if panel.tpsChart.window != metrics.TimeWindow5m {
		t.Error("expected TPS chart window to be 5m")
	}
	if panel.connectionsChart.window != metrics.TimeWindow5m {
		t.Error("expected Connections chart window to be 5m")
	}
	if panel.cacheHitChart.window != metrics.TimeWindow5m {
		t.Error("expected Cache Hit chart window to be 5m")
	}
}

func TestTimeSeriesPanel_SetData(t *testing.T) {
	panel := NewTimeSeriesPanel()

	tpsData := []float64{100, 150, 200}
	connData := []float64{10, 20, 30}
	cacheData := []float64{95, 96, 97}

	panel.SetTPSData(tpsData)
	panel.SetConnectionsData(connData)
	panel.SetCacheHitData(cacheData)

	if len(panel.tpsChart.data) != 3 {
		t.Error("TPS data not set correctly")
	}
	if len(panel.connectionsChart.data) != 3 {
		t.Error("Connections data not set correctly")
	}
	if len(panel.cacheHitChart.data) != 3 {
		t.Error("Cache Hit data not set correctly")
	}
}

func TestTimeSeriesPanel_SetSize(t *testing.T) {
	panel := NewTimeSeriesPanel()

	panel.SetSize(120, 30)

	if panel.width != 120 {
		t.Errorf("expected panel width 120, got %d", panel.width)
	}
	if panel.height != 30 {
		t.Errorf("expected panel height 30, got %d", panel.height)
	}

	// Charts should have updated sizes (30 / 3 = 10)
	expectedChartHeight := 30 / 3
	if panel.tpsChart.height != expectedChartHeight {
		t.Errorf("expected chart height %d, got %d", expectedChartHeight, panel.tpsChart.height)
	}
}

func TestTimeSeriesPanel_View(t *testing.T) {
	panel := NewTimeSeriesPanel()
	panel.SetSize(80, 30)

	// With no data - should show collecting messages
	view := panel.View()

	// Should have three "Collecting data..." messages
	count := strings.Count(view, "Collecting data...")
	if count != 3 {
		t.Errorf("expected 3 'Collecting data...' messages, got %d", count)
	}
}

func TestDefaultTimeSeriesConfig(t *testing.T) {
	config := DefaultTimeSeriesConfig()

	if config.Width != 80 {
		t.Errorf("expected default width 80, got %d", config.Width)
	}
	if config.Height != 8 {
		t.Errorf("expected default height 8, got %d", config.Height)
	}
	if config.Color != asciigraph.Green {
		t.Error("expected default color green")
	}
	if config.Window != metrics.TimeWindow1h {
		t.Error("expected default window 1h")
	}
	if config.MinPoints != 3 {
		t.Errorf("expected default minPoints 3, got %d", config.MinPoints)
	}
}

func TestTimeSeriesChart_MinimumDimensions(t *testing.T) {
	config := TimeSeriesConfig{
		Width:     5,  // Too small
		Height:    1,  // Too small
		MinPoints: 0,  // Invalid
	}
	chart := NewTimeSeriesChart(config)

	// Should enforce minimums
	if chart.width < 20 {
		t.Errorf("width should be at least 20, got %d", chart.width)
	}
	if chart.height < 3 {
		t.Errorf("height should be at least 3, got %d", chart.height)
	}
	if chart.minPoints < 1 {
		t.Errorf("minPoints should be at least 1, got %d", chart.minPoints)
	}
}

// Benchmark tests - target: <50ms per render

func BenchmarkTimeSeriesChart_Render(b *testing.B) {
	config := DefaultTimeSeriesConfig()
	config.Width = 80
	config.Height = 10
	chart := NewTimeSeriesChart(config)
	chart.SetTitle("TPS")
	chart.SetWindow(metrics.TimeWindow1h)

	// Typical workload: 360 data points (1h window)
	data := make([]float64, 360)
	for i := range data {
		data[i] = float64(100 + i%50)
	}
	chart.SetData(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = chart.View()
	}
}

func BenchmarkTimeSeriesChart_RenderLarge(b *testing.B) {
	config := DefaultTimeSeriesConfig()
	config.Width = 120
	config.Height = 15
	chart := NewTimeSeriesChart(config)
	chart.SetTitle("TPS")
	chart.SetWindow(metrics.TimeWindow24h)

	// Large workload: 1440 data points (24h window)
	data := make([]float64, 1440)
	for i := range data {
		data[i] = float64(50 + i%100)
	}
	chart.SetData(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = chart.View()
	}
}

func BenchmarkTimeSeriesChart_RenderEmpty(b *testing.B) {
	config := DefaultTimeSeriesConfig()
	config.Width = 80
	config.Height = 10
	chart := NewTimeSeriesChart(config)
	chart.SetTitle("TPS")
	chart.SetWindow(metrics.TimeWindow1h)
	// No data - renders "Collecting data..."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = chart.View()
	}
}

func BenchmarkTimeSeriesPanel_Render(b *testing.B) {
	panel := NewTimeSeriesPanel()
	panel.SetSize(120, 30)
	panel.SetWindow(metrics.TimeWindow1h)

	// Typical workload per chart
	data := make([]float64, 360)
	for i := range data {
		data[i] = float64(100 + i%50)
	}
	panel.SetTPSData(data)
	panel.SetConnectionsData(data)
	panel.SetCacheHitData(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = panel.View()
	}
}

func BenchmarkTimeSeriesChart_Resample(b *testing.B) {
	config := DefaultTimeSeriesConfig()
	config.Width = 80
	chart := NewTimeSeriesChart(config)

	// Large data that requires resampling
	data := make([]float64, 1440)
	for i := range data {
		data[i] = float64(i)
	}
	chart.SetData(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = chart.resampleData()
	}
}
