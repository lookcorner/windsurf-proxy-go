# Windsurf Proxy Go - Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git

# Copy go.mod and go.sum first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o windsurf-proxy ./cmd/windsurf-proxy

# Runtime stage
FROM alpine:3.18

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /build/windsurf-proxy /app/windsurf-proxy
COPY --from=builder /build/configs/config.yaml.example /app/config.yaml.example

# Create non-root user
RUN addgroup -g 1000 windsurf && \
    adduser -u 1000 -G windsurf -s /bin/sh -D windsurf

USER windsurf

# Expose port
EXPOSE 8000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8000/health || exit 1

# Entry point
ENTRYPOINT ["/app/windsurf-proxy"]
CMD ["-c", "/app/config.yaml"]