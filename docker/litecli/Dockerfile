# Multi-arch litecli Docker image
# Build: docker buildx build --platform linux/amd64,linux/arm64 -t willibrandon/litecli:latest --push .

FROM python:3.12-slim

# Install litecli
RUN pip install --no-cache-dir litecli

# Default entrypoint
ENTRYPOINT ["litecli"]
