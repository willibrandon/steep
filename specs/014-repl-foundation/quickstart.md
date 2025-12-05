# Quickstart: Bidirectional Replication Foundation

This guide covers building, installing, and running the steep_repl extension and steep-repl daemon.

## Prerequisites

- PostgreSQL 18 installed and running
- Rust toolchain (latest stable) with cargo-pgrx
- Go 1.21+
- Administrator/sudo access for service installation

## 1. Build the Extension

```bash
# Navigate to extension directory
cd extensions/steep_repl

# Initialize pgrx for PostgreSQL 18
cargo pgrx init --pg18 $(which pg_config)

# Build the extension
cargo pgrx package --pg18

# Install to PostgreSQL
cargo pgrx install --pg18
```

## 2. Install the Extension

Connect to PostgreSQL and create the extension:

```sql
-- Connect to your database
psql -U postgres -d mydb

-- Create the extension
CREATE EXTENSION steep_repl;

-- Verify installation
\dt steep_repl.*

-- Expected output:
--              List of relations
--   Schema   |       Name        | Type  | Owner
-- -----------+-------------------+-------+-------
--  steep_repl | audit_log         | table | postgres
--  steep_repl | coordinator_state | table | postgres
--  steep_repl | nodes             | table | postgres
```

## 3. Build the Daemon

```bash
# From repository root
cd /path/to/steep

# Build steep-repl daemon
go build -o bin/steep-repl ./cmd/steep-repl

# Verify build
./bin/steep-repl --version
```

## 4. Configure the Daemon

Create or edit the configuration file:

**Linux/macOS**: `~/.config/steep/config.yaml`
**Windows**: `%APPDATA%\steep\config.yaml`

```yaml
# Replication daemon configuration
repl:
  enabled: true
  node_id: "node-a"
  node_name: "Primary Node"

  # PostgreSQL connection
  postgresql:
    host: localhost
    port: 5432
    database: postgres
    user: postgres
    # Use password_command for secure credential management
    password_command: "pass show postgres/local"

  # gRPC server (node-to-node)
  grpc:
    port: 5433
    tls:
      cert_file: ~/.config/steep/certs/node-cert.pem
      key_file: ~/.config/steep/certs/node-key.pem
      ca_file: ~/.config/steep/certs/ca-cert.pem

  # HTTP health endpoint
  http:
    enabled: true
    port: 8080

  # IPC (TUI communication)
  ipc:
    enabled: true
```

## 5. Generate TLS Certificates (for multi-node)

```bash
# Create certificates directory
mkdir -p ~/.config/steep/certs
cd ~/.config/steep/certs

# Generate CA key and certificate
openssl ecparam -name prime256v1 -genkey -noout -out ca-key.pem
openssl req -x509 -new -nodes -key ca-key.pem \
    -subj "/CN=SteepReplCA" -days 730 -out ca-cert.pem

# Generate node key and certificate
openssl ecparam -name prime256v1 -genkey -noout -out node-key.pem
openssl req -new -key node-key.pem \
    -subj "/CN=node-a" -out node.csr
openssl x509 -req -in node.csr -CA ca-cert.pem -CAkey ca-key.pem \
    -CAcreateserial -out node-cert.pem -days 365

# Cleanup CSR
rm node.csr
```

## 6. Install and Start the Service

### Linux (systemd)

```bash
# Install as system service
sudo ./bin/steep-repl install

# Start the service
sudo ./bin/steep-repl start

# Check status
sudo ./bin/steep-repl status
```

### macOS (launchd)

```bash
# Install as user service
./bin/steep-repl install --user

# Start the service
./bin/steep-repl start

# Check status
./bin/steep-repl status
```

### Windows (SCM)

```powershell
# Run as Administrator
.\bin\steep-repl.exe install

# Start the service
.\bin\steep-repl.exe start

# Check status
.\bin\steep-repl.exe status
```

## 7. Verify Installation

### Check daemon status

```bash
./bin/steep-repl status

# Expected output:
# Steep Replication Daemon
# State:        running
# PID:          12345
# Uptime:       5m32s
# PostgreSQL:   connected (18.0)
# gRPC:         listening on :5433
# IPC:          listening
# Node:         node-a (coordinator)
```

### Check HTTP health endpoint

```bash
curl http://localhost:8080/health | jq

# Expected output:
# {
#   "status": "healthy",
#   "components": {
#     "postgresql": {"healthy": true, "status": "connected", "version": "18.0"},
#     "grpc": {"healthy": true, "status": "listening", "port": 5433},
#     "ipc": {"healthy": true, "status": "listening"}
#   },
#   ...
# }
```

### Check PostgreSQL tables

```sql
-- Verify node registration
SELECT * FROM steep_repl.nodes;

-- Check audit log
SELECT occurred_at, action, actor, success
FROM steep_repl.audit_log
ORDER BY occurred_at DESC
LIMIT 10;
```

## 8. Common Commands

```bash
# Service management
steep-repl install [--user]  # Install service
steep-repl uninstall         # Remove service
steep-repl start             # Start service
steep-repl stop              # Stop service
steep-repl restart           # Restart service
steep-repl status [--json]   # Show status

# Debug mode (foreground)
steep-repl run --debug

# Check remote node health
steep-repl health --remote node-b:5433

# View logs
steep-repl logs [-f] [-n 50]
```

## 9. Troubleshooting

### Extension installation fails

```
ERROR: steep_repl requires PostgreSQL 18 or later
```

**Solution**: Upgrade to PostgreSQL 18. This extension requires PG18 features.

### Daemon fails to connect

```
ERROR: failed to connect to PostgreSQL: connection refused
```

**Solution**: Verify PostgreSQL is running and credentials are correct:
```bash
psql -h localhost -U postgres -d postgres -c "SELECT version();"
```

### gRPC TLS handshake fails

```
ERROR: transport: authentication handshake failed
```

**Solution**: Verify certificate files exist and are readable:
```bash
ls -la ~/.config/steep/certs/
openssl verify -CAfile ca-cert.pem node-cert.pem
```

### Service won't start (Windows)

**Solution**: Run Command Prompt or PowerShell as Administrator:
```powershell
# Check Windows Event Log
Get-EventLog -LogName Application -Source steep-repl -Newest 10
```

### IPC connection refused

```
steep-repl: disconnected
```

**Solution**: Verify daemon is running and IPC is enabled:
```bash
# Check if socket/pipe exists
# Linux/macOS:
ls -la /tmp/steep-repl.sock

# Windows (PowerShell):
[System.IO.Directory]::GetFiles("\\.\pipe\") | Select-String "steep"
```

## 10. Next Steps

- **Add more nodes**: Install daemon on additional PostgreSQL instances
- **Configure replication**: See feature 014-b for schema synchronization
- **Set up identity ranges**: See feature 014-c for conflict-free inserts
- **Configure alerts**: Add steep-repl alerts in config.yaml
