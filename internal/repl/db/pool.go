package db

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/repl/config"
)

// MinPostgreSQLVersion is the minimum required PostgreSQL version.
const MinPostgreSQLVersion = 180000 // 18.0.0

// Pool wraps pgxpool.Pool with additional features for steep-repl.
type Pool struct {
	pool   *pgxpool.Pool
	config *config.Config

	// Connection state
	mu         sync.RWMutex
	connected  bool
	version    int // PostgreSQL version number (e.g., 180000)
	versionStr string
	lastError  error
	lastCheck  time.Time
}

// PoolStatus holds the current pool status.
type PoolStatus struct {
	Connected  bool      `json:"connected"`
	Version    string    `json:"version,omitempty"`
	VersionNum int       `json:"version_num,omitempty"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Database   string    `json:"database"`
	LastCheck  time.Time `json:"last_check,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// NewPool creates a new Pool with the given configuration.
// It attempts to connect with exponential backoff retry logic.
func NewPool(ctx context.Context, cfg *config.Config) (*Pool, error) {
	p := &Pool{
		config: cfg,
	}

	// Try to connect with retry
	if err := p.connectWithRetry(ctx); err != nil {
		return nil, err
	}

	return p, nil
}

// connectWithRetry attempts to connect with exponential backoff.
func (p *Pool) connectWithRetry(ctx context.Context) error {
	const maxRetries = 5
	const baseDelay = 1 * time.Second
	const maxDelay = 30 * time.Second

	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Calculate delay with exponential backoff
			delay := time.Duration(math.Pow(2, float64(attempt-1))) * baseDelay
			if delay > maxDelay {
				delay = maxDelay
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := p.connect(ctx)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryableError(err) {
			return err
		}
	}

	return fmt.Errorf("failed to connect after %d attempts: %w", maxRetries, lastErr)
}

// connect establishes a connection to PostgreSQL.
func (p *Pool) connect(ctx context.Context) error {
	connString, err := p.buildConnectionString()
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return fmt.Errorf("failed to parse connection config: %w", err)
	}

	// Configure pool settings
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 1
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute
	poolConfig.HealthCheckPeriod = 30 * time.Second
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "steep-repl"

	// Create pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Validate connection and check version
	version, versionStr, err := p.validateConnection(ctx, pool)
	if err != nil {
		pool.Close()
		return err
	}

	p.mu.Lock()
	p.pool = pool
	p.connected = true
	p.version = version
	p.versionStr = versionStr
	p.lastCheck = time.Now()
	p.lastError = nil
	p.mu.Unlock()

	return nil
}

// buildConnectionString builds a PostgreSQL connection string from config.
func (p *Pool) buildConnectionString() (string, error) {
	cfg := p.config.PostgreSQL

	// Get connection parameters with environment variable fallback
	host := getEnvOrDefault("PGHOST", cfg.Host)
	port := getEnvOrDefaultInt("PGPORT", cfg.Port)
	database := getEnvOrDefault("PGDATABASE", cfg.Database)
	user := getEnvOrDefault("PGUSER", cfg.User)
	sslmode := getEnvOrDefault("PGSSLMODE", cfg.SSLMode)

	// Get password
	password, err := p.getPassword()
	if err != nil {
		return "", err
	}

	// Build connection string
	connString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		user,
		password,
		host,
		port,
		database,
		sslmode,
	)

	return connString, nil
}

// getPassword retrieves the password using the configured method.
func (p *Pool) getPassword() (string, error) {
	cfg := p.config.PostgreSQL

	// Priority 1: password_command
	if cfg.PasswordCommand != "" {
		return executePasswordCommand(cfg.PasswordCommand)
	}

	// Priority 2: PGPASSWORD environment variable
	if password, ok := os.LookupEnv("PGPASSWORD"); ok {
		return password, nil
	}

	// Priority 3: Empty password (for trust authentication)
	return "", nil
}

// executePasswordCommand executes the password command with timeout.
func executePasswordCommand(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty password command")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("password command timed out")
		}
		return "", fmt.Errorf("password command failed: %w (stderr: %s)", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// validateConnection validates the connection and checks PostgreSQL version.
func (p *Pool) validateConnection(ctx context.Context, pool *pgxpool.Pool) (int, string, error) {
	// Query server version
	var versionNumStr string
	var versionStr string

	err := pool.QueryRow(ctx, "SHOW server_version_num").Scan(&versionNumStr)
	if err != nil {
		return 0, "", fmt.Errorf("failed to get PostgreSQL version: %w", err)
	}

	versionNum, err := strconv.Atoi(versionNumStr)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse PostgreSQL version number: %w", err)
	}

	err = pool.QueryRow(ctx, "SHOW server_version").Scan(&versionStr)
	if err != nil {
		return 0, "", fmt.Errorf("failed to get PostgreSQL version string: %w", err)
	}

	// Check minimum version
	if versionNum < MinPostgreSQLVersion {
		return 0, "", fmt.Errorf(
			"PostgreSQL %s (version %d) is not supported; minimum required version is 18.0 (180000)",
			versionStr, versionNum,
		)
	}

	return versionNum, versionStr, nil
}

// Pool returns the underlying pgxpool.Pool.
func (p *Pool) Pool() *pgxpool.Pool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pool
}

// Acquire acquires a connection from the pool.
func (p *Pool) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()

	if pool == nil {
		return nil, fmt.Errorf("pool not connected")
	}

	return pool.Acquire(ctx)
}

// QueryRow executes a query and returns a single row.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()

	if pool == nil {
		return &errorRow{err: fmt.Errorf("pool not connected")}
	}

	return pool.QueryRow(ctx, sql, args...)
}

// Exec executes a query without returning rows.
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) error {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()

	if pool == nil {
		return fmt.Errorf("pool not connected")
	}

	_, err := pool.Exec(ctx, sql, args...)
	return err
}

// Status returns the current pool status.
func (p *Pool) Status() PoolStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := PoolStatus{
		Connected: p.connected,
		Host:      p.config.PostgreSQL.Host,
		Port:      p.config.PostgreSQL.Port,
		Database:  p.config.PostgreSQL.Database,
		LastCheck: p.lastCheck,
	}

	if p.connected {
		status.Version = p.versionStr
		status.VersionNum = p.version
	}

	if p.lastError != nil {
		status.Error = p.lastError.Error()
	}

	return status
}

// HealthCheck performs a health check on the connection.
func (p *Pool) HealthCheck(ctx context.Context) error {
	p.mu.RLock()
	pool := p.pool
	p.mu.RUnlock()

	if pool == nil {
		return fmt.Errorf("pool not connected")
	}

	// Simple ping query
	var result int
	err := pool.QueryRow(ctx, "SELECT 1").Scan(&result)

	p.mu.Lock()
	p.lastCheck = time.Now()
	if err != nil {
		p.lastError = err
		p.connected = false
	} else {
		p.lastError = nil
		p.connected = true
	}
	p.mu.Unlock()

	return err
}

// Close closes the connection pool.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pool != nil {
		p.pool.Close()
		p.pool = nil
	}
	p.connected = false
}

// IsConnected returns true if the pool is connected.
func (p *Pool) IsConnected() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.connected
}

// Version returns the PostgreSQL version number.
func (p *Pool) Version() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.version
}

// VersionString returns the PostgreSQL version string.
func (p *Pool) VersionString() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.versionStr
}

// errorRow implements pgx.Row for error cases.
type errorRow struct {
	err error
}

func (r *errorRow) Scan(dest ...any) error {
	return r.err
}

// isRetryableError checks if an error is retryable.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Network errors are retryable
	var netErr *net.OpError
	if ok := isNetError(err, &netErr); ok {
		return true
	}

	// Connection refused is retryable
	errStr := err.Error()
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "i/o timeout") {
		return true
	}

	return false
}

// isNetError checks if err wraps a net.OpError.
func isNetError(err error, target **net.OpError) bool {
	for err != nil {
		if opErr, ok := err.(*net.OpError); ok {
			*target = opErr
			return true
		}
		if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
			err = unwrapper.Unwrap()
		} else {
			break
		}
	}
	return false
}

// getEnvOrDefault returns the environment variable value or a default.
func getEnvOrDefault(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}

// getEnvOrDefaultInt returns the environment variable as int or a default.
func getEnvOrDefaultInt(key string, defaultValue int) int {
	if value, ok := os.LookupEnv(key); ok {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
