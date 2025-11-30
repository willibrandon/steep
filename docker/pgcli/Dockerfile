# Multi-arch pgcli Docker image
# Build: docker buildx build --platform linux/amd64,linux/arm64 -t willibrandon/pgcli:latest --push .

FROM python:3.12-slim

# Install pgcli with psycopg-binary (includes libpq bindings)
RUN pip install --no-cache-dir pgcli psycopg-binary

# Default entrypoint
ENTRYPOINT ["pgcli"]
