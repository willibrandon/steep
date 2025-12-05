package repl_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
)

// TestDaemon_NewDaemon verifies daemon can be created with valid config.
func TestDaemon_NewDaemon(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := &config.Config{
		NodeID:   "test-node",
		NodeName: "Test Node",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "postgres",
			User:     "postgres",
		},
		GRPC: config.GRPCConfig{
			Port: 15433,
		},
		IPC: config.IPCConfig{
			Enabled: true,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
	}

	d, err := daemon.New(cfg, true)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	if d == nil {
		t.Fatal("Daemon should not be nil")
	}

	// Verify initial state
	if d.State() != daemon.StateStopped {
		t.Errorf("Initial state = %v, want %v", d.State(), daemon.StateStopped)
	}
}

// TestDaemon_ConfigValidation verifies config validation works.
func TestDaemon_ConfigValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: &config.Config{
				Enabled:  true,
				NodeID:   "node-1",
				NodeName: "Node 1",
				PostgreSQL: config.PostgreSQLConfig{
					Host:     "localhost",
					Port:     5432,
					Database: "postgres",
					User:     "postgres",
				},
				GRPC: config.GRPCConfig{
					Port: 5433,
				},
			},
			wantErr: false,
		},
		{
			name: "missing node_id",
			cfg: &config.Config{
				Enabled:  true,
				NodeName: "Node 1",
				PostgreSQL: config.PostgreSQLConfig{
					Host:     "localhost",
					Port:     5432,
					Database: "postgres",
					User:     "postgres",
				},
			},
			wantErr: true,
		},
		{
			name: "missing postgresql host",
			cfg: &config.Config{
				Enabled:  true,
				NodeID:   "node-1",
				NodeName: "Node 1",
				PostgreSQL: config.PostgreSQLConfig{
					Port:     5432,
					Database: "postgres",
					User:     "postgres",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestDaemon_Status verifies status reporting works.
func TestDaemon_Status(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := &config.Config{
		NodeID:   "status-test-node",
		NodeName: "Status Test Node",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "postgres",
			User:     "postgres",
		},
		GRPC: config.GRPCConfig{
			Port: 15434,
		},
		IPC: config.IPCConfig{
			Enabled: false,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
	}

	d, err := daemon.New(cfg, true)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	status := d.Status()
	if status == nil {
		t.Fatal("Status should not be nil")
	}

	if status.NodeID != cfg.NodeID {
		t.Errorf("Status.NodeID = %q, want %q", status.NodeID, cfg.NodeID)
	}

	if status.NodeName != cfg.NodeName {
		t.Errorf("Status.NodeName = %q, want %q", status.NodeName, cfg.NodeName)
	}

	if status.State != daemon.StateStopped {
		t.Errorf("Status.State = %v, want %v", status.State, daemon.StateStopped)
	}
}

// TestDaemon_Uptime verifies uptime calculation.
func TestDaemon_Uptime(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := &config.Config{
		NodeID:   "uptime-test-node",
		NodeName: "Uptime Test Node",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "postgres",
			User:     "postgres",
		},
		GRPC: config.GRPCConfig{
			Port: 15435,
		},
	}

	d, err := daemon.New(cfg, true)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	// Before start, uptime should be 0
	if d.Uptime() != 0 {
		t.Errorf("Uptime before start = %v, want 0", d.Uptime())
	}
}

// TestDaemon_PIDFile verifies PID file operations.
func TestDaemon_PIDFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temp directory for PID file
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Write PID file
	err := daemon.WritePIDFile(pidPath)
	if err != nil {
		t.Fatalf("WritePIDFile failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Error("PID file should exist")
	}

	// Read PID file
	readPID, err := daemon.ReadPIDFile(pidPath)
	if err != nil {
		t.Fatalf("ReadPIDFile failed: %v", err)
	}

	pid := os.Getpid()
	if readPID != pid {
		t.Errorf("Read PID = %d, want %d", readPID, pid)
	}

	// Remove PID file
	err = daemon.RemovePIDFile(pidPath)
	if err != nil {
		t.Fatalf("RemovePIDFile failed: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("PID file should be removed")
	}
}

// TestDaemon_StartStop verifies daemon can start and stop with PostgreSQL container.
func TestDaemon_StartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Use the same container setup as extension tests
	pool := setupPostgresWithExtension(t, ctx)

	// Get the connection details from the pool
	connConfig := pool.Config().ConnConfig

	// Create socket path (must be short due to Unix socket path limits)
	socketPath := tempSocketPath(t)

	cfg := &config.Config{
		NodeID:   "start-stop-test",
		NodeName: "Start Stop Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
		},
		GRPC: config.GRPCConfig{
			Port: 15436,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    socketPath,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
	}

	d, err := daemon.New(cfg, true)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	// Start daemon
	err = d.Start()
	if err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}

	// Give it time to start
	time.Sleep(500 * time.Millisecond)

	// Verify running state
	if d.State() != daemon.StateRunning {
		t.Errorf("State after start = %v, want %v", d.State(), daemon.StateRunning)
	}

	// Check uptime is non-zero
	if d.Uptime() == 0 {
		t.Error("Uptime should be > 0 after start")
	}

	// Stop daemon
	err = d.Stop()
	if err != nil {
		t.Fatalf("Failed to stop daemon: %v", err)
	}

	// Verify stopped state
	if d.State() != daemon.StateStopped {
		t.Errorf("State after stop = %v, want %v", d.State(), daemon.StateStopped)
	}
}

// TestDaemon_PostgreSQLConnection verifies daemon connects to PostgreSQL.
func TestDaemon_PostgreSQLConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)
	connConfig := pool.Config().ConnConfig

	socketPath := tempSocketPath(t)

	cfg := &config.Config{
		NodeID:   "pg-conn-test",
		NodeName: "PG Connection Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
		},
		GRPC: config.GRPCConfig{
			Port: 15437,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    socketPath,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
	}

	d, err := daemon.New(cfg, true)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	err = d.Start()
	if err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}
	defer d.Stop()

	time.Sleep(500 * time.Millisecond)

	// Check PostgreSQL status
	status := d.Status()
	if status.PostgreSQL.Status != "connected" {
		t.Errorf("PostgreSQL status = %q, want 'connected'", status.PostgreSQL.Status)
	}

	// Pool should be available
	if d.Pool() == nil {
		t.Error("Pool should not be nil after start")
	}

	if !d.Pool().IsConnected() {
		t.Error("Pool should be connected")
	}
}
