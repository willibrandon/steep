package alerts

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Expression represents a parsed metric expression that can be evaluated.
type Expression interface {
	// Evaluate computes the expression value given metric values.
	Evaluate(metrics MetricValues) (float64, error)

	// MetricNames returns all metric names referenced in the expression.
	MetricNames() []string
}

// MetricRef represents a reference to a metric by name.
type MetricRef struct {
	Name string
}

// Evaluate returns the value of the referenced metric.
func (m *MetricRef) Evaluate(metrics MetricValues) (float64, error) {
	value, ok := metrics.Get(m.Name)
	if !ok {
		return 0, fmt.Errorf("metric %q not available", m.Name)
	}
	return value, nil
}

// MetricNames returns the metric name this expression references.
func (m *MetricRef) MetricNames() []string {
	return []string{m.Name}
}

// Constant represents a constant numeric value.
type Constant struct {
	Value float64
}

// Evaluate returns the constant value.
func (c *Constant) Evaluate(_ MetricValues) (float64, error) {
	return c.Value, nil
}

// MetricNames returns an empty slice (constants don't reference metrics).
func (c *Constant) MetricNames() []string {
	return nil
}

// BinaryOp represents a binary operation (e.g., +, -, *, /).
type BinaryOp struct {
	Left  Expression
	Op    string
	Right Expression
}

// Evaluate computes the binary operation.
func (b *BinaryOp) Evaluate(metrics MetricValues) (float64, error) {
	left, err := b.Left.Evaluate(metrics)
	if err != nil {
		return 0, err
	}

	right, err := b.Right.Evaluate(metrics)
	if err != nil {
		return 0, err
	}

	switch b.Op {
	case "+":
		return left + right, nil
	case "-":
		return left - right, nil
	case "*":
		return left * right, nil
	case "/":
		if right == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return left / right, nil
	default:
		return 0, fmt.Errorf("unknown operator: %s", b.Op)
	}
}

// MetricNames returns all metric names referenced by this expression.
func (b *BinaryOp) MetricNames() []string {
	names := b.Left.MetricNames()
	names = append(names, b.Right.MetricNames()...)
	return names
}

// Parser parses metric expressions.
type Parser struct {
	input string
	pos   int
}

// NewParser creates a new expression parser.
func NewParser() *Parser {
	return &Parser{}
}

// Parse parses a metric expression string.
// Supports:
//   - Simple metric references: "active_connections"
//   - Binary operations: "active_connections / max_connections"
//   - Constants: "100", "0.95"
//   - Parentheses: "(a + b) * c"
func (p *Parser) Parse(expr string) (Expression, error) {
	p.input = strings.TrimSpace(expr)
	p.pos = 0

	if p.input == "" {
		return nil, fmt.Errorf("empty expression")
	}

	result, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	// Ensure we consumed the entire input
	p.skipWhitespace()
	if p.pos < len(p.input) {
		return nil, fmt.Errorf("unexpected character at position %d: %c", p.pos, p.input[p.pos])
	}

	return result, nil
}

// parseExpression parses addition and subtraction (lowest precedence).
func (p *Parser) parseExpression() (Expression, error) {
	left, err := p.parseTerm()
	if err != nil {
		return nil, err
	}

	for {
		p.skipWhitespace()
		if p.pos >= len(p.input) {
			break
		}

		op := p.input[p.pos]
		if op != '+' && op != '-' {
			break
		}
		p.pos++

		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}

		left = &BinaryOp{Left: left, Op: string(op), Right: right}
	}

	return left, nil
}

// parseTerm parses multiplication and division (higher precedence).
func (p *Parser) parseTerm() (Expression, error) {
	left, err := p.parseFactor()
	if err != nil {
		return nil, err
	}

	for {
		p.skipWhitespace()
		if p.pos >= len(p.input) {
			break
		}

		op := p.input[p.pos]
		if op != '*' && op != '/' {
			break
		}
		p.pos++

		right, err := p.parseFactor()
		if err != nil {
			return nil, err
		}

		left = &BinaryOp{Left: left, Op: string(op), Right: right}
	}

	return left, nil
}

// parseFactor parses the highest precedence elements (numbers, identifiers, parentheses).
func (p *Parser) parseFactor() (Expression, error) {
	p.skipWhitespace()

	if p.pos >= len(p.input) {
		return nil, fmt.Errorf("unexpected end of expression")
	}

	// Check for parentheses
	if p.input[p.pos] == '(' {
		p.pos++ // consume '('
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		p.skipWhitespace()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++ // consume ')'
		return expr, nil
	}

	// Check for number (including negative numbers and decimals)
	if p.isNumberStart() {
		return p.parseNumber()
	}

	// Check for identifier (metric name)
	if p.isIdentifierStart() {
		return p.parseIdentifier()
	}

	return nil, fmt.Errorf("unexpected character: %c", p.input[p.pos])
}

// parseNumber parses a numeric constant.
func (p *Parser) parseNumber() (Expression, error) {
	start := p.pos

	// Handle optional negative sign
	if p.pos < len(p.input) && p.input[p.pos] == '-' {
		p.pos++
	}

	// Parse digits before decimal point
	for p.pos < len(p.input) && unicode.IsDigit(rune(p.input[p.pos])) {
		p.pos++
	}

	// Parse optional decimal point and digits after
	if p.pos < len(p.input) && p.input[p.pos] == '.' {
		p.pos++
		for p.pos < len(p.input) && unicode.IsDigit(rune(p.input[p.pos])) {
			p.pos++
		}
	}

	numStr := p.input[start:p.pos]
	value, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid number: %s", numStr)
	}

	return &Constant{Value: value}, nil
}

// parseIdentifier parses a metric name identifier.
func (p *Parser) parseIdentifier() (Expression, error) {
	start := p.pos

	// Identifiers must start with a letter or underscore
	for p.pos < len(p.input) && p.isIdentifierChar(p.input[p.pos]) {
		p.pos++
	}

	name := p.input[start:p.pos]
	if name == "" {
		return nil, fmt.Errorf("expected identifier")
	}

	return &MetricRef{Name: name}, nil
}

// skipWhitespace advances past whitespace characters.
func (p *Parser) skipWhitespace() {
	for p.pos < len(p.input) && unicode.IsSpace(rune(p.input[p.pos])) {
		p.pos++
	}
}

// isNumberStart returns true if the current character could start a number.
func (p *Parser) isNumberStart() bool {
	if p.pos >= len(p.input) {
		return false
	}
	c := p.input[p.pos]
	return unicode.IsDigit(rune(c)) || c == '.'
}

// isIdentifierStart returns true if the current character could start an identifier.
func (p *Parser) isIdentifierStart() bool {
	if p.pos >= len(p.input) {
		return false
	}
	c := rune(p.input[p.pos])
	return unicode.IsLetter(c) || c == '_'
}

// isIdentifierChar returns true if the character is valid in an identifier.
func (p *Parser) isIdentifierChar(c byte) bool {
	r := rune(c)
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// ParseExpression is a convenience function to parse an expression string.
func ParseExpression(expr string) (Expression, error) {
	parser := NewParser()
	return parser.Parse(expr)
}
