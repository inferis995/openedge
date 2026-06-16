# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the driver-manager
RUN CGO_ENABLED=0 GOOS=linux go build -o driver-manager ./services/driver-manager/

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates docker wget

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/driver-manager .

# Run the binary
HEALTHCHECK --interval=30s --timeout=10s --start-period=20s --retries=3 \
  CMD wget -qO- http://localhost:8081/health || exit 1

CMD ["./driver-manager"]
