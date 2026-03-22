# Multi-stage build for minimal image size
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

WORKDIR /build

# Copy go module files (assuming build context is mysql-monitor directory)
# We need go.mod from parent's main directory
COPY main/performance_schema_monitor.go .

# Create a temporary go.mod for building
RUN go mod init mysql-monitor && \
    go get github.com/go-sql-driver/mysql@latest

# Build binary with static linking
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o perf_monitor \
    performance_schema_monitor.go

# Final stage - minimal runtime image
FROM alpine:3.19

# Install ca-certificates for HTTPS connections
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 monitor && \
    adduser -D -u 1000 -G monitor monitor

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/perf_monitor .

# Change ownership
RUN chown -R monitor:monitor /app

# Switch to non-root user
USER monitor

# Default environment variables (can be overridden)
ENV MYSQL_HOST=localhost \
    MYSQL_PORT=3306 \
    MYSQL_USER=dev \
    MYSQL_PASSWORD="" \
    MONITOR_INTERVAL=10 \
    MONITOR_THRESHOLD=10 \
    MONITOR_NAME="MySQL Monitor" \
    TZ=Asia/Shanghai

# Run the monitor
CMD ["/bin/sh", "-c", "echo \"Starting ${MONITOR_NAME}...\" && ./perf_monitor -host=${MYSQL_HOST} -port=${MYSQL_PORT} -user=${MYSQL_USER} -password=${MYSQL_PASSWORD} -interval=${MONITOR_INTERVAL} -threshold=${MONITOR_THRESHOLD}"]
