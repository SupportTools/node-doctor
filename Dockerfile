# Dockerfile for Node Doctor - Kubernetes node monitoring and auto-remediation
# Multi-stage build for minimal production image

# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache \
    git \
    make \
    gcc \
    musl-dev \
    linux-headers

# Set working directory
WORKDIR /build

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
# CGO_ENABLED=1 required for some system calls (statfs, etc.)
# -ldflags for build metadata injection
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME} -w -s" \
    -o node-doctor \
    ./cmd/node-doctor

# Runtime stage - use alpine for compatibility with system calls
FROM alpine:3.19

# Install runtime dependencies
# ca-certificates: HTTPS support for Kubernetes API
# tzdata: Timezone data for accurate logging
RUN apk add --no-cache \
    ca-certificates \
    tzdata

# Create non-root user (will run as root in DaemonSet due to privileged requirements, but good practice)
RUN addgroup -S node-doctor && adduser -S node-doctor -G node-doctor

# Copy binary from builder
COPY --from=builder /build/node-doctor /usr/local/bin/node-doctor

# Ensure binary is executable
RUN chmod +x /usr/local/bin/node-doctor

# Create configuration directory
RUN mkdir -p /etc/node-doctor && \
    chown -R node-doctor:node-doctor /etc/node-doctor

# Expose ports
# 8080: HTTP health/status endpoint
# 9100: Prometheus metrics endpoint
EXPOSE 8080 9100

# Set working directory
WORKDIR /

# Health check (will be overridden by Kubernetes probes)
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/usr/local/bin/node-doctor", "--version"]

# Default command
# In DaemonSet, this will be overridden with config file path
ENTRYPOINT ["/usr/local/bin/node-doctor"]
CMD ["--help"]

# Metadata labels
LABEL org.opencontainers.image.title="Node Doctor" \
      org.opencontainers.image.description="Kubernetes node monitoring and auto-remediation DaemonSet" \
      org.opencontainers.image.vendor="SupportTools" \
      org.opencontainers.image.url="https://github.com/supporttools/node-doctor" \
      org.opencontainers.image.source="https://github.com/supporttools/node-doctor" \
      org.opencontainers.image.licenses="Apache-2.0"
