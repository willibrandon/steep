package repl_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	replgrpc "github.com/willibrandon/steep/internal/repl/grpc"
	pb "github.com/willibrandon/steep/internal/repl/grpc/proto"
)

// TestReinit_PartialByTableList tests partial reinitialization of specific tables.
// This is T045: Integration test for partial reinit by table list.
//
// Scenario:
// 1. Set up two nodes with data synchronized
// 2. Corrupt data in one table on target
// 3. Run reinit --node target --tables orders
// 4. Verify only that table was resynchronized
func TestReinit_PartialByTableList(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	env := setupTwoNodeEnv(t, ctx)

	// === Step 1: Create test tables on source ===
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE orders (
			id SERIAL PRIMARY KEY,
			customer TEXT NOT NULL,
			total DECIMAL(10,2)
		);
		CREATE TABLE products (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			price DECIMAL(10,2)
		);
		CREATE TABLE customers (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables on source: %v", err)
	}

	// Insert test data
	_, err = env.sourcePool.Exec(ctx, `
		INSERT INTO orders (customer, total) SELECT 'customer_' || i, i * 10.00 FROM generate_series(1, 50) AS i;
		INSERT INTO products (name, price) SELECT 'product_' || i, i * 5.00 FROM generate_series(1, 30) AS i;
		INSERT INTO customers (name, email) SELECT 'cust_' || i, 'cust' || i || '@example.com' FROM generate_series(1, 20) AS i;
	`)
	if err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	// Create publication
	_, err = env.sourcePool.Exec(ctx, `
		CREATE PUBLICATION steep_pub_source_node FOR TABLE orders, products, customers
	`)
	if err != nil {
		t.Fatalf("Failed to create publication: %v", err)
	}

	// === Step 2: Create matching tables on target ===
	_, err = env.targetPool.Exec(ctx, `
		CREATE TABLE orders (id SERIAL PRIMARY KEY, customer TEXT NOT NULL, total DECIMAL(10,2));
		CREATE TABLE products (id SERIAL PRIMARY KEY, name TEXT NOT NULL, price DECIMAL(10,2));
		CREATE TABLE customers (id SERIAL PRIMARY KEY, name TEXT NOT NULL, email TEXT);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables on target: %v", err)
	}

	// === Step 3: Initialize target from source ===
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	if err != nil {
		t.Fatalf("Failed to dial target: %v", err)
	}
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	// Start initialization
	resp, err := initClient.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	if err != nil {
		t.Fatalf("StartInit failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("StartInit error: %s", resp.Error)
	}

	// Wait for initialization to complete
	var initState string
	for range 120 {
		err = env.targetPool.QueryRow(ctx, `
			SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
		`).Scan(&initState)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		if initState == "synchronized" {
			break
		}
		if initState == "failed" {
			t.Fatalf("Init failed")
		}
		time.Sleep(time.Second)
	}
	if initState != "synchronized" {
		t.Fatalf("Init did not complete, state: %s", initState)
	}

	// Verify data was copied
	var ordersCount, productsCount, customersCount int
	env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM orders").Scan(&ordersCount)
	env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM products").Scan(&productsCount)
	env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM customers").Scan(&customersCount)

	if ordersCount != 50 || productsCount != 30 || customersCount != 20 {
		t.Fatalf("Data mismatch after init: orders=%d, products=%d, customers=%d",
			ordersCount, productsCount, customersCount)
	}

	t.Log("Initial sync completed successfully")

	// === Step 4: Corrupt data in orders table on target ===
	// This simulates data divergence that requires reinit
	_, err = env.targetPool.Exec(ctx, `
		UPDATE orders SET total = -999.99 WHERE id <= 10;
		DELETE FROM orders WHERE id > 40;
	`)
	if err != nil {
		t.Fatalf("Failed to corrupt orders data: %v", err)
	}

	// Verify corruption
	var corruptedCount int
	env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM orders WHERE total = -999.99").Scan(&corruptedCount)
	if corruptedCount != 10 {
		t.Fatalf("Corruption check failed: expected 10 corrupted rows, got %d", corruptedCount)
	}

	var ordersAfterCorrupt int
	env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM orders").Scan(&ordersAfterCorrupt)
	t.Logf("Orders after corruption: %d rows (was 50)", ordersAfterCorrupt)

	// === Step 5: Call reinit for just the orders table ===
	reinitResp, err := initClient.StartReinit(ctx, &pb.StartReinitRequest{
		NodeId: "target-node",
		Scope: &pb.ReinitScope{
			Scope: &pb.ReinitScope_Tables{
				Tables: &pb.TableList{
					Tables: []string{"public.orders"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("StartReinit failed: %v", err)
	}
	if !reinitResp.Success {
		t.Fatalf("StartReinit error: %s", reinitResp.Error)
	}

	t.Logf("Reinit started, state: %s", reinitResp.State.String())

	// === Step 6: Verify ONLY the orders table was affected ===
	// We specified only public.orders, so TablesAffected should be exactly 1
	if reinitResp.TablesAffected != 1 {
		t.Fatalf("Expected exactly 1 table affected (orders), got %d", reinitResp.TablesAffected)
	}
	t.Logf("Tables affected: %d (expected 1)", reinitResp.TablesAffected)

	// Verify orders table was truncated (should be empty now)
	var ordersAfterReinit int
	err = env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM orders").Scan(&ordersAfterReinit)
	if err != nil {
		t.Fatalf("Failed to count orders: %v", err)
	}
	if ordersAfterReinit != 0 {
		t.Fatalf("orders table should be empty after reinit, got %d rows", ordersAfterReinit)
	}
	t.Log("Verified: orders table was truncated (0 rows)")

	// Verify state transition
	err = env.targetPool.QueryRow(ctx, `
		SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
	`).Scan(&initState)
	if err != nil {
		t.Fatalf("Failed to get state: %v", err)
	}
	// For partial reinit, state should be catching_up (waiting for resync)
	if initState != "catching_up" && initState != "reinitializing" {
		t.Errorf("Expected state 'catching_up' or 'reinitializing', got %q", initState)
	}
	t.Logf("State after reinit: %s", initState)

	// === Step 7: Verify products and customers tables were NOT affected ===
	// These tables should still have their original data
	var productsAfter, customersAfter int
	env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM products").Scan(&productsAfter)
	env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM customers").Scan(&customersAfter)

	if productsAfter != 30 {
		t.Errorf("products table was affected by partial reinit: got %d rows, want 30", productsAfter)
	}
	if customersAfter != 20 {
		t.Errorf("customers table was affected by partial reinit: got %d rows, want 20", customersAfter)
	}

	t.Log("Verified other tables were not affected by partial reinit")
}

// TestReinit_SchemaScope tests reinitialization of all tables in a schema.
func TestReinit_SchemaScope(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	env := setupTwoNodeEnv(t, ctx)

	// Create schema with tables on source
	_, err := env.sourcePool.Exec(ctx, `
		CREATE SCHEMA sales;
		CREATE TABLE sales.orders (id SERIAL PRIMARY KEY, customer TEXT);
		CREATE TABLE sales.invoices (id SERIAL PRIMARY KEY, order_id INTEGER);
		INSERT INTO sales.orders (customer) SELECT 'cust_' || i FROM generate_series(1, 25) AS i;
		INSERT INTO sales.invoices (order_id) SELECT i FROM generate_series(1, 25) AS i;
		CREATE PUBLICATION steep_pub_source_node FOR TABLES IN SCHEMA sales;
	`)
	if err != nil {
		t.Fatalf("Failed to setup source: %v", err)
	}

	// Create matching schema on target
	_, err = env.targetPool.Exec(ctx, `
		CREATE SCHEMA sales;
		CREATE TABLE sales.orders (id SERIAL PRIMARY KEY, customer TEXT);
		CREATE TABLE sales.invoices (id SERIAL PRIMARY KEY, order_id INTEGER);
	`)
	if err != nil {
		t.Fatalf("Failed to setup target: %v", err)
	}

	// Initialize
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	resp, err := initClient.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	if err != nil {
		t.Fatalf("StartInit failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("StartInit error: %s", resp.Error)
	}

	// Wait for sync
	for range 60 {
		var state string
		env.targetPool.QueryRow(ctx, `
			SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
		`).Scan(&state)
		if state == "synchronized" {
			break
		}
		if state == "failed" {
			t.Fatalf("Init failed")
		}
		time.Sleep(time.Second)
	}

	// Verify data
	var ordersCount, invoicesCount int
	env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM sales.orders").Scan(&ordersCount)
	env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM sales.invoices").Scan(&invoicesCount)
	if ordersCount != 25 || invoicesCount != 25 {
		t.Fatalf("Data mismatch: orders=%d invoices=%d", ordersCount, invoicesCount)
	}

	// Corrupt both tables
	_, err = env.targetPool.Exec(ctx, `
		DELETE FROM sales.orders WHERE id > 10;
		DELETE FROM sales.invoices WHERE id > 10;
	`)
	if err != nil {
		t.Fatalf("Failed to corrupt: %v", err)
	}

	// Reinit by schema
	reinitResp, err := initClient.StartReinit(ctx, &pb.StartReinitRequest{
		NodeId: "target-node",
		Scope: &pb.ReinitScope{
			Scope: &pb.ReinitScope_Schema{
				Schema: "sales",
			},
		},
	})
	if err != nil {
		t.Fatalf("StartReinit failed: %v", err)
	}
	if !reinitResp.Success {
		t.Fatalf("StartReinit error: %s", reinitResp.Error)
	}

	t.Logf("Schema reinit started: state=%s tables_affected=%d",
		reinitResp.State.String(), reinitResp.TablesAffected)

	// Verify reinit affected exactly 2 tables (sales.orders and sales.invoices)
	if reinitResp.TablesAffected != 2 {
		t.Fatalf("Expected exactly 2 tables affected (sales.orders, sales.invoices), got %d", reinitResp.TablesAffected)
	}

	// Verify both tables were truncated
	var ordersAfter, invoicesAfter int
	err = env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM sales.orders").Scan(&ordersAfter)
	if err != nil {
		t.Fatalf("Failed to count sales.orders: %v", err)
	}
	err = env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM sales.invoices").Scan(&invoicesAfter)
	if err != nil {
		t.Fatalf("Failed to count sales.invoices: %v", err)
	}

	if ordersAfter != 0 {
		t.Errorf("sales.orders should be empty after schema reinit, got %d rows", ordersAfter)
	}
	if invoicesAfter != 0 {
		t.Errorf("sales.invoices should be empty after schema reinit, got %d rows", invoicesAfter)
	}

	t.Log("Verified: both schema tables were truncated")
}

// TestReinit_FullNode tests complete node reinitialization.
func TestReinit_FullNode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	env := setupTwoNodeEnv(t, ctx)

	// Create test table
	_, err := env.sourcePool.Exec(ctx, `
		CREATE TABLE full_reinit_test (id SERIAL PRIMARY KEY, data TEXT);
		INSERT INTO full_reinit_test (data) SELECT 'row_' || i FROM generate_series(1, 50) AS i;
		CREATE PUBLICATION steep_pub_source_node FOR TABLE full_reinit_test;
	`)
	if err != nil {
		t.Fatalf("Failed to setup source: %v", err)
	}

	_, err = env.targetPool.Exec(ctx, `
		CREATE TABLE full_reinit_test (id SERIAL PRIMARY KEY, data TEXT)
	`)
	if err != nil {
		t.Fatalf("Failed to setup target: %v", err)
	}

	// Initialize
	conn, err := replgrpc.Dial(ctx, fmt.Sprintf("localhost:%d", env.targetGRPCPort), "", "", "")
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	initClient := pb.NewInitServiceClient(conn)

	resp, err := initClient.StartInit(ctx, &pb.StartInitRequest{
		TargetNodeId: "target-node",
		SourceNodeId: "source-node",
		Method:       pb.InitMethod_INIT_METHOD_SNAPSHOT,
		SourceNodeInfo: &pb.SourceNodeInfo{
			Host:     env.sourceHost,
			Port:     int32(env.sourcePort),
			Database: "testdb",
			User:     "test",
		},
	})
	if err != nil {
		t.Fatalf("StartInit failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("StartInit error: %s", resp.Error)
	}

	// Wait for sync
	var initState string
	for range 60 {
		env.targetPool.QueryRow(ctx, `
			SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
		`).Scan(&initState)
		if initState == "synchronized" {
			break
		}
		if initState == "failed" {
			t.Fatalf("Init failed")
		}
		time.Sleep(time.Second)
	}
	if initState != "synchronized" {
		t.Fatalf("Init did not complete: %s", initState)
	}

	// Verify subscription exists
	var subExists bool
	env.targetPool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM pg_subscription WHERE subname LIKE 'steep_sub_%')
	`).Scan(&subExists)
	if !subExists {
		t.Fatal("Subscription should exist after init")
	}

	// Full reinit
	reinitResp, err := initClient.StartReinit(ctx, &pb.StartReinitRequest{
		NodeId: "target-node",
		Scope: &pb.ReinitScope{
			Scope: &pb.ReinitScope_Full{
				Full: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("StartReinit failed: %v", err)
	}
	if !reinitResp.Success {
		t.Fatalf("StartReinit error: %s", reinitResp.Error)
	}

	t.Logf("Full reinit completed: state=%s", reinitResp.State.String())

	// Verify state is UNINITIALIZED
	env.targetPool.QueryRow(ctx, `
		SELECT init_state FROM steep_repl.nodes WHERE node_id = 'target-node'
	`).Scan(&initState)

	if initState != "uninitialized" {
		t.Errorf("Expected state 'uninitialized' after full reinit, got %q", initState)
	}

	// Verify subscription was dropped
	env.targetPool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM pg_subscription WHERE subname LIKE 'steep_sub_%')
	`).Scan(&subExists)
	if subExists {
		t.Error("Subscription should be dropped after full reinit")
	}

	// Verify data was truncated
	var count int
	env.targetPool.QueryRow(ctx, "SELECT COUNT(*) FROM full_reinit_test").Scan(&count)
	if count != 0 {
		t.Errorf("Table should be empty after full reinit, got %d rows", count)
	}

	t.Log("Full reinit verified: subscription dropped, data truncated, state reset")
}
