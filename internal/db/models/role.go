package models

import "time"

// Role represents a PostgreSQL role (user or group).
type Role struct {
	OID             uint32
	Name            string
	IsSuperuser     bool
	CanLogin        bool
	CanCreateRole   bool
	CanCreateDB     bool
	CanBypassRLS    bool
	Inherit         bool
	Replication     bool
	ConnectionLimit int // -1 = unlimited
	ValidUntil      *time.Time
	Config          []string
	MembershipCount int64
	OwnedObjects    int64
	MemberOf        []string // Flattened role names this role belongs to
}

// RoleMembership represents membership of one role in another.
type RoleMembership struct {
	RoleOID       uint32
	RoleName      string
	MemberOID     uint32
	MemberName    string
	GrantorOID    uint32
	GrantorName   string
	AdminOption   bool
	InheritOption bool
	SetOption     bool
}

// RoleDetails contains full role information including memberships and owned objects.
type RoleDetails struct {
	Role
	Memberships []RoleMembership // Roles this role belongs to
	Members     []RoleMembership // Roles that are members of this role
	OwnedTables []OwnedObject
	DefaultACLs []DefaultACL
}

// OwnedObject represents a database object owned by a role.
type OwnedObject struct {
	ObjectType string // "table", "view", "sequence", etc.
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

// Permission represents a privilege grant on a database object.
type Permission struct {
	ObjectType    PermissionObjectType
	ObjectOID     uint32
	ObjectName    string
	Grantee       string
	Grantor       string
	PrivilegeType PrivilegeType
	IsGrantable   bool
}
