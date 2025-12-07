package repl_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
)

// =============================================================================
// Standalone tests (no container needed)
// =============================================================================

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

// =============================================================================
// Daemon Integration Test Suite - shares a single container across all tests
// =============================================================================

type DaemonIntegrationSuite struct {
	suite.Suite
	ctx        context.Context
	cancel     context.CancelFunc
	container  testcontainers.Container
	pool       *pgxpool.Pool
	connConfig *config.PostgreSQLConfig
	// Per-test daemon (recreated for each test for isolation)
	daemon     *daemon.Daemon
	socketPath string
}

func TestDaemonIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(DaemonIntegrationSuite))
}

func (s *DaemonIntegrationSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)

	const testPassword = "test"
	os.Setenv("PGPASSWORD", testPassword)

	req := testcontainers.ContainerRequest{
		Image:        "ghcr.io/willibrandon/pg18-steep-repl:latest",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": testPassword,
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(s.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	s.Require().NoError(err, "Failed to start PostgreSQL container")
	s.container = container

	host, err := container.Host(s.ctx)
	s.Require().NoError(err)

	port, err := container.MappedPort(s.ctx, "5432")
	s.Require().NoError(err)

	connStr := fmt.Sprintf("postgres://test:%s@%s:%s/testdb?sslmode=disable", testPassword, host, port.Port())
	pool, err := pgxpool.New(s.ctx, connStr)
	s.Require().NoError(err)
	s.pool = pool

	// Create extension
	_, err = pool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension")

	// Store connection config for daemon creation
	connConfig := pool.Config().ConnConfig
	s.connConfig = &config.PostgreSQLConfig{
		Host:     connConfig.Host,
		Port:     int(connConfig.Port),
		Database: connConfig.Database,
		User:     connConfig.User,
	}

	s.T().Log("DaemonIntegrationSuite: Shared container ready")
}

func (s *DaemonIntegrationSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.container != nil {
		_ = s.container.Terminate(context.Background())
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *DaemonIntegrationSuite) SetupTest() {
	// Clean up nodes table before each test
	_, _ = s.pool.Exec(s.ctx, "DELETE FROM steep_repl.nodes")

	// Generate unique socket path for this test
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	s.socketPath = fmt.Sprintf("/tmp/sr-%s.sock", hex.EncodeToString(b))
}

func (s *DaemonIntegrationSuite) TearDownTest() {
	if s.daemon != nil {
		s.daemon.Stop()
		s.daemon = nil
	}
	if s.socketPath != "" {
		os.Remove(s.socketPath)
	}
}

// =============================================================================
// Tests
// =============================================================================

func (s *DaemonIntegrationSuite) TestStartStop() {
	cfg := &config.Config{
		NodeID:     "start-stop-test",
		NodeName:   "Start Stop Test",
		PostgreSQL: *s.connConfig,
		GRPC: config.GRPCConfig{
			Port: 15436,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    s.socketPath,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
	}

	d, err := daemon.New(cfg, true)
	s.Require().NoError(err, "Failed to create daemon")
	s.daemon = d

	// Start daemon
	err = d.Start()
	s.Require().NoError(err, "Failed to start daemon")

	// Give it time to start
	time.Sleep(500 * time.Millisecond)

	// Verify running state
	s.Assert().Equal(daemon.StateRunning, d.State(), "State after start should be running")

	// Check uptime is non-zero
	s.Assert().NotZero(d.Uptime(), "Uptime should be > 0 after start")

	// Stop daemon
	err = d.Stop()
	s.Require().NoError(err, "Failed to stop daemon")

	// Verify stopped state
	s.Assert().Equal(daemon.StateStopped, d.State(), "State after stop should be stopped")
}

func (s *DaemonIntegrationSuite) TestPostgreSQLConnection() {
	cfg := &config.Config{
		NodeID:     "pg-conn-test",
		NodeName:   "PG Connection Test",
		PostgreSQL: *s.connConfig,
		GRPC: config.GRPCConfig{
			Port: 15437,
		},
		IPC: config.IPCConfig{
			Enabled: true,
			Path:    s.socketPath,
		},
		HTTP: config.HTTPConfig{
			Enabled: false,
		},
	}

	d, err := daemon.New(cfg, true)
	s.Require().NoError(err, "Failed to create daemon")
	s.daemon = d

	err = d.Start()
	s.Require().NoError(err, "Failed to start daemon")

	time.Sleep(500 * time.Millisecond)

	// Check PostgreSQL status
	status := d.Status()
	s.Assert().Equal("connected", status.PostgreSQL.Status, "PostgreSQL should be connected")

	// Pool should be available
	s.Assert().NotNil(d.Pool(), "Pool should not be nil after start")
	s.Assert().True(d.Pool().IsConnected(), "Pool should be connected")
}
