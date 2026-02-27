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

# Build flags (development - includes debug symbols)
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"

# Build flags (release - stripped symbols for protection against reverse engineering)
# -s: Strip symbol table (removes function/variable names)
# -w: Strip DWARF debugging info (removes source file references)
# -trimpath: Remove file system paths from compiled executable
LDFLAGS_RELEASE := -trimpath -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"

# Garble settings (advanced obfuscation)
# Requires: go install mvdan.cc/garble@latest
GARBLE := $(shell go env GOPATH)/bin/garble
# -literals: Obfuscate string literals
# -tiny: Produce smaller binaries by removing extra info
# -seed=random: Use random seed for each build (different obfuscation each time)
GARBLE_FLAGS := -literals -tiny -seed=random

# Swag settings
SWAG := $(shell go env GOPATH)/bin/swag

# Targets
.PHONY: all build build-release build-obfuscated build-all build-all-release build-all-obfuscated clean test test-race test-deadlock fmt vet lint tidy run run-headless run-bg run-debug stop run-manager run-manager-bg stop-manager help swagger openrpc openrpc-start build-debug

# Default target
all: build

# Build for current platform (development - with debug symbols)
build:
	@echo "Building $(BINARY_NAME) (development)..."
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME) ./cmd/cdev

# Build with deadlock detection (development + go-deadlock)
# Use this to detect potential deadlocks during development
build-debug:
	@echo "Building $(BINARY_NAME) with DEADLOCK DETECTION..."
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) -tags deadlock $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME) ./cmd/cdev
	@echo "Deadlock detection build complete. Mutexes will report potential deadlocks."

# Build for current platform (release - stripped, protected)
build-release:
	@echo "Building $(BINARY_NAME) (release - protected)..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS_RELEASE) -o $(BINARY_DIR)/$(BINARY_NAME) ./cmd/cdev
	@echo "Release build complete. Symbols stripped for protection."

# Build for all platforms (development)
build-all: build-darwin-arm64 build-darwin-amd64 build-windows-amd64 build-linux-amd64

# Build for all platforms (release - stripped, protected)
build-all-release: build-darwin-arm64-release build-darwin-amd64-release build-windows-amd64-release build-linux-amd64-release
	@echo ""
	@echo "All release builds complete. Binaries are protected against reverse engineering."

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

# Release builds (stripped symbols for protection)
build-darwin-arm64-release:
	@echo "Building for macOS ARM64 (release)..."
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS_RELEASE) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/cdev

build-darwin-amd64-release:
	@echo "Building for macOS AMD64 (release)..."
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS_RELEASE) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/cdev

build-windows-amd64-release:
	@echo "Building for Windows AMD64 (release)..."
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS_RELEASE) -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/cdev

build-linux-amd64-release:
	@echo "Building for Linux AMD64 (release)..."
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS_RELEASE) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/cdev

# Obfuscated builds (maximum protection using garble)
# Hides: function names, module paths, package names, string literals
build-obfuscated:
	@echo "Building $(BINARY_NAME) (obfuscated - maximum protection)..."
	@which $(GARBLE) > /dev/null || (echo "garble not installed. Run: go install mvdan.cc/garble@latest" && exit 1)
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=0 $(GARBLE) $(GARBLE_FLAGS) build $(LDFLAGS_RELEASE) -o $(BINARY_DIR)/$(BINARY_NAME) ./cmd/cdev
	@echo "Obfuscated build complete. Module paths and literals are hidden."

# Build all platforms with obfuscation (maximum protection)
build-all-obfuscated: build-darwin-arm64-obfuscated build-darwin-amd64-obfuscated build-linux-amd64-obfuscated
	@echo ""
	@echo "All obfuscated builds complete. Maximum protection against reverse engineering."
	@echo "Note: Windows obfuscated build requires Windows or cross-compilation setup."

build-darwin-arm64-obfuscated:
	@echo "Building for macOS ARM64 (obfuscated)..."
	@which $(GARBLE) > /dev/null || (echo "garble not installed. Run: go install mvdan.cc/garble@latest" && exit 1)
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GARBLE) $(GARBLE_FLAGS) build $(LDFLAGS_RELEASE) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/cdev

build-darwin-amd64-obfuscated:
	@echo "Building for macOS AMD64 (obfuscated)..."
	@which $(GARBLE) > /dev/null || (echo "garble not installed. Run: go install mvdan.cc/garble@latest" && exit 1)
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GARBLE) $(GARBLE_FLAGS) build $(LDFLAGS_RELEASE) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/cdev

build-linux-amd64-obfuscated:
	@echo "Building for Linux AMD64 (obfuscated)..."
	@which $(GARBLE) > /dev/null || (echo "garble not installed. Run: go install mvdan.cc/garble@latest" && exit 1)
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GARBLE) $(GARBLE_FLAGS) build $(LDFLAGS_RELEASE) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/cdev

# Run the application in TERMINAL mode (default, Claude runs in current terminal)
# Usage: make run or make run REPO=/path/to/repo EXTERNAL_URL=https://example.com
REPO ?=
EXTERNAL_URL ?=
run: build
	@echo "Starting cdev in TERMINAL mode (Claude will run in this terminal)..."
	CDEV_LOGGING_LEVEL=debug ./$(BINARY_DIR)/$(BINARY_NAME) start --config configs/config.yaml \
		$(if $(REPO),--repo $(REPO)) \
		$(if $(EXTERNAL_URL),--external-url $(EXTERNAL_URL))

# Run with DEADLOCK DETECTION enabled
# Usage: make run-debug or make run-debug REPO=/path/to/repo EXTERNAL_URL=https://example.com
run-debug: build-debug
	@echo "Starting cdev with DEADLOCK DETECTION enabled..."
	CDEV_LOGGING_LEVEL=debug ./$(BINARY_DIR)/$(BINARY_NAME) start --config configs/config.yaml \
		$(if $(REPO),--repo $(REPO)) \
		$(if $(EXTERNAL_URL),--external-url $(EXTERNAL_URL))

# Run the application in HEADLESS mode (Claude runs as background subprocess)
# Usage: make run-headless or make run-headless REPO=/path/to/repo
run-headless: build
	@echo "Starting cdev in HEADLESS mode (Claude runs as background process)..."
ifneq ($(REPO),)
	CDEV_LOGGING_LEVEL=debug ./$(BINARY_DIR)/$(BINARY_NAME) start --headless --config configs/config.yaml --repo $(REPO)
else
	CDEV_LOGGING_LEVEL=debug ./$(BINARY_DIR)/$(BINARY_NAME) start --headless --config configs/config.yaml
endif

# Run server in background (always headless mode for background operation)
run-bg: build
	@echo "Starting cdev server in background (headless mode)..."
	@mkdir -p ~/.cdev
ifneq ($(REPO),)
	@CDEV_LOGGING_LEVEL=debug nohup ./$(BINARY_DIR)/$(BINARY_NAME) start --headless --config configs/config.yaml --repo $(REPO) > ~/.cdev/server.log 2>&1 &
else
	@CDEV_LOGGING_LEVEL=debug nohup ./$(BINARY_DIR)/$(BINARY_NAME) start --headless --config configs/config.yaml > ~/.cdev/server.log 2>&1 &
endif
	@sleep 1
	@echo "Server started on port 16180. Logs: ~/.cdev/server.log"
	@echo "  HTTP API:   http://127.0.0.1:16180/api/"
	@echo "  WebSocket:  ws://127.0.0.1:16180/ws"
	@echo "  Swagger:    http://127.0.0.1:16180/swagger/"
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

# Run tests with deadlock detection
test-deadlock:
	@echo "Running tests with DEADLOCK DETECTION..."
	$(GOTEST) -tags deadlock -v ./...

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
# Install: brew install golangci-lint OR go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
GOLANGCI_LINT := $(shell which golangci-lint 2>/dev/null || echo $(shell go env GOPATH)/bin/golangci-lint)
lint:
	@if [ ! -x "$(GOLANGCI_LINT)" ]; then \
		echo "golangci-lint not installed. Run one of:"; \
		echo "  brew install golangci-lint"; \
		echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi
	$(GOLANGCI_LINT) run

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
# Usage: make openrpc (requires server running on port 16180)
# Or: make openrpc-start (starts server, generates schema, stops server)
openrpc:
	@echo "Fetching OpenRPC schema from server..."
	@curl -s http://localhost:16180/api/rpc/discover | jq . > api/openrpc/openrpc.json 2>/dev/null || \
		(echo "Error: Server not running on port 16180. Use 'make openrpc-start' instead." && exit 1)
	@echo "OpenRPC schema generated in api/openrpc/openrpc.json"

# Generate OpenRPC schema (starts temporary server)
openrpc-start: build
	@echo "Starting temporary server to generate OpenRPC schema..."
	@mkdir -p api/openrpc
	@./$(BINARY_DIR)/$(BINARY_NAME) start --config configs/config.yaml &
	@sleep 2
	@curl -s http://localhost:16180/api/rpc/discover | jq . > api/openrpc/openrpc.json 2>/dev/null || true
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
	@echo "Build Commands:"
	@echo "  make build                Build for current platform (development)"
	@echo "  make build-debug          Build with DEADLOCK DETECTION (go-deadlock)"
	@echo "  make build-release        Build with stripped symbols (basic protection)"
	@echo "  make build-obfuscated     Build with garble obfuscation (maximum protection)"
	@echo ""
	@echo "  make build-all            Build all platforms (development)"
	@echo "  make build-all-release    Build all platforms (basic protection)"
	@echo "  make build-all-obfuscated Build all platforms (maximum protection)"
	@echo ""
	@echo "Protection Levels:"
	@echo "  Development:  Full debug symbols, all paths visible"
	@echo "  Release:      Stripped symbols, no absolute paths"
	@echo "  Obfuscated:   Garble obfuscation, hidden module paths & literals"
	@echo ""
	@echo "Run Server (port 16180):"
	@echo ""
	@echo "  TERMINAL MODE (default) - Claude runs in current terminal:"
	@echo "    make run               Run in terminal mode (Claude visible in this terminal)"
	@echo "    make run REPO=/path    Run with specific repo in terminal mode"
	@echo "    make run EXTERNAL_URL=https://...  Run with external URL"
	@echo ""
	@echo "  HEADLESS MODE - Claude runs as background subprocess:"
	@echo "    make run-headless      Run in headless mode (Claude as background process)"
	@echo "    make run-headless REPO=/path  Run headless with specific repo"
	@echo ""
	@echo "  BACKGROUND (daemon):"
	@echo "    make run-bg            Run server in background (always headless)"
	@echo "    make stop              Stop the server"
	@echo ""
	@echo "  DEBUG MODE (deadlock detection):"
	@echo "    make run-debug         Run with go-deadlock to detect potential deadlocks"
	@echo "    make run-debug REPO=/path  Run debug mode with specific repo"
	@echo "    make run-debug EXTERNAL_URL=https://...  Run debug with external URL"
	@echo ""
	@echo "  Note: Set security.require_auth=true in config to auto-generate QR token"
	@echo ""
	@echo "Testing:"
	@echo "  make test                  Run tests"
	@echo "  make test-verbose          Run tests with verbose output"
	@echo "  make test-race             Run tests with race detection"
	@echo "  make test-deadlock         Run tests with deadlock detection"
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
