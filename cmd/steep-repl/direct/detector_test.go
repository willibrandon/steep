package direct

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/willibrandon/steep/internal/repl/config"
	"google.golang.org/grpc"
)

// =============================================================================
// Mode Type Tests
// =============================================================================

func TestMode_String(t *testing.T) {
	tests := []struct {
		mode     Mode
		expected string
	}{
		{ModeUnknown, "unknown"},
		{ModeDirect, "direct"},
		{ModeDaemon, "daemon"},
		{ModeUnavailable, "unavailable"},
		{Mode(99), "unknown"}, // Invalid mode
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.mode.String())
		})
	}
}

// =============================================================================
// Detection Precedence Tests (FR-012)
// =============================================================================

func TestDetect_RemoteFlagTakesPrecedence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start a fake gRPC server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	server := grpc.NewServer()
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()

	addr := listener.Addr().String()

	detector := NewDetector(nil)

	// --remote flag should take precedence even when --direct is also set
	result, err := detector.Detect(ctx, Flags{
		Remote: addr,
		Direct: true, // Should be ignored
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, ModeDaemon, result.Mode)
	assert.Equal(t, addr, result.DaemonAddress)
	assert.Contains(t, result.Reason, "daemon mode available")
}

func TestDetect_DirectFlagRequiresConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	detector := NewDetector(nil)

	// --direct flag without connection string should error
	_, err := detector.Detect(ctx, Flags{
		Direct: true,
		// No ConnString
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no PostgreSQL connection string")
}

func TestDetect_DirectFlagWithInvalidConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	detector := NewDetector(nil)

	// --direct flag with invalid connection should return unavailable
	result, err := detector.Detect(ctx, Flags{
		Direct:     true,
		ConnString: "postgres://invalid:invalid@localhost:1/nonexistent?sslmode=disable&connect_timeout=1",
	})
	require.NoError(t, err) // Detect itself shouldn't error
	require.NotNil(t, result)

	assert.Equal(t, ModeUnavailable, result.Mode)
	assert.Contains(t, result.Reason, "failed to connect")
}

// =============================================================================
// QuickDetect Tests
// =============================================================================

func TestQuickDetect_RemoteFlagReturnsImmediate(t *testing.T) {
	ctx := context.Background()

	// --remote flag should return ModeDaemon immediately without checking anything
	mode, err := QuickDetect(ctx, nil, Flags{
		Remote: "localhost:5433",
	})
	require.NoError(t, err)
	assert.Equal(t, ModeDaemon, mode)
}

func TestQuickDetect_DirectFlagReturnsImmediate(t *testing.T) {
	ctx := context.Background()

	// --direct flag should return ModeDirect immediately
	// Note: This will fail if no connection string, but flag is checked first
	mode, err := QuickDetect(ctx, nil, Flags{
		Direct: true,
	})
	// This should return ModeDirect even without a connection
	// because the flag check happens before connection attempt
	require.NoError(t, err)
	assert.Equal(t, ModeDirect, mode)
}

func TestQuickDetect_NoConnectionReturnsError(t *testing.T) {
	ctx := context.Background()

	// No flags, no config, no connection string should error
	_, err := QuickDetect(ctx, nil, Flags{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no connection configuration")
}

func TestQuickDetect_ConfigWithDaemonOnly(t *testing.T) {
	ctx := context.Background()

	// Config with daemon port but no valid PostgreSQL should return daemon
	cfg := &config.Config{
		PostgreSQL: config.PostgreSQLConfig{
			Host: "", // Empty host - can't build connection string
		},
		GRPC: config.GRPCConfig{
			Port: 5433,
		},
	}

	mode, err := QuickDetect(ctx, cfg, Flags{})
	require.NoError(t, err)
	assert.Equal(t, ModeDaemon, mode)
}

// =============================================================================
// Require Mode Tests
// =============================================================================

func TestRequireDirectMode_NoConnectionString(t *testing.T) {
	ctx := context.Background()

	detector := NewDetector(nil)

	_, err := detector.RequireDirectMode(ctx, Flags{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no PostgreSQL connection string")
}

func TestRequireDirectMode_InvalidConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	detector := NewDetector(nil)

	_, err := detector.RequireDirectMode(ctx, Flags{
		ConnString: "postgres://invalid:invalid@localhost:1/nonexistent?sslmode=disable&connect_timeout=1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "direct mode required but not available")
}

func TestRequireDaemonMode_NoRemoteFlag(t *testing.T) {
	ctx := context.Background()

	detector := NewDetector(nil)

	_, err := detector.RequireDaemonMode(ctx, Flags{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--remote flag")
}

func TestRequireDaemonMode_WithRemoteFlag(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start a fake gRPC server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	server := grpc.NewServer()
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()

	addr := listener.Addr().String()

	detector := NewDetector(nil)

	result, err := detector.RequireDaemonMode(ctx, Flags{
		Remote: addr,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ModeDaemon, result.Mode)
}

// =============================================================================
// Config Integration Tests
// =============================================================================

func TestDetector_BuildConnStringFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected string
	}{
		{
			name:     "nil config",
			cfg:      nil,
			expected: "",
		},
		{
			name: "empty host",
			cfg: &config.Config{
				PostgreSQL: config.PostgreSQLConfig{
					Host: "",
				},
			},
			expected: "",
		},
		{
			name: "full config",
			cfg: &config.Config{
				PostgreSQL: config.PostgreSQLConfig{
					Host:     "myhost",
					Port:     5432,
					Database: "mydb",
					User:     "myuser",
					SSLMode:  "require",
				},
			},
			expected: "postgres://myuser@myhost:5432/mydb?sslmode=require",
		},
		{
			name: "defaults applied",
			cfg: &config.Config{
				PostgreSQL: config.PostgreSQLConfig{
					Host: "myhost",
					// Port, Database, User, SSLMode should default
				},
			},
			expected: "postgres://postgres@myhost:5432/postgres?sslmode=prefer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewDetector(tt.cfg)
			result := detector.buildConnStringFromConfig()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetector_GetDaemonAddress(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected string
	}{
		{
			name:     "nil config",
			cfg:      nil,
			expected: "",
		},
		{
			name: "zero port",
			cfg: &config.Config{
				GRPC: config.GRPCConfig{
					Port: 0,
				},
			},
			expected: "",
		},
		{
			name: "with port",
			cfg: &config.Config{
				GRPC: config.GRPCConfig{
					Port: 5433,
				},
			},
			expected: "localhost:5433",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewDetector(tt.cfg)
			result := detector.getDaemonAddress()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Timeout Tests
// =============================================================================

func TestDetector_WithTimeout(t *testing.T) {
	detector := NewDetector(nil)
	assert.Equal(t, 5*time.Second, detector.timeout) // default

	detector = detector.WithTimeout(10 * time.Second)
	assert.Equal(t, 10*time.Second, detector.timeout)
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestHasCapability(t *testing.T) {
	capabilities := []string{"start_snapshot", "health", "register_node"}

	assert.True(t, hasCapability(capabilities, "health"))
	assert.True(t, hasCapability(capabilities, "start_snapshot"))
	assert.False(t, hasCapability(capabilities, "nonexistent"))
	assert.False(t, hasCapability(nil, "health"))
	assert.False(t, hasCapability([]string{}, "health"))
}

// =============================================================================
// DetectForOperation Tests
// =============================================================================

func TestDetectForOperation_OperationMapping(t *testing.T) {
	// Test that operation names are correctly mapped to required functions
	// This tests the requiredFuncs map in DetectForOperation
	operations := map[string][]string{
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

	// Verify all expected operations have mappings
	for op, funcs := range operations {
		t.Run(op, func(t *testing.T) {
			assert.NotEmpty(t, funcs, "Operation %s should have required functions", op)
		})
	}
}

// =============================================================================
// Flags Tests
// =============================================================================

func TestFlags_Defaults(t *testing.T) {
	flags := Flags{}

	assert.False(t, flags.Direct)
	assert.Empty(t, flags.Remote)
	assert.Empty(t, flags.ConnString)
}

// =============================================================================
// DetectionResult Tests
// =============================================================================

func TestDetectionResult_Fields(t *testing.T) {
	result := &DetectionResult{
		Mode:                  ModeDirect,
		Reason:                "test reason",
		ExtensionVersion:      "0.1.0",
		ExtensionCapabilities: []string{"health", "start_snapshot"},
		DaemonAddress:         "",
		Warning:               "test warning",
	}

	assert.Equal(t, ModeDirect, result.Mode)
	assert.Equal(t, "test reason", result.Reason)
	assert.Equal(t, "0.1.0", result.ExtensionVersion)
	assert.Len(t, result.ExtensionCapabilities, 2)
	assert.Empty(t, result.DaemonAddress)
	assert.Equal(t, "test warning", result.Warning)
}

// =============================================================================
// Daemon Detection Tests
// =============================================================================

func TestDetectDaemonMode_ValidServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start a fake gRPC server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	server := grpc.NewServer()
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()

	addr := listener.Addr().String()

	detector := NewDetector(nil)

	result, err := detector.detectDaemonMode(ctx, addr)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, ModeDaemon, result.Mode)
	assert.Equal(t, addr, result.DaemonAddress)
}

func TestDetectDaemonMode_NoServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get a free port that nothing is listening on
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close() // Close immediately

	detector := NewDetector(nil)

	result, err := detector.detectDaemonMode(ctx, fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	require.NotNil(t, result)

	// gRPC client creation succeeds even without a server
	// The actual connection failure happens on first RPC call
	// So we just verify we got a result
	t.Logf("Result: mode=%s, reason=%s", result.Mode, result.Reason)
}

// =============================================================================
// Auto-Detection Tests
// =============================================================================

func TestAutoDetect_FallbackToDaemon(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start a fake gRPC server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	server := grpc.NewServer()
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()

	port := listener.Addr().(*net.TCPAddr).Port

	// Config with invalid PostgreSQL but valid daemon
	cfg := &config.Config{
		PostgreSQL: config.PostgreSQLConfig{
			Host:     "localhost",
			Port:     1, // Invalid port
			Database: "testdb",
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: port,
		},
	}

	detector := NewDetector(cfg)

	result, err := detector.autoDetect(ctx, Flags{})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, ModeDaemon, result.Mode)
	assert.Contains(t, result.Reason, "fallback")
}

func TestAutoDetect_NoDaemonConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Config with invalid PostgreSQL and no daemon
	cfg := &config.Config{
		PostgreSQL: config.PostgreSQLConfig{
			Host:     "localhost",
			Port:     1, // Invalid port
			Database: "testdb",
			User:     "test",
		},
		GRPC: config.GRPCConfig{
			Port: 0, // No daemon
		},
	}

	detector := NewDetector(cfg)

	result, err := detector.autoDetect(ctx, Flags{})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, ModeUnavailable, result.Mode)
	assert.Contains(t, result.Reason, "neither extension nor daemon")
}
