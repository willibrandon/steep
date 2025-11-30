package roles_test

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
		Image:        "postgres:18-alpine",
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

// createTestRoles creates test roles for testing.
func createTestRoles(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	// Create a role with login
	_, err := pool.Exec(ctx, `CREATE ROLE test_user WITH LOGIN PASSWORD 'test123'`)
	if err != nil {
		t.Fatalf("Failed to create test_user role: %v", err)
	}

	// Create a role that can create databases
	_, err = pool.Exec(ctx, `CREATE ROLE test_admin WITH LOGIN CREATEDB`)
	if err != nil {
		t.Fatalf("Failed to create test_admin role: %v", err)
	}

	// Create a group role (no login)
	_, err = pool.Exec(ctx, `CREATE ROLE test_group NOLOGIN`)
	if err != nil {
		t.Fatalf("Failed to create test_group role: %v", err)
	}

	// Add membership: test_user is a member of test_group
	_, err = pool.Exec(ctx, `GRANT test_group TO test_user`)
	if err != nil {
		t.Fatalf("Failed to grant membership: %v", err)
	}
}

// TestGetRoles verifies GetRoles returns all roles.
func TestGetRoles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestRoles(t, ctx, pool)

	roles, err := queries.GetRoles(ctx, pool)
	if err != nil {
		t.Fatalf("GetRoles failed: %v", err)
	}

	// Should have at least the postgres/test user and our created roles
	if len(roles) < 3 {
		t.Errorf("Expected at least 3 roles, got %d", len(roles))
	}

	// Find our test roles
	var foundUser, foundAdmin, foundGroup bool
	for _, r := range roles {
		switch r.Name {
		case "test_user":
			foundUser = true
			if !r.CanLogin {
				t.Error("test_user should have CanLogin=true")
			}
			if r.IsSuperuser {
				t.Error("test_user should not be superuser")
			}
		case "test_admin":
			foundAdmin = true
			if !r.CanLogin {
				t.Error("test_admin should have CanLogin=true")
			}
			if !r.CanCreateDB {
				t.Error("test_admin should have CanCreateDB=true")
			}
		case "test_group":
			foundGroup = true
			if r.CanLogin {
				t.Error("test_group should have CanLogin=false")
			}
		}
	}

	if !foundUser {
		t.Error("test_user not found in roles")
	}
	if !foundAdmin {
		t.Error("test_admin not found in roles")
	}
	if !foundGroup {
		t.Error("test_group not found in roles")
	}
}

// TestGetRoleMemberships verifies GetRoleMemberships returns membership relationships.
func TestGetRoleMemberships(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestRoles(t, ctx, pool)

	memberships, err := queries.GetRoleMemberships(ctx, pool)
	if err != nil {
		t.Fatalf("GetRoleMemberships failed: %v", err)
	}

	// Find test_user -> test_group membership
	var found bool
	for _, m := range memberships {
		if m.MemberName == "test_user" && m.RoleName == "test_group" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected to find test_user membership in test_group")
	}
}

// TestGetRoleMembershipsFor verifies GetRoleMembershipsFor returns memberships for a specific role.
func TestGetRoleMembershipsFor(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestRoles(t, ctx, pool)

	// First get test_user OID
	roles, err := queries.GetRoles(ctx, pool)
	if err != nil {
		t.Fatalf("GetRoles failed: %v", err)
	}

	var testUserOID uint32
	for _, r := range roles {
		if r.Name == "test_user" {
			testUserOID = r.OID
			break
		}
	}
	if testUserOID == 0 {
		t.Fatal("test_user not found")
	}

	// Get memberships for test_user
	memberships, err := queries.GetRoleMembershipsFor(ctx, pool, testUserOID)
	if err != nil {
		t.Fatalf("GetRoleMembershipsFor failed: %v", err)
	}

	// test_user should be member of test_group
	if len(memberships) != 1 {
		t.Errorf("Expected 1 membership for test_user, got %d", len(memberships))
	}

	if len(memberships) > 0 && memberships[0].RoleName != "test_group" {
		t.Errorf("Expected test_user to be member of test_group, got %s", memberships[0].RoleName)
	}
}

// TestGetRoleDetails verifies GetRoleDetails returns detailed role information.
func TestGetRoleDetails(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestRoles(t, ctx, pool)

	// First get test_user OID
	roles, err := queries.GetRoles(ctx, pool)
	if err != nil {
		t.Fatalf("GetRoles failed: %v", err)
	}

	var testUserOID uint32
	for _, r := range roles {
		if r.Name == "test_user" {
			testUserOID = r.OID
			break
		}
	}
	if testUserOID == 0 {
		t.Fatal("test_user not found")
	}

	// Get details
	details, err := queries.GetRoleDetails(ctx, pool, testUserOID)
	if err != nil {
		t.Fatalf("GetRoleDetails failed: %v", err)
	}

	if details == nil {
		t.Fatal("Expected non-nil role details")
	}

	if details.Name != "test_user" {
		t.Errorf("Expected name='test_user', got '%s'", details.Name)
	}

	// Should show membership in test_group
	if len(details.Memberships) != 1 {
		t.Errorf("Expected 1 membership, got %d", len(details.Memberships))
	}
}

// TestFormatRoleAttributes verifies FormatRoleAttributes helper function.
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

// TestFormatConnectionLimit verifies FormatConnectionLimit helper function.
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

// createTestTable creates a test table for permission tests.
func createTestTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uint32 {
	t.Helper()

	// Create a test table
	_, err := pool.Exec(ctx, `CREATE TABLE test_permissions_table (id serial PRIMARY KEY, name text)`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Get the table OID
	var tableOID uint32
	err = pool.QueryRow(ctx, `SELECT oid FROM pg_class WHERE relname = 'test_permissions_table'`).Scan(&tableOID)
	if err != nil {
		t.Fatalf("Failed to get table OID: %v", err)
	}

	return tableOID
}

// TestGetTablePermissions verifies GetTablePermissions returns permissions on a table.
func TestGetTablePermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestRoles(t, ctx, pool)
	tableOID := createTestTable(t, ctx, pool)

	// Grant SELECT to test_user
	_, err := pool.Exec(ctx, `GRANT SELECT ON test_permissions_table TO test_user`)
	if err != nil {
		t.Fatalf("Failed to grant SELECT: %v", err)
	}

	// Get permissions
	perms, err := queries.GetTablePermissions(ctx, pool, tableOID)
	if err != nil {
		t.Fatalf("GetTablePermissions failed: %v", err)
	}

	// Should have at least the owner permissions and the grant to test_user
	var foundTestUserSelect bool
	for _, p := range perms {
		if p.Grantee == "test_user" && p.PrivilegeType == "SELECT" {
			foundTestUserSelect = true
			break
		}
	}

	if !foundTestUserSelect {
		t.Error("Expected to find SELECT permission for test_user")
	}
}

// TestGrantTablePrivilege verifies GrantTablePrivilege grants a privilege.
func TestGrantTablePrivilege(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestRoles(t, ctx, pool)
	tableOID := createTestTable(t, ctx, pool)

	// Grant INSERT to test_user
	err := queries.GrantTablePrivilege(ctx, pool, "public", "test_permissions_table", "test_user", "INSERT", false)
	if err != nil {
		t.Fatalf("GrantTablePrivilege failed: %v", err)
	}

	// Verify the grant
	perms, err := queries.GetTablePermissions(ctx, pool, tableOID)
	if err != nil {
		t.Fatalf("GetTablePermissions failed: %v", err)
	}

	var foundInsert bool
	for _, p := range perms {
		if p.Grantee == "test_user" && p.PrivilegeType == "INSERT" {
			foundInsert = true
			if p.IsGrantable {
				t.Error("Expected is_grantable=false for grant without WITH GRANT OPTION")
			}
			break
		}
	}

	if !foundInsert {
		t.Error("Expected to find INSERT permission for test_user after grant")
	}
}

// TestGrantTablePrivilegeWithGrantOption verifies WITH GRANT OPTION works.
func TestGrantTablePrivilegeWithGrantOption(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestRoles(t, ctx, pool)
	tableOID := createTestTable(t, ctx, pool)

	// Grant UPDATE to test_admin with grant option
	err := queries.GrantTablePrivilege(ctx, pool, "public", "test_permissions_table", "test_admin", "UPDATE", true)
	if err != nil {
		t.Fatalf("GrantTablePrivilege with grant option failed: %v", err)
	}

	// Verify the grant
	perms, err := queries.GetTablePermissions(ctx, pool, tableOID)
	if err != nil {
		t.Fatalf("GetTablePermissions failed: %v", err)
	}

	var foundUpdate bool
	for _, p := range perms {
		if p.Grantee == "test_admin" && p.PrivilegeType == "UPDATE" {
			foundUpdate = true
			if !p.IsGrantable {
				t.Error("Expected is_grantable=true for grant WITH GRANT OPTION")
			}
			break
		}
	}

	if !foundUpdate {
		t.Error("Expected to find UPDATE permission for test_admin after grant")
	}
}

// TestRevokeTablePrivilege verifies RevokeTablePrivilege revokes a privilege.
func TestRevokeTablePrivilege(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pool := setupPostgres(t, ctx)
	createTestRoles(t, ctx, pool)
	tableOID := createTestTable(t, ctx, pool)

	// First grant DELETE to test_user
	_, err := pool.Exec(ctx, `GRANT DELETE ON test_permissions_table TO test_user`)
	if err != nil {
		t.Fatalf("Failed to grant DELETE: %v", err)
	}

	// Verify it was granted
	perms, err := queries.GetTablePermissions(ctx, pool, tableOID)
	if err != nil {
		t.Fatalf("GetTablePermissions failed: %v", err)
	}

	var foundDeleteBefore bool
	for _, p := range perms {
		if p.Grantee == "test_user" && p.PrivilegeType == "DELETE" {
			foundDeleteBefore = true
			break
		}
	}
	if !foundDeleteBefore {
		t.Fatal("Expected DELETE to be granted before revoke test")
	}

	// Now revoke it
	err = queries.RevokeTablePrivilege(ctx, pool, "public", "test_permissions_table", "test_user", "DELETE", false)
	if err != nil {
		t.Fatalf("RevokeTablePrivilege failed: %v", err)
	}

	// Verify it was revoked
	perms, err = queries.GetTablePermissions(ctx, pool, tableOID)
	if err != nil {
		t.Fatalf("GetTablePermissions failed after revoke: %v", err)
	}

	for _, p := range perms {
		if p.Grantee == "test_user" && p.PrivilegeType == "DELETE" {
			t.Error("Expected DELETE permission to be revoked from test_user")
		}
	}
}
