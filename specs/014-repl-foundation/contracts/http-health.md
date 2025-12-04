# HTTP Health Endpoint

**Port**: Configurable (default 8080)
**Path**: `/health`
**Method**: `GET`

## Endpoint: GET /health

Returns the health status of the steep-repl daemon and its components.

### Response

**Status Codes**:
- `200 OK` - All components healthy
- `503 Service Unavailable` - One or more components unhealthy

**Content-Type**: `application/json`

### Response Body

```json
{
  "status": "healthy",
  "components": {
    "postgresql": {
      "healthy": true,
      "status": "connected",
      "version": "18.0",
      "latency_ms": 2
    },
    "grpc": {
      "healthy": true,
      "status": "listening",
      "port": 5433
    },
    "ipc": {
      "healthy": true,
      "status": "listening"
    }
  },
  "node": {
    "node_id": "node-a",
    "node_name": "Primary Node",
    "is_coordinator": true
  },
  "uptime_seconds": 3600,
  "version": "0.1.0"
}
```

### Unhealthy Response Example

```json
{
  "status": "unhealthy",
  "components": {
    "postgresql": {
      "healthy": false,
      "status": "disconnected",
      "error": "connection refused"
    },
    "grpc": {
      "healthy": true,
      "status": "listening",
      "port": 5433
    },
    "ipc": {
      "healthy": true,
      "status": "listening"
    }
  },
  "node": {
    "node_id": "node-a",
    "node_name": "Primary Node",
    "is_coordinator": false
  },
  "uptime_seconds": 120,
  "version": "0.1.0"
}
```

### Health Determination Logic

Overall status is `healthy` if:
1. PostgreSQL connection is established
2. gRPC server is listening (if enabled)
3. IPC listener is active

If any required component is unhealthy, overall status becomes `unhealthy` and HTTP status code is 503.

---

## Endpoint: GET /ready

Kubernetes-style readiness probe. Returns 200 only when the daemon is fully initialized and ready to serve requests.

**Status Codes**:
- `200 OK` - Ready to serve
- `503 Service Unavailable` - Not ready

**Response Body**:
```json
{
  "ready": true
}
```

or

```json
{
  "ready": false,
  "reason": "postgresql connection not established"
}
```

---

## Endpoint: GET /live

Kubernetes-style liveness probe. Returns 200 if the daemon process is alive.

**Status Codes**:
- `200 OK` - Alive

**Response Body**:
```json
{
  "alive": true
}
```

---

## Go Implementation

```go
// internal/repl/health/http.go

type HealthResponse struct {
    Status     string                     `json:"status"`
    Components map[string]ComponentHealth `json:"components"`
    Node       NodeInfo                   `json:"node"`
    Uptime     int64                      `json:"uptime_seconds"`
    Version    string                     `json:"version"`
}

type ComponentHealth struct {
    Healthy   bool   `json:"healthy"`
    Status    string `json:"status"`
    Version   string `json:"version,omitempty"`
    Port      int    `json:"port,omitempty"`
    LatencyMs int64  `json:"latency_ms,omitempty"`
    Error     string `json:"error,omitempty"`
}

type NodeInfo struct {
    NodeID        string `json:"node_id"`
    NodeName      string `json:"node_name"`
    IsCoordinator bool   `json:"is_coordinator"`
}

func (h *HTTPServer) healthHandler(w http.ResponseWriter, r *http.Request) {
    resp := h.buildHealthResponse()

    status := http.StatusOK
    if resp.Status != "healthy" {
        status = http.StatusServiceUnavailable
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(resp)
}
```

---

## Configuration

```yaml
# config.yaml
repl:
  http:
    enabled: true
    port: 8080
    # Optional: bind address (default: 0.0.0.0)
    bind: "0.0.0.0"
```

If `http.enabled` is false, no HTTP server is started.
