# Data Model: Configuration Viewer

**Feature**: 008-configuration-viewer
**Date**: 2025-11-27

## Entities

### Parameter

Represents a PostgreSQL configuration parameter from `pg_settings`.

```go
// Package models contains data structures for database entities.
package models

// Parameter represents a PostgreSQL configuration setting from pg_settings.
type Parameter struct {
    // Name is the configuration parameter name (e.g., "shared_buffers")
    Name string

    // Setting is the current value as a string
    Setting string

    // Unit is the measurement unit if applicable (e.g., "MB", "ms", "8kB")
    // Empty string if no unit
    Unit string

    // Category is the logical group (e.g., "Resource Usage / Memory")
    Category string

    // ShortDesc is a brief description of the parameter
    ShortDesc string

    // ExtraDesc is extended description with additional details
    // May be empty
    ExtraDesc string

    // Context indicates when the parameter can be changed
    // Values: internal, postmaster, sighup, backend, superuser, user
    Context string

    // VarType is the data type (bool, integer, real, string, enum)
    VarType string

    // Source indicates where the current value was set
    // Values: default, command line, configuration file, database, user, session
    Source string

    // MinVal is the minimum allowed value for numeric types
    // Empty string for non-numeric or unbounded
    MinVal string

    // MaxVal is the maximum allowed value for numeric types
    // Empty string for non-numeric or unbounded
    MaxVal string

    // EnumVals is the list of valid values for enum type parameters
    // nil for non-enum types
    EnumVals []string

    // BootVal is the default/compiled-in value
    BootVal string

    // ResetVal is the value the parameter would reset to
    ResetVal string

    // SourceFile is the config file where the setting was defined
    // Empty string if not from a file
    SourceFile string

    // SourceLine is the line number in the config file
    // 0 if not from a file
    SourceLine int

    // PendingRestart is true if a change requires server restart
    PendingRestart bool
}

// IsModified returns true if the current setting differs from the default.
func (p *Parameter) IsModified() bool {
    // Handle NULL boot_val (some internal params)
    if p.BootVal == "" {
        return false
    }
    return p.Setting != p.BootVal
}

// RequiresRestart returns true if changing this parameter requires a restart.
func (p *Parameter) RequiresRestart() bool {
    return p.Context == "postmaster"
}

// RequiresReload returns true if changing this parameter requires a reload.
func (p *Parameter) RequiresReload() bool {
    return p.Context == "sighup"
}

// CanUserChange returns true if any user can modify this parameter.
func (p *Parameter) CanUserChange() bool {
    return p.Context == "user"
}

// TopLevelCategory returns the category before the first " / " separator.
func (p *Parameter) TopLevelCategory() string {
    if idx := strings.Index(p.Category, " / "); idx != -1 {
        return p.Category[:idx]
    }
    return p.Category
}
```

### ConfigData

Container for all configuration data from a single refresh.

```go
// ConfigData contains all configuration information for a single refresh.
type ConfigData struct {
    // Parameters is the list of all configuration parameters
    Parameters []Parameter

    // ModifiedCount is the count of parameters differing from defaults
    ModifiedCount int

    // PendingRestartCount is the count of parameters needing restart
    PendingRestartCount int

    // Categories is the list of unique top-level categories for filtering
    Categories []string
}

// NewConfigData creates an initialized ConfigData structure.
func NewConfigData() *ConfigData {
    return &ConfigData{
        Parameters: make([]Parameter, 0, 400), // ~350 params typical
        Categories: make([]string, 0, 20),
    }
}

// FilterByCategory returns parameters matching the given top-level category.
// If category is empty, returns all parameters.
func (d *ConfigData) FilterByCategory(category string) []Parameter {
    if category == "" {
        return d.Parameters
    }

    result := make([]Parameter, 0)
    for _, p := range d.Parameters {
        if p.TopLevelCategory() == category {
            result = append(result, p)
        }
    }
    return result
}

// FilterBySearch returns parameters whose name or description contains the term.
// Search is case-insensitive.
func (d *ConfigData) FilterBySearch(term string) []Parameter {
    if term == "" {
        return d.Parameters
    }

    term = strings.ToLower(term)
    result := make([]Parameter, 0)
    for _, p := range d.Parameters {
        if strings.Contains(strings.ToLower(p.Name), term) ||
           strings.Contains(strings.ToLower(p.ShortDesc), term) {
            result = append(result, p)
        }
    }
    return result
}
```

## Relationships

```
┌─────────────────────────────────────────────────────────────────┐
│                         ConfigData                               │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Parameters []Parameter                                   │   │
│  │  ┌────────────────────────────────────────────────────┐  │   │
│  │  │ Parameter                                           │  │   │
│  │  │  - Name: string (PK)                               │  │   │
│  │  │  - Setting: string                                 │  │   │
│  │  │  - Unit: string                                    │  │   │
│  │  │  - Category: string ─────────────────────┐         │  │   │
│  │  │  - ShortDesc: string                     │         │  │   │
│  │  │  - ExtraDesc: string                     │         │  │   │
│  │  │  - Context: string                       │         │  │   │
│  │  │  - VarType: string                       │         │  │   │
│  │  │  - Source: string                        │         │  │   │
│  │  │  - MinVal: string                        │         │  │   │
│  │  │  - MaxVal: string                        │         │  │   │
│  │  │  - EnumVals: []string                    │         │  │   │
│  │  │  - BootVal: string                       │         │  │   │
│  │  │  - ResetVal: string                      │         │  │   │
│  │  │  - SourceFile: string                    │         │  │   │
│  │  │  - SourceLine: int                       │         │  │   │
│  │  │  - PendingRestart: bool                  │         │  │   │
│  │  └────────────────────────────────────────────────────┘  │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  Categories []string ◄─────────────────────────────────────────┘│
│  ModifiedCount: int                                              │
│  PendingRestartCount: int                                        │
└─────────────────────────────────────────────────────────────────┘
```

## Validation Rules

### Parameter

| Field | Rule | Error Message |
|-------|------|---------------|
| Name | Required, non-empty | "parameter name is required" |
| Context | Must be one of: internal, postmaster, sighup, backend, superuser, user | "invalid context value" |
| VarType | Must be one of: bool, integer, real, string, enum | "invalid vartype value" |

### ConfigData

| Field | Rule | Error Message |
|-------|------|---------------|
| Parameters | Non-nil slice | "parameters slice is nil" |

## State Transitions

Parameters are read-only in this feature. No state transitions apply.

## Database Query

```sql
-- GetAllParameters retrieves all configuration parameters
SELECT
    name,
    setting,
    COALESCE(unit, '') AS unit,
    category,
    short_desc,
    COALESCE(extra_desc, '') AS extra_desc,
    context,
    vartype,
    source,
    COALESCE(min_val, '') AS min_val,
    COALESCE(max_val, '') AS max_val,
    enumvals,
    COALESCE(boot_val, '') AS boot_val,
    COALESCE(reset_val, '') AS reset_val,
    COALESCE(sourcefile, '') AS sourcefile,
    COALESCE(sourceline, 0) AS sourceline,
    COALESCE(pending_restart, false) AS pending_restart
FROM pg_settings
ORDER BY name;
```

**Performance Notes**:
- pg_settings is a virtual view (no disk I/O)
- ~350 rows returned (fixed count)
- Query execution: < 10ms
- Result size: ~100KB
