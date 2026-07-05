set dotenv-load
set dotenv-filename := ".env"

build_dir := "bin"
module := "github.com/isyll/go-grpc-starter"

# List available recipes.
default:
    @just --list

# Run the gRPC server.
run:
    @go run ./cmd/server

# Run with hot reload (air).
dev:
    @air

# Run the optional HTTP/JSON gateway (opt-in; proxies to the gRPC server).
gateway:
    @go run ./cmd/gateway

# Generate protobuf code (buf).
proto:
    @buf lint && buf generate

# Check protos for breaking changes against main.
proto-breaking:
    @buf breaking --against ".git#branch=main"

# Scan dependencies for known vulnerabilities.
vuln:
    @go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Generate sqlc code from migrations + queries.
sqlc:
    @sqlc generate

# Format Go code.
fmt:
    @gofmt -w cmd internal pkg
    @go mod tidy

# Lint (golangci-lint).
lint:
    @golangci-lint run

# Run tests.
test:
    @go test ./... -race -shuffle=on -timeout 60s

# Tests with coverage.
test-cover:
    @go test ./... -race -coverprofile=coverage.out -covermode=atomic
    @go tool cover -func=coverage.out

# Build all binaries with version metadata stamped in.
build:
    @mkdir -p {{ build_dir }}
    @go build -trimpath -ldflags="-s -w \
      -X {{ module }}/pkg/version.version=$(git describe --tags --always 2>/dev/null || echo dev) \
      -X {{ module }}/pkg/version.commit=$(git rev-parse --short HEAD 2>/dev/null || echo none) \
      -X {{ module }}/pkg/version.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -o {{ build_dir }}/ ./cmd/...

# Apply all migrations.
migrate:
    @go run ./cmd/migrate -up

# Roll back N migrations (default 1).
migrate-down steps="1":
    @go run ./cmd/migrate -down -steps {{ steps }}

# Create a new migration pair.
migrate-create name:
    @migrate create -ext sql -dir migrations -seq {{ name }}

# Migration status.
migrate-status:
    @go run ./cmd/migrate -status

# Run all background workers (email, push, event dispatcher).
worker:
    @go run ./cmd/worker

# Start postgres + redis + minio with docker.
up:
    @docker compose up -d postgres redis minio

# Start the full stack with docker.
up-all:
    @docker compose up -d --build

# Stop the docker stack.
down:
    @docker compose down

# Clean build artifacts.
clean:
    @rm -rf {{ build_dir }} tmp/ coverage.out
    @go clean
