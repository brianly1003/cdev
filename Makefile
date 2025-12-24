# cdev Makefile

# Version info
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go settings
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOMOD := $(GOCMD) mod
GOFMT := $(GOCMD) fmt
GOVET := $(GOCMD) vet

# Binary name
BINARY_NAME := cdev
BINARY_DIR := bin
DIST_DIR := dist

# Build flags
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"

# Swag settings
SWAG := $(shell go env GOPATH)/bin/swag

# Targets
.PHONY: all build build-all clean test test-race fmt vet lint tidy run run-bg stop run-manager run-manager-bg stop-manager help swagger openrpc openrpc-start

# Default target
all: build

# Build for current platform
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME) ./cmd/cdev

# Build for all platforms
build-all: build-darwin-arm64 build-darwin-amd64 build-windows-amd64 build-linux-amd64

build-darwin-arm64:
	@echo "Building for macOS ARM64..."
	@mkdir -p $(DIST_DIR)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/cdev

build-darwin-amd64:
	@echo "Building for macOS AMD64..."
	@mkdir -p $(DIST_DIR)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/cdev

build-windows-amd64:
	@echo "Building for Windows AMD64..."
	@mkdir -p $(DIST_DIR)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/cdev

build-linux-amd64:
	@echo "Building for Linux AMD64..."
	@mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/cdev

# Run the application (usage: make run or make run REPO=/path/to/repo)
REPO ?=
run: build
ifneq ($(REPO),)
	CDEV_LOGGING_LEVEL=debug ./$(BINARY_DIR)/$(BINARY_NAME) start --config configs/config.yaml --repo $(REPO)
else
	CDEV_LOGGING_LEVEL=debug ./$(BINARY_DIR)/$(BINARY_NAME) start --config configs/config.yaml
endif

# Run server in background
run-bg: build
	@echo "Starting cdev server in background..."
ifneq ($(REPO),)
	@CDEV_LOGGING_LEVEL=debug nohup ./$(BINARY_DIR)/$(BINARY_NAME) start --config configs/config.yaml --repo $(REPO) > ~/.cdev/server.log 2>&1 &
else
	@CDEV_LOGGING_LEVEL=debug nohup ./$(BINARY_DIR)/$(BINARY_NAME) start --config configs/config.yaml > ~/.cdev/server.log 2>&1 &
endif
	@sleep 1
	@echo "Server started on port 8766. Logs: ~/.cdev/server.log"
	@echo "  HTTP API:   http://127.0.0.1:8766/api/"
	@echo "  WebSocket:  ws://127.0.0.1:8766/ws"
	@echo "  Swagger:    http://127.0.0.1:8766/swagger/"
	@echo "To stop: make stop"

# Stop the server
stop:
	@echo "Stopping cdev server..."
	@pkill -f "$(BINARY_NAME) start" 2>/dev/null || echo "No server running"

# DEPRECATED: Old workspace-manager commands (will be removed in v3.0)
run-manager: build
	@echo "WARNING: workspace-manager is deprecated. Use 'make run' instead."
	CDEV_LOGGING_LEVEL=debug ./$(BINARY_DIR)/$(BINARY_NAME) workspace-manager start --config configs/config.yaml

run-manager-bg: build
	@echo "WARNING: workspace-manager is deprecated. Use 'make run-bg' instead."
	@CDEV_LOGGING_LEVEL=debug nohup ./$(BINARY_DIR)/$(BINARY_NAME) workspace-manager start --config configs/config.yaml > ~/.cdev/manager.log 2>&1 &
	@sleep 1
	@echo "Workspace manager started. Logs: ~/.cdev/manager.log"

stop-manager:
	@echo "WARNING: workspace-manager is deprecated. Use 'make stop' instead."
	@pkill -f "cdev workspace-manager" 2>/dev/null || echo "No workspace manager running"

# Run tests
test:
	$(GOTEST) -v ./...

# Run tests with race detection
test-race:
	$(GOTEST) -race -v ./...

# Run tests with coverage (standard HTML)
test-coverage:
	@mkdir -p test
	$(GOTEST) -coverprofile=test/coverage.out ./...
	$(GOCMD) tool cover -html=test/coverage.out -o test/coverage.html
	@echo "Coverage report generated: test/coverage.html"

# Run tests with coverage treemap (beautiful SVG visualization)
# Requires: go install github.com/nikolaydubina/go-cover-treemap@latest
test-coverage-treemap:
	@mkdir -p test
	@echo "Running tests with coverage..."
	$(GOTEST) -coverprofile=test/coverage.out ./...
	@echo "Generating treemap visualization..."
	@which go-cover-treemap > /dev/null || (echo "go-cover-treemap not installed. Run: go install github.com/nikolaydubina/go-cover-treemap@latest" && exit 1)
	go-cover-treemap -coverprofile test/coverage.out > test/coverage.svg
	@echo "Coverage treemap generated: test/coverage.svg"
	@echo "Opening in browser..."
	@open test/coverage.svg 2>/dev/null || xdg-open test/coverage.svg 2>/dev/null || echo "Open test/coverage.svg manually"

# Run tests with go-test-coverage (threshold enforcement + badge generation)
# Requires: go install github.com/vladopajic/go-test-coverage/v2@latest
test-coverage-check:
	@mkdir -p test
	@echo "Running tests with coverage..."
	$(GOTEST) -coverprofile=test/coverage.out ./...
	@echo "Checking coverage thresholds..."
	@which go-test-coverage > /dev/null || (echo "go-test-coverage not installed. Run: go install github.com/vladopajic/go-test-coverage/v2@latest" && exit 1)
	go-test-coverage --config=.testcoverage.yml --profile=test/coverage.out

# Generate coverage badge (SVG)
test-coverage-badge:
	@mkdir -p test
	@echo "Running tests with coverage..."
	$(GOTEST) -coverprofile=test/coverage.out ./...
	@which go-test-coverage > /dev/null || (echo "go-test-coverage not installed. Run: go install github.com/vladopajic/go-test-coverage/v2@latest" && exit 1)
	go-test-coverage --config=.testcoverage.yml --profile=test/coverage.out --badge-file-name=test/coverage-badge.svg
	@echo "Coverage badge generated: test/coverage-badge.svg"

# Run tests with coverage and show percentage
test-coverage-report:
	@mkdir -p test
	@echo "Running tests with coverage..."
	@$(GOTEST) -coverprofile=test/coverage.out ./... 2>/dev/null
	@echo ""
	@echo "=== Coverage Summary ==="
	@$(GOCMD) tool cover -func=test/coverage.out | tail -1
	@echo ""
	@echo "=== Coverage by Package ==="
	@$(GOCMD) tool cover -func=test/coverage.out | grep -E "^github" | awk '{print $$1 "\t" $$3}'
	@echo ""
	@echo "Coverage Reports:"
	@echo "  make test-coverage         - Standard HTML report (test/coverage.html)"
	@echo "  make test-coverage-treemap - Visual SVG treemap (test/coverage.svg)"
	@echo "  make test-coverage-check   - Threshold enforcement"
	@echo "  make test-coverage-badge   - Generate SVG badge (test/coverage-badge.svg)"

# Run tests for a specific package
test-pkg:
	@mkdir -p test
	@if [ -z "$(PKG)" ]; then echo "Usage: make test-pkg PKG=./internal/hub"; exit 1; fi
	$(GOTEST) -v -coverprofile=test/coverage.out $(PKG)
	$(GOCMD) tool cover -func=test/coverage.out | tail -1

# Run tests with verbose output
test-verbose:
	$(GOTEST) -v ./...

# Run benchmarks
bench:
	$(GOTEST) -bench=. -benchmem ./...

# Run benchmarks for a specific package
bench-pkg:
	@if [ -z "$(PKG)" ]; then echo "Usage: make bench-pkg PKG=./internal/hub"; exit 1; fi
	$(GOTEST) -bench=. -benchmem $(PKG)

# Format code
fmt:
	$(GOFMT) ./...

# Run go vet
vet:
	$(GOVET) ./...

# Run linter (requires golangci-lint)
lint:
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Run: brew install golangci-lint" && exit 1)
	golangci-lint run

# Tidy dependencies
tidy:
	$(GOMOD) tidy

# Generate Swagger/OpenAPI docs
swagger:
	@echo "Generating Swagger docs..."
	@which $(SWAG) > /dev/null || (echo "swag not installed. Run: go install github.com/swaggo/swag/cmd/swag@latest" && exit 1)
	$(SWAG) init -g cmd/cdev/main.go -o api/swagger --packageName swagger
	@echo "Swagger docs generated in api/swagger/"

# Generate OpenRPC schema from running server
# Usage: make openrpc (requires server running on port 8766)
# Or: make openrpc-start (starts server, generates schema, stops server)
openrpc:
	@echo "Fetching OpenRPC schema from server..."
	@curl -s http://localhost:8766/api/rpc/discover | jq . > api/openrpc/openrpc.json 2>/dev/null || \
		(echo "Error: Server not running on port 8766. Use 'make openrpc-start' instead." && exit 1)
	@echo "OpenRPC schema generated in api/openrpc/openrpc.json"

# Generate OpenRPC schema (starts temporary server)
openrpc-start: build
	@echo "Starting temporary server to generate OpenRPC schema..."
	@mkdir -p api/openrpc
	@./$(BINARY_DIR)/$(BINARY_NAME) start --config configs/config.yaml &
	@sleep 2
	@curl -s http://localhost:8766/api/rpc/discover | jq . > api/openrpc/openrpc.json 2>/dev/null || true
	@pkill -f "$(BINARY_NAME) start" 2>/dev/null || true
	@if [ -s api/openrpc/openrpc.json ]; then \
		echo "OpenRPC schema generated in api/openrpc/openrpc.json"; \
	else \
		echo "Error: Failed to generate OpenRPC schema"; \
		exit 1; \
	fi

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BINARY_DIR) $(DIST_DIR)
	@rm -f test/coverage.out test/coverage.html test/coverage.svg test/coverage-badge.svg

# Install locally
install: build
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	@cp $(BINARY_DIR)/$(BINARY_NAME) /usr/local/bin/

# Generate checksums for releases
checksums:
	@echo "Generating checksums..."
	@cd $(DIST_DIR) && shasum -a 256 * > checksums.txt
	@cat $(DIST_DIR)/checksums.txt

# Help
help:
	@echo "cdev Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make              Build for current platform"
	@echo "  make build        Build for current platform"
	@echo "  make build-all    Build for all platforms (macOS, Windows, Linux)"
	@echo ""
	@echo "Run Server (port 8766):"
	@echo "  make run             Run server (debug mode, foreground)"
	@echo "  make run REPO=/path  Run with specific repo"
	@echo "  make run-bg          Run server in background"
	@echo "  make stop            Stop the server"
	@echo ""
	@echo "Testing:"
	@echo "  make test                  Run tests"
	@echo "  make test-verbose          Run tests with verbose output"
	@echo "  make test-race             Run tests with race detection"
	@echo "  make test-pkg PKG=./path   Run tests for specific package"
	@echo "  make bench                 Run all benchmarks"
	@echo "  make bench-pkg PKG=./path  Run benchmarks for specific package"
	@echo ""
	@echo "Coverage:"
	@echo "  make test-coverage         Standard HTML report"
	@echo "  make test-coverage-treemap Visual SVG treemap (beautiful)"
	@echo "  make test-coverage-check   Threshold enforcement"
	@echo "  make test-coverage-badge   Generate SVG badge for README"
	@echo "  make test-coverage-report  Show coverage summary in terminal"
	@echo ""
	@echo "Code Quality:"
	@echo "  make fmt          Format code"
	@echo "  make vet          Run go vet"
	@echo "  make lint         Run golangci-lint"
	@echo "  make tidy         Tidy dependencies"
	@echo ""
	@echo "Documentation:"
	@echo "  make swagger        Generate OpenAPI 3.0 docs"
	@echo "  make openrpc        Generate OpenRPC schema (server must be running)"
	@echo "  make openrpc-start  Generate OpenRPC schema (starts temp server)"
	@echo ""
	@echo "Build & Deploy:"
	@echo "  make clean        Clean build artifacts"
	@echo "  make install      Install to /usr/local/bin"
	@echo "  make checksums    Generate SHA256 checksums for releases"
	@echo ""
	@echo "Version: $(VERSION)"
