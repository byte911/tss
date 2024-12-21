.PHONY: build run test clean

# Build variables
BINARY_NAME=nats-project
GO=go

# Build the application
build:
	$(GO) build -o bin/$(BINARY_NAME) cmd/server/main.go

# Run the application
run: build
	./bin/$(BINARY_NAME)

# Run tests
test:
	$(GO) test -v ./...

# Clean build artifacts
clean:
	rm -f bin/$(BINARY_NAME)
	$(GO) clean

# Format code
fmt:
	$(GO) fmt ./...

# Run linter
lint:
	golangci-lint run

# Download dependencies
deps:
	$(GO) mod tidy
	$(GO) mod verify

# Generate mocks for testing
mocks:
	mockgen -source=internal/service/message.go -destination=test/mocks/message_mock.go

# Run in development mode
dev: deps
	$(GO) run cmd/server/main.go
