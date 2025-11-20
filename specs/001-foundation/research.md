# Research: Foundation Infrastructure

**Feature**: 001-foundation | **Date**: 2025-11-19

## Overview

This document consolidates research findings for implementing Steep's foundation infrastructure using Go, Bubbletea TUI framework, pgx PostgreSQL driver, and related libraries.

---

## 1. Bubbletea Architecture Patterns

### Decision: Elm Architecture with Message Passing

**Rationale**: Bubbletea implements the Elm Architecture (Model-View-Update pattern), providing predictable state management and clean separation of concerns.

**Implementation Pattern**:
```go
type Model struct {
    // State
    currentView ViewType
    views       map[ViewType]ViewModel
    dbPool      *pgxpool.Pool
    // ... other state
}

func (m Model) Init() tea.Cmd {
    return tea.Batch(
        connectToDatabase,
        tea.EnterAltScreen,
    )
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        return m.handleKeypress(msg)
    case tea.WindowSizeMsg:
        m.width, m.height = msg.Width, msg.Height
        return m, nil
    case DatabaseConnectedMsg:
        m.connected = true
        return m, nil
    }
    return m, nil
}

func (m Model) View() string {
    return lipgloss.JoinVertical(
        lipgloss.Left,
        m.header(),
        m.currentViewRender(),
        m.statusBar(),
        m.helpBar(),
    )
}
```

**Best Practices**:
- **Immutable State**: Return new model copies in Update(), never mutate existing state
- **Message Types**: Define custom message types for domain events (DatabaseConnectedMsg, ConnectionLostMsg, etc.)
- **Command Batching**: Use `tea.Batch()` to combine multiple commands
- **Tick Commands**: Use `tea.Tick()` for periodic updates (status bar refresh)

**Alternatives Considered**:
- **tview**: More feature-rich but less flexible, doesn't follow Elm architecture
- **termui**: Widget-based, less suitable for dynamic TUI applications
- **tcell directly**: Too low-level, would require implementing framework features

**References**:
- Bubbletea examples: `/Users/brandon/src/bubbletea/examples/`
- Official tutorial: https://github.com/charmbracelet/bubbletea/tree/master/tutorials

---

## 2. pgx Connection Pooling Configuration

### Decision: pgxpool with Conservative Defaults

**Rationale**: pgxpool provides automatic connection management, health checks, and optimal performance for concurrent queries. Conservative pool sizing prevents overwhelming the database.

**Configuration**:
```go
config, err := pgxpool.ParseConfig(connString)
if err != nil {
    return nil, err
}

// Conservative pool sizing for monitoring tool
config.MaxConns = 10
config.MinConns = 2
config.MaxConnLifetime = time.Hour
config.MaxConnIdleTime = 30 * time.Minute
config.HealthCheckPeriod = time.Minute

pool, err := pgxpool.NewWithConfig(context.Background(), config)
```

**Best Practices**:
- **MaxConns**: 10 connections sufficient for foundation (1 status bar query + buffer)
- **MinConns**: 2 connections keep pool warm
- **MaxConnLifetime**: 1 hour prevents stale connections
- **HealthCheckPeriod**: 1 minute detects database unavailability
- **Context Usage**: Always pass context for timeout control

**Connection String Format**:
```
postgres://user@localhost:5432/dbname?sslmode=prefer&pool_max_conns=10
```

**Error Handling**:
- Retry transient errors (connection refused, timeout) with exponential backoff
- Fail fast on authentication errors (invalid credentials)
- Display actionable error messages ("Check PostgreSQL is running on localhost:5432")

**Alternatives Considered**:
- **database/sql with pq**: Standard library interface but less efficient than pgx
- **pgx without pooling**: Manual connection management, error-prone
- **GORM**: ORM overhead unnecessary for simple queries

**References**:
- pgx documentation: https://pkg.go.dev/github.com/jackc/pgx/v5
- pgxpool best practices: https://github.com/jackc/pgx/wiki/Getting-started-with-pgx-through-database-sql

---

## 3. Viper Configuration Management

### Decision: Viper with YAML Files and Environment Variable Overrides

**Rationale**: Viper provides hierarchical configuration with multiple sources (files, env vars, defaults), automatic type conversion, and live reload support.

**Configuration Structure**:
```yaml
# ~/.config/steep/config.yaml
connection:
  host: localhost
  port: 5432
  database: postgres
  user: brandon
  password_command: "pass show postgres/local"  # External password manager
  sslmode: prefer
  pool_max_conns: 10
  pool_min_conns: 2

ui:
  theme: dark
  refresh_interval: 1s
  date_format: "2006-01-02 15:04:05"

debug: false
```

**Implementation**:
```go
func LoadConfig() (*Config, error) {
    viper.SetConfigName("steep")
    viper.SetConfigType("yaml")
    viper.AddConfigPath("$HOME/.config/steep")
    viper.AddConfigPath(".")

    // Environment variable overrides
    viper.AutomaticEnv()
    viper.SetEnvPrefix("STEEP")
    viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

    // Defaults
    viper.SetDefault("connection.host", "localhost")
    viper.SetDefault("connection.port", 5432)
    viper.SetDefault("ui.refresh_interval", "1s")

    if err := viper.ReadInConfig(); err != nil {
        if _, ok := err.(viper.ConfigFileNotFoundError); ok {
            return createDefaultConfig()
        }
        return nil, err
    }

    var config Config
    if err := viper.Unmarshal(&config); err != nil {
        return nil, err
    }

    return &config, nil
}
```

**Best Practices**:
- **Config Paths**: Check `~/.config/steep/` first (XDG Base Directory), fallback to `.`
- **Environment Variables**: Support `STEEP_*` env vars (e.g., `STEEP_CONNECTION_HOST`)
- **PostgreSQL Env Vars**: Also check standard `PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`
- **Default Creation**: Auto-create config with commented examples if missing
- **Validation**: Validate required fields (host, port, database) after loading

**Alternatives Considered**:
- **Standard library flag package**: No file support, manual parsing
- **cobra + viper**: Cobra adds CLI flags (unnecessary for simple TUI)
- **godotenv**: Only supports .env files, no YAML

**References**:
- Viper documentation: https://github.com/spf13/viper
- XDG Base Directory spec: https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html

---

## 4. Password Command Execution

### Decision: Execute External Commands with Timeout

**Rationale**: Secure password retrieval via external password managers (pass, 1password, lastpass) without storing plaintext in config files.

**Implementation**:
```go
func ExecutePasswordCommand(cmd string) (string, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    parts := strings.Fields(cmd)
    if len(parts) == 0 {
        return "", errors.New("empty password command")
    }

    command := exec.CommandContext(ctx, parts[0], parts[1:]...)
    output, err := command.Output()
    if err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            return "", errors.New("password command timed out")
        }
        return "", fmt.Errorf("password command failed: %w", err)
    }

    password := strings.TrimSpace(string(output))
    if password == "" {
        return "", errors.New("password command returned empty string")
    }

    return password, nil
}
```

**Security Considerations**:
- **Timeout**: 5-second limit prevents hanging on slow commands
- **No Shell Expansion**: Use `exec.CommandContext()` with parsed args, not shell
- **Trim Whitespace**: Remove newlines/spaces from command output
- **Clear Memory**: Consider zeroing password string after use (not critical for short-lived TUI)

**Supported Password Managers**:
- `pass show postgres/local` (Unix password manager)
- `1password get item "PostgreSQL" --field password` (1Password CLI)
- `security find-generic-password -s postgres -w` (macOS Keychain)
- Custom scripts returning password to stdout

**Fallback Strategy**:
1. Check `password_command` in config → execute if present
2. Check `PGPASSWORD` environment variable → use if set
3. Check `~/.pgpass` file → parse for matching credentials (PostgreSQL standard)
4. Prompt user interactively → read from stdin with hidden input

**Alternatives Considered**:
- **Encrypted config file**: Requires master password, key management complexity
- **System keyring libraries**: Platform-specific, adds dependencies
- **Hardcoded passwords**: Insecure, rejected by constitution

---

## 5. Lipgloss Styling and Layout

### Decision: Centralized Theme with Semantic Colors

**Rationale**: Consistent visual styling across all views, easy theme switching, semantic color names for accessibility.

**Theme Definition**:
```go
package styles

import "github.com/charmbracelet/lipgloss"

var (
    // Color palette
    ColorPrimary   = lipgloss.Color("39")  // Cyan
    ColorSuccess   = lipgloss.Color("42")  // Green
    ColorWarning   = lipgloss.Color("226") // Yellow
    ColorError     = lipgloss.Color("196") // Red
    ColorMuted     = lipgloss.Color("245") // Gray
    ColorBackground = lipgloss.Color("235") // Dark gray

    // Base styles
    BaseStyle = lipgloss.NewStyle().
        Foreground(lipgloss.Color("252"))

    HeaderStyle = BaseStyle.Copy().
        Foreground(ColorPrimary).
        Bold(true).
        MarginBottom(1)

    StatusBarStyle = BaseStyle.Copy().
        Background(lipgloss.Color("237")).
        Padding(0, 1)

    ErrorStyle = BaseStyle.Copy().
        Foreground(ColorError).
        Bold(true)

    // Component styles
    TableHeaderStyle = BaseStyle.Copy().
        Foreground(ColorPrimary).
        Bold(true).
        BorderStyle(lipgloss.NormalBorder()).
        BorderBottom(true)

    HelpStyle = BaseStyle.Copy().
        Foreground(ColorMuted)
)
```

**Layout Patterns**:
```go
// Vertical stacking
func (m Model) View() string {
    return lipgloss.JoinVertical(
        lipgloss.Left,
        m.header(),
        m.content(),
        m.footer(),
    )
}

// Horizontal splitting
func (m Model) content() string {
    left := m.sidebar()
    right := m.mainPanel()
    return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// Responsive sizing
func (m Model) mainPanel() string {
    width := m.width - sidebarWidth - 2
    height := m.height - headerHeight - footerHeight
    return lipgloss.NewStyle().
        Width(width).
        Height(height).
        Render(m.currentView.View())
}
```

**Best Practices**:
- **Color Numbers**: Use ANSI 256-color codes for compatibility
- **Semantic Names**: Name styles by purpose (ErrorStyle) not appearance (RedStyle)
- **Responsive**: Calculate dimensions based on terminal size
- **Borders**: Use sparingly, prefer spacing for visual separation
- **Truncation**: Use lipgloss.Width() and truncate text for overflow

**Alternatives Considered**:
- **Inline styling**: Harder to maintain, inconsistent appearance
- **True color (24-bit)**: Not all terminals support, 256-color sufficient
- **Manual ANSI codes**: Error-prone, lipgloss provides abstraction

**References**:
- Lipgloss examples: https://github.com/charmbracelet/lipgloss/tree/master/examples
- ANSI color codes: https://www.ditig.com/256-colors-cheat-sheet

---

## 6. Testing Strategy with Testcontainers

### Decision: Testcontainers for Integration Tests, Table-Driven Unit Tests

**Rationale**: Testcontainers provides real PostgreSQL instances for integration tests, ensuring queries work against actual databases. Table-driven tests cover edge cases systematically.

**Integration Test Example**:
```go
func TestDatabaseConnection(t *testing.T) {
    ctx := context.Background()

    // Start PostgreSQL container
    postgres, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
        ContainerRequest: testcontainers.ContainerRequest{
            Image: "postgres:18-alpine",
            ExposedPorts: []string{"5432/tcp"},
            Env: map[string]string{
                "POSTGRES_PASSWORD": "test",
                "POSTGRES_DB":       "testdb",
            },
            WaitingFor: wait.ForLog("database system is ready to accept connections"),
        },
        Started: true,
    })
    require.NoError(t, err)
    defer postgres.Terminate(ctx)

    // Get connection string
    host, err := postgres.Host(ctx)
    require.NoError(t, err)
    port, err := postgres.MappedPort(ctx, "5432")
    require.NoError(t, err)

    connString := fmt.Sprintf("postgres://postgres:test@%s:%s/testdb", host, port.Port())

    // Test connection
    pool, err := pgxpool.New(ctx, connString)
    require.NoError(t, err)
    defer pool.Close()

    var version string
    err = pool.QueryRow(ctx, "SELECT version()").Scan(&version)
    require.NoError(t, err)
    assert.Contains(t, version, "PostgreSQL")
}
```

**Unit Test Example (Table-Driven)**:
```go
func TestPasswordCommandExecution(t *testing.T) {
    tests := []struct {
        name    string
        command string
        want    string
        wantErr bool
    }{
        {
            name:    "simple echo",
            command: "echo mypassword",
            want:    "mypassword",
            wantErr: false,
        },
        {
            name:    "empty command",
            command: "",
            want:    "",
            wantErr: true,
        },
        {
            name:    "command not found",
            command: "nonexistent-command",
            want:    "",
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ExecutePasswordCommand(tt.command)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
                assert.Equal(t, tt.want, got)
            }
        })
    }
}
```

**Best Practices**:
- **Isolation**: Each integration test gets fresh container
- **Cleanup**: Use `defer` to ensure container termination
- **Timeouts**: Set reasonable timeouts for container startup
- **Parallel Execution**: Mark independent tests with `t.Parallel()`
- **Coverage**: Aim for 80%+ coverage on business logic

**Test Organization**:
```
tests/
├── integration/
│   ├── connection_test.go       # Database connectivity
│   ├── queries_test.go          # Query execution
│   └── reconnection_test.go     # Reconnection logic
└── unit/
    ├── config_test.go           # Configuration parsing
    ├── password_test.go         # Password command
    └── ui/
        ├── table_test.go        # Table component
        └── statusbar_test.go    # Status bar
```

**Alternatives Considered**:
- **Mock database**: Doesn't test actual SQL compatibility
- **Shared test database**: State leakage between tests
- **Docker Compose**: Manual setup, not automated

**References**:
- Testcontainers Go: https://golang.testcontainers.org/
- Go testing best practices: https://go.dev/doc/tutorial/add-a-test

---

## 7. Error Handling and Logging

### Decision: Structured Logging with slog, Actionable Error Messages

**Rationale**: Go 1.21+ includes structured logging via `slog` package. Actionable error messages guide users to solutions.

**Logging Implementation**:
```go
import "log/slog"

func main() {
    // Default: errors/warnings to stderr only
    logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
        Level: slog.LevelWarn,
    }))

    // Debug mode: enable info logs
    if os.Getenv("STEEP_DEBUG") == "1" || debugFlag {
        logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
            Level: slog.LevelInfo,
        }))
    }

    slog.SetDefault(logger)
}

// Usage
slog.Error("database connection failed",
    "host", config.Host,
    "port", config.Port,
    "error", err)

slog.Warn("reconnection attempt failed",
    "attempt", attemptNum,
    "next_retry", nextRetry)

slog.Info("configuration loaded",
    "path", configPath)
```

**Error Message Best Practices**:
```go
// Bad: vague error
return fmt.Errorf("connection failed: %w", err)

// Good: actionable error
return fmt.Errorf(
    "connection refused: ensure PostgreSQL is running on %s:%d\n"+
    "Try: systemctl status postgresql (Linux) or brew services list (macOS)",
    host, port,
)
```

**Error Types**:
```go
var (
    ErrDatabaseUnavailable = errors.New("database unavailable")
    ErrAuthenticationFailed = errors.New("authentication failed")
    ErrConfigNotFound = errors.New("configuration file not found")
)
```

**Alternatives Considered**:
- **logrus**: Third-party, slog is now standard library
- **zap**: High performance but complex API
- **Custom logging**: Reinventing wheel, slog sufficient

---

## 8. Terminal Size Handling and Responsive Layout

### Decision: tea.WindowSizeMsg with Minimum Size Validation

**Rationale**: Terminal size changes require re-rendering. Gracefully handle small terminals with minimum size warnings.

**Implementation**:
```go
const (
    MinWidth  = 80
    MinHeight = 24
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height

        if m.width < MinWidth || m.height < MinHeight {
            m.sizeWarning = fmt.Sprintf(
                "Terminal too small: %dx%d (minimum %dx%d)",
                m.width, m.height, MinWidth, MinHeight,
            )
        } else {
            m.sizeWarning = ""
        }

        // Propagate size to child views
        for _, view := range m.views {
            view.SetSize(m.width, m.height)
        }

        return m, nil
    }
    return m, nil
}

func (m Model) View() string {
    if m.sizeWarning != "" {
        return ErrorStyle.Render(m.sizeWarning)
    }
    return m.normalView()
}
```

**Best Practices**:
- **Minimum Size**: 80x24 is standard terminal size
- **Dynamic Calculation**: Compute component sizes based on available space
- **Text Truncation**: Use `lipgloss.Width()` and truncate long strings
- **Scrolling**: Implement viewport scrolling for content exceeding height

---

## Summary

All technical decisions documented with rationale, implementation patterns, and best practices. No unresolved "NEEDS CLARIFICATION" items. Ready to proceed to Phase 1 (Design & Contracts).

**Key Technology Choices**:
1. **Bubbletea**: Elm architecture for predictable state management
2. **pgxpool**: Connection pooling with conservative defaults
3. **Viper**: Hierarchical configuration with multi-source support
4. **password_command**: Secure external password retrieval
5. **Lipgloss**: Centralized theme with semantic styling
6. **Testcontainers**: Real PostgreSQL for integration tests
7. **slog**: Standard library structured logging
8. **Responsive Layout**: WindowSizeMsg handling with minimum size validation

All research artifacts completed. Proceeding to Phase 1.
