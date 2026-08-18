FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o openedge ./services/cli/

FROM alpine:3.24
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/openedge /usr/local/bin/openedge
ENTRYPOINT ["openedge"]
