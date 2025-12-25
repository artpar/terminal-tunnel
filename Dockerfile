FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o terminal-tunnel ./cmd/terminal-tunnel

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/terminal-tunnel .

EXPOSE 8765

CMD ["./terminal-tunnel", "relay", "--port", "8765"]
