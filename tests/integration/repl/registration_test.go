package repl_test

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
)

// =============================================================================
// Database Registration Test Suite
// Tests for the explicit database registration workflow (database-registration.md)
// =============================================================================

type RegistrationTestSuite struct {
	suite.Suite
	ctx          context.Context
	cancel       context.CancelFunc
	container    testcontainers.Container
	postgresPool *pgxpool.Pool // Connection to postgres database
	testdbPool   *pgxpool.Pool // Connection to testdb database
	host         string
	port         string
}

func TestRegistrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(RegistrationTestSuite))
}

func (s *RegistrationTestSuite) SetupSuite() {
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
	s.host = host

	port, err := container.MappedPort(s.ctx, "5432")
	s.Require().NoError(err)
	s.port = port.Port()

	// Connect to postgres database (where central catalog lives)
	postgresConnStr := fmt.Sprintf("postgres://test:%s@%s:%s/postgres?sslmode=disable", testPassword, host, s.port)
	postgresPool, err := pgxpool.New(s.ctx, postgresConnStr)
	s.Require().NoError(err)
	s.postgresPool = postgresPool

	// Connect to testdb database (for testing register_current_db)
	testdbConnStr := fmt.Sprintf("postgres://test:%s@%s:%s/testdb?sslmode=disable", testPassword, host, s.port)
	testdbPool, err := pgxpool.New(s.ctx, testdbConnStr)
	s.Require().NoError(err)
	s.testdbPool = testdbPool

	s.T().Log("RegistrationTestSuite: Shared container ready")
}

func (s *RegistrationTestSuite) TearDownSuite() {
	if s.postgresPool != nil {
		s.postgresPool.Close()
	}
	if s.testdbPool != nil {
		s.testdbPool.Close()
	}
	if s.container != nil {
		_ = s.container.Terminate(context.Background())
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *RegistrationTestSuite) SetupTest() {
	// Ensure extension is installed in postgres and testdb
	_, err := s.postgresPool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension in postgres")

	_, err = s.testdbPool.Exec(s.ctx, "CREATE EXTENSION IF NOT EXISTS steep_repl")
	s.Require().NoError(err, "Failed to create extension in testdb")

	// Clear the databases catalog for clean tests
	_, _ = s.postgresPool.Exec(s.ctx, "DELETE FROM steep_repl.databases")
}

// =============================================================================
// Schema Tests
// =============================================================================

func (s *RegistrationTestSuite) TestDatabasesTableExists() {
	var exists bool
	err := s.postgresPool.QueryRow(s.ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_tables WHERE schemaname = 'steep_repl' AND tablename = 'databases')",
	).Scan(&exists)
	s.Require().NoError(err)
	s.Assert().True(exists, "steep_repl.databases table should exist in postgres")
}

func (s *RegistrationTestSuite) TestDatabasesTableStructure() {
	expectedColumns := []string{"datname", "registered_at", "enabled", "options"}

	for _, col := range expectedColumns {
		var columnExists bool
		query := `
			SELECT EXISTS(
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'steep_repl'
				  AND table_name = 'databases'
				  AND column_name = $1
			)
		`
		err := s.postgresPool.QueryRow(s.ctx, query, col).Scan(&columnExists)
		s.Require().NoError(err, "Failed to check column databases.%s", col)
		s.Assert().True(columnExists, "Column databases.%s should exist", col)
	}
}

func (s *RegistrationTestSuite) TestDatabasesEnabledIndex() {
	var exists bool
	err := s.postgresPool.QueryRow(s.ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname = 'steep_repl' AND tablename = 'databases' AND indexname = 'databases_enabled_idx')",
	).Scan(&exists)
	s.Require().NoError(err)
	s.Assert().True(exists, "databases_enabled_idx index should exist")
}

// =============================================================================
// register_db() Tests (postgres-only function)
// =============================================================================

func (s *RegistrationTestSuite) TestRegisterDbFromPostgres() {
	// Register a database from postgres
	var result string
	err := s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.register_db('testdb')").Scan(&result)
	s.Require().NoError(err)
	s.Assert().Contains(result, "registered successfully")

	// Verify it was added to the catalog
	var exists bool
	err = s.postgresPool.QueryRow(s.ctx,
		"SELECT EXISTS(SELECT 1 FROM steep_repl.databases WHERE datname = 'testdb' AND enabled = true)",
	).Scan(&exists)
	s.Require().NoError(err)
	s.Assert().True(exists, "testdb should be registered and enabled")
}

func (s *RegistrationTestSuite) TestRegisterDbFromNonPostgresDatabase() {
	// Attempting to call register_db from testdb should fail
	var result string
	err := s.testdbPool.QueryRow(s.ctx, "SELECT steep_repl.register_db('somedb')").Scan(&result)
	s.Require().Error(err, "register_db should fail when called from non-postgres database")
	s.Assert().Contains(err.Error(), "must be called from postgres")
}

func (s *RegistrationTestSuite) TestRegisterDbNonExistentDatabase() {
	// Attempting to register a non-existent database should fail
	var result string
	err := s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.register_db('nonexistent_db_12345')").Scan(&result)
	s.Require().Error(err)
	s.Assert().Contains(err.Error(), "does not exist")
}

func (s *RegistrationTestSuite) TestRegisterDbIdempotent() {
	// Register the same database twice - should succeed (idempotent)
	var result1, result2 string

	err := s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.register_db('testdb')").Scan(&result1)
	s.Require().NoError(err)

	err = s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.register_db('testdb')").Scan(&result2)
	s.Require().NoError(err)

	// Verify only one entry exists
	var count int
	err = s.postgresPool.QueryRow(s.ctx,
		"SELECT count(*) FROM steep_repl.databases WHERE datname = 'testdb'",
	).Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equal(1, count, "Should have exactly one entry for testdb")
}

// =============================================================================
// unregister_db() Tests
// =============================================================================

func (s *RegistrationTestSuite) TestUnregisterDb() {
	// First register, then unregister
	_, err := s.postgresPool.Exec(s.ctx, "SELECT steep_repl.register_db('testdb')")
	s.Require().NoError(err)

	var result string
	err = s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.unregister_db('testdb')").Scan(&result)
	s.Require().NoError(err)
	s.Assert().Contains(result, "unregistered successfully")

	// Verify it was removed
	var exists bool
	err = s.postgresPool.QueryRow(s.ctx,
		"SELECT EXISTS(SELECT 1 FROM steep_repl.databases WHERE datname = 'testdb')",
	).Scan(&exists)
	s.Require().NoError(err)
	s.Assert().False(exists, "testdb should no longer be in catalog")
}

func (s *RegistrationTestSuite) TestUnregisterDbNotRegistered() {
	// Unregistering a database that was never registered should not error
	var result string
	err := s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.unregister_db('never_registered_db')").Scan(&result)
	s.Require().NoError(err)
	s.Assert().Contains(result, "was not registered")
}

func (s *RegistrationTestSuite) TestUnregisterDbFromNonPostgres() {
	// Attempting to call unregister_db from testdb should fail
	var result string
	err := s.testdbPool.QueryRow(s.ctx, "SELECT steep_repl.unregister_db('somedb')").Scan(&result)
	s.Require().Error(err)
	s.Assert().Contains(err.Error(), "must be called from postgres")
}

// =============================================================================
// disable_db() / enable_db() Tests
// =============================================================================

func (s *RegistrationTestSuite) TestDisableDb() {
	// Register first
	_, err := s.postgresPool.Exec(s.ctx, "SELECT steep_repl.register_db('testdb')")
	s.Require().NoError(err)

	// Disable
	var result string
	err = s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.disable_db('testdb')").Scan(&result)
	s.Require().NoError(err)
	s.Assert().Contains(result, "disabled")

	// Verify enabled = false
	var enabled bool
	err = s.postgresPool.QueryRow(s.ctx,
		"SELECT enabled FROM steep_repl.databases WHERE datname = 'testdb'",
	).Scan(&enabled)
	s.Require().NoError(err)
	s.Assert().False(enabled, "testdb should be disabled")
}

func (s *RegistrationTestSuite) TestEnableDb() {
	// Register and disable first
	_, err := s.postgresPool.Exec(s.ctx, "SELECT steep_repl.register_db('testdb')")
	s.Require().NoError(err)
	_, err = s.postgresPool.Exec(s.ctx, "SELECT steep_repl.disable_db('testdb')")
	s.Require().NoError(err)

	// Enable
	var result string
	err = s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.enable_db('testdb')").Scan(&result)
	s.Require().NoError(err)
	s.Assert().Contains(result, "enabled")

	// Verify enabled = true
	var enabled bool
	err = s.postgresPool.QueryRow(s.ctx,
		"SELECT enabled FROM steep_repl.databases WHERE datname = 'testdb'",
	).Scan(&enabled)
	s.Require().NoError(err)
	s.Assert().True(enabled, "testdb should be enabled")
}

func (s *RegistrationTestSuite) TestDisableDbNotRegistered() {
	var result string
	err := s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.disable_db('not_registered_db')").Scan(&result)
	s.Require().NoError(err)
	s.Assert().Contains(result, "is not registered")
}

// =============================================================================
// list_databases() Tests
// =============================================================================

func (s *RegistrationTestSuite) TestListDatabasesEmpty() {
	// With no registrations, should return empty result
	rows, err := s.postgresPool.Query(s.ctx, "SELECT * FROM steep_repl.list_databases()")
	s.Require().NoError(err)
	defer rows.Close()

	var count int
	for rows.Next() {
		count++
	}
	s.Assert().Equal(0, count, "list_databases should return 0 rows when no databases registered")
}

func (s *RegistrationTestSuite) TestListDatabasesWithRegistrations() {
	// Register testdb
	_, err := s.postgresPool.Exec(s.ctx, "SELECT steep_repl.register_db('testdb')")
	s.Require().NoError(err)

	// List databases
	rows, err := s.postgresPool.Query(s.ctx, "SELECT datname, enabled FROM steep_repl.list_databases()")
	s.Require().NoError(err)
	defer rows.Close()

	var results []struct {
		datname string
		enabled bool
	}
	for rows.Next() {
		var r struct {
			datname string
			enabled bool
		}
		err := rows.Scan(&r.datname, &r.enabled)
		s.Require().NoError(err)
		results = append(results, r)
	}

	s.Assert().Len(results, 1, "Should have 1 registered database")
	s.Assert().Equal("testdb", results[0].datname)
	s.Assert().True(results[0].enabled)
}

// =============================================================================
// get_enabled_databases() Tests
// =============================================================================

func (s *RegistrationTestSuite) TestGetEnabledDatabasesEmpty() {
	var result *string
	err := s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.get_enabled_databases()").Scan(&result)
	s.Require().NoError(err)
	s.Assert().Nil(result, "get_enabled_databases should return NULL when no databases registered")
}

func (s *RegistrationTestSuite) TestGetEnabledDatabasesWithRegistrations() {
	// Register testdb
	_, err := s.postgresPool.Exec(s.ctx, "SELECT steep_repl.register_db('testdb')")
	s.Require().NoError(err)

	var result string
	err = s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.get_enabled_databases()").Scan(&result)
	s.Require().NoError(err)
	s.Assert().Equal("testdb", result)
}

func (s *RegistrationTestSuite) TestGetEnabledDatabasesExcludesDisabled() {
	// Register and disable testdb
	_, err := s.postgresPool.Exec(s.ctx, "SELECT steep_repl.register_db('testdb')")
	s.Require().NoError(err)
	_, err = s.postgresPool.Exec(s.ctx, "SELECT steep_repl.disable_db('testdb')")
	s.Require().NoError(err)

	var result *string
	err = s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.get_enabled_databases()").Scan(&result)
	s.Require().NoError(err)
	s.Assert().Nil(result, "get_enabled_databases should return NULL when all databases are disabled")
}

// =============================================================================
// register_current_db() Tests (libpq-based cross-database registration)
// =============================================================================

func (s *RegistrationTestSuite) TestRegisterCurrentDbFromTestdb() {
	// Call register_current_db from testdb - should register itself in postgres catalog
	var result string
	err := s.testdbPool.QueryRow(s.ctx, "SELECT steep_repl.register_current_db()").Scan(&result)
	s.Require().NoError(err, "register_current_db should succeed from testdb")
	s.Assert().Contains(result, "registered successfully")

	// Verify testdb was registered in postgres catalog
	var exists bool
	err = s.postgresPool.QueryRow(s.ctx,
		"SELECT EXISTS(SELECT 1 FROM steep_repl.databases WHERE datname = 'testdb' AND enabled = true)",
	).Scan(&exists)
	s.Require().NoError(err)
	s.Assert().True(exists, "testdb should be registered in postgres catalog via libpq")
}

func (s *RegistrationTestSuite) TestRegisterCurrentDbFromPostgresFails() {
	// Calling register_current_db from postgres should fail with helpful message
	var result string
	err := s.postgresPool.QueryRow(s.ctx, "SELECT steep_repl.register_current_db()").Scan(&result)
	s.Require().Error(err, "register_current_db should fail when called from postgres")
	s.Assert().Contains(err.Error(), "Already in postgres")
}

func (s *RegistrationTestSuite) TestRegisterCurrentDbIdempotent() {
	// Call register_current_db twice - should be idempotent
	var result1, result2 string

	err := s.testdbPool.QueryRow(s.ctx, "SELECT steep_repl.register_current_db()").Scan(&result1)
	s.Require().NoError(err)

	err = s.testdbPool.QueryRow(s.ctx, "SELECT steep_repl.register_current_db()").Scan(&result2)
	s.Require().NoError(err)

	// Verify only one entry exists
	var count int
	err = s.postgresPool.QueryRow(s.ctx,
		"SELECT count(*) FROM steep_repl.databases WHERE datname = 'testdb'",
	).Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equal(1, count, "Should have exactly one entry for testdb")
}

// =============================================================================
// Static Worker Integration Tests
// =============================================================================

func (s *RegistrationTestSuite) TestStaticWorkerReadsFromCatalog() {
	// This test verifies the static worker behavior by checking that
	// workers are only spawned for registered databases.
	// We can't directly test worker spawning, but we can verify the
	// catalog query returns expected results.

	// Register testdb
	_, err := s.postgresPool.Exec(s.ctx, "SELECT steep_repl.register_db('testdb')")
	s.Require().NoError(err)

	// The static worker queries: SELECT string_agg(datname, ',') FROM steep_repl.databases WHERE enabled = true
	var enabledDbs string
	err = s.postgresPool.QueryRow(s.ctx,
		"SELECT string_agg(datname, ',') FROM steep_repl.databases WHERE enabled = true",
	).Scan(&enabledDbs)
	s.Require().NoError(err)
	s.Assert().Equal("testdb", enabledDbs)

	// Disable testdb
	_, err = s.postgresPool.Exec(s.ctx, "SELECT steep_repl.disable_db('testdb')")
	s.Require().NoError(err)

	// Now the query should return NULL
	var disabledResult *string
	err = s.postgresPool.QueryRow(s.ctx,
		"SELECT string_agg(datname, ',') FROM steep_repl.databases WHERE enabled = true",
	).Scan(&disabledResult)
	s.Require().NoError(err)
	s.Assert().Nil(disabledResult, "No enabled databases should be returned")
}
