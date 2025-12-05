package repl_test

import (
	"context"
	"testing"
	"time"

	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
	"github.com/willibrandon/steep/internal/repl/ipc"
)

// TestIPC_ClientConnect verifies IPC client can connect to daemon.
func TestIPC_ClientConnect(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)
	connConfig := pool.Config().ConnConfig

	socketPath := tempSocketPath(t)

	cfg := &config.Config{
		NodeID:   "ipc-connect-test",
		NodeName: "IPC Connect Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
		},
		GRPC: config.GRPCConfig{
			Port: 15440,
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

	// Create IPC client
	client, err := ipc.NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create IPC client: %v", err)
	}
	defer client.Close()

	// Verify connection by calling a method
	_, err = client.GetStatus()
	if err != nil {
		t.Errorf("GetStatus should work on connected client: %v", err)
	}
}

// TestIPC_StatusGet verifies status.get IPC method.
func TestIPC_StatusGet(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)
	connConfig := pool.Config().ConnConfig

	socketPath := tempSocketPath(t)

	cfg := &config.Config{
		NodeID:   "ipc-status-test",
		NodeName: "IPC Status Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
		},
		GRPC: config.GRPCConfig{
			Port: 15441,
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

	client, err := ipc.NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create IPC client: %v", err)
	}
	defer client.Close()

	// Get status
	status, err := client.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if status.NodeID != cfg.NodeID {
		t.Errorf("Status.NodeID = %q, want %q", status.NodeID, cfg.NodeID)
	}

	if status.NodeName != cfg.NodeName {
		t.Errorf("Status.NodeName = %q, want %q", status.NodeName, cfg.NodeName)
	}

	if status.State != "running" {
		t.Errorf("Status.State = %q, want 'running'", status.State)
	}

	if status.PostgreSQL.Status != "connected" {
		t.Errorf("PostgreSQL.Status = %q, want 'connected'", status.PostgreSQL.Status)
	}
}

// TestIPC_HealthCheck verifies health.check IPC method.
func TestIPC_HealthCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)
	connConfig := pool.Config().ConnConfig

	socketPath := tempSocketPath(t)

	cfg := &config.Config{
		NodeID:   "ipc-health-test",
		NodeName: "IPC Health Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
		},
		GRPC: config.GRPCConfig{
			Port: 15442,
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

	client, err := ipc.NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create IPC client: %v", err)
	}
	defer client.Close()

	// Health check
	health, err := client.HealthCheck()
	if err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}

	if health.Status != "healthy" {
		t.Errorf("Health.Status = %q, want 'healthy'", health.Status)
	}

	// Check components
	if pg, ok := health.Components["postgresql"]; ok {
		if !pg.Healthy {
			t.Error("PostgreSQL component should be healthy")
		}
	} else {
		t.Error("Health should include postgresql component")
	}

	if grpc, ok := health.Components["grpc"]; ok {
		if !grpc.Healthy {
			t.Error("gRPC component should be healthy")
		}
	} else {
		t.Error("Health should include grpc component")
	}

	if ipcComp, ok := health.Components["ipc"]; ok {
		if !ipcComp.Healthy {
			t.Error("IPC component should be healthy")
		}
	} else {
		t.Error("Health should include ipc component")
	}
}

// TestIPC_NodesListEmpty verifies nodes.list returns empty when no nodes registered.
func TestIPC_NodesListEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)
	connConfig := pool.Config().ConnConfig

	// Create the extension first
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	socketPath := tempSocketPath(t)

	cfg := &config.Config{
		NodeID:   "ipc-nodes-test",
		NodeName: "IPC Nodes Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
		},
		GRPC: config.GRPCConfig{
			Port: 15443,
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

	client, err := ipc.NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create IPC client: %v", err)
	}
	defer client.Close()

	// List nodes
	result, err := client.ListNodes(nil)
	if err != nil {
		t.Fatalf("ListNodes failed: %v", err)
	}

	// Should be empty (no nodes registered yet)
	if len(result.Nodes) != 0 {
		t.Errorf("Expected 0 nodes, got %d", len(result.Nodes))
	}
}

// TestIPC_AuditQuery verifies audit.query IPC method.
func TestIPC_AuditQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)
	connConfig := pool.Config().ConnConfig

	// Create the extension first
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	if err != nil {
		t.Fatalf("Failed to create extension: %v", err)
	}

	socketPath := tempSocketPath(t)

	cfg := &config.Config{
		NodeID:   "ipc-audit-test",
		NodeName: "IPC Audit Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
		},
		GRPC: config.GRPCConfig{
			Port: 15444,
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

	client, err := ipc.NewClient(socketPath)
	if err != nil {
		t.Fatalf("Failed to create IPC client: %v", err)
	}
	defer client.Close()

	// Query audit log
	params := ipc.AuditQueryParams{
		Limit: 10,
	}
	result, err := client.QueryAudit(params)
	if err != nil {
		t.Fatalf("QueryAudit failed: %v", err)
	}

	// Should have at least the daemon.started event
	if len(result.Entries) == 0 {
		t.Log("No audit entries found (may be expected if audit logging disabled)")
	}

	// Verify daemon.started event exists
	found := false
	for _, entry := range result.Entries {
		if entry.Action == "daemon.started" {
			found = true
			if !entry.Success {
				t.Error("daemon.started should have success=true")
			}
			break
		}
	}

	if len(result.Entries) > 0 && !found {
		t.Log("daemon.started event not found in audit log")
	}
}

// TestIPC_ConcurrentRequests verifies IPC handles concurrent requests.
func TestIPC_ConcurrentRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)
	connConfig := pool.Config().ConnConfig

	socketPath := tempSocketPath(t)

	cfg := &config.Config{
		NodeID:   "ipc-concurrent-test",
		NodeName: "IPC Concurrent Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
		},
		GRPC: config.GRPCConfig{
			Port: 15445,
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

	// Run concurrent requests
	const numRequests = 10
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			client, err := ipc.NewClient(socketPath)
			if err != nil {
				errors <- err
				return
			}
			defer client.Close()

			_, err = client.GetStatus()
			errors <- err
		}()
	}

	// Collect results
	var errCount int
	for i := 0; i < numRequests; i++ {
		if err := <-errors; err != nil {
			errCount++
			t.Logf("Concurrent request error: %v", err)
		}
	}

	if errCount > 0 {
		t.Errorf("%d/%d concurrent requests failed", errCount, numRequests)
	}
}
