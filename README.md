# Kapstra Docker Alert Agent

A lightweight Docker log monitoring agent that detects error patterns and forwards alerts via webhooks.

## Features

- Real-time Docker container log monitoring
- Pattern-based alert detection (ERROR, WARN, FATAL, PANIC)
- Webhook alert forwarding with retry logic
- Container filtering by name or labels
- Minimal resource usage (<100MB memory)
- Health check endpoint
- Graceful shutdown handling

## Quick Start

### Using Docker Compose

1. **Build and run with existing containers:**
```bash
docker-compose up -d
```

1. **Run with example containers:**
```bash
docker-compose -f docker-compose.example.yml up -d
```

### Using Docker

1. **Build the image:**
```bash
docker build -t alert-agent .
```

1. **Run the container:**
```bash
docker run -d \
  --name alert-agent \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -e ALERT_AGENT_CONTAINER_FILTERS="nginx,redis" \
  -e WEBHOOK_URL="https://your-webhook-url.com" \
  -p 8080:8080 \
  alert-agent
```

## Configuration

### Environment Variables

| Variable                        | Default                       | Description                          |
|---------------------------------|-------------------------------|--------------------------------------|
| `ALERT_AGENT_HEALTH_PORT`       | `8080`                        | Health check port                    |
| `ALERT_AGENT_LOG_LEVEL`         | `info`                        | Log level (debug, info, warn, error) |
| `ALERT_AGENT_DOCKER_HOST`       | `unix:///var/run/docker.sock` | Docker socket path                   |
| `ALERT_AGENT_CONTAINER_FILTERS` | `""`                          | Comma-separated container filters    |
| `ALERT_AGENT_LOG_LEVELS`        | `ERROR,FATAL,PANIC,WARN`      | Log levels to monitor                |
| `ALERT_AGENT_CHECK_INTERVAL`    | `30s`                         | Container scan interval              |
| `ALERT_AGENT_BUFFER_SIZE`       | `1000`                        | Log buffer size                      |
| `WEBHOOK_URL`                   | `""`                          | Webhook URL for alerts               |

### Configuration File

Create a `config.yaml` file:

```yaml
server:
  health_port: 8080
  log_level: info

monitoring:
  container_filters:
    - "label:monitor=true"  # Monitor containers with monitor=true label
    - "name:nginx"          # Monitor containers with "nginx" in name
  log_levels:
    - "ERROR"
    - "FATAL" 
    - "PANIC"
    - "WARN"

alerts:
  destinations:
    - name: "webhook"
      type: "webhook"
      config:
        url: "https://your-webhook-url.com/webhook"
```

## Container Filtering

### By Name
```yaml
container_filters:
  - "nginx"           # Contains "nginx"
  - "name:prod-"      # Starts with "prod-"
```

### By Labels
```yaml
container_filters:
  - "label:monitor=true"      # Has label monitor=true
  - "label:environment=prod"  # Has label environment=prod
```

## Webhook Payload

The agent sends JSON payloads to configured webhooks:

```json
{
  "timestamp": "2025-08-15T21:19:43Z",
  "container_id": "d751ee8ee598",
  "container_name": "nginx",
  "level": "ERROR",
  "severity": "HIGH",
  "message": "404 error occurred",
  "metadata": {}
}
```

## Health Check

The agent exposes a health endpoint at `http://localhost:8080/health`

## Testing

Generate test alerts:

```bash
# Generate 404 errors in nginx
curl http://localhost:8081/nonexistent-page

# Check agent logs
docker logs alert-agent
```


## Resource Usage

- Memory: <100MB
- CPU: <1% idle, <5% under load
- Disk: <50MB image size

## Security

- Runs as non-root user
- Read-only Docker socket access
- Minimal Alpine base image
- No sensitive data in logs
