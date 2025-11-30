# pgcli Docker Image

Multi-architecture Docker image for [pgcli](https://www.pgcli.com/) - a PostgreSQL CLI with auto-completion and syntax highlighting.

## Quick Start

```bash
# Pull the image
docker pull willibrandon/pgcli:latest

# Run pgcli
docker run --rm -it --net host willibrandon/pgcli "postgres://user:pass@localhost:5432/db"
```

## Why This Image?

The official `dbcliorg/pgcli` image:
- Last updated April 2022 (stuck at v3.4.1)
- Only supports `linux/amd64` (no ARM64/Apple Silicon)

This image:
- Current version (v4.3.0)
- Multi-architecture: `linux/amd64` and `linux/arm64`
- Includes `psycopg-binary` for libpq bindings
- Updated with each Steep release

## Supported Architectures

| Architecture | Platform |
|--------------|----------|
| amd64 | Intel/AMD x86_64 |
| arm64 | Apple Silicon (M1/M2/M3/M4), AWS Graviton, etc. |

## Usage with Steep

The Steep SQL Editor uses this image for the `:repl docker` command:

```
:repl docker        # Auto-detect pgcli or psql via Docker
:repl docker pgcli  # Force Docker pgcli specifically
:repl docker psql   # Force Docker psql (postgres:alpine)
```

### Fallback Order

When you run `:repl` in Steep:
1. Local `pgcli` (if installed)
2. Local `psql` (if installed)
3. Docker `willibrandon/pgcli`
4. Docker `postgres:alpine` (psql)

### Windows Support

On Windows, Steep automatically replaces `localhost` with `host.docker.internal` in connection strings, since Docker runs in a VM.

## Building Locally

```bash
# Build for current platform
docker build -t pgcli:local .

# Build multi-arch and push
docker buildx build --platform linux/amd64,linux/arm64 \
  -t willibrandon/pgcli:latest \
  -t willibrandon/pgcli:4.3.0 \
  --push .
```

## Image Details

- **Base**: `python:3.12-slim`
- **Packages**: `pgcli`, `psycopg-binary`
- **Entrypoint**: `pgcli`

## Tags

| Tag | Description |
|-----|-------------|
| `latest` | Most recent build |
| `4.3.0` | Specific pgcli version |

## Updating for New pgcli Releases

When a new pgcli version is released on [PyPI](https://pypi.org/project/pgcli/):

1. **Check current version:**
   ```bash
   pip index versions pgcli
   # or
   curl -s https://pypi.org/pypi/pgcli/json | jq -r '.info.version'
   ```

2. **Build and push with new tag:**
   ```bash
   cd docker/pgcli

   # Build multi-arch and push (replace X.Y.Z with new version)
   docker buildx build --platform linux/amd64,linux/arm64 \
     -t willibrandon/pgcli:latest \
     -t willibrandon/pgcli:X.Y.Z \
     --push .
   ```

3. **Verify the push:**
   ```bash
   docker manifest inspect willibrandon/pgcli:latest
   ```

4. **Update documentation** if needed (this README, CLAUDE.md)

Note: The Dockerfile uses `pip install pgcli` without pinning, so rebuilding automatically picks up the latest version. Version tags are for reference only.

## Links

- [Docker Hub](https://hub.docker.com/r/willibrandon/pgcli)
- [pgcli Documentation](https://www.pgcli.com/)
- [pgcli Releases](https://github.com/dbcli/pgcli/releases)
- [Steep Project](https://github.com/willibrandon/steep)
