package app

import (
	"fmt"
	"strings"
)

// FormatConnectionError formats a connection error with actionable guidance
func FormatConnectionError(err error) string {
	errMsg := err.Error()

	// Check for common error patterns and provide guidance
	if strings.Contains(errMsg, "connection refused") {
		return fmt.Sprintf(
			"Connection refused: PostgreSQL is not accepting connections.\n\n"+
				"Troubleshooting steps:\n"+
				"  1. Verify PostgreSQL is running:\n"+
				"     - macOS:   brew services list | grep postgresql\n"+
				"     - Linux:   systemctl status postgresql\n"+
				"  2. Check if PostgreSQL is listening on the expected port\n"+
				"  3. Verify firewall settings allow the connection\n"+
				"\nOriginal error: %s", errMsg)
	}

	if strings.Contains(errMsg, "authentication failed") || strings.Contains(errMsg, "password authentication failed") {
		return fmt.Sprintf(
			"Authentication failed: Invalid username or password.\n\n"+
				"Troubleshooting steps:\n"+
				"  1. Verify your username and password are correct\n"+
				"  2. Check password_command is configured correctly in config.yaml\n"+
				"  3. Ensure PGPASSWORD environment variable is set (if using env auth)\n"+
				"  4. Try interactive password prompt (clear password_command and PGPASSWORD)\n"+
				"\nOriginal error: %s", errMsg)
	}

	if strings.Contains(errMsg, "database") && strings.Contains(errMsg, "does not exist") {
		return fmt.Sprintf(
			"Database does not exist.\n\n"+
				"Troubleshooting steps:\n"+
				"  1. Verify the database name in your configuration\n"+
				"  2. Create the database: createdb <database_name>\n"+
				"  3. List available databases: psql -l\n"+
				"\nOriginal error: %s", errMsg)
	}

	if strings.Contains(errMsg, "no such host") || strings.Contains(errMsg, "unknown host") {
		return fmt.Sprintf(
			"Host not found: Cannot resolve hostname.\n\n"+
				"Troubleshooting steps:\n"+
				"  1. Verify the hostname in your configuration\n"+
				"  2. Try using IP address instead of hostname\n"+
				"  3. Check DNS resolution: ping <hostname>\n"+
				"\nOriginal error: %s", errMsg)
	}

	if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline exceeded") {
		return fmt.Sprintf(
			"Connection timeout: Database did not respond in time.\n\n"+
				"Troubleshooting steps:\n"+
				"  1. Check network connectivity to the database server\n"+
				"  2. Verify the database is not overloaded\n"+
				"  3. Check for network firewall rules blocking the connection\n"+
				"\nOriginal error: %s", errMsg)
	}

	if strings.Contains(errMsg, "SSL") || strings.Contains(errMsg, "TLS") {
		return fmt.Sprintf(
			"SSL/TLS error: Secure connection failed.\n\n"+
				"Troubleshooting steps:\n"+
				"  1. Try setting sslmode to 'disable' for testing (not recommended for production)\n"+
				"  2. Verify SSL certificates are valid\n"+
				"  3. Check if the server requires SSL (pg_hba.conf)\n"+
				"\nOriginal error: %s", errMsg)
	}

	if strings.Contains(errMsg, "permission denied") {
		return fmt.Sprintf(
			"Permission denied: User does not have required privileges.\n\n"+
				"Troubleshooting steps:\n"+
				"  1. Verify the user has CONNECT privilege on the database\n"+
				"  2. Check pg_hba.conf allows connections from your host\n"+
				"  3. Grant permissions: GRANT CONNECT ON DATABASE <db> TO <user>\n"+
				"\nOriginal error: %s", errMsg)
	}

	// Default error formatting
	return fmt.Sprintf(
		"Database connection error:\n\n"+
			"%s\n\n"+
			"Check your configuration in config.yaml or environment variables.\n"+
			"Run with --debug flag for detailed logs.", errMsg)
}

// FormatPasswordCommandError formats a password command execution error
func FormatPasswordCommandError(err error, command string) string {
	errMsg := err.Error()

	if strings.Contains(errMsg, "timed out") {
		return fmt.Sprintf(
			"Password command timed out after 5 seconds.\n\n"+
				"Command: %s\n\n"+
				"Troubleshooting steps:\n"+
				"  1. Test the command manually in your terminal\n"+
				"  2. Ensure the password manager is unlocked\n"+
				"  3. Check if the command requires user interaction\n"+
				"\nOriginal error: %s", command, errMsg)
	}

	if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "executable file not found") {
		return fmt.Sprintf(
			"Password command not found.\n\n"+
				"Command: %s\n\n"+
				"Troubleshooting steps:\n"+
				"  1. Verify the command is in your PATH\n"+
				"  2. Use absolute path to the executable\n"+
				"  3. Check if the password manager is installed\n"+
				"\nOriginal error: %s", command, errMsg)
	}

	return fmt.Sprintf(
		"Password command failed.\n\n"+
			"Command: %s\n\n"+
			"Troubleshooting steps:\n"+
			"  1. Test the command manually: %s\n"+
			"  2. Check the command output and errors\n"+
			"  3. Verify the password manager is configured correctly\n"+
			"\nOriginal error: %s", command, command, errMsg)
}

// FormatReconnectionFailure formats a permanent reconnection failure message
func FormatReconnectionFailure(err error, maxAttempts int) error {
	return fmt.Errorf(
		"Failed to reconnect after %d attempts.\n\n"+
			"The database connection could not be restored.\n\n"+
			"Troubleshooting steps:\n"+
			"  1. Verify PostgreSQL is running and accessible\n"+
			"  2. Check network connectivity\n"+
			"  3. Review database logs for errors\n"+
			"  4. Restart the application to try again\n"+
			"\nLast error: %s", maxAttempts, err.Error())
}
