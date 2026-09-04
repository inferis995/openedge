FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY internal/ internal/
COPY services/driver-redis/ services/driver-redis/

# Build
WORKDIR /app/services/driver-redis
RUN CGO_ENABLED=0 GOOS=linux go build -o /driver-redis

FROM alpine:3.22

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
COPY --from=builder /driver-redis .

# Run as non-root user for security
RUN adduser -D -u 1000 appuser
USER appuser

CMD ["./driver-redis"]
