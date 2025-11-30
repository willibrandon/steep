package queries

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willibrandon/steep/internal/db/models"
)

// RoleAttributeInfo is a subset of Role used for formatting attributes.
type RoleAttributeInfo struct {
	IsSuperuser   bool
	CanLogin      bool
	CanCreateRole bool
	CanCreateDB   bool
	CanBypassRLS  bool
}

// GetRoles returns all roles with their attributes.
func GetRoles(ctx context.Context, pool *pgxpool.Pool) ([]models.Role, error) {
	query := `
SELECT
    r.oid,
    r.rolname,
    r.rolsuper,
    r.rolinherit,
    r.rolcreaterole,
    r.rolcreatedb,
    r.rolcanlogin,
    r.rolreplication,
    r.rolbypassrls,
    r.rolconnlimit,
    r.rolvaliduntil,
    COALESCE(r.rolconfig, '{}') AS rolconfig,
    (SELECT COUNT(*) FROM pg_auth_members m WHERE m.member = r.oid) AS membership_count,
    (SELECT COUNT(*) FROM pg_class c WHERE c.relowner = r.oid AND c.relkind IN ('r', 'v', 'S', 'm')) AS owned_objects
FROM pg_roles r
ORDER BY r.rolname`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query roles: %w", err)
	}
	defer rows.Close()

	var roles []models.Role
	for rows.Next() {
		var r models.Role
		var config []string
		err := rows.Scan(
			&r.OID,
			&r.Name,
			&r.IsSuperuser,
			&r.Inherit,
			&r.CanCreateRole,
			&r.CanCreateDB,
			&r.CanLogin,
			&r.Replication,
			&r.CanBypassRLS,
			&r.ConnectionLimit,
			&r.ValidUntil,
			&config,
			&r.MembershipCount,
			&r.OwnedObjects,
		)
		if err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		r.Config = config
		roles = append(roles, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate roles: %w", err)
	}

	return roles, nil
}

// GetRoleMemberships returns all role membership relationships.
func GetRoleMemberships(ctx context.Context, pool *pgxpool.Pool) ([]models.RoleMembership, error) {
	query := `
SELECT
    m.roleid AS role_oid,
    r1.rolname AS role_name,
    m.member AS member_oid,
    r2.rolname AS member_name,
    m.grantor AS grantor_oid,
    r3.rolname AS grantor_name,
    m.admin_option,
    COALESCE(m.inherit_option, true) AS inherit_option,
    COALESCE(m.set_option, true) AS set_option
FROM pg_auth_members m
JOIN pg_roles r1 ON r1.oid = m.roleid
JOIN pg_roles r2 ON r2.oid = m.member
JOIN pg_roles r3 ON r3.oid = m.grantor
ORDER BY r1.rolname, r2.rolname`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query memberships: %w", err)
	}
	defer rows.Close()

	var memberships []models.RoleMembership
	for rows.Next() {
		var m models.RoleMembership
		err := rows.Scan(
			&m.RoleOID,
			&m.RoleName,
			&m.MemberOID,
			&m.MemberName,
			&m.GrantorOID,
			&m.GrantorName,
			&m.AdminOption,
			&m.InheritOption,
			&m.SetOption,
		)
		if err != nil {
			return nil, fmt.Errorf("scan membership: %w", err)
		}
		memberships = append(memberships, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memberships: %w", err)
	}

	return memberships, nil
}

// GetRoleMembershipsFor returns memberships for a specific role.
func GetRoleMembershipsFor(ctx context.Context, pool *pgxpool.Pool, roleOID uint32) ([]models.RoleMembership, error) {
	query := `
SELECT
    m.roleid AS role_oid,
    r1.rolname AS role_name,
    m.member AS member_oid,
    r2.rolname AS member_name,
    m.grantor AS grantor_oid,
    r3.rolname AS grantor_name,
    m.admin_option,
    COALESCE(m.inherit_option, true) AS inherit_option,
    COALESCE(m.set_option, true) AS set_option
FROM pg_auth_members m
JOIN pg_roles r1 ON r1.oid = m.roleid
JOIN pg_roles r2 ON r2.oid = m.member
JOIN pg_roles r3 ON r3.oid = m.grantor
WHERE m.member = $1
ORDER BY r1.rolname`

	rows, err := pool.Query(ctx, query, roleOID)
	if err != nil {
		return nil, fmt.Errorf("query memberships for role: %w", err)
	}
	defer rows.Close()

	var memberships []models.RoleMembership
	for rows.Next() {
		var m models.RoleMembership
		err := rows.Scan(
			&m.RoleOID,
			&m.RoleName,
			&m.MemberOID,
			&m.MemberName,
			&m.GrantorOID,
			&m.GrantorName,
			&m.AdminOption,
			&m.InheritOption,
			&m.SetOption,
		)
		if err != nil {
			return nil, fmt.Errorf("scan membership: %w", err)
		}
		memberships = append(memberships, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memberships: %w", err)
	}

	return memberships, nil
}

// GetRoleDetails returns detailed information for a specific role.
func GetRoleDetails(ctx context.Context, pool *pgxpool.Pool, roleOID uint32) (*models.RoleDetails, error) {
	// Get basic role info
	query := `
SELECT
    r.oid,
    r.rolname,
    r.rolsuper,
    r.rolinherit,
    r.rolcreaterole,
    r.rolcreatedb,
    r.rolcanlogin,
    r.rolreplication,
    r.rolbypassrls,
    r.rolconnlimit,
    r.rolvaliduntil,
    COALESCE(r.rolconfig, '{}') AS rolconfig
FROM pg_roles r
WHERE r.oid = $1`

	var role models.Role
	var config []string
	err := pool.QueryRow(ctx, query, roleOID).Scan(
		&role.OID,
		&role.Name,
		&role.IsSuperuser,
		&role.Inherit,
		&role.CanCreateRole,
		&role.CanCreateDB,
		&role.CanLogin,
		&role.Replication,
		&role.CanBypassRLS,
		&role.ConnectionLimit,
		&role.ValidUntil,
		&config,
	)
	if err != nil {
		return nil, fmt.Errorf("get role: %w", err)
	}
	role.Config = config

	// Get memberships (roles this role belongs to)
	memberships, err := GetRoleMembershipsFor(ctx, pool, roleOID)
	if err != nil {
		return nil, fmt.Errorf("get memberships: %w", err)
	}

	// Get members (roles that are members of this role)
	membersQuery := `
SELECT
    m.roleid AS role_oid,
    r1.rolname AS role_name,
    m.member AS member_oid,
    r2.rolname AS member_name,
    m.grantor AS grantor_oid,
    r3.rolname AS grantor_name,
    m.admin_option,
    COALESCE(m.inherit_option, true) AS inherit_option,
    COALESCE(m.set_option, true) AS set_option
FROM pg_auth_members m
JOIN pg_roles r1 ON r1.oid = m.roleid
JOIN pg_roles r2 ON r2.oid = m.member
JOIN pg_roles r3 ON r3.oid = m.grantor
WHERE m.roleid = $1
ORDER BY r2.rolname`

	membersRows, err := pool.Query(ctx, membersQuery, roleOID)
	if err != nil {
		return nil, fmt.Errorf("query members: %w", err)
	}
	defer membersRows.Close()

	var members []models.RoleMembership
	for membersRows.Next() {
		var m models.RoleMembership
		err := membersRows.Scan(
			&m.RoleOID,
			&m.RoleName,
			&m.MemberOID,
			&m.MemberName,
			&m.GrantorOID,
			&m.GrantorName,
			&m.AdminOption,
			&m.InheritOption,
			&m.SetOption,
		)
		if err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		members = append(members, m)
	}

	if err := membersRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate members: %w", err)
	}

	// Get owned objects
	ownedQuery := `
SELECT
    CASE c.relkind
        WHEN 'r' THEN 'table'
        WHEN 'v' THEN 'view'
        WHEN 'm' THEN 'materialized view'
        WHEN 'i' THEN 'index'
        WHEN 'S' THEN 'sequence'
        WHEN 'c' THEN 'composite type'
        WHEN 'f' THEN 'foreign table'
        WHEN 'p' THEN 'partitioned table'
        ELSE c.relkind::text
    END AS object_type,
    n.nspname || '.' || c.relname AS object_name,
    c.oid AS object_oid
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relowner = $1
  AND c.relkind IN ('r', 'v', 'm', 'S', 'f', 'p')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY object_type, object_name
LIMIT 100`

	ownedRows, err := pool.Query(ctx, ownedQuery, roleOID)
	if err != nil {
		return nil, fmt.Errorf("query owned objects: %w", err)
	}
	defer ownedRows.Close()

	var ownedTables []models.OwnedObject
	for ownedRows.Next() {
		var obj models.OwnedObject
		err := ownedRows.Scan(&obj.ObjectType, &obj.ObjectName, &obj.ObjectOID)
		if err != nil {
			return nil, fmt.Errorf("scan owned object: %w", err)
		}
		ownedTables = append(ownedTables, obj)
	}

	if err := ownedRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate owned objects: %w", err)
	}

	return &models.RoleDetails{
		Role:        role,
		Memberships: memberships,
		Members:     members,
		OwnedTables: ownedTables,
	}, nil
}

// FormatRoleAttributes returns a compact attribute string for display.
// Example: "SL" for superuser with login, "L" for login only, "-" for none.
func FormatRoleAttributes(r RoleAttributeInfo) string {
	var attrs []rune
	if r.IsSuperuser {
		attrs = append(attrs, 'S')
	}
	if r.CanLogin {
		attrs = append(attrs, 'L')
	}
	if r.CanCreateRole {
		attrs = append(attrs, 'R')
	}
	if r.CanCreateDB {
		attrs = append(attrs, 'D')
	}
	if r.CanBypassRLS {
		attrs = append(attrs, 'B')
	}
	if len(attrs) == 0 {
		return "-"
	}
	return string(attrs)
}

// FormatConnectionLimit formats the connection limit for display.
func FormatConnectionLimit(limit int) string {
	if limit < 0 {
		return "∞" // Unlimited
	}
	return fmt.Sprintf("%d", limit)
}

// FormatValidUntil formats the password expiration for display.
func FormatValidUntil(t *time.Time) string {
	if t == nil {
		return "never"
	}
	if t.Before(time.Now()) {
		return "EXPIRED"
	}
	return t.Format("2006-01-02")
}
