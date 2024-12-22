# NATS Task Scheduler

A distributed task scheduling system built on NATS messaging system, providing robust task management, execution, and monitoring capabilities.

## Features

### Task Management
- Task creation and configuration
- Task dependency management
- Task lifecycle management
- Priority-based scheduling
- Cron scheduling support

### Execution
- Distributed task execution
- Resource isolation and management
- Process-based execution
- Comprehensive logging
- Retry mechanism for failed tasks

### Load Balancing
- Multiple load balancing strategies
  - Round-Robin
  - Least Load
- Dynamic executor scaling
- Health monitoring

### Monitoring and Alerting
- Real-time metrics collection
  - System metrics (CPU, Memory)
  - Task metrics
  - Executor metrics
- Configurable alert rules
  - Task execution timeout
  - Resource usage thresholds
  - Task failure patterns
- Multiple notification channels
  - Email
  - Webhook
  - Message queue

## Architecture

```
                                    ┌─────────────────┐
                                    │                 │
                                    │  NATS JetStream │
                                    │                 │
                                    └─────────────────┘
                                           │
                    ┌────────────────┬─────┴─────┬────────────────┐
                    │                │           │                │
            ┌───────┴───────┐┌──────┴───────┐┌──┴─────────┐┌────┴─────────┐
            │  Scheduler    ││  Executor    ││  Monitor   ││   Alert      │
            │  Service     ││  Service     ││  Service   ││   Service    │
            └───────┬───────┘└──────┬───────┘└──┬─────────┘└────┬─────────┘
                    │               │            │               │
            ┌───────┴───────┐┌──────┴───────┐┌──┴─────────┐┌────┴─────────┐
            │ Dependency    ││ Resource     ││ Metrics    ││  Alert       │
            │ Manager      ││ Manager      ││ Collector  ││  Manager     │
            └───────────────┘└──────────────┘└────────────┘└──────────────┘
```

## Requirements

- Go 1.21 or higher
- NATS Server 2.10 or higher with JetStream enabled
- Docker (optional)

## Getting Started

1. Install NATS Server with JetStream:
   ```bash
   docker run -d --name nats-server -p 4222:4222 nats -js
   ```

2. Clone this repository:
   ```bash
   git clone https://github.com/t77yq/nats-project.git
   cd nats-project
   ```

3. Install dependencies:
   ```bash
   go mod tidy
   ```

4. Start the services:
   ```bash
   go run cmd/server/main.go
   ```

## Configuration

Configuration is managed through environment variables:

```bash
# NATS
NATS_URL=nats://localhost:4222

# Scheduler
MAX_TASKS=1000
MAX_RETRIES=3
RETRY_DELAY=5s

# Executor
MAX_CPU=80
MAX_MEMORY=1073741824  # 1GB
LOG_DIR=/var/log/tasks
MAX_LOG_SIZE=104857600 # 100MB
MAX_LOG_AGE=168h      # 7 days

# Monitor
METRICS_INTERVAL=10s
ALERT_CHECK_INTERVAL=30s
```

## Development

### Project Structure

```
.
├── cmd/
│   └── server/            # Application entry points
├── internal/
│   ├── model/            # Data models
│   ├── scheduler/        # Task scheduling
│   ├── executor/         # Task execution
│   ├── monitor/          # Monitoring and metrics
│   └── storage/          # Data persistence
├── pkg/                  # Public packages
└── test/                 # Test files
```

### Running Tests

Run all tests:
```bash
go test ./...
```

Run specific tests:
```bash
go test ./internal/monitor -v
```

Run integration tests:
```bash
go test ./... -tags=integration
```

## API Documentation

### Task Management

- Create Task
  ```http
  POST /api/v1/tasks
  ```

- Get Task Status
  ```http
  GET /api/v1/tasks/{id}
  ```

- Cancel Task
  ```http
  POST /api/v1/tasks/{id}/cancel
  ```

### Monitoring

- Get Metrics
  ```http
  GET /api/v1/metrics
  ```

- Get Alerts
  ```http
  GET /api/v1/alerts
  ```

## Contributing

1. Fork the repository
2. Create your feature branch
3. Commit your changes
4. Push to the branch
5. Create a new Pull Request

## License

MIT
