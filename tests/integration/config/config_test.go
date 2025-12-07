package config_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/willibrandon/steep/internal/db/queries"
)

// =============================================================================
// Config Test Suite - shares a single container across all tests
// =============================================================================

type ConfigTestSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	container testcontainers.Container
	pool      *pgxpool.Pool
}

func TestConfigSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(ConfigTestSuite))
}

func (s *ConfigTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)

	const testPassword = "test"
	os.Setenv("PGPASSWORD", testPassword)

	req := testcontainers.ContainerRequest{
		Image:        "postgres:18-alpine",
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

	s.T().Log("ConfigTestSuite: Shared container ready")
}

func (s *ConfigTestSuite) TearDownSuite() {
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

// =============================================================================
// Tests
// =============================================================================

func (s *ConfigTestSuite) TestGetAllParameters_ReturnsData() {
	data, err := queries.GetAllParameters(s.ctx, s.pool)
	s.Require().NoError(err, "GetAllParameters failed")

	// PostgreSQL should have at least 200 configuration parameters
	s.Assert().GreaterOrEqual(len(data.Parameters), 200, "Expected at least 200 parameters")

	// Should have categories
	s.Assert().NotEmpty(data.Categories, "Expected at least one category")

	// Verify some well-known parameters exist
	foundSharedBuffers := false
	foundMaxConnections := false

	for _, p := range data.Parameters {
		if p.Name == "shared_buffers" {
			foundSharedBuffers = true
			// shared_buffers should have a unit
			s.Assert().NotEmpty(p.Unit, "shared_buffers should have a unit")
		}
		if p.Name == "max_connections" {
			foundMaxConnections = true
			// max_connections should be an integer
			s.Assert().Equal("integer", p.VarType, "max_connections vartype should be 'integer'")
		}
	}

	s.Assert().True(foundSharedBuffers, "Expected to find shared_buffers parameter")
	s.Assert().True(foundMaxConnections, "Expected to find max_connections parameter")
}

func (s *ConfigTestSuite) TestGetAllParameters_ParameterFields() {
	data, err := queries.GetAllParameters(s.ctx, s.pool)
	s.Require().NoError(err, "GetAllParameters failed")

	// Check that at least one parameter has all required fields
	for _, p := range data.Parameters {
		s.Assert().NotEmpty(p.Name, "Found parameter with empty name")
		s.Assert().NotEmpty(p.Category, "Parameter %q has empty category", p.Name)
		s.Assert().NotEmpty(p.Context, "Parameter %q has empty context", p.Name)
		s.Assert().NotEmpty(p.VarType, "Parameter %q has empty vartype", p.Name)
		s.Assert().NotEmpty(p.ShortDesc, "Parameter %q has empty short_desc", p.Name)
	}
}

func (s *ConfigTestSuite) TestGetAllParameters_ModifiedCount() {
	data, err := queries.GetAllParameters(s.ctx, s.pool)
	s.Require().NoError(err, "GetAllParameters failed")

	// Count modified parameters manually
	manualCount := 0
	for _, p := range data.Parameters {
		if p.IsModified() {
			manualCount++
		}
	}

	s.Assert().Equal(manualCount, data.ModifiedCount, "ModifiedCount should match manual count")
}

func (s *ConfigTestSuite) TestGetAllParameters_CategoriesUnique() {
	data, err := queries.GetAllParameters(s.ctx, s.pool)
	s.Require().NoError(err, "GetAllParameters failed")

	seen := make(map[string]bool)
	for _, cat := range data.Categories {
		s.Assert().False(seen[cat], "Duplicate category: %q", cat)
		seen[cat] = true
	}
}
