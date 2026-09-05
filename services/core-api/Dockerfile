# Build stage
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

ARG TARGETOS TARGETARCH
WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -a -installsuffix cgo -o core-api ./services/core-api

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

RUN apk add --no-cache ca-certificates tzdata wget postgresql16-client && \
    addgroup -S openedge && adduser -S -G openedge openedge && \
    chown openedge:openedge /app

COPY --from=builder --chown=openedge:openedge /app/core-api .
COPY --chown=openedge:openedge migrations ./migrations

USER openedge

EXPOSE 8081

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8081/health || exit 1

CMD ["./core-api"]
