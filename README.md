# NATS Large Scale Project

This is a large-scale Go project built on NATS messaging system, implementing best practices for distributed systems.

## Features

- High-performance message processing
- Scalable microservices architecture
- Fault tolerance and high availability
- Monitoring and observability
- Service discovery and load balancing

## Requirements

- Go 1.21 or higher
- NATS Server
- Docker (optional)

## Getting Started

1. Install NATS Server
2. Clone this repository
3. Run `go mod tidy` to install dependencies
4. Start the services using `make run`

## Project Structure

```
.
├── cmd/                    # Application entry points
├── internal/              # Private application and library code
├── pkg/                   # Public library code
├── api/                   # API definitions
├── config/               # Configuration files
├── scripts/              # Scripts for development
└── test/                 # Additional test applications and test data
```

## Configuration

Configuration is managed through config files in the `config` directory and environment variables.

## Development

To start development:

```bash
make dev
```

## Testing

Run tests:

```bash
make test
```

## License

MIT
