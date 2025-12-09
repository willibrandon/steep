// Package direct provides direct PostgreSQL connection for steep-repl CLI.
//
// This package enables CLI commands to communicate directly with PostgreSQL
// using the steep_repl extension, without requiring a daemon process.
// It supports connection via connection string (-c flag) or environment
// variables (PGHOST, PGPORT, PGDATABASE, PGUSER, PGPASSWORD).
//
// T012: Create internal/repl/direct/client.go for PostgreSQL direct connection
package direct

import (
	"bytes"
	"context"
	"encoding/json"
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
)

// MinPostgreSQLVersion is the minimum required PostgreSQL version.
const MinPostgreSQLVersion = 180000 // 18.0.0

// Client provides direct PostgreSQL connectivity for steep-repl CLI.
// It can be created from a connection string or environment variables.
type Client struct {
	pool *pgxpool.Pool

	// Connection state
	mu         sync.RWMutex
	connected  bool
	version    int
	versionStr string
	lastError  error

	// Extension state
	extInstalled   bool
	extVersion     string
	bgworkerActive bool
}

// ClientOptions configures the direct client connection.
type ClientOptions struct {
	// ConnString is a PostgreSQL connection string (DSN or URL format).
	// If provided, it takes precedence over individual parameters.
	ConnString string

	// Individual connection parameters (used if ConnString is empty)
	Host     string
	Port     int
	Database string
	User     string
	Password string
	SSLMode  string

	// PasswordCommand executes to get the password (takes precedence over Password)
	PasswordCommand string

	// ApplicationName sets the application_name connection parameter.
	ApplicationName string

	// MaxRetries is the number of connection retry attempts (default: 5)
	MaxRetries int

	// RetryDelay is the base delay between retries (default: 1s)
	RetryDelay time.Duration
}

// NewClient creates a new direct PostgreSQL client.
func NewClient(ctx context.Context, opts ClientOptions) (*Client, error) {
	c := &Client{}

	// Set defaults
	if opts.MaxRetries == 0 {
		opts.MaxRetries = 5
	}
	if opts.RetryDelay == 0 {
		opts.RetryDelay = time.Second
	}
	if opts.ApplicationName == "" {
		opts.ApplicationName = "steep-repl-direct"
	}

	// Connect with retry
	if err := c.connectWithRetry(ctx, opts); err != nil {
		return nil, err
	}

	// Check extension status
	if err := c.checkExtension(ctx); err != nil {
		c.Close()
		return nil, err
	}

	return c, nil
}

// NewClientFromConnString creates a client from a connection string.
func NewClientFromConnString(ctx context.Context, connString string) (*Client, error) {
	return NewClient(ctx, ClientOptions{
		ConnString: connString,
	})
}

// NewClientFromEnv creates a client from environment variables.
// Supports PGHOST, PGPORT, PGDATABASE, PGUSER, PGPASSWORD, PGSSLMODE.
func NewClientFromEnv(ctx context.Context) (*Client, error) {
	port := 5432
	if portStr := os.Getenv("PGPORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	return NewClient(ctx, ClientOptions{
		Host:     getEnvOrDefault("PGHOST", "localhost"),
		Port:     port,
		Database: getEnvOrDefault("PGDATABASE", "postgres"),
		User:     getEnvOrDefault("PGUSER", "postgres"),
		Password: os.Getenv("PGPASSWORD"),
		SSLMode:  getEnvOrDefault("PGSSLMODE", "prefer"),
	})
}

// connectWithRetry attempts to connect with exponential backoff.
func (c *Client) connectWithRetry(ctx context.Context, opts ClientOptions) error {
	maxDelay := 30 * time.Second
	var lastErr error

	for attempt := 0; attempt < opts.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(math.Pow(2, float64(attempt-1))) * opts.RetryDelay
			if delay > maxDelay {
				delay = maxDelay
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := c.connect(ctx, opts)
		if err == nil {
			return nil
		}

		lastErr = err

		if !isRetryableError(err) {
			return err
		}
	}

	return fmt.Errorf("failed to connect after %d attempts: %w", opts.MaxRetries, lastErr)
}

// connect establishes a connection to PostgreSQL.
func (c *Client) connect(ctx context.Context, opts ClientOptions) error {
	connString, err := buildConnectionString(opts)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return fmt.Errorf("failed to parse connection config: %w", err)
	}

	// Configure pool settings for CLI usage (single command, short-lived)
	poolConfig.MaxConns = 5
	poolConfig.MinConns = 1
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 5 * time.Minute
	poolConfig.HealthCheckPeriod = 30 * time.Second
	poolConfig.ConnConfig.RuntimeParams["application_name"] = opts.ApplicationName

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Validate connection and check version
	version, versionStr, err := validateConnection(ctx, pool)
	if err != nil {
		pool.Close()
		return err
	}

	c.mu.Lock()
	c.pool = pool
	c.connected = true
	c.version = version
	c.versionStr = versionStr
	c.lastError = nil
	c.mu.Unlock()

	return nil
}

// buildConnectionString builds a connection string from options.
func buildConnectionString(opts ClientOptions) (string, error) {
	// Use provided connection string if available
	if opts.ConnString != "" {
		return opts.ConnString, nil
	}

	// Get password
	password := opts.Password
	if opts.PasswordCommand != "" {
		var err error
		password, err = executePasswordCommand(opts.PasswordCommand)
		if err != nil {
			return "", err
		}
	}

	// Build connection string from individual parameters
	host := getEnvOrDefault("PGHOST", opts.Host)
	if host == "" {
		host = "localhost"
	}

	port := opts.Port
	if port == 0 {
		if portStr := os.Getenv("PGPORT"); portStr != "" {
			if p, err := strconv.Atoi(portStr); err == nil {
				port = p
			}
		}
		if port == 0 {
			port = 5432
		}
	}

	database := getEnvOrDefault("PGDATABASE", opts.Database)
	if database == "" {
		database = "postgres"
	}

	user := getEnvOrDefault("PGUSER", opts.User)
	if user == "" {
		user = "postgres"
	}

	sslmode := getEnvOrDefault("PGSSLMODE", opts.SSLMode)
	if sslmode == "" {
		sslmode = "prefer"
	}

	// Check PGPASSWORD if no password set
	if password == "" {
		password = os.Getenv("PGPASSWORD")
	}

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
func validateConnection(ctx context.Context, pool *pgxpool.Pool) (int, string, error) {
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

	if versionNum < MinPostgreSQLVersion {
		return 0, "", fmt.Errorf(
			"PostgreSQL %s (version %d) is not supported; minimum required version is 18.0 (180000)",
			versionStr, versionNum,
		)
	}

	return versionNum, versionStr, nil
}

// checkExtension checks if the steep_repl extension is installed and functional.
func (c *Client) checkExtension(ctx context.Context) error {
	// Check if extension is installed
	var extVersion string
	err := c.pool.QueryRow(ctx,
		"SELECT extversion FROM pg_extension WHERE extname = 'steep_repl'",
	).Scan(&extVersion)

	if err == pgx.ErrNoRows {
		c.extInstalled = false
		return fmt.Errorf("steep_repl extension is not installed; run 'CREATE EXTENSION steep_repl' as a superuser")
	}
	if err != nil {
		return fmt.Errorf("failed to check extension: %w", err)
	}

	c.extInstalled = true
	c.extVersion = extVersion

	// Check if background worker is available.
	// PostgreSQL sets backend_type to bgw_type (not literal "background worker"),
	// and for our worker bgw_type = bgw_name = "steep_repl_worker".
	var bgworkerAvailable bool
	err = c.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE backend_type LIKE 'steep_repl%')",
	).Scan(&bgworkerAvailable)
	if err != nil {
		// Non-fatal, just log
		bgworkerAvailable = false
	}
	c.bgworkerActive = bgworkerAvailable

	return nil
}

// Pool returns the underlying connection pool.
func (c *Client) Pool() *pgxpool.Pool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pool
}

// QueryRow executes a query and returns a single row.
func (c *Client) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c.mu.RLock()
	pool := c.pool
	c.mu.RUnlock()

	if pool == nil {
		return &errorRow{err: fmt.Errorf("client not connected")}
	}

	return pool.QueryRow(ctx, sql, args...)
}

// Exec executes a query without returning rows.
func (c *Client) Exec(ctx context.Context, sql string, args ...any) error {
	c.mu.RLock()
	pool := c.pool
	c.mu.RUnlock()

	if pool == nil {
		return fmt.Errorf("client not connected")
	}

	_, err := pool.Exec(ctx, sql, args...)
	return err
}

// Query executes a query and returns rows.
func (c *Client) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.mu.RLock()
	pool := c.pool
	c.mu.RUnlock()

	if pool == nil {
		return nil, fmt.Errorf("client not connected")
	}

	return pool.Query(ctx, sql, args...)
}

// Acquire acquires a connection from the pool.
func (c *Client) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	c.mu.RLock()
	pool := c.pool
	c.mu.RUnlock()

	if pool == nil {
		return nil, fmt.Errorf("client not connected")
	}

	return pool.Acquire(ctx)
}

// Close closes the client connection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pool != nil {
		c.pool.Close()
		c.pool = nil
	}
	c.connected = false
}

// IsConnected returns true if the client is connected.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// Version returns the PostgreSQL version number.
func (c *Client) Version() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}

// VersionString returns the PostgreSQL version string.
func (c *Client) VersionString() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.versionStr
}

// ExtensionInstalled returns true if steep_repl extension is installed.
func (c *Client) ExtensionInstalled() bool {
	return c.extInstalled
}

// ExtensionVersion returns the steep_repl extension version.
func (c *Client) ExtensionVersion() string {
	return c.extVersion
}

// BackgroundWorkerActive returns true if the background worker is running.
func (c *Client) BackgroundWorkerActive() bool {
	return c.bgworkerActive
}

// Health calls steep_repl.health() and returns the result.
func (c *Client) Health(ctx context.Context) (*HealthResult, error) {
	var result HealthResult

	err := c.pool.QueryRow(ctx, `
		SELECT status, extension_version, pg_version,
		       background_worker_running, shared_memory_available,
		       active_operations, last_error
		FROM steep_repl.health()
	`).Scan(
		&result.Status,
		&result.ExtensionVersion,
		&result.PGVersion,
		&result.BackgroundWorkerRunning,
		&result.SharedMemoryAvailable,
		&result.ActiveOperations,
		&result.LastError,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get health status: %w", err)
	}

	return &result, nil
}

// HealthResult contains the result of steep_repl.health().
type HealthResult struct {
	Status                  string  `json:"status"`
	ExtensionVersion        string  `json:"extension_version"`
	PGVersion               string  `json:"pg_version"`
	BackgroundWorkerRunning bool    `json:"background_worker_running"`
	SharedMemoryAvailable   bool    `json:"shared_memory_available"`
	ActiveOperations        int     `json:"active_operations"`
	LastError               *string `json:"last_error,omitempty"`
}

// String returns a JSON representation of the health result.
func (h *HealthResult) String() string {
	data, _ := json.MarshalIndent(h, "", "  ")
	return string(data)
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

	var netErr *net.OpError
	if isNetError(err, &netErr) {
		return true
	}

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
