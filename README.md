# Steep - PostgreSQL Monitoring TUI

A terminal-based PostgreSQL monitoring tool built with Go and [Bubbletea](https://github.com/charmbracelet/bubbletea).

## Features

- **Real-time Dashboard** - Monitor database metrics, connections, and server status
- **Multiple Views** - Dashboard, Activity, Queries, Locks, Tables, and Replication monitoring
- **Keyboard Navigation** - Vim-style and intuitive keyboard shortcuts
- **Automatic Reconnection** - Resilient connection handling with exponential backoff
- **Password Management** - Secure password handling via environment variables or commands
- **SSL/TLS Support** - Full SSL/TLS configuration including certificate verification
- **Structured Logging** - JSON-formatted logs for debugging
- **Customizable Themes** - Dark and light themes (dark by default)

## Installation

### Prerequisites

- Go 1.21 or later
- PostgreSQL 12 or later
- Terminal with 80x24 minimum dimensions

### From Source

```bash
# Clone the repository
git clone https://github.com/willibrandon/steep.git
cd steep

# Build the application
make build

# Run
./bin/steep
```

### Build Options

```bash
make build      # Build the steep binary
make clean      # Remove build artifacts
make help       # Show available targets
```

## Quick Start

### Basic Usage

1. Create a configuration file:

```bash
# Copy the example configuration
cp config.yaml.example config.yaml

# Edit with your database connection details
nano config.yaml
```

2. Set up password authentication:

```bash
# Option 1: Environment variable (for empty password)
export PGPASSWORD=""

# Option 2: Environment variable (with password)
export PGPASSWORD="your-password"

# Option 3: Use password command in config.yaml
# password_command: "security find-generic-password -s 'postgres' -w"
```

3. Run Steep:

```bash
./bin/steep
```

## Configuration

### Configuration File

Steep reads configuration from `config.yaml` in the current directory or `~/.config/steep/config.yaml`.

```yaml
connection:
  host: localhost
  port: 5432
  database: postgres
  user: postgres

  # SSL/TLS Configuration
  sslmode: prefer  # disable, allow, prefer, require, verify-ca, verify-full
  # sslrootcert: /path/to/ca.crt
  # sslcert: /path/to/client.crt
  # sslkey: /path/to/client.key

  # Connection pool settings
  pool_max_conns: 10
  pool_min_conns: 2

ui:
  theme: dark              # dark or light
  refresh_interval: 1s     # Status bar refresh rate
  date_format: "2006-01-02 15:04:05"

debug: false
```

### Environment Variables

All configuration options can be overridden with environment variables:

```bash
export STEEP_CONNECTION_HOST=localhost
export STEEP_CONNECTION_PORT=5432
export STEEP_CONNECTION_DATABASE=mydb
export STEEP_CONNECTION_USER=myuser
export STEEP_CONNECTION_SSLMODE=verify-full
export STEEP_UI_THEME=dark
export STEEP_DEBUG=true
```

### Password Configuration

Steep supports multiple password authentication methods in order of precedence:

1. **Password Command** (most secure for production)
   ```yaml
   connection:
     password_command: "security find-generic-password -s 'postgres' -w"
   ```

2. **Environment Variable**
   ```bash
   export PGPASSWORD="your-password"
   ```

3. **Interactive Prompt** (if no other method is configured)

See [Password Authentication](docs/PASSWORD_AUTH.md) for detailed setup instructions.

### SSL/TLS Configuration

Steep supports all PostgreSQL SSL modes. See [SSL Configuration Guide](docs/SSL_CONFIGURATION.md) for detailed setup.

**Quick SSL Examples:**

```yaml
# Local development (no SSL)
connection:
  sslmode: disable

# Production (SSL with CA verification)
connection:
  sslmode: verify-full
  sslrootcert: /path/to/ca-certificate.crt

# Production with client certificates
connection:
  sslmode: verify-full
  sslrootcert: /path/to/ca-certificate.crt
  sslcert: /path/to/client.crt
  sslkey: /path/to/client.key
```

## Usage

### Keyboard Shortcuts

#### Global

- `q` or `Ctrl+C` - Quit application
- `h` or `?` - Toggle help screen
- `Esc` - Close help screen

#### View Navigation

- `1` - Dashboard view
- `2` - Activity view
- `3` - Queries view
- `4` - Locks view
- `5` - Tables view
- `6` - Replication view
- `Tab` - Next view
- `Shift+Tab` - Previous view

### Views

#### Dashboard
- Database overview statistics
- Server version information
- Connection status
- Quick health metrics

#### Activity (Coming Soon)
- Real-time connection monitoring
- Session statistics
- Wait events

#### Queries (Coming Soon)
- Long-running queries
- Query performance metrics
- Statement statistics

#### Locks (Coming Soon)
- Lock monitoring
- Blocking queries
- Deadlock detection

#### Tables (Coming Soon)
- Table sizes and statistics
- Index usage
- Bloat detection

#### Replication (Coming Soon)
- Replication lag
- Replica status
- WAL statistics

### Status Bar

The status bar displays:
- Connection status indicator (●)
- Database name
- Current timestamp
- Active connections count
- Reconnection status (when applicable)

### Debug Mode

Enable debug logging to troubleshoot issues:

```bash
./bin/steep --debug
```

Logs are written to `/tmp/steep.log` (or system temp directory).

View logs in real-time:
```bash
tail -f /tmp/steep.log | jq
```

## Error Handling

Steep provides helpful error messages for common issues:

### Connection Errors
- PostgreSQL not running
- Authentication failures
- Database doesn't exist
- Network issues
- SSL/TLS errors

### Automatic Reconnection

If the database connection is lost, Steep automatically attempts to reconnect with exponential backoff:

- Attempt 1: 1 second delay
- Attempt 2: 2 seconds delay
- Attempt 3: 4 seconds delay
- Attempt 4: 8 seconds delay
- Attempt 5: 16 seconds delay
- Maximum: 5 attempts, 30 second cap

## Development

### Project Structure

```
steep/
├── cmd/steep/          # Main application entry point
├── internal/
│   ├── app/            # Application model and message handlers
│   ├── config/         # Configuration management
│   ├── db/             # Database connection and operations
│   ├── logger/         # Structured logging
│   └── ui/             # User interface components
│       ├── components/ # Reusable UI components
│       ├── views/      # View implementations
│       └── styles/     # Color schemes and styling
├── docs/               # Documentation
└── specs/              # Feature specifications
```

### Building from Source

```bash
# Install dependencies
go mod download

# Build
go build -o bin/steep cmd/steep/main.go

# Run tests
go test ./...

# Run with race detector
go run -race cmd/steep/main.go
```

### Code Style

- Follow [Effective Go](https://golang.org/doc/effective_go.html) guidelines
- Use `gofmt` for formatting
- Run `go vet` before committing
- Write meaningful commit messages

## Troubleshooting

### Terminal Too Small

```
Terminal too small: 70x20 (minimum required: 80x24)
Please resize your terminal and try again.
```

**Solution:** Resize your terminal to at least 80 columns by 24 rows.

### Connection Refused

```
Connection refused: PostgreSQL is not accepting connections.
```

**Solutions:**
1. Verify PostgreSQL is running: `brew services list | grep postgresql`
2. Check port configuration in config.yaml
3. Verify firewall settings

### Authentication Failed

```
Authentication failed: Invalid username or password.
```

**Solutions:**
1. Verify credentials in config.yaml
2. Check PGPASSWORD environment variable
3. Test password command manually
4. Try interactive password prompt

### SSL Errors

```
SSL/TLS error: Secure connection failed.
```

**Solutions:**
1. Verify sslmode is compatible with server configuration
2. Check certificate paths and permissions
3. See [SSL Configuration Guide](docs/SSL_CONFIGURATION.md)

### Permission Denied

```
Permission denied: User does not have required privileges.
```

**Solutions:**
1. Verify database user has CONNECT privilege
2. Check pg_hba.conf allows connections from your host
3. Grant required permissions

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Acknowledgments

- [Bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
- [pgx](https://github.com/jackc/pgx) - PostgreSQL driver
- [Viper](https://github.com/spf13/viper) - Configuration management

## Support

- **Issues:** [GitHub Issues](https://github.com/willibrandon/steep/issues)
- **Discussions:** [GitHub Discussions](https://github.com/willibrandon/steep/discussions)
- **Documentation:** [docs/](docs/)

## Roadmap

- [ ] Table statistics view
- [ ] Query performance view
- [ ] Lock monitoring view
- [ ] Replication monitoring
- [ ] Export metrics to Prometheus
- [ ] Alert configuration
- [ ] Light theme
- [ ] Custom color schemes
