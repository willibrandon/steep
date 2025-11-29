# Roles and Permissions Contract

**Package**: `internal/db/queries`

This contract defines the interface for role and permission management.

## Interface Definition

```go
// RoleProvider provides methods for querying and managing database roles.
type RoleProvider interface {
    // GetRoles returns all roles with their attributes.
    GetRoles(ctx context.Context) ([]Role, error)

    // GetRoleDetails returns detailed information for a specific role.
    GetRoleDetails(ctx context.Context, roleOID uint32) (*RoleDetails, error)

    // GetRoleMemberships returns all role membership relationships.
    GetRoleMemberships(ctx context.Context) ([]RoleMembership, error)

    // GetRoleMembershipsFor returns memberships for a specific role.
    GetRoleMembershipsFor(ctx context.Context, roleOID uint32) ([]RoleMembership, error)
}

// PermissionProvider provides methods for querying and modifying permissions.
type PermissionProvider interface {
    // GetTablePermissions returns all permissions on a table.
    GetTablePermissions(ctx context.Context, tableOID uint32) ([]Permission, error)

    // GetSchemaPermissions returns all permissions on a schema.
    GetSchemaPermissions(ctx context.Context, schemaOID uint32) ([]Permission, error)

    // GrantTablePrivilege grants a privilege on a table to a role.
    GrantTablePrivilege(ctx context.Context, schema, table, role string, privilege PrivilegeType, withGrantOption bool) error

    // RevokeTablePrivilege revokes a privilege from a role.
    RevokeTablePrivilege(ctx context.Context, schema, table, role string, privilege PrivilegeType, cascade bool) error
}

// RoleDetails contains full role information including memberships and owned objects.
type RoleDetails struct {
    Role
    MemberOf      []RoleMembership // Roles this role belongs to
    Members       []RoleMembership // Roles that are members of this role
    OwnedObjects  []OwnedObject    // Objects owned by this role
    DefaultACLs   []DefaultACL     // Default privileges for this role
}

// OwnedObject represents a database object owned by a role.
type OwnedObject struct {
    ObjectType string // "table", "schema", "function", etc.
    ObjectName string // Fully qualified name
    ObjectOID  uint32
}

// DefaultACL represents default privileges set for a role.
type DefaultACL struct {
    Schema        string
    ObjectType    string // "r" = relation, "S" = sequence, "f" = function, etc.
    Grantee       string
    PrivilegeType string
}
```

## SQL Queries

### Get All Roles

```sql
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
    -- Count of direct memberships
    (SELECT COUNT(*) FROM pg_auth_members m WHERE m.member = r.oid) AS membership_count,
    -- Count of objects owned
    (SELECT COUNT(*) FROM pg_class c WHERE c.relowner = r.oid) AS owned_objects
FROM pg_roles r
ORDER BY r.rolname;
```

### Get Role Details

```sql
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
    r.rolconfig
FROM pg_roles r
WHERE r.oid = $1;
```

### Get Role Memberships

```sql
SELECT
    m.roleid AS role_oid,
    r1.rolname AS role_name,
    m.member AS member_oid,
    r2.rolname AS member_name,
    m.grantor AS grantor_oid,
    r3.rolname AS grantor_name,
    m.admin_option,
    m.inherit_option,
    m.set_option
FROM pg_auth_members m
JOIN pg_roles r1 ON r1.oid = m.roleid
JOIN pg_roles r2 ON r2.oid = m.member
JOIN pg_roles r3 ON r3.oid = m.grantor
ORDER BY r1.rolname, r2.rolname;
```

### Get Memberships for Specific Role

```sql
-- Roles that $1 is a member of
SELECT
    m.roleid AS role_oid,
    r1.rolname AS role_name,
    m.member AS member_oid,
    r2.rolname AS member_name,
    m.grantor AS grantor_oid,
    r3.rolname AS grantor_name,
    m.admin_option,
    m.inherit_option,
    m.set_option
FROM pg_auth_members m
JOIN pg_roles r1 ON r1.oid = m.roleid
JOIN pg_roles r2 ON r2.oid = m.member
JOIN pg_roles r3 ON r3.oid = m.grantor
WHERE m.member = $1
ORDER BY r1.rolname;
```

### Get Table Permissions

```sql
SELECT
    'table' AS object_type,
    c.oid AS object_oid,
    n.nspname || '.' || c.relname AS object_name,
    (aclexplode(c.relacl)).grantee::regrole::text AS grantee,
    (aclexplode(c.relacl)).grantor::regrole::text AS grantor,
    (aclexplode(c.relacl)).privilege_type AS privilege_type,
    (aclexplode(c.relacl)).is_grantable AS is_grantable
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.oid = $1
  AND c.relacl IS NOT NULL
ORDER BY grantee, privilege_type;
```

### Get Objects Owned by Role

```sql
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
ORDER BY object_type, object_name;
```

### Grant Table Privilege

```sql
-- Generated SQL (not parameterized - identifiers must be quoted)
GRANT {privilege} ON {schema}.{table} TO {role};

-- With grant option
GRANT {privilege} ON {schema}.{table} TO {role} WITH GRANT OPTION;
```

Go implementation:

```go
func (q *Queries) GrantTablePrivilege(ctx context.Context, schema, table, role string,
    privilege PrivilegeType, withGrantOption bool) error {

    sql := fmt.Sprintf("GRANT %s ON %s.%s TO %s",
        privilege,
        quoteIdentifier(schema),
        quoteIdentifier(table),
        quoteIdentifier(role))

    if withGrantOption {
        sql += " WITH GRANT OPTION"
    }

    _, err := q.pool.Exec(ctx, sql)
    return err
}
```

### Revoke Table Privilege

```sql
-- Without cascade (default)
REVOKE {privilege} ON {schema}.{table} FROM {role};

-- With cascade (also revokes dependent privileges)
REVOKE {privilege} ON {schema}.{table} FROM {role} CASCADE;
```

## ACL Decoding

PostgreSQL stores ACLs in a compact format. The `aclexplode()` function converts them to rows.

ACL privilege abbreviations:
| Abbreviation | Privilege |
|--------------|-----------|
| r | SELECT (read) |
| w | UPDATE (write) |
| a | INSERT (append) |
| d | DELETE |
| D | TRUNCATE |
| x | REFERENCES |
| t | TRIGGER |
| C | CREATE |
| c | CONNECT |
| T | TEMPORARY |
| X | EXECUTE |
| U | USAGE |
| s | SET |
| A | ALTER SYSTEM |
| m | MAINTAIN |

## Display Formatting

```go
// FormatRoleAttributes returns a compact attribute string for display.
// Example: "SL" for superuser with login, "L" for login only, "-" for none.
func FormatRoleAttributes(r Role) string {
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
```

## Error Handling

| Error | Condition | User Message |
|-------|-----------|--------------|
| `ErrReadOnlyMode` | GRANT/REVOKE in read-only mode | "Permission changes blocked: application is in read-only mode" |
| `ErrInsufficientPrivileges` | User can't grant/revoke | "Cannot modify permissions: you don't have GRANT OPTION on this object" |
| `ErrRoleNotFound` | Target role doesn't exist | "Role '{name}' not found" |
| `ErrObjectNotFound` | Target object doesn't exist | "Object '{schema}.{name}' not found" |
| `ErrSelfGrant` | Granting to self | "Cannot grant privilege to yourself" |
