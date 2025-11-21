# Dockerfile for Node Doctor - Kubernetes node monitoring and auto-remediation
# Optimized multi-stage build for minimal production image (<50MB target)

# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies (git for go mod download)
RUN apk add --no-cache git

# Set working directory
WORKDIR /build

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with static linking
# CGO_ENABLED=0 creates a fully static binary (no C dependencies)
# -ldflags for build metadata injection and binary stripping
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

# Use TARGETARCH from buildx for multi-platform builds
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags="-X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME} -w -s" \
    -trimpath \
    -o node-doctor \
    ./cmd/node-doctor

# Runtime stage - use distroless for minimal size
FROM gcr.io/distroless/static-debian12:nonroot

# Copy binary from builder
COPY --from=builder /build/node-doctor /usr/local/bin/node-doctor

# Expose ports
# 8080: HTTP health/status endpoint
# 9100: Prometheus metrics endpoint
EXPOSE 8080 9100

# Set working directory
WORKDIR /

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
