FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /driver-lorawan ./services/driver-lorawan/

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /driver-lorawan /driver-lorawan
CMD ["/driver-lorawan"]
