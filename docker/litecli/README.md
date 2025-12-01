# litecli Docker Image

Multi-architecture Docker image for [litecli](https://litecli.com/) - a SQLite CLI with auto-completion and syntax highlighting.

## Quick Start

```bash
# Pull the image
docker pull willibrandon/litecli:latest

# Run litecli
docker run --rm -it -v /path/to/database.db:/data/db.db willibrandon/litecli /data/db.db
```

## Why This Image?

There is no official litecli Docker image. This image provides:
- Multi-architecture: `linux/amd64` and `linux/arm64`
- Current litecli version from PyPI
- Updated with each Steep release

## Supported Architectures

| Architecture | Platform |
|--------------|----------|
| amd64 | Intel/AMD x86_64 |
| arm64 | Apple Silicon (M1/M2/M3/M4), AWS Graviton, etc. |

## Usage with Steep

The Steep SQL Editor uses this image for the `:repl docker` command:

```
:repl docker sqlite   # Auto-detect litecli or sqlite3 via Docker
:repl docker litecli  # Force Docker litecli specifically
:repl docker sqlite3  # Force Docker sqlite3 (keinos/sqlite3)
```

### Fallback Order

When you run `:repl sqlite` in Steep:
1. Local `litecli` (if installed)
2. Local `sqlite3` (if installed)
3. Docker `willibrandon/litecli`
4. Docker `keinos/sqlite3`

## Building Locally

```bash
# Build for current platform
docker build -t litecli:local .

# Build multi-arch and push
docker buildx build --platform linux/amd64,linux/arm64 \
  -t willibrandon/litecli:latest \
  --push .
```

## Image Details

- **Base**: `python:3.12-slim`
- **Packages**: `litecli`
- **Entrypoint**: `litecli`

## Tags

| Tag | Description |
|-----|-------------|
| `latest` | Most recent build |

## Updating for New litecli Releases

When a new litecli version is released on [PyPI](https://pypi.org/project/litecli/):

1. **Check current version:**
   ```bash
   pip index versions litecli
   # or
   curl -s https://pypi.org/pypi/litecli/json | jq -r '.info.version'
   ```

2. **Build and push:**
   ```bash
   cd docker/litecli

   # Build multi-arch and push
   docker buildx build --platform linux/amd64,linux/arm64 \
     -t willibrandon/litecli:latest \
     --push .
   ```

3. **Verify the push:**
   ```bash
   docker manifest inspect willibrandon/litecli:latest
   ```

Note: The Dockerfile uses `pip install litecli` without pinning, so rebuilding automatically picks up the latest version.

## Links

- [Docker Hub](https://hub.docker.com/r/willibrandon/litecli)
- [litecli Documentation](https://litecli.com/)
- [litecli on PyPI](https://pypi.org/project/litecli/)
- [Steep Project](https://github.com/willibrandon/steep)
