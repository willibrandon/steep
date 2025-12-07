package roles_test

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
// Roles Test Suite - shares a single container across all tests
// =============================================================================

type RolesTestSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	container testcontainers.Container
	pool      *pgxpool.Pool
}

func TestRolesSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(RolesTestSuite))
}

func (s *RolesTestSuite) SetupSuite() {
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

	s.T().Log("RolesTestSuite: Shared container ready")
}

func (s *RolesTestSuite) TearDownSuite() {
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

func (s *RolesTestSuite) SetupTest() {
	// Clean up test roles and table from previous tests
	_, _ = s.pool.Exec(s.ctx, "DROP TABLE IF EXISTS test_permissions_table CASCADE")
	_, _ = s.pool.Exec(s.ctx, "DROP ROLE IF EXISTS test_user")
	_, _ = s.pool.Exec(s.ctx, "DROP ROLE IF EXISTS test_admin")
	_, _ = s.pool.Exec(s.ctx, "DROP ROLE IF EXISTS test_group")
}

// createTestRoles creates test roles for testing.
func (s *RolesTestSuite) createTestRoles() {
	// Create a role with login
	_, err := s.pool.Exec(s.ctx, `CREATE ROLE test_user WITH LOGIN PASSWORD 'test123'`)
	s.Require().NoError(err, "Failed to create test_user role")

	// Create a role that can create databases
	_, err = s.pool.Exec(s.ctx, `CREATE ROLE test_admin WITH LOGIN CREATEDB`)
	s.Require().NoError(err, "Failed to create test_admin role")

	// Create a group role (no login)
	_, err = s.pool.Exec(s.ctx, `CREATE ROLE test_group NOLOGIN`)
	s.Require().NoError(err, "Failed to create test_group role")

	// Add membership: test_user is a member of test_group
	_, err = s.pool.Exec(s.ctx, `GRANT test_group TO test_user`)
	s.Require().NoError(err, "Failed to grant membership")
}

// createTestTable creates a test table for permission tests and returns its OID.
func (s *RolesTestSuite) createTestTable() uint32 {
	// Create a test table
	_, err := s.pool.Exec(s.ctx, `CREATE TABLE test_permissions_table (id serial PRIMARY KEY, name text)`)
	s.Require().NoError(err, "Failed to create test table")

	// Get the table OID
	var tableOID uint32
	err = s.pool.QueryRow(s.ctx, `SELECT oid FROM pg_class WHERE relname = 'test_permissions_table'`).Scan(&tableOID)
	s.Require().NoError(err, "Failed to get table OID")

	return tableOID
}

// =============================================================================
// Tests
// =============================================================================

func (s *RolesTestSuite) TestGetRoles() {
	s.createTestRoles()

	roles, err := queries.GetRoles(s.ctx, s.pool)
	s.Require().NoError(err, "GetRoles failed")

	// Should have at least the postgres/test user and our created roles
	s.Assert().GreaterOrEqual(len(roles), 3, "Expected at least 3 roles")

	// Find our test roles
	var foundUser, foundAdmin, foundGroup bool
	for _, r := range roles {
		switch r.Name {
		case "test_user":
			foundUser = true
			s.Assert().True(r.CanLogin, "test_user should have CanLogin=true")
			s.Assert().False(r.IsSuperuser, "test_user should not be superuser")
		case "test_admin":
			foundAdmin = true
			s.Assert().True(r.CanLogin, "test_admin should have CanLogin=true")
			s.Assert().True(r.CanCreateDB, "test_admin should have CanCreateDB=true")
		case "test_group":
			foundGroup = true
			s.Assert().False(r.CanLogin, "test_group should have CanLogin=false")
		}
	}

	s.Assert().True(foundUser, "test_user not found in roles")
	s.Assert().True(foundAdmin, "test_admin not found in roles")
	s.Assert().True(foundGroup, "test_group not found in roles")
}

func (s *RolesTestSuite) TestGetRoleMemberships() {
	s.createTestRoles()

	memberships, err := queries.GetRoleMemberships(s.ctx, s.pool)
	s.Require().NoError(err, "GetRoleMemberships failed")

	// Find test_user -> test_group membership
	var found bool
	for _, m := range memberships {
		if m.MemberName == "test_user" && m.RoleName == "test_group" {
			found = true
			break
		}
	}

	s.Assert().True(found, "Expected to find test_user membership in test_group")
}

func (s *RolesTestSuite) TestGetRoleMembershipsFor() {
	s.createTestRoles()

	// First get test_user OID
	roles, err := queries.GetRoles(s.ctx, s.pool)
	s.Require().NoError(err, "GetRoles failed")

	var testUserOID uint32
	for _, r := range roles {
		if r.Name == "test_user" {
			testUserOID = r.OID
			break
		}
	}
	s.Require().NotZero(testUserOID, "test_user not found")

	// Get memberships for test_user
	memberships, err := queries.GetRoleMembershipsFor(s.ctx, s.pool, testUserOID)
	s.Require().NoError(err, "GetRoleMembershipsFor failed")

	// test_user should be member of test_group
	s.Assert().Len(memberships, 1, "Expected 1 membership for test_user")

	if len(memberships) > 0 {
		s.Assert().Equal("test_group", memberships[0].RoleName, "Expected test_user to be member of test_group")
	}
}

func (s *RolesTestSuite) TestGetRoleDetails() {
	s.createTestRoles()

	// First get test_user OID
	roles, err := queries.GetRoles(s.ctx, s.pool)
	s.Require().NoError(err, "GetRoles failed")

	var testUserOID uint32
	for _, r := range roles {
		if r.Name == "test_user" {
			testUserOID = r.OID
			break
		}
	}
	s.Require().NotZero(testUserOID, "test_user not found")

	// Get details
	details, err := queries.GetRoleDetails(s.ctx, s.pool, testUserOID)
	s.Require().NoError(err, "GetRoleDetails failed")
	s.Require().NotNil(details, "Expected non-nil role details")

	s.Assert().Equal("test_user", details.Name, "Expected name='test_user'")

	// Should show membership in test_group
	s.Assert().Len(details.Memberships, 1, "Expected 1 membership")
}

func (s *RolesTestSuite) TestGetTablePermissions() {
	s.createTestRoles()
	tableOID := s.createTestTable()

	// Grant SELECT to test_user
	_, err := s.pool.Exec(s.ctx, `GRANT SELECT ON test_permissions_table TO test_user`)
	s.Require().NoError(err, "Failed to grant SELECT")

	// Get permissions
	perms, err := queries.GetTablePermissions(s.ctx, s.pool, tableOID)
	s.Require().NoError(err, "GetTablePermissions failed")

	// Should have at least the owner permissions and the grant to test_user
	var foundTestUserSelect bool
	for _, p := range perms {
		if p.Grantee == "test_user" && p.PrivilegeType == "SELECT" {
			foundTestUserSelect = true
			break
		}
	}

	s.Assert().True(foundTestUserSelect, "Expected to find SELECT permission for test_user")
}

func (s *RolesTestSuite) TestGrantTablePrivilege() {
	s.createTestRoles()
	tableOID := s.createTestTable()

	// Grant INSERT to test_user
	err := queries.GrantTablePrivilege(s.ctx, s.pool, "public", "test_permissions_table", "test_user", "INSERT", false)
	s.Require().NoError(err, "GrantTablePrivilege failed")

	// Verify the grant
	perms, err := queries.GetTablePermissions(s.ctx, s.pool, tableOID)
	s.Require().NoError(err, "GetTablePermissions failed")

	var foundInsert bool
	for _, p := range perms {
		if p.Grantee == "test_user" && p.PrivilegeType == "INSERT" {
			foundInsert = true
			s.Assert().False(p.IsGrantable, "Expected is_grantable=false for grant without WITH GRANT OPTION")
			break
		}
	}

	s.Assert().True(foundInsert, "Expected to find INSERT permission for test_user after grant")
}

func (s *RolesTestSuite) TestGrantTablePrivilegeWithGrantOption() {
	s.createTestRoles()
	tableOID := s.createTestTable()

	// Grant UPDATE to test_admin with grant option
	err := queries.GrantTablePrivilege(s.ctx, s.pool, "public", "test_permissions_table", "test_admin", "UPDATE", true)
	s.Require().NoError(err, "GrantTablePrivilege with grant option failed")

	// Verify the grant
	perms, err := queries.GetTablePermissions(s.ctx, s.pool, tableOID)
	s.Require().NoError(err, "GetTablePermissions failed")

	var foundUpdate bool
	for _, p := range perms {
		if p.Grantee == "test_admin" && p.PrivilegeType == "UPDATE" {
			foundUpdate = true
			s.Assert().True(p.IsGrantable, "Expected is_grantable=true for grant WITH GRANT OPTION")
			break
		}
	}

	s.Assert().True(foundUpdate, "Expected to find UPDATE permission for test_admin after grant")
}

func (s *RolesTestSuite) TestRevokeTablePrivilege() {
	s.createTestRoles()
	tableOID := s.createTestTable()

	// First grant DELETE to test_user
	_, err := s.pool.Exec(s.ctx, `GRANT DELETE ON test_permissions_table TO test_user`)
	s.Require().NoError(err, "Failed to grant DELETE")

	// Verify it was granted
	perms, err := queries.GetTablePermissions(s.ctx, s.pool, tableOID)
	s.Require().NoError(err, "GetTablePermissions failed")

	var foundDeleteBefore bool
	for _, p := range perms {
		if p.Grantee == "test_user" && p.PrivilegeType == "DELETE" {
			foundDeleteBefore = true
			break
		}
	}
	s.Require().True(foundDeleteBefore, "Expected DELETE to be granted before revoke test")

	// Now revoke it
	err = queries.RevokeTablePrivilege(s.ctx, s.pool, "public", "test_permissions_table", "test_user", "DELETE", false)
	s.Require().NoError(err, "RevokeTablePrivilege failed")

	// Verify it was revoked
	perms, err = queries.GetTablePermissions(s.ctx, s.pool, tableOID)
	s.Require().NoError(err, "GetTablePermissions failed after revoke")

	for _, p := range perms {
		s.Assert().False(p.Grantee == "test_user" && p.PrivilegeType == "DELETE",
			"Expected DELETE permission to be revoked from test_user")
	}
}

// =============================================================================
// Standalone tests (no container needed - pure unit tests)
// =============================================================================

func TestFormatRoleAttributes(t *testing.T) {
	tests := []struct {
		name     string
		role     queries.RoleAttributeInfo
		expected string
	}{
		{
			name:     "no attributes",
			role:     queries.RoleAttributeInfo{},
			expected: "-",
		},
		{
			name:     "superuser only",
			role:     queries.RoleAttributeInfo{IsSuperuser: true},
			expected: "S",
		},
		{
			name:     "login only",
			role:     queries.RoleAttributeInfo{CanLogin: true},
			expected: "L",
		},
		{
			name:     "superuser and login",
			role:     queries.RoleAttributeInfo{IsSuperuser: true, CanLogin: true},
			expected: "SL",
		},
		{
			name: "all attributes",
			role: queries.RoleAttributeInfo{
				IsSuperuser:   true,
				CanLogin:      true,
				CanCreateRole: true,
				CanCreateDB:   true,
				CanBypassRLS:  true,
			},
			expected: "SLRDB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := queries.FormatRoleAttributes(tt.role)
			if result != tt.expected {
				t.Errorf("FormatRoleAttributes() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatConnectionLimit(t *testing.T) {
	tests := []struct {
		limit    int
		expected string
	}{
		{-1, "∞"},
		{0, "0"},
		{1, "1"},
		{100, "100"},
	}

	for _, tt := range tests {
		result := queries.FormatConnectionLimit(tt.limit)
		if result != tt.expected {
			t.Errorf("FormatConnectionLimit(%d) = %q, want %q", tt.limit, result, tt.expected)
		}
	}
}
