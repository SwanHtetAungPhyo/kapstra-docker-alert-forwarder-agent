FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-w -s' -o alert-agent cmd/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/alert-agent .
COPY --from=builder /app/config.yaml ./config.yaml

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD pgrep alert-agent || exit 1

ENV ALERT_AGENT_HEALTH_PORT=8080
ENV ALERT_AGENT_LOG_LEVEL=info
ENV ALERT_AGENT_DOCKER_HOST=unix:///var/run/docker.sock

CMD ["./alert-agent"]