package models

// ConnectionProfile represents a database connection configuration profile
type ConnectionProfile struct {
	// Connection details
	Host     string
	Port     int
	Database string
	User     string

	// Authentication
	PasswordCommand string // Command to retrieve password from external password manager
	Password        string // Runtime password (never stored in config)

	// SSL/TLS configuration
	SSLMode string // disable, prefer, require

	// Connection pool settings
	PoolMaxConns int
	PoolMinConns int
}

// ConnectionString builds a PostgreSQL connection string from the profile
func (p *ConnectionProfile) ConnectionString() string {
	// Note: Password should be handled separately through pgx ConnConfig
	// This is just the base connection string
	return ""
}
