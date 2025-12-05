# IPC Message Protocol

**Transport**: Named pipes (Windows) / Unix sockets (Linux, macOS)
**Encoding**: JSON with newline delimiter (JSONL)
**Path**:
- Windows: `\\.\pipe\steep-repl`
- Linux/macOS: `/tmp/steep-repl.sock`

## Protocol

Each message is a single line of JSON followed by `\n`. The TUI (client) sends requests; the daemon (server) sends responses.

### Request Format

```json
{
  "id": "uuid-v4",
  "method": "method.name",
  "params": { ... }
}
```

### Response Format

```json
{
  "id": "uuid-v4",
  "result": { ... },
  "error": null
}
```

or on error:

```json
{
  "id": "uuid-v4",
  "result": null,
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable error message"
  }
}
```

---

## Methods

### status.get

Get current daemon status.

**Request**:
```json
{
  "id": "...",
  "method": "status.get",
  "params": {}
}
```

**Response**:
```json
{
  "id": "...",
  "result": {
    "state": "running",
    "pid": 12345,
    "uptime_seconds": 3600,
    "start_time": "2025-12-04T10:00:00Z",
    "version": "0.1.0",
    "postgresql": {
      "connected": true,
      "version": "18.0",
      "host": "localhost",
      "port": 5432
    },
    "grpc": {
      "listening": true,
      "port": 5433,
      "tls_enabled": true
    },
    "node": {
      "node_id": "node-a",
      "node_name": "Primary Node",
      "is_coordinator": true,
      "cluster_size": 2
    }
  },
  "error": null
}
```

---

### health.check

Get component health status.

**Request**:
```json
{
  "id": "...",
  "method": "health.check",
  "params": {}
}
```

**Response**:
```json
{
  "id": "...",
  "result": {
    "status": "healthy",
    "components": {
      "postgresql": {
        "healthy": true,
        "status": "connected",
        "latency_ms": 2
      },
      "grpc": {
        "healthy": true,
        "status": "listening"
      },
      "ipc": {
        "healthy": true,
        "status": "listening"
      }
    }
  },
  "error": null
}
```

---

### nodes.list

List all nodes in the cluster.

**Request**:
```json
{
  "id": "...",
  "method": "nodes.list",
  "params": {
    "status_filter": ["healthy", "degraded"]
  }
}
```

**Response**:
```json
{
  "id": "...",
  "result": {
    "nodes": [
      {
        "node_id": "node-a",
        "node_name": "Primary Node",
        "host": "192.168.1.10",
        "port": 5432,
        "priority": 80,
        "is_coordinator": true,
        "last_seen": "2025-12-04T12:00:00Z",
        "status": "healthy"
      },
      {
        "node_id": "node-b",
        "node_name": "Secondary Node",
        "host": "192.168.1.11",
        "port": 5432,
        "priority": 50,
        "is_coordinator": false,
        "last_seen": "2025-12-04T11:59:55Z",
        "status": "healthy"
      }
    ],
    "coordinator_id": "node-a"
  },
  "error": null
}
```

---

### nodes.get

Get single node details.

**Request**:
```json
{
  "id": "...",
  "method": "nodes.get",
  "params": {
    "node_id": "node-a"
  }
}
```

**Response**:
```json
{
  "id": "...",
  "result": {
    "node": {
      "node_id": "node-a",
      "node_name": "Primary Node",
      "host": "192.168.1.10",
      "port": 5432,
      "priority": 80,
      "is_coordinator": true,
      "last_seen": "2025-12-04T12:00:00Z",
      "status": "healthy"
    }
  },
  "error": null
}
```

---

### audit.query

Query audit log entries.

**Request**:
```json
{
  "id": "...",
  "method": "audit.query",
  "params": {
    "limit": 100,
    "offset": 0,
    "action_filter": ["node.registered", "coordinator.elected"],
    "since": "2025-12-04T00:00:00Z"
  }
}
```

**Response**:
```json
{
  "id": "...",
  "result": {
    "entries": [
      {
        "id": 1,
        "occurred_at": "2025-12-04T10:00:00Z",
        "action": "daemon.started",
        "actor": "steep_repl@localhost",
        "target_type": "daemon",
        "target_id": "node-a",
        "success": true
      }
    ],
    "total": 1,
    "has_more": false
  },
  "error": null
}
```

---

## Error Codes

| Code | Description |
|------|-------------|
| `INVALID_REQUEST` | Malformed JSON or missing required field |
| `METHOD_NOT_FOUND` | Unknown method name |
| `INTERNAL_ERROR` | Server-side error |
| `NOT_CONNECTED` | PostgreSQL not connected |
| `NODE_NOT_FOUND` | Requested node_id does not exist |
| `PERMISSION_DENIED` | Operation not allowed |

---

## Go Types

```go
// internal/repl/ipc/messages.go

type Request struct {
    ID     string          `json:"id"`
    Method string          `json:"method"`
    Params json.RawMessage `json:"params"`
}

type Response struct {
    ID     string          `json:"id"`
    Result json.RawMessage `json:"result,omitempty"`
    Error  *Error          `json:"error,omitempty"`
}

type Error struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}

// Status
type StatusResult struct {
    State         string          `json:"state"`
    PID           int             `json:"pid"`
    UptimeSeconds int64           `json:"uptime_seconds"`
    StartTime     time.Time       `json:"start_time"`
    Version       string          `json:"version"`
    PostgreSQL    PostgreSQLInfo  `json:"postgresql"`
    GRPC          GRPCInfo        `json:"grpc"`
    Node          NodeInfo        `json:"node"`
}

type PostgreSQLInfo struct {
    Connected bool   `json:"connected"`
    Version   string `json:"version,omitempty"`
    Host      string `json:"host,omitempty"`
    Port      int    `json:"port,omitempty"`
}

type GRPCInfo struct {
    Listening  bool `json:"listening"`
    Port       int  `json:"port,omitempty"`
    TLSEnabled bool `json:"tls_enabled"`
}

type NodeInfo struct {
    NodeID        string `json:"node_id"`
    NodeName      string `json:"node_name"`
    IsCoordinator bool   `json:"is_coordinator"`
    ClusterSize   int    `json:"cluster_size"`
}
```
