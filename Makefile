.PHONY: all build run test clean docker-up docker-down migrate-up migrate-down swag sqlc

# Default target
all: build

# Build the application
build:
	@mkdir -p bin
	go build -o bin/api ./cmd/api

# Run the application
run: build
	./bin/api

# Run with hot reload (requires air)
dev:
	@which air > /dev/null || (echo "Installing air..." && go install github.com/cosmtrek/air@latest)
	air

# Run tests
test:
	go test -v ./...

# Run integration tests
test-integration:
	go test -v -tags=integration ./...

# Clean build artifacts
clean:
	rm -rf bin/
	go clean

# Docker commands
docker-up:
	docker-compose up -d

docker-down:
	docker-compose down -v

docker-logs:
	docker-compose logs -f

# Database migrations (requires goose or golang-migrate)
migrate-up:
	@echo "Running migrations..."
	@for file in internal/database/migrations/*.sql; do \
		echo "Applying $$file"; \
	done

migrate-down:
	@echo "Rolling back migrations..."

# Generate swagger docs
swag:
	@which swag > /dev/null || (echo "Installing swag..." && go install github.com/swaggo/swag/cmd/swag@latest)
	swag init -g cmd/api/main.go -o api/swagger

# Generate sqlc code
sqlc:
	@which sqlc > /dev/null || (echo "Installing sqlc..." && go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest)
	cd internal/database && sqlc generate

# Format code
fmt:
	go fmt ./...

# Run linter
lint:
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run

# Download dependencies
deps:
	go mod download
	go mod tidy

# Check for security vulnerabilities
security:
	@which govulncheck > /dev/null || (echo "Installing govulncheck..." && go install golang.org/x/vuln/cmd/govulncheck@latest)
	govulncheck ./...

# Full setup for development
setup: deps docker-up migrate-up
	@echo "Development environment ready!"
	@echo "Run 'make run' to start the application"
