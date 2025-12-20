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
.PHONY: all build build-all clean test test-race fmt vet lint tidy run help swagger

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
	./$(BINARY_DIR)/$(BINARY_NAME) start --config configs/config.yaml --repo $(REPO)
else
	./$(BINARY_DIR)/$(BINARY_NAME) start --config configs/config.yaml
endif

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
	@echo "  make run REPO=/path  Build and run with --repo flag (default: .)"
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
	@echo "  make swagger      Generate OpenAPI 3.0 docs"
	@echo ""
	@echo "Build & Deploy:"
	@echo "  make clean        Clean build artifacts"
	@echo "  make install      Install to /usr/local/bin"
	@echo "  make checksums    Generate SHA256 checksums for releases"
	@echo ""
	@echo "Version: $(VERSION)"
