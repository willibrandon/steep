package alerts

import (
	"testing"
	"time"
)

// mockMetrics implements MetricValues for testing.
type mockMetrics struct {
	values    map[string]float64
	timestamp time.Time
}

func (m *mockMetrics) Get(name string) (float64, bool) {
	v, ok := m.values[name]
	return v, ok
}

func (m *mockMetrics) Timestamp() time.Time {
	return m.timestamp
}

func TestParseSimpleMetric(t *testing.T) {
	parser := NewParser()
	expr, err := parser.Parse("active_connections")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &mockMetrics{
		values:    map[string]float64{"active_connections": 50},
		timestamp: time.Now(),
	}

	value, err := expr.Evaluate(metrics)
	if err != nil {
		t.Fatalf("unexpected evaluation error: %v", err)
	}

	if value != 50 {
		t.Errorf("expected 50, got %v", value)
	}
}

func TestParseConstant(t *testing.T) {
	parser := NewParser()
	expr, err := parser.Parse("100.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &mockMetrics{values: map[string]float64{}}

	value, err := expr.Evaluate(metrics)
	if err != nil {
		t.Fatalf("unexpected evaluation error: %v", err)
	}

	if value != 100.5 {
		t.Errorf("expected 100.5, got %v", value)
	}
}

func TestParseDivision(t *testing.T) {
	parser := NewParser()
	expr, err := parser.Parse("active_connections / max_connections")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &mockMetrics{
		values: map[string]float64{
			"active_connections": 80,
			"max_connections":    100,
		},
		timestamp: time.Now(),
	}

	value, err := expr.Evaluate(metrics)
	if err != nil {
		t.Fatalf("unexpected evaluation error: %v", err)
	}

	expected := 0.8
	if value != expected {
		t.Errorf("expected %v, got %v", expected, value)
	}
}

func TestParseMultiplication(t *testing.T) {
	parser := NewParser()
	expr, err := parser.Parse("tps * 60")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &mockMetrics{
		values:    map[string]float64{"tps": 100},
		timestamp: time.Now(),
	}

	value, err := expr.Evaluate(metrics)
	if err != nil {
		t.Fatalf("unexpected evaluation error: %v", err)
	}

	if value != 6000 {
		t.Errorf("expected 6000, got %v", value)
	}
}

func TestParseAddition(t *testing.T) {
	parser := NewParser()
	expr, err := parser.Parse("a + b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &mockMetrics{
		values:    map[string]float64{"a": 10, "b": 20},
		timestamp: time.Now(),
	}

	value, err := expr.Evaluate(metrics)
	if err != nil {
		t.Fatalf("unexpected evaluation error: %v", err)
	}

	if value != 30 {
		t.Errorf("expected 30, got %v", value)
	}
}

func TestParseSubtraction(t *testing.T) {
	parser := NewParser()
	expr, err := parser.Parse("total - used")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &mockMetrics{
		values:    map[string]float64{"total": 100, "used": 40},
		timestamp: time.Now(),
	}

	value, err := expr.Evaluate(metrics)
	if err != nil {
		t.Fatalf("unexpected evaluation error: %v", err)
	}

	if value != 60 {
		t.Errorf("expected 60, got %v", value)
	}
}

func TestParseParentheses(t *testing.T) {
	parser := NewParser()
	expr, err := parser.Parse("(a + b) * c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &mockMetrics{
		values:    map[string]float64{"a": 2, "b": 3, "c": 4},
		timestamp: time.Now(),
	}

	value, err := expr.Evaluate(metrics)
	if err != nil {
		t.Fatalf("unexpected evaluation error: %v", err)
	}

	// (2 + 3) * 4 = 20
	if value != 20 {
		t.Errorf("expected 20, got %v", value)
	}
}

func TestParseOperatorPrecedence(t *testing.T) {
	parser := NewParser()
	expr, err := parser.Parse("a + b * c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &mockMetrics{
		values:    map[string]float64{"a": 2, "b": 3, "c": 4},
		timestamp: time.Now(),
	}

	value, err := expr.Evaluate(metrics)
	if err != nil {
		t.Fatalf("unexpected evaluation error: %v", err)
	}

	// 2 + (3 * 4) = 14 (not 20)
	if value != 14 {
		t.Errorf("expected 14, got %v", value)
	}
}

func TestParseComplexExpression(t *testing.T) {
	parser := NewParser()
	// active_connections / max_connections for ratio-based alerting
	expr, err := parser.Parse("(active_connections / max_connections) * 100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &mockMetrics{
		values: map[string]float64{
			"active_connections": 80,
			"max_connections":    100,
		},
		timestamp: time.Now(),
	}

	value, err := expr.Evaluate(metrics)
	if err != nil {
		t.Fatalf("unexpected evaluation error: %v", err)
	}

	// (80 / 100) * 100 = 80%
	if value != 80 {
		t.Errorf("expected 80, got %v", value)
	}
}

func TestParseDivisionByZero(t *testing.T) {
	parser := NewParser()
	expr, err := parser.Parse("a / b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &mockMetrics{
		values:    map[string]float64{"a": 10, "b": 0},
		timestamp: time.Now(),
	}

	_, err = expr.Evaluate(metrics)
	if err == nil {
		t.Error("expected division by zero error")
	}
}

func TestParseMissingMetric(t *testing.T) {
	parser := NewParser()
	expr, err := parser.Parse("unknown_metric")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &mockMetrics{
		values:    map[string]float64{},
		timestamp: time.Now(),
	}

	_, err = expr.Evaluate(metrics)
	if err == nil {
		t.Error("expected missing metric error")
	}
}

func TestParseEmptyExpression(t *testing.T) {
	parser := NewParser()
	_, err := parser.Parse("")
	if err == nil {
		t.Error("expected error for empty expression")
	}
}

func TestParseInvalidExpression(t *testing.T) {
	parser := NewParser()

	invalidExprs := []string{
		"a +",     // incomplete
		"+ b",     // missing left operand
		"(a + b",  // unclosed paren
		"a b",     // missing operator
		"a + + b", // double operator
	}

	for _, expr := range invalidExprs {
		_, err := parser.Parse(expr)
		if err == nil {
			t.Errorf("expected error for expression %q", expr)
		}
	}
}

func TestMetricNames(t *testing.T) {
	parser := NewParser()
	expr, err := parser.Parse("active_connections / max_connections")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := expr.MetricNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 metric names, got %d", len(names))
	}

	// Check that both metrics are present
	found := map[string]bool{}
	for _, name := range names {
		found[name] = true
	}

	if !found["active_connections"] {
		t.Error("expected active_connections in metric names")
	}
	if !found["max_connections"] {
		t.Error("expected max_connections in metric names")
	}
}

func TestConstantMetricNames(t *testing.T) {
	parser := NewParser()
	expr, err := parser.Parse("100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := expr.MetricNames()
	if len(names) != 0 {
		t.Errorf("expected 0 metric names for constant, got %d", len(names))
	}
}
