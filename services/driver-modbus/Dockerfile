# Build stage
FROM golang:1.27-alpine AS builder

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
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o driver-modbus ./services/driver-modbus

# Runtime stage
FROM alpine:3.24

# Pick up OS security patches published since the base tag was last rebuilt.
#
# A base tag is a moving pointer, and it moves on the registry's schedule, not
# on the security one: the nightly scan found openssl 3.5.7-r0 (CVE-2026-14456,
# HIGH) in the Alpine bases while Alpine itself had already published 3.5.8-r0.
# Without this line the image we ship carries whatever the base happened to hold
# on the day it was built, and the only remedy is waiting for somebody else to
# rebuild a tag we do not control.
#
# --no-cache leaves no index behind, so the layer does not grow the image.
RUN apk upgrade --no-cache

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Copy the binary from builder
COPY --from=builder /app/driver-modbus .

# Health check - check if process is running
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD pgrep driver-modbus || exit 1

# Run the binary
CMD ["./driver-modbus"]
