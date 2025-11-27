package config_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/db/queries"
)

// setupPostgres creates a PostgreSQL test container.
func setupPostgres(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start PostgreSQL container: %v", err)
	}

	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("Failed to get container port: %v", err)
	}

	connStr := "postgres://test:test@" + host + ":" + port.Port() + "/testdb?sslmode=disable"

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("Failed to create connection pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// TestGetAllParameters_ReturnsData verifies GetAllParameters returns configuration data.
func TestGetAllParameters_ReturnsData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	data, err := queries.GetAllParameters(ctx, pool)
	if err != nil {
		t.Fatalf("GetAllParameters failed: %v", err)
	}

	// PostgreSQL should have at least 200 configuration parameters
	if len(data.Parameters) < 200 {
		t.Errorf("Expected at least 200 parameters, got %d", len(data.Parameters))
	}

	// Should have categories
	if len(data.Categories) == 0 {
		t.Error("Expected at least one category")
	}

	// Verify some well-known parameters exist
	foundSharedBuffers := false
	foundMaxConnections := false

	for _, p := range data.Parameters {
		if p.Name == "shared_buffers" {
			foundSharedBuffers = true
			// shared_buffers should have a unit
			if p.Unit == "" {
				t.Error("shared_buffers should have a unit")
			}
		}
		if p.Name == "max_connections" {
			foundMaxConnections = true
			// max_connections should be an integer
			if p.VarType != "integer" {
				t.Errorf("max_connections vartype = %q, want 'integer'", p.VarType)
			}
		}
	}

	if !foundSharedBuffers {
		t.Error("Expected to find shared_buffers parameter")
	}
	if !foundMaxConnections {
		t.Error("Expected to find max_connections parameter")
	}
}

// TestGetAllParameters_ParameterFields verifies parameter fields are populated.
func TestGetAllParameters_ParameterFields(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	data, err := queries.GetAllParameters(ctx, pool)
	if err != nil {
		t.Fatalf("GetAllParameters failed: %v", err)
	}

	// Check that at least one parameter has all required fields
	for _, p := range data.Parameters {
		if p.Name == "" {
			t.Error("Found parameter with empty name")
		}
		if p.Category == "" {
			t.Errorf("Parameter %q has empty category", p.Name)
		}
		if p.Context == "" {
			t.Errorf("Parameter %q has empty context", p.Name)
		}
		if p.VarType == "" {
			t.Errorf("Parameter %q has empty vartype", p.Name)
		}
		if p.ShortDesc == "" {
			t.Errorf("Parameter %q has empty short_desc", p.Name)
		}
	}
}

// TestGetAllParameters_ModifiedCount verifies modified count is calculated.
func TestGetAllParameters_ModifiedCount(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	data, err := queries.GetAllParameters(ctx, pool)
	if err != nil {
		t.Fatalf("GetAllParameters failed: %v", err)
	}

	// Count modified parameters manually
	manualCount := 0
	for _, p := range data.Parameters {
		if p.IsModified() {
			manualCount++
		}
	}

	if data.ModifiedCount != manualCount {
		t.Errorf("ModifiedCount = %d, manual count = %d", data.ModifiedCount, manualCount)
	}
}

// TestGetAllParameters_CategoriesUnique verifies categories are unique.
func TestGetAllParameters_CategoriesUnique(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)

	data, err := queries.GetAllParameters(ctx, pool)
	if err != nil {
		t.Fatalf("GetAllParameters failed: %v", err)
	}

	seen := make(map[string]bool)
	for _, cat := range data.Categories {
		if seen[cat] {
			t.Errorf("Duplicate category: %q", cat)
		}
		seen[cat] = true
	}
}
