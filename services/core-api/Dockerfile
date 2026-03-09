# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o core-api ./services/core-api

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install runtime dependencies including backup tools
RUN apk add --no-cache ca-certificates tzdata postgresql16-client curl

# Install InfluxDB CLI
RUN curl -L https://dl.influxdata.com/influxdb/releases/influxdb2-client-2.7.3-linux-amd64.tar.gz -o influxdb2-client.tar.gz && \
    tar xzf influxdb2-client.tar.gz && \
    cp influx /usr/local/bin/influx && \
    rm influxdb2-client.tar.gz influx

# Copy the binary from builder
COPY --from=builder /app/core-api .

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/organizations || exit 1

# Run the binary
CMD ["./core-api"]
