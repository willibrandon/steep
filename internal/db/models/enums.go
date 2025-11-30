package models

// OperationType represents the type of maintenance operation.
type OperationType string

const (
	OpVacuum              OperationType = "VACUUM"
	OpVacuumFull          OperationType = "VACUUM FULL"
	OpVacuumAnalyze       OperationType = "VACUUM ANALYZE"
	OpAnalyze             OperationType = "ANALYZE"
	OpReindexTable        OperationType = "REINDEX TABLE"
	OpReindexConcurrently OperationType = "REINDEX CONCURRENTLY"
	OpReindexIndex        OperationType = "REINDEX INDEX"
	OpCheckBloat          OperationType = "BLOAT"
)

// OperationStatus represents the current status of a maintenance operation.
type OperationStatus string

const (
	StatusPending   OperationStatus = "pending"
	StatusRunning   OperationStatus = "running"
	StatusCompleted OperationStatus = "completed"
	StatusCancelled OperationStatus = "cancelled"
	StatusFailed    OperationStatus = "failed"
)

// PermissionObjectType represents the type of database object for permissions.
type PermissionObjectType string

const (
	ObjectTypeTable    PermissionObjectType = "table"
	ObjectTypeSchema   PermissionObjectType = "schema"
	ObjectTypeDatabase PermissionObjectType = "database"
	ObjectTypeSequence PermissionObjectType = "sequence"
	ObjectTypeFunction PermissionObjectType = "function"
)

// PrivilegeType represents a database privilege type.
type PrivilegeType string

const (
	PrivilegeSelect     PrivilegeType = "SELECT"
	PrivilegeInsert     PrivilegeType = "INSERT"
	PrivilegeUpdate     PrivilegeType = "UPDATE"
	PrivilegeDelete     PrivilegeType = "DELETE"
	PrivilegeTruncate   PrivilegeType = "TRUNCATE"
	PrivilegeReferences PrivilegeType = "REFERENCES"
	PrivilegeTrigger    PrivilegeType = "TRIGGER"
	PrivilegeCreate     PrivilegeType = "CREATE"
	PrivilegeConnect    PrivilegeType = "CONNECT"
	PrivilegeUsage      PrivilegeType = "USAGE"
	PrivilegeExecute    PrivilegeType = "EXECUTE"
	PrivilegeAll        PrivilegeType = "ALL"
)
