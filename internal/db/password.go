package db

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

// GetPassword retrieves the database password using the following precedence:
// 1. Execute password_command if configured
// 2. Use PGPASSWORD environment variable if set
// 3. Prompt interactively for password
func GetPassword(passwordCommand string) (string, error) {
	// Try password_command first
	if passwordCommand != "" {
		password, err := executePasswordCommand(passwordCommand)
		if err != nil {
			return "", fmt.Errorf("password command failed: %w", err)
		}
		return password, nil
	}

	// Try PGPASSWORD environment variable (even if empty)
	if _, exists := os.LookupEnv("PGPASSWORD"); exists {
		return os.Getenv("PGPASSWORD"), nil
	}

	// Fall back to interactive prompt
	password, err := promptForPassword()
	if err != nil {
		return "", fmt.Errorf("interactive password prompt failed: %w", err)
	}

	return password, nil
}

// executePasswordCommand executes the configured password command with a 5-second timeout
func executePasswordCommand(command string) (string, error) {
	// Create context with 5-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Parse command - split on spaces (simple parsing, may need improvement for complex commands)
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty password command")
	}

	// Create command
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)

	// Capture stdout
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute command
	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out after 5 seconds")
		}
		return "", fmt.Errorf("command failed: %w (stderr: %s)", err, stderr.String())
	}

	// Trim whitespace from output
	password := strings.TrimSpace(stdout.String())
	if password == "" {
		return "", fmt.Errorf("command returned empty password")
	}

	return password, nil
}

// promptForPassword prompts the user to enter a password interactively
// The password input is hidden from the terminal
func promptForPassword() (string, error) {
	fmt.Fprint(os.Stderr, "Enter database password: ")

	// Read password with hidden input
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}

	fmt.Fprintln(os.Stderr) // Print newline after password input

	password := string(passwordBytes)
	if password == "" {
		return "", fmt.Errorf("empty password entered")
	}

	return password, nil
}

// promptForPasswordWithPrompt prompts with a custom message
func promptForPasswordWithPrompt(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)

	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}

	fmt.Fprintln(os.Stderr)

	password := string(passwordBytes)
	if password == "" {
		return "", fmt.Errorf("empty password entered")
	}

	return password, nil
}

// PromptForConnection prompts the user for connection details interactively
// This can be used when no configuration file exists
func PromptForConnection() (host, port, database, user, password string, err error) {
	reader := bufio.NewReader(os.Stdin)

	// Host
	fmt.Fprint(os.Stderr, "PostgreSQL host [localhost]: ")
	host, _ = reader.ReadString('\n')
	host = strings.TrimSpace(host)
	if host == "" {
		host = "localhost"
	}

	// Port
	fmt.Fprint(os.Stderr, "PostgreSQL port [5432]: ")
	port, _ = reader.ReadString('\n')
	port = strings.TrimSpace(port)
	if port == "" {
		port = "5432"
	}

	// Database
	fmt.Fprint(os.Stderr, "Database name [postgres]: ")
	database, _ = reader.ReadString('\n')
	database = strings.TrimSpace(database)
	if database == "" {
		database = "postgres"
	}

	// User
	fmt.Fprint(os.Stderr, "Database user: ")
	user, _ = reader.ReadString('\n')
	user = strings.TrimSpace(user)
	if user == "" {
		return "", "", "", "", "", fmt.Errorf("username cannot be empty")
	}

	// Password
	password, err = promptForPassword()
	if err != nil {
		return "", "", "", "", "", err
	}

	return host, port, database, user, password, nil
}
