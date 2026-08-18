# Build stage
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

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
