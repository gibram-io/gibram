# Multi-stage build for minimal image size
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary (static linking)
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.Version=${VERSION:-dev}" \
    -o gibram-server ./cmd/server

# Final stage - minimal Alpine image
FROM alpine:3.19

# Install ca-certificates for TLS and netcat for healthcheck
RUN apk add --no-cache ca-certificates tzdata netcat-openbsd

# Create non-root user
RUN addgroup -g 1000 gibram && \
    adduser -D -u 1000 -G gibram gibram

# Create directories
RUN mkdir -p /etc/gibram /var/lib/gibram/data /var/lib/gibram/certs && \
    chown -R gibram:gibram /etc/gibram /var/lib/gibram

# Copy binary from builder
COPY --from=builder /build/gibram-server /usr/local/bin/gibram-server
RUN chmod +x /usr/local/bin/gibram-server

# Copy default config
COPY config.example.yaml /etc/gibram/config.yaml.example

# Switch to non-root user
USER gibram

WORKDIR /var/lib/gibram

# Expose port
EXPOSE 6161

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD nc -z localhost 6161 || exit 1

# Default command
ENTRYPOINT ["/usr/local/bin/gibram-server"]
CMD ["--config", "/etc/gibram/config.yaml"]
