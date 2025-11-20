# Quickstart Guide: Foundation Infrastructure

**Feature**: 001-foundation | **For**: Developers implementing this feature

## Prerequisites

- Go 1.21 or higher installed
- PostgreSQL 11+ running locally (18 recommended)
- Terminal with 256-color support (xterm-256color or better)
- Minimum terminal size: 80x24

## Development Setup

### 1. Initialize Go Module

```bash
cd /Users/brandon/src/steep
go mod init github.com/yourusername/steep
```

### 2. Install Dependencies

```bash
# Core dependencies
go get github.com/charmbracelet/bubbletea@v0.25
go get github.com/charmbracelet/bubbles@v0.18
go get github.com/charmbracelet/lipgloss@v0.9
go get github.com/jackc/pgx/v5@latest
go get github.com/jackc/pgxpool/v5@latest
go get github.com/spf13/viper@v1.18

# Testing dependencies
go get github.com/stretchr/testify@latest
go get github.com/testcontainers/testcontainers-go@latest
go get github.com/testcontainers/testcontainers-go/modules/postgres@latest
```

### 3. Create Directory Structure

```bash
mkdir -p cmd/steep
mkdir -p internal/{app,config,db/models,ui/{components,views,styles}}
mkdir -p configs
mkdir -p tests/{integration,unit}
```

### 4. Set Up PostgreSQL for Testing

```bash
# macOS (Homebrew)
brew install postgresql@18
brew services start postgresql@18
createdb steep_test

# Linux (Ubuntu/Debian)
sudo apt install postgresql-18
sudo systemctl start postgresql
sudo -u postgres createdb steep_test

# Verify connection
psql -d steep_test -c "SELECT version();"
```

## Implementation Order

Follow this sequence to build the foundation incrementally:

### Phase 1: Configuration (Day 1)

**Goal**: Load configuration from YAML files and environment variables

**Files to Create**:
1. `internal/config/config.go` - Config struct and Viper loading logic
2. `internal/config/defaults.go` - Default configuration values
3. `configs/steep.yaml.example` - Example configuration file
4. `tests/unit/config_test.go` - Configuration loading tests

**Validation**:
```bash
go test ./tests/unit/config_test.go -v
```

**Expected Output**: Config loads from file, environment variables override, defaults applied

---

### Phase 2: Database Connection (Day 2)

**Goal**: Establish PostgreSQL connection with pgxpool

**Files to Create**:
1. `internal/db/connection.go` - pgxpool connection setup
2. `internal/db/password.go` - Password command execution
3. `internal/db/models/profile.go` - Connection profile model
4. `tests/integration/connection_test.go` - Connection tests with testcontainers

**Validation**:
```bash
go test ./tests/integration/connection_test.go -v
```

**Expected Output**: Connection pool created, validation query executes, version retrieved

---

### Phase 3: UI Components (Day 3)

**Goal**: Build reusable Table, StatusBar, and Help components with Lipgloss styling

**Files to Create**:
1. `internal/ui/styles/theme.go` - Centralized Lipgloss styles
2. `internal/ui/components/table.go` - Table component
3. `internal/ui/components/statusbar.go` - Status bar component
4. `internal/ui/components/help.go` - Help text component
5. `internal/ui/keys.go` - Keyboard binding definitions

**Validation**:
```bash
# Manual test: Create standalone component demo
go run examples/table_demo.go
```

**Expected Output**: Components render with consistent styling

---

### Phase 4: Bubbletea Application (Day 4-5)

**Goal**: Integrate components into Bubbletea application with view switching

**Files to Create**:
1. `internal/app/app.go` - Main Bubbletea model and lifecycle
2. `internal/ui/views/dashboard.go` - Placeholder dashboard view
3. `cmd/steep/main.go` - Application entry point

**Validation**:
```bash
go build -o steep cmd/steep/main.go
./steep
```

**Expected Output**: Application launches, connects to database, status bar shows connection state, keyboard navigation works

---

### Phase 5: Error Handling & Reconnection (Day 6)

**Goal**: Graceful error handling and automatic reconnection with exponential backoff

**Files to Create**:
1. `internal/db/reconnection.go` - Reconnection logic
2. `internal/app/errors.go` - Error message formatting

**Validation**:
```bash
# Start steep, then stop PostgreSQL, verify reconnection attempts
./steep
# In another terminal:
brew services stop postgresql
# Wait, observe reconnection attempts in steep
brew services start postgresql
# Verify reconnection succeeds
```

**Expected Output**: App detects disconnection, attempts 5 reconnections with exponential backoff, reconnects when database available

---

### Phase 6: Testing & Polish (Day 7)

**Goal**: Complete test coverage and documentation

**Files to Create**:
1. `tests/unit/password_test.go` - Password command tests
2. `tests/integration/reconnection_test.go` - Reconnection tests
3. `Makefile` - Build and test automation
4. `README.md` - Project documentation

**Validation**:
```bash
make test          # Run all tests
make test-coverage # Generate coverage report
make build         # Build binary
```

**Expected Output**: >80% test coverage, all integration tests pass, binary builds successfully

---

## Key Implementation Notes

### Configuration Loading (internal/config/config.go)

```go
func LoadConfig() (*Config, error) {
    viper.SetConfigName("steep")
    viper.SetConfigType("yaml")
    viper.AddConfigPath("$HOME/.config/steep")
    viper.AddConfigPath(".")

    // Environment variable support
    viper.AutomaticEnv()
    viper.SetEnvPrefix("STEEP")
    viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

    // Defaults
    viper.SetDefault("connection.host", "localhost")
    viper.SetDefault("connection.port", 5432)
    viper.SetDefault("connection.database", "postgres")
    viper.SetDefault("ui.theme", "dark")
    viper.SetDefault("ui.refresh_interval", "1s")

    if err := viper.ReadInConfig(); err != nil {
        if _, ok := err.(viper.ConfigFileNotFoundError); ok {
            return createDefaultConfig()
        }
        return nil, err
    }

    var config Config
    return &config, viper.Unmarshal(&config)
}
```

### Database Connection (internal/db/connection.go)

```go
func NewConnectionPool(cfg *config.ConnectionConfig) (*pgxpool.Pool, error) {
    // Build connection string
    connString := fmt.Sprintf(
        "postgres://%s@%s:%d/%s?sslmode=%s",
        cfg.User, cfg.Host, cfg.Port, cfg.Database, cfg.SSLMode,
    )

    // Parse and configure pool
    poolConfig, err := pgxpool.ParseConfig(connString)
    if err != nil {
        return nil, err
    }

    poolConfig.MaxConns = int32(cfg.PoolMaxConns)
    poolConfig.MinConns = int32(cfg.PoolMinConns)
    poolConfig.MaxConnLifetime = time.Hour
    poolConfig.HealthCheckPeriod = time.Minute

    // Create pool
    pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
    if err != nil {
        return nil, fmt.Errorf(
            "connection refused: ensure PostgreSQL is running on %s:%d",
            cfg.Host, cfg.Port,
        )
    }

    // Validate connection
    var version string
    err = pool.QueryRow(context.Background(), "SELECT version()").Scan(&version)
    if err != nil {
        pool.Close()
        return nil, fmt.Errorf("connection validation failed: %w", err)
    }

    return pool, nil
}
```

### Bubbletea Application (internal/app/app.go)

```go
type Model struct {
    config        *config.Config
    dbPool        *pgxpool.Pool
    currentView   ViewType
    views         map[ViewType]ViewModel
    width, height int
    connected     bool
    statusBarData StatusBarData
    helpVisible   bool
    quitting      bool
}

func (m Model) Init() tea.Cmd {
    return tea.Batch(
        connectToDatabase(m.config),
        tickStatusBar,
    )
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "q", "ctrl+c":
            m.quitting = true
            return m, tea.Quit
        case "h", "?":
            m.helpVisible = !m.helpVisible
            return m, nil
        case "tab":
            m.nextView()
            return m, nil
        }
    case tea.WindowSizeMsg:
        m.width, m.height = msg.Width, msg.Height
        for _, view := range m.views {
            view.SetSize(m.width, m.height)
        }
    case DatabaseConnectedMsg:
        m.connected = true
        m.dbPool = msg.pool
        return m, nil
    case StatusBarTickMsg:
        m.statusBarData = msg.data
        return m, tickStatusBar
    }
    return m, nil
}

func (m Model) View() string {
    if m.quitting {
        return "Goodbye!\n"
    }

    return lipgloss.JoinVertical(
        lipgloss.Left,
        m.headerView(),
        m.currentViewRender(),
        m.statusBarView(),
        m.helpBarView(),
    )
}
```

## Testing Checklist

Before marking Phase 1 complete, verify:

- [ ] Configuration loads from YAML file
- [ ] Environment variables override config values
- [ ] Default config created if file missing
- [ ] Database connection established with pgxpool
- [ ] Password command executes successfully
- [ ] Connection validation query works
- [ ] Status bar displays connection state and timestamp
- [ ] Keyboard shortcuts work (q, h, tab)
- [ ] Help screen displays and dismisses
- [ ] View switching cycles through views
- [ ] Terminal resize handled gracefully
- [ ] Minimum terminal size validated (80x24)
- [ ] Connection loss detected and reconnection attempted
- [ ] Exponential backoff parameters correct (1s initial, 30s max, 5 attempts)
- [ ] Error messages are actionable
- [ ] Debug flag enables info logging
- [ ] Integration tests pass with testcontainers
- [ ] Unit tests pass for all components
- [ ] Test coverage >80%

## Common Issues & Solutions

### Issue: "connection refused" error

**Solution**: Verify PostgreSQL is running
```bash
# macOS
brew services list | grep postgresql
brew services start postgresql@18

# Linux
systemctl status postgresql
sudo systemctl start postgresql
```

### Issue: Config file not found

**Solution**: Application should auto-create default config
```bash
mkdir -p ~/.config/steep
cp configs/steep.yaml.example ~/.config/steep/config.yaml
```

### Issue: "password command failed"

**Solution**: Test password command separately
```bash
# Test command directly
pass show postgres/local

# Verify timeout (5 seconds)
time pass show postgres/local
```

### Issue: Terminal too small warning

**Solution**: Resize terminal to minimum 80x24
```bash
# Check current size
tput cols
tput lines

# Resize if needed (varies by terminal emulator)
```

## Next Steps

After completing foundation implementation:

1. Run `/speckit.tasks` to generate task breakdown
2. Implement tasks in order (tracked in tasks.md)
3. Commit after each major milestone
4. Move to Feature 002 (Dashboard & Activity Monitoring)

---

**Estimated Implementation Time**: 7 days (1 developer)

**Dependencies**: None (foundation feature)

**Blocks**: All future features (002-012) depend on this foundation
