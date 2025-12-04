# PostgreSQL 18 + steep_repl Docker Image

Docker image for integration testing that includes PostgreSQL 18 with the `steep_repl` extension pre-installed.

## Image

```
ghcr.io/willibrandon/pg18-steep-repl:latest
```

## Building

Build multi-architecture image (amd64 + arm64):

```bash
# From repository root
docker buildx build \
  -f tests/integration/repl/Dockerfile.pg18-pgrx \
  -t ghcr.io/willibrandon/pg18-steep-repl:latest \
  --platform linux/amd64,linux/arm64 \
  .
```

Push to registry:

```bash
docker push ghcr.io/willibrandon/pg18-steep-repl:latest
```

Build for local testing (single architecture, faster):

```bash
# From repository root
docker build \
  -f tests/integration/repl/Dockerfile.pg18-pgrx \
  -t ghcr.io/willibrandon/pg18-steep-repl:latest \
  .
```

## Features

- PostgreSQL 18 (bookworm)
- `steep_repl` extension pre-compiled and installed
- Extension auto-created on first startup via init script
- Multi-architecture support (amd64, arm64)

## Usage

```bash
# Run container
docker run -d \
  --name steep-pg18 \
  -e POSTGRES_PASSWORD=test \
  -p 5432:5432 \
  ghcr.io/willibrandon/pg18-steep-repl:latest

# Connect and verify extension
psql -h localhost -U postgres -c "SELECT * FROM pg_extension WHERE extname = 'steep_repl';"
```

## Integration Tests

The integration tests in this directory use testcontainers-go to automatically pull and run this image:

```bash
# Run all repl integration tests
go test -v ./tests/integration/repl/...

# Run specific test
go test -v ./tests/integration/repl/... -run TestExtension_Install
```
