# Build stage
FROM golang:1.27-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY internal ./internal
COPY services/engine-historian/*.go ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o engine-historian .

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

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

# Copy the binary from builder
COPY --from=builder /app/engine-historian .

# Set the entrypoint
ENTRYPOINT ["./engine-historian"]
