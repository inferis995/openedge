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

WORKDIR /app
COPY --from=builder /driver-redis .

# Run as non-root user for security
RUN adduser -D -u 1000 appuser
USER appuser

CMD ["./driver-redis"]
