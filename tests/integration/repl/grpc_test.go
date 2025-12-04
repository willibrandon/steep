package repl_test

import (
	"context"
	"testing"
	"time"

	"github.com/willibrandon/steep/internal/repl/config"
	"github.com/willibrandon/steep/internal/repl/daemon"
	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
)

// TestGRPC_ServerStart verifies gRPC server starts and listens.
func TestGRPC_ServerStart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)
	connConfig := pool.Config().ConnConfig

	socketPath := tempSocketPath(t)

	cfg := &config.Config{
		NodeID:   "grpc-server-test",
		NodeName: "gRPC Server Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
			},
		GRPC: config.GRPCConfig{
			Port: 15450,
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

	// Verify gRPC server is running
	status := d.Status()
	if status.GRPC.Status != "listening" {
		t.Errorf("gRPC status = %q, want 'listening'", status.GRPC.Status)
	}

	if status.GRPC.Port != cfg.GRPC.Port {
		t.Errorf("gRPC port = %d, want %d", status.GRPC.Port, cfg.GRPC.Port)
	}
}

// TestGRPC_HealthCheck verifies gRPC health check RPC.
func TestGRPC_HealthCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)
	connConfig := pool.Config().ConnConfig

	socketPath := tempSocketPath(t)

	grpcPort := 15451
	cfg := &config.Config{
		NodeID:   "grpc-health-test",
		NodeName: "gRPC Health Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
			},
		GRPC: config.GRPCConfig{
			Port: grpcPort,
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

	// Connect to gRPC server
	conn, err := replgrpc.Dial(ctx, "localhost:15451", "", "", "")
	if err != nil {
		t.Fatalf("Failed to dial gRPC server: %v", err)
	}
	defer conn.Close()

	client := pb.NewCoordinatorClient(conn)

	// Health check
	resp, err := client.HealthCheck(ctx, &pb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}

	if resp.Status != pb.HealthCheckResponse_SERVING {
		t.Errorf("Status = %v, want SERVING", resp.Status)
	}

	if resp.NodeId != cfg.NodeID {
		t.Errorf("NodeId = %q, want %q", resp.NodeId, cfg.NodeID)
	}

	if resp.NodeName != cfg.NodeName {
		t.Errorf("NodeName = %q, want %q", resp.NodeName, cfg.NodeName)
	}

	// Check PostgreSQL component
	pgComp, ok := resp.Components["postgresql"]
	if !ok {
		t.Error("Response should include postgresql component")
	} else if !pgComp.Healthy {
		t.Error("PostgreSQL component should be healthy")
	}
}

// TestGRPC_RegisterNode verifies node registration RPC.
func TestGRPC_RegisterNode(t *testing.T) {
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

	grpcPort := 15452
	cfg := &config.Config{
		NodeID:   "grpc-register-test",
		NodeName: "gRPC Register Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
			},
		GRPC: config.GRPCConfig{
			Port: grpcPort,
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

	// Connect to gRPC server
	conn, err := replgrpc.Dial(ctx, "localhost:15452", "", "", "")
	if err != nil {
		t.Fatalf("Failed to dial gRPC server: %v", err)
	}
	defer conn.Close()

	client := pb.NewCoordinatorClient(conn)

	// Register a node
	resp, err := client.RegisterNode(ctx, &pb.RegisterNodeRequest{
		NodeId:   "test-node-1",
		NodeName: "Test Node 1",
		Host:     "192.168.1.100",
		Port:     5432,
		Priority: 50,
	})
	if err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}

	if !resp.Success {
		t.Errorf("RegisterNode should succeed, got error: %s", resp.Error)
	}

	// Verify node was registered by getting nodes
	nodesResp, err := client.GetNodes(ctx, &pb.GetNodesRequest{})
	if err != nil {
		t.Fatalf("GetNodes failed: %v", err)
	}

	found := false
	for _, n := range nodesResp.Nodes {
		if n.NodeId == "test-node-1" {
			found = true
			if n.NodeName != "Test Node 1" {
				t.Errorf("NodeName = %q, want %q", n.NodeName, "Test Node 1")
			}
			if n.Host != "192.168.1.100" {
				t.Errorf("Host = %q, want %q", n.Host, "192.168.1.100")
			}
			if n.Port != 5432 {
				t.Errorf("Port = %d, want %d", n.Port, 5432)
			}
			if n.Priority != 50 {
				t.Errorf("Priority = %d, want %d", n.Priority, 50)
			}
		}
	}

	if !found {
		t.Error("Registered node not found in GetNodes response")
	}
}

// TestGRPC_GetNodes verifies get nodes RPC.
func TestGRPC_GetNodes(t *testing.T) {
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

	grpcPort := 15453
	cfg := &config.Config{
		NodeID:   "grpc-getnodes-test",
		NodeName: "gRPC GetNodes Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
			},
		GRPC: config.GRPCConfig{
			Port: grpcPort,
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

	// Connect to gRPC server
	conn, err := replgrpc.Dial(ctx, "localhost:15453", "", "", "")
	if err != nil {
		t.Fatalf("Failed to dial gRPC server: %v", err)
	}
	defer conn.Close()

	client := pb.NewCoordinatorClient(conn)

	// Initially no nodes
	resp, err := client.GetNodes(ctx, &pb.GetNodesRequest{})
	if err != nil {
		t.Fatalf("GetNodes failed: %v", err)
	}

	if len(resp.Nodes) != 0 {
		t.Errorf("Expected 0 nodes initially, got %d", len(resp.Nodes))
	}

	// Register multiple nodes
	for i, node := range []struct {
		id       string
		name     string
		priority int32
	}{
		{"node-a", "Node A", 100},
		{"node-b", "Node B", 50},
		{"node-c", "Node C", 75},
	} {
		_, err := client.RegisterNode(ctx, &pb.RegisterNodeRequest{
			NodeId:   node.id,
			NodeName: node.name,
			Host:     "192.168.1." + string(rune('1'+i)),
			Port:     5432,
			Priority: node.priority,
		})
		if err != nil {
			t.Fatalf("Failed to register node %s: %v", node.id, err)
		}
	}

	// Get all nodes
	resp, err = client.GetNodes(ctx, &pb.GetNodesRequest{})
	if err != nil {
		t.Fatalf("GetNodes failed: %v", err)
	}

	if len(resp.Nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(resp.Nodes))
	}

	// Verify nodes are ordered by priority DESC
	if len(resp.Nodes) >= 2 {
		if resp.Nodes[0].Priority < resp.Nodes[1].Priority {
			t.Error("Nodes should be ordered by priority descending")
		}
	}
}

// TestGRPC_Heartbeat verifies heartbeat RPC.
func TestGRPC_Heartbeat(t *testing.T) {
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

	grpcPort := 15454
	cfg := &config.Config{
		NodeID:   "grpc-heartbeat-test",
		NodeName: "gRPC Heartbeat Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
			},
		GRPC: config.GRPCConfig{
			Port: grpcPort,
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

	// Connect to gRPC server
	conn, err := replgrpc.Dial(ctx, "localhost:15454", "", "", "")
	if err != nil {
		t.Fatalf("Failed to dial gRPC server: %v", err)
	}
	defer conn.Close()

	client := pb.NewCoordinatorClient(conn)

	// First register a node
	_, err = client.RegisterNode(ctx, &pb.RegisterNodeRequest{
		NodeId:   "heartbeat-node",
		NodeName: "Heartbeat Node",
		Host:     "192.168.1.50",
		Port:     5432,
		Priority: 50,
	})
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	// Send heartbeat
	resp, err := client.Heartbeat(ctx, &pb.HeartbeatRequest{
		NodeId: "heartbeat-node",
	})
	if err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}

	if !resp.Acknowledged {
		t.Error("Heartbeat should be acknowledged")
	}
}

// TestGRPC_MultipleClients verifies multiple clients can connect.
func TestGRPC_MultipleClients(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgresWithExtension(t, ctx)
	connConfig := pool.Config().ConnConfig

	socketPath := tempSocketPath(t)

	grpcPort := 15455
	cfg := &config.Config{
		NodeID:   "grpc-multiclient-test",
		NodeName: "gRPC MultiClient Test",
		PostgreSQL: config.PostgreSQLConfig{
			Host:     connConfig.Host,
			Port:     int(connConfig.Port),
			Database: connConfig.Database,
			User:     connConfig.User,
			},
		GRPC: config.GRPCConfig{
			Port: grpcPort,
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

	// Connect multiple clients concurrently
	const numClients = 5
	errors := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		go func() {
			conn, err := replgrpc.Dial(ctx, "localhost:15455", "", "", "")
			if err != nil {
				errors <- err
				return
			}
			defer conn.Close()

			client := pb.NewCoordinatorClient(conn)
			_, err = client.HealthCheck(ctx, &pb.HealthCheckRequest{})
			errors <- err
		}()
	}

	// Collect results
	var errCount int
	for i := 0; i < numClients; i++ {
		if err := <-errors; err != nil {
			errCount++
			t.Logf("Client error: %v", err)
		}
	}

	if errCount > 0 {
		t.Errorf("%d/%d clients failed", errCount, numClients)
	}
}
