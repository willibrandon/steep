# Research: Bidirectional Replication Foundation

**Date**: 2025-12-04
**Feature**: 014-repl-foundation

## 1. pgrx PostgreSQL Extension Framework

### Decision: Use pgrx with Cargo features for version gating

**Rationale**: pgrx is the standard Rust framework for PostgreSQL extensions, supporting PG13-18 with compile-time version gating via Cargo features. This aligns with our PG18 requirement.

**Alternatives considered**:
- Raw C extensions: More complex, no memory safety
- pgx (Go): Would require separate build system; Rust preferred for extension

### Key Implementation Patterns

**Schema and Table Creation**:
```rust
extension_sql!(
    r#"
    CREATE SCHEMA steep_repl;

    CREATE TABLE steep_repl.nodes (
        node_id TEXT PRIMARY KEY,
        node_name TEXT NOT NULL,
        host TEXT NOT NULL,
        port INTEGER NOT NULL DEFAULT 5432,
        priority INTEGER NOT NULL DEFAULT 50,
        is_coordinator BOOLEAN NOT NULL DEFAULT false,
        last_seen TIMESTAMPTZ,
        status TEXT NOT NULL DEFAULT 'unknown'
    );
    "#,
    name = "create_schema",
    bootstrap,
);
```

**PostgreSQL 18 Requirement**:
```toml
# Cargo.toml - Only enable pg18 feature
[features]
default = ["pg18"]
pg18 = ["pgrx/pg18"]
# Remove pg13-pg17 features entirely
```

**Cross-Platform Build Matrix**:
| Platform | Output | Build Command |
|----------|--------|---------------|
| Linux x86_64 | `.so` | `cargo pgrx package --pg18` |
| Linux arm64 | `.so` | `cargo pgrx package --pg18 --target aarch64-unknown-linux-gnu` |
| macOS arm64 | `.dylib` | `cargo pgrx package --pg18` |
| Windows x64 | `.dll` | `cargo pgrx package --pg18` |

**Testing**:
```rust
#[cfg(any(test, feature = "pg_test"))]
#[pg_schema]
mod tests {
    use pgrx::prelude::*;

    #[pg_test]
    fn test_schema_exists() -> Result<(), spi::Error> {
        let result = Spi::get_one::<bool>(
            "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'steep_repl')"
        )?;
        assert_eq!(result, Some(true));
        Ok(())
    }
}
```

---

## 2. kardianos/service Cross-Platform Service Management

### Decision: Follow existing steep-agent patterns

**Rationale**: steep-agent already implements robust cross-platform service management. Reusing these patterns ensures consistency and leverages proven code.

**Alternatives considered**:
- Custom service management: Unnecessary reinvention
- systemd-only: Not cross-platform

### Key Implementation Patterns

**Service Configuration Structure**:
```go
type ServiceConfig struct {
    ConfigPath string
    UserMode   bool
    Debug      bool
}

svcConfig := &service.Config{
    Name:        "steep-repl",
    DisplayName: "Steep Replication Coordinator",
    Description: "Coordinates bidirectional PostgreSQL replication",
    Arguments:   []string{"run", "--config", configPath},
}
```

**Platform-Specific Options**:
```go
switch runtime.GOOS {
case "darwin":
    svcConfig.Option = service.KeyValue{
        "KeepAlive": true,
        "RunAtLoad": true,
    }
case "linux":
    svcConfig.Option = service.KeyValue{
        "Restart": "on-failure",
    }
case "windows":
    svcConfig.Option = service.KeyValue{
        "OnFailure":              "restart",
        "OnFailureDelayDuration": "5s",
    }
}
```

**Logging to Platform System Log**:
- Use kardianos/service's built-in system logger
- Falls back to file logging via lumberjack for application logs
- Windows: Event Log; Linux: syslog; macOS: os_log

---

## 3. Cross-Platform IPC (Named Pipes / Unix Sockets)

### Decision: Use go-winio for Windows, net.Listen("unix") for Unix

**Rationale**: go-winio is the de facto standard for Windows named pipes (used by Docker, containerd). Standard library handles Unix sockets.

**Alternatives considered**:
- natefinch/npipe: Less active maintenance
- golang-ipc: Additional abstraction layer not needed

### Key Implementation Patterns

**Cross-Platform Listener**:
```go
// internal/repl/ipc/listener.go
func NewIPCListener(name string) (net.Listener, error) {
    if runtime.GOOS == "windows" {
        return winio.ListenPipe(`\\.\pipe\`+name, nil)
    }
    sockPath := filepath.Join(os.TempDir(), name+".sock")
    // Clean up stale socket
    os.Remove(sockPath)
    return net.Listen("unix", sockPath)
}
```

**Naming Convention**:
| Platform | Path |
|----------|------|
| Windows | `\\.\pipe\steep-repl` |
| Linux/macOS | `/tmp/steep-repl.sock` |

**Stale Endpoint Cleanup**:
- Unix: `os.Remove(sockPath)` before `net.Listen()`
- Windows: Automatic via `FILE_FLAG_FIRST_PIPE_INSTANCE` or connection timeout

**Error Handling**:
```go
conn, err := listener.Accept()
if err != nil {
    if err == winio.ErrPipeListenerClosed {
        return // Graceful shutdown
    }
    log.Printf("accept error: %v", err)
    continue
}
go handleConnection(conn)
```

---

## 4. gRPC with Mutual TLS (mTLS)

### Decision: Use grpc-go with RequireAndVerifyClientCert

**Rationale**: mTLS provides strongest authentication for node-to-node communication. Both nodes verify each other's certificates.

**Alternatives considered**:
- TLS without client auth: Weaker security
- Shared secret: Key management complexity
- No encryption: Unacceptable for replication coordination

### Key Implementation Patterns

**Server mTLS Configuration**:
```go
func loadServerCredentials(certFile, keyFile, clientCAFile string) (credentials.TransportCredentials, error) {
    serverCert, _ := tls.LoadX509KeyPair(certFile, keyFile)

    pemClientCA, _ := ioutil.ReadFile(clientCAFile)
    certPool := x509.NewCertPool()
    certPool.AppendCertsFromPEM(pemClientCA)

    config := &tls.Config{
        Certificates: []tls.Certificate{serverCert},
        ClientAuth:   tls.RequireAndVerifyClientCert,
        ClientCAs:    certPool,
        MinVersion:   tls.VersionTLS13,
    }

    return credentials.NewTLS(config), nil
}
```

**Client mTLS Configuration**:
```go
func loadClientCredentials(clientCertFile, clientKeyFile, serverCAFile string) (credentials.TransportCredentials, error) {
    clientCert, _ := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)

    pemServerCA, _ := ioutil.ReadFile(serverCAFile)
    certPool := x509.NewCertPool()
    certPool.AppendCertsFromPEM(pemServerCA)

    config := &tls.Config{
        Certificates: []tls.Certificate{clientCert},
        RootCAs:      certPool,
        MinVersion:   tls.VersionTLS13,
    }

    return credentials.NewTLS(config), nil
}
```

**Certificate File Layout**:
```
~/.config/steep/certs/
├── ca-cert.pem           # Cluster CA certificate
├── node-cert.pem         # This node's certificate
└── node-key.pem          # This node's private key
```

**gRPC Health Check Service**:
```go
import "google.golang.org/grpc/health"
import "google.golang.org/grpc/health/grpc_health_v1"

healthServer := health.NewServer()
grpc_health_v1.RegisterHealthServer(server, healthServer)
healthServer.SetServingStatus("steep.repl.v1.Coordinator", grpc_health_v1.HealthCheckResponse_SERVING)
```

---

## 5. Configuration Integration

### Decision: Extend existing config.yaml with repl section

**Rationale**: Single config file for all Steep components (TUI, agent, repl). Viper already handles platform-specific paths.

### Configuration Structure

```yaml
# ~/.config/steep/config.yaml
repl:
  enabled: true
  node_id: "node-a"
  node_name: "Primary Node"

  postgresql:
    host: localhost
    port: 5432
    database: postgres
    user: steep_repl
    password_command: "pass show steep/repl"

  grpc:
    port: 5433
    tls:
      cert_file: ~/.config/steep/certs/node-cert.pem
      key_file: ~/.config/steep/certs/node-key.pem
      ca_file: ~/.config/steep/certs/ca-cert.pem

  http:
    enabled: true
    port: 8080

  ipc:
    enabled: true
    # Path auto-detected based on platform
```

---

## 6. Testing Strategy

### Unit Tests
- **Rust (extension)**: `cargo pgrx test --pg18`
- **Go (daemon)**: `go test ./internal/repl/...`

### Integration Tests
- Use testcontainers with PostgreSQL 18 image
- Test extension installation via SQL
- Test daemon connectivity and IPC

### Cross-Platform Testing
- GitHub Actions matrix: `[windows-latest, ubuntu-latest, macos-latest]`
- Windows is primary target; test first

---

## Dependencies Summary

| Component | Dependency | Version | Purpose |
|-----------|------------|---------|---------|
| Extension | pgrx | latest | PostgreSQL extension framework |
| Daemon | kardianos/service | 1.2.4 | Cross-platform service management |
| Daemon | pgx/v5 | 5.7.x | PostgreSQL driver with pooling |
| Daemon | Microsoft/go-winio | latest | Windows named pipes |
| Daemon | grpc-go | latest | Node-to-node communication |
| Daemon | google.golang.org/grpc/health | - | gRPC health check service |
