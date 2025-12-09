// Package direct provides direct PostgreSQL execution for steep-repl CLI.
//
// T023: Create cmd/steep-repl/direct/detector.go with auto-detection logic (FR-012)
//
// This file implements auto-detection logic to determine whether to use
// direct mode (PostgreSQL extension) or daemon mode (gRPC) for executing
// operations. The detection follows this precedence:
//
//  1. Explicit --remote flag: Use daemon mode
//  2. Explicit --direct flag: Use direct mode
//  3. Auto-detect: Try direct mode first, fall back to daemon if unavailable
package direct

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/willibrandon/steep/internal/repl/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Mode represents the execution mode for CLI commands.
type Mode int

const (
	// ModeUnknown indicates the mode has not been determined.
	ModeUnknown Mode = iota

	// ModeDirect uses direct PostgreSQL connection via the steep_repl extension.
	// This is the preferred mode when the extension is installed and functional.
	ModeDirect

	// ModeDaemon uses the gRPC daemon for operations.
	// This is the legacy mode used when the extension is not available.
	ModeDaemon

	// ModeUnavailable indicates neither direct nor daemon mode is available.
	ModeUnavailable
)

// String returns a human-readable name for the mode.
func (m Mode) String() string {
	switch m {
	case ModeDirect:
		return "direct"
	case ModeDaemon:
		return "daemon"
	case ModeUnavailable:
		return "unavailable"
	default:
		return "unknown"
	}
}

// Flags contains the command-line flags that affect mode detection.
type Flags struct {
	// Direct is true when --direct flag is specified.
	// Forces direct PostgreSQL connection via extension.
	Direct bool

	// Remote is the daemon address when --remote flag is specified.
	// Forces daemon mode with the specified address.
	Remote string

	// ConnString is the PostgreSQL connection string from -c flag.
	// Used for direct mode connections.
	ConnString string
}

// DetectionResult contains the result of mode detection.
type DetectionResult struct {
	// Mode is the detected execution mode.
	Mode Mode

	// Reason explains why this mode was selected.
	Reason string

	// ExtensionVersion is the steep_repl extension version (if direct mode).
	ExtensionVersion string

	// ExtensionCapabilities lists the available extension capabilities.
	ExtensionCapabilities []string

	// DaemonAddress is the daemon address (if daemon mode).
	DaemonAddress string

	// Warning contains any warnings about the detection.
	Warning string
}

// Detector performs mode detection for CLI commands.
type Detector struct {
	// Configuration for detection
	cfg *config.Config

	// Detection timeout
	timeout time.Duration
}

// NewDetector creates a new mode detector.
func NewDetector(cfg *config.Config) *Detector {
	return &Detector{
		cfg:     cfg,
		timeout: 5 * time.Second,
	}
}

// WithTimeout sets the detection timeout.
func (d *Detector) WithTimeout(timeout time.Duration) *Detector {
	d.timeout = timeout
	return d
}

// Detect determines the execution mode based on flags and available services.
// Detection precedence (FR-012):
//  1. --remote flag: Use daemon mode
//  2. --direct flag: Use direct mode
//  3. Auto-detect: Try direct first, fall back to daemon
func (d *Detector) Detect(ctx context.Context, flags Flags) (*DetectionResult, error) {
	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	// 1. Explicit --remote flag takes highest precedence
	if flags.Remote != "" {
		return d.detectDaemonMode(ctx, flags.Remote)
	}

	// 2. Explicit --direct flag
	if flags.Direct {
		return d.detectDirectMode(ctx, flags.ConnString)
	}

	// 3. Auto-detect: try direct first, fall back to daemon
	return d.autoDetect(ctx, flags)
}

// detectDirectMode checks if direct mode is available.
func (d *Detector) detectDirectMode(ctx context.Context, connString string) (*DetectionResult, error) {
	result := &DetectionResult{
		Mode: ModeUnavailable,
	}

	// Build connection string
	if connString == "" {
		connString = d.buildConnStringFromConfig()
	}

	if connString == "" {
		return nil, fmt.Errorf("no PostgreSQL connection string available; use -c flag or set PGHOST/PGPORT/PGDATABASE/PGUSER environment variables")
	}

	// Try to connect
	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		result.Reason = fmt.Sprintf("failed to connect to PostgreSQL: %v", err)
		return result, nil
	}
	defer conn.Close(ctx)

	// Check if extension is installed
	extResult, err := d.checkExtension(ctx, conn)
	if err != nil {
		result.Reason = fmt.Sprintf("failed to check extension: %v", err)
		return result, nil
	}

	if !extResult.installed {
		result.Reason = "steep_repl extension is not installed"
		return result, nil
	}

	result.Mode = ModeDirect
	result.Reason = "direct mode available via PostgreSQL extension"
	result.ExtensionVersion = extResult.version
	result.ExtensionCapabilities = extResult.capabilities

	// Add warning if background worker is not running
	if !extResult.bgworkerRunning {
		result.Warning = "background worker not running; some operations may not be available"
	}

	return result, nil
}

// detectDaemonMode checks if daemon mode is available.
func (d *Detector) detectDaemonMode(ctx context.Context, address string) (*DetectionResult, error) {
	result := &DetectionResult{
		Mode:          ModeUnavailable,
		DaemonAddress: address,
	}

	// Try to connect to daemon
	conn, err := grpc.NewClient(address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		result.Reason = fmt.Sprintf("failed to create gRPC client: %v", err)
		return result, nil
	}
	defer conn.Close()

	// Check connection state
	state := conn.GetState()
	if state.String() == "TRANSIENT_FAILURE" || state.String() == "SHUTDOWN" {
		result.Reason = fmt.Sprintf("daemon connection state: %s", state)
		return result, nil
	}

	result.Mode = ModeDaemon
	result.Reason = "daemon mode available via gRPC"

	return result, nil
}

// autoDetect tries direct mode first, then falls back to daemon.
func (d *Detector) autoDetect(ctx context.Context, flags Flags) (*DetectionResult, error) {
	// Try direct mode first
	directResult, err := d.detectDirectMode(ctx, flags.ConnString)
	if err != nil {
		return nil, err
	}

	if directResult.Mode == ModeDirect {
		directResult.Reason = "auto-detected: PostgreSQL extension available"
		return directResult, nil
	}

	// Try daemon as fallback
	daemonAddr := d.getDaemonAddress()
	if daemonAddr == "" {
		// No daemon configured
		return &DetectionResult{
			Mode:   ModeUnavailable,
			Reason: fmt.Sprintf("neither extension nor daemon available; direct mode: %s", directResult.Reason),
		}, nil
	}

	daemonResult, err := d.detectDaemonMode(ctx, daemonAddr)
	if err != nil {
		return nil, err
	}

	if daemonResult.Mode == ModeDaemon {
		daemonResult.Reason = fmt.Sprintf("auto-detected: using daemon fallback (extension: %s)", directResult.Reason)
		return daemonResult, nil
	}

	return &DetectionResult{
		Mode:   ModeUnavailable,
		Reason: fmt.Sprintf("neither extension nor daemon available; direct: %s; daemon: %s", directResult.Reason, daemonResult.Reason),
	}, nil
}

// extensionCheckResult contains the result of checking the extension.
type extensionCheckResult struct {
	installed        bool
	version          string
	capabilities     []string
	bgworkerRunning  bool
	sharedMemoryAvailable bool
}

// checkExtension checks if the steep_repl extension is installed and functional.
func (d *Detector) checkExtension(ctx context.Context, conn *pgx.Conn) (*extensionCheckResult, error) {
	result := &extensionCheckResult{}

	// Check if extension is installed
	var extVersion string
	err := conn.QueryRow(ctx,
		"SELECT extversion FROM pg_extension WHERE extname = 'steep_repl'",
	).Scan(&extVersion)

	if err == pgx.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return nil, err
	}

	result.installed = true
	result.version = extVersion

	// Check available SQL functions to determine capabilities
	rows, err := conn.Query(ctx, `
		SELECT proname
		FROM pg_proc
		WHERE pronamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'steep_repl')
		AND proname IN ('start_snapshot', 'snapshot_progress', 'cancel_snapshot',
		                'start_merge', 'merge_progress', 'analyze_overlap',
		                'register_node', 'heartbeat', 'node_status',
		                'health')
		ORDER BY proname
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var capabilities []string
	for rows.Next() {
		var funcName string
		if err := rows.Scan(&funcName); err != nil {
			continue
		}
		capabilities = append(capabilities, funcName)
	}
	result.capabilities = capabilities

	// Check if background worker is running
	// PostgreSQL sets backend_type to bgw_type (e.g., "steep_repl_worker")
	var bgworkerRunning bool
	err = conn.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE backend_type LIKE 'steep_repl%')",
	).Scan(&bgworkerRunning)
	if err == nil {
		result.bgworkerRunning = bgworkerRunning
	}

	// Check if shared memory is available by trying to call health()
	if hasCapability(capabilities, "health") {
		var status string
		var shmemAvailable bool
		err = conn.QueryRow(ctx,
			"SELECT status, shared_memory_available FROM steep_repl.health()",
		).Scan(&status, &shmemAvailable)
		if err == nil {
			result.sharedMemoryAvailable = shmemAvailable
		}
	}

	return result, nil
}

// buildConnStringFromConfig builds a connection string from config.
// Note: The config uses PasswordCommand rather than a direct password field,
// so this function doesn't include password. The internal/repl/direct.Client
// handles password retrieval via password_command or PGPASSWORD env.
func (d *Detector) buildConnStringFromConfig() string {
	if d.cfg == nil {
		return ""
	}

	// Try to build from PostgreSQL config section
	pgCfg := d.cfg.PostgreSQL
	if pgCfg.Host == "" {
		return ""
	}

	port := pgCfg.Port
	if port == 0 {
		port = 5432
	}

	database := pgCfg.Database
	if database == "" {
		database = "postgres"
	}

	user := pgCfg.User
	if user == "" {
		user = "postgres"
	}

	sslmode := pgCfg.SSLMode
	if sslmode == "" {
		sslmode = "prefer"
	}

	// Build connection string without password - let pgx handle PGPASSWORD
	return fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=%s",
		user, pgCfg.Host, port, database, sslmode)
}

// getDaemonAddress returns the daemon address from config.
func (d *Detector) getDaemonAddress() string {
	if d.cfg == nil {
		return ""
	}

	// Check GRPC config - daemon listens on localhost by default
	if d.cfg.GRPC.Port != 0 {
		return fmt.Sprintf("localhost:%d", d.cfg.GRPC.Port)
	}

	return ""
}

// hasCapability checks if a capability is in the list.
func hasCapability(capabilities []string, name string) bool {
	for _, c := range capabilities {
		if c == name {
			return true
		}
	}
	return false
}

// RequireDirectMode returns an error if direct mode is not available.
func (d *Detector) RequireDirectMode(ctx context.Context, flags Flags) (*DetectionResult, error) {
	result, err := d.detectDirectMode(ctx, flags.ConnString)
	if err != nil {
		return nil, err
	}

	if result.Mode != ModeDirect {
		return nil, fmt.Errorf("direct mode required but not available: %s", result.Reason)
	}

	return result, nil
}

// RequireDaemonMode returns an error if daemon mode is not available.
func (d *Detector) RequireDaemonMode(ctx context.Context, flags Flags) (*DetectionResult, error) {
	if flags.Remote == "" {
		return nil, fmt.Errorf("daemon mode requires --remote flag")
	}

	result, err := d.detectDaemonMode(ctx, flags.Remote)
	if err != nil {
		return nil, err
	}

	if result.Mode != ModeDaemon {
		return nil, fmt.Errorf("daemon mode required but not available: %s", result.Reason)
	}

	return result, nil
}

// DetectForOperation performs mode detection for a specific operation.
// Some operations may only be available in certain modes.
func (d *Detector) DetectForOperation(ctx context.Context, flags Flags, operation string) (*DetectionResult, error) {
	result, err := d.Detect(ctx, flags)
	if err != nil {
		return nil, err
	}

	if result.Mode == ModeUnavailable {
		return result, nil
	}

	// Check operation-specific requirements
	if result.Mode == ModeDirect {
		// Map operations to required extension functions
		requiredFuncs := map[string][]string{
			"snapshot_generate": {"start_snapshot", "snapshot_progress"},
			"snapshot_apply":    {"start_snapshot", "snapshot_progress"},
			"merge":             {"start_merge", "merge_progress"},
			"node_register":     {"register_node"},
			"node_heartbeat":    {"heartbeat"},
			"node_status":       {"node_status"},
			"health":            {"health"},
			"schema_capture":    {"capture_fingerprints"},
			"schema_compare":    {"compare_fingerprints"},
		}

		if required, ok := requiredFuncs[operation]; ok {
			for _, fn := range required {
				if !hasCapability(result.ExtensionCapabilities, fn) {
					result.Warning = fmt.Sprintf("operation '%s' requires extension function '%s' which is not available", operation, fn)
				}
			}
		}
	}

	return result, nil
}

// QuickDetect performs a fast mode detection without full capability checking.
// Use this for operations that just need to know which mode to use.
func QuickDetect(ctx context.Context, cfg *config.Config, flags Flags) (Mode, error) {
	// Explicit flags take precedence
	if flags.Remote != "" {
		return ModeDaemon, nil
	}
	if flags.Direct {
		return ModeDirect, nil
	}

	// Quick check for extension
	connString := flags.ConnString
	if connString == "" && cfg != nil && cfg.PostgreSQL.Host != "" {
		detector := NewDetector(cfg)
		connString = detector.buildConnStringFromConfig()
	}

	if connString == "" {
		// No PostgreSQL connection, check for daemon
		if cfg != nil && cfg.GRPC.Port != 0 {
			return ModeDaemon, nil
		}
		return ModeUnavailable, fmt.Errorf("no connection configuration available")
	}

	// Quick extension check
	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		// Can't connect to PostgreSQL, try daemon
		if cfg != nil && cfg.GRPC.Port != 0 {
			return ModeDaemon, nil
		}
		return ModeUnavailable, err
	}
	defer conn.Close(ctx)

	// Check if extension is installed (fast query)
	var exists bool
	err = conn.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'steep_repl')",
	).Scan(&exists)

	if err == nil && exists {
		return ModeDirect, nil
	}

	// Extension not available, try daemon
	if cfg != nil && cfg.GRPC.Port != 0 {
		return ModeDaemon, nil
	}

	return ModeUnavailable, fmt.Errorf("steep_repl extension not installed and no daemon configured")
}
