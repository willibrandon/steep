// Package setup provides setup wizard and configuration check components.
package setup

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/willibrandon/steep/internal/ui/styles"
)

// ConnStringConfig holds the connection string builder configuration.
type ConnStringConfig struct {
	Host            string
	Port            string
	User            string
	Password        string
	Database        string
	ApplicationName string
	SSLMode         SSLMode
	ConnectTimeout  string
	TargetSession   string // target_session_attrs for failover
}

// ConnStringState holds the connection string builder UI state.
type ConnStringState struct {
	Config        ConnStringConfig
	SelectedField int
	EditingField  int
	InputBuffer   string
	TestResult    string // Result of connection test
	TestError     bool   // True if test failed
	Testing       bool   // True if test in progress
}

// NewConnStringState creates a new connection string builder state with defaults.
func NewConnStringState(host, port string) *ConnStringState {
	return &ConnStringState{
		Config: ConnStringConfig{
			Host:            host,
			Port:            port,
			User:            "replicator",
			Password:        "",
			Database:        "",
			ApplicationName: "walreceiver",
			SSLMode:         SSLPrefer,
			ConnectTimeout:  "10",
			TargetSession:   "",
		},
		SelectedField: 0,
		EditingField:  -1,
	}
}

// ConnStringRenderConfig holds rendering configuration.
type ConnStringRenderConfig struct {
	Width    int
	Height   int
	ReadOnly bool
}

// RenderConnStringBuilder renders the connection string builder.
func RenderConnStringBuilder(state *ConnStringState, cfg ConnStringRenderConfig) string {
	var b strings.Builder

	// Header
	b.WriteString("\n")
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.ColorAccent)
	b.WriteString(headerStyle.Render("Connection String Builder"))
	b.WriteString("\n\n")

	labelStyle := lipgloss.NewStyle().Width(20)
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("255"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	// Field definitions
	fields := []struct {
		label    string
		value    string
		required bool
	}{
		{"Host:", state.Config.Host, true},
		{"Port:", state.Config.Port, true},
		{"User:", state.Config.User, true},
		{"Password:", maskPassword(state.Config.Password), false},
		{"Database:", state.Config.Database, false},
		{"Application Name:", state.Config.ApplicationName, false},
		{"SSL Mode:", string(state.Config.SSLMode), true},
		{"Connect Timeout:", state.Config.ConnectTimeout, false},
		{"Target Session:", state.Config.TargetSession, false},
	}

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Connection Parameters"))
	b.WriteString("\n\n")

	for i, f := range fields {
		label := labelStyle.Render(f.label)
		value := f.value
		if value == "" {
			value = hintStyle.Render("(empty)")
		}

		// Mark required fields
		req := ""
		if f.required && f.value == "" {
			req = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(" *")
		}

		if state.SelectedField == i {
			if state.EditingField == i {
				b.WriteString(selectedStyle.Render(label + " " + state.InputBuffer + "▌"))
			} else {
				b.WriteString(selectedStyle.Render(label + " " + value + req))
			}
		} else {
			b.WriteString(label + " " + valueStyle.Render(value) + req)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Live preview section
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Generated Connection String"))
	b.WriteString("\n\n")

	connStr := GenerateConnString(&state.Config)
	codeStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("255")).
		Padding(0, 1)

	// Wrap long connection strings
	maxWidth := cfg.Width - 4
	if maxWidth < 40 {
		maxWidth = 40
	}
	wrapped := wrapText(connStr, maxWidth)
	b.WriteString(codeStyle.Render(wrapped))
	b.WriteString("\n\n")

	// primary_conninfo format (for recovery.conf/postgresql.auto.conf)
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("primary_conninfo (for standby)"))
	b.WriteString("\n\n")
	primaryConnInfo := GeneratePrimaryConnInfoFromConfig(&state.Config)
	wrapped = wrapText(primaryConnInfo, maxWidth)
	b.WriteString(codeStyle.Render(wrapped))
	b.WriteString("\n")

	// Test result
	if state.TestResult != "" {
		b.WriteString("\n")
		if state.TestError {
			errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
			b.WriteString(errorStyle.Render("✗ " + state.TestResult))
		} else {
			successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
			b.WriteString(successStyle.Render("✓ " + state.TestResult))
		}
		b.WriteString("\n")
	}

	if state.Testing {
		b.WriteString("\n")
		b.WriteString(hintStyle.Render("Testing connection..."))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(renderConnStringFooter(state))

	return b.String()
}

// GenerateConnString generates a PostgreSQL connection string (URI format).
func GenerateConnString(cfg *ConnStringConfig) string {
	// Build URI format: postgresql://user:password@host:port/database?params
	var b strings.Builder

	b.WriteString("postgresql://")

	if cfg.User != "" {
		b.WriteString(url.PathEscape(cfg.User))
		if cfg.Password != "" {
			b.WriteString(":")
			b.WriteString(url.PathEscape(cfg.Password))
		}
		b.WriteString("@")
	}

	b.WriteString(cfg.Host)
	if cfg.Port != "" && cfg.Port != "5432" {
		b.WriteString(":")
		b.WriteString(cfg.Port)
	}

	b.WriteString("/")
	if cfg.Database != "" {
		b.WriteString(url.PathEscape(cfg.Database))
	}

	// Query parameters
	params := url.Values{}
	if cfg.SSLMode != "" {
		params.Add("sslmode", string(cfg.SSLMode))
	}
	if cfg.ApplicationName != "" {
		params.Add("application_name", cfg.ApplicationName)
	}
	if cfg.ConnectTimeout != "" {
		params.Add("connect_timeout", cfg.ConnectTimeout)
	}
	if cfg.TargetSession != "" {
		params.Add("target_session_attrs", cfg.TargetSession)
	}

	if len(params) > 0 {
		b.WriteString("?")
		b.WriteString(params.Encode())
	}

	return b.String()
}

// GeneratePrimaryConnInfoFromConfig generates a primary_conninfo string for standby configuration.
func GeneratePrimaryConnInfoFromConfig(cfg *ConnStringConfig) string {
	// Key=value format for primary_conninfo
	var parts []string

	if cfg.Host != "" {
		parts = append(parts, fmt.Sprintf("host=%s", cfg.Host))
	}
	if cfg.Port != "" {
		parts = append(parts, fmt.Sprintf("port=%s", cfg.Port))
	}
	if cfg.User != "" {
		parts = append(parts, fmt.Sprintf("user=%s", cfg.User))
	}
	if cfg.Password != "" {
		// Escape single quotes in password
		escapedPass := strings.ReplaceAll(cfg.Password, "'", "\\'")
		parts = append(parts, fmt.Sprintf("password='%s'", escapedPass))
	}
	if cfg.SSLMode != "" {
		parts = append(parts, fmt.Sprintf("sslmode=%s", cfg.SSLMode))
	}
	if cfg.ApplicationName != "" {
		parts = append(parts, fmt.Sprintf("application_name=%s", cfg.ApplicationName))
	}
	if cfg.ConnectTimeout != "" {
		parts = append(parts, fmt.Sprintf("connect_timeout=%s", cfg.ConnectTimeout))
	}
	if cfg.TargetSession != "" {
		parts = append(parts, fmt.Sprintf("target_session_attrs=%s", cfg.TargetSession))
	}

	return strings.Join(parts, " ")
}

// GetConnStringForTest returns a connection string suitable for testing.
func GetConnStringForTest(cfg *ConnStringConfig) string {
	return GenerateConnString(cfg)
}

// renderConnStringFooter renders the footer with available actions.
func renderConnStringFooter(state *ConnStringState) string {
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	hints := []string{
		"[j/k]nav",
		"[Enter]edit",
		"[Space]cycle SSL",
		"[t]test",
		"[y]copy",
		"[Esc]close",
	}

	return hintStyle.Render(strings.Join(hints, "  "))
}

// maskPassword masks password characters for display.
func maskPassword(password string) string {
	if password == "" {
		return ""
	}
	return strings.Repeat("•", len(password))
}

// wrapText wraps text to fit within maxWidth.
func wrapText(text string, maxWidth int) string {
	if len(text) <= maxWidth {
		return text
	}

	var lines []string
	for len(text) > maxWidth {
		lines = append(lines, text[:maxWidth])
		text = text[maxWidth:]
	}
	if len(text) > 0 {
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

// GetConnStringMaxField returns the maximum field index for navigation.
func GetConnStringMaxField() int {
	return 8 // 9 fields (0-8)
}

// GetConnStringFieldValue returns the current value of a field by index.
func GetConnStringFieldValue(state *ConnStringState, field int) string {
	switch field {
	case 0:
		return state.Config.Host
	case 1:
		return state.Config.Port
	case 2:
		return state.Config.User
	case 3:
		return state.Config.Password
	case 4:
		return state.Config.Database
	case 5:
		return state.Config.ApplicationName
	case 6:
		return string(state.Config.SSLMode)
	case 7:
		return state.Config.ConnectTimeout
	case 8:
		return state.Config.TargetSession
	default:
		return ""
	}
}

// SetConnStringFieldValue sets the value of a field by index.
func SetConnStringFieldValue(state *ConnStringState, field int, value string) {
	switch field {
	case 0:
		state.Config.Host = value
	case 1:
		state.Config.Port = value
	case 2:
		state.Config.User = value
	case 3:
		state.Config.Password = value
	case 4:
		state.Config.Database = value
	case 5:
		state.Config.ApplicationName = value
	case 6:
		state.Config.SSLMode = SSLMode(value)
	case 7:
		state.Config.ConnectTimeout = value
	case 8:
		state.Config.TargetSession = value
	}
}

// IsConnStringFieldEditable returns true if the field can be edited with text input.
func IsConnStringFieldEditable(field int) bool {
	// SSL mode (field 6) is cycled with space, not edited
	return field != 6
}
