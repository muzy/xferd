.PHONY: build clean test run install release help

# Variables
BINARY_NAME=xferd
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_DATE)"

# Build targets
build: ## Build the binary
	@echo "Building $(BINARY_NAME) $(VERSION)..."
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/xferd

build-linux: ## Build for Linux
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 ./cmd/xferd

build-windows: ## Build for Windows
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-windows-amd64.exe ./cmd/xferd

build-all: build-linux build-windows ## Build for all platforms

# Development targets
run: ## Run with example config
	go run ./cmd/xferd -config config.example.yml

test: ## Run all tests with race detector
	@echo "Running all tests..."
	go test -v -race -coverprofile=coverage.out ./...

test-short: ## Run tests without race detector (faster)
	@echo "Running tests (short mode)..."
	go test -v ./...

test-coverage: test ## Run tests with coverage report
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-coverage-func: test ## Show coverage by function
	go tool cover -func=coverage.out

test-unit: ## Run unit tests only (no integration tests)
	@echo "Running unit tests..."
	go test -v -short ./...

test-config: ## Run config package tests
	@echo "Running config tests..."
	go test -v ./internal/config

test-shadow: ## Run shadow package tests
	@echo "Running shadow tests..."
	go test -v ./internal/shadow

test-watcher: ## Run watcher package tests
	@echo "Running watcher tests..."
	go test -v ./internal/watcher

test-ingress: ## Run ingress package tests
	@echo "Running ingress tests..."
	go test -v ./internal/ingress

test-uploader: ## Run uploader package tests
	@echo "Running uploader tests..."
	go test -v ./internal/uploader

test-verbose: ## Run tests with verbose output
	@echo "Running all tests (verbose)..."
	go test -v -race -count=1 ./...

test-bench: ## Run benchmarks
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

test-clean: ## Clean test cache and test binaries
	@echo "Cleaning test cache..."
	go clean -testcache
	@echo "Removing test binaries..."
	find . -name "*.test" -type f -delete

test-integration: ## Run integration tests (end-to-end tests with real filesystem)
	@echo "Running integration tests..."
	go test -v -tags=integration ./internal/service

test-integration-verbose: ## Run integration tests with verbose output
	@echo "Running integration tests (verbose)..."
	go test -v -tags=integration -count=1 ./internal/service

test-all: test test-integration ## Run all tests including integration tests
	@echo "All tests completed"

test-e2e: ## Run E2E binary tests (compiles and runs actual binary)
	@echo "Running E2E binary tests..."
	go test -v -tags=e2e ./test/e2e/...

test-e2e-verbose: ## Run E2E binary tests with verbose output
	@echo "Running E2E binary tests (verbose)..."
	go test -v -tags=e2e -count=1 ./test/e2e/...

test-complete: test test-integration test-e2e ## Run all test types (unit, integration, E2E)
	@echo "Complete test suite finished"
	@echo "Cleaning up test artifacts..."
	@find . -name "*.test" -type f -delete 2>/dev/null || true

lint: ## Run linter
	golangci-lint run ./...

fmt: ## Format code
	go fmt ./...
	gofmt -s -w .

vet: ## Run go vet
	go vet ./...

# Installation targets
install: build ## Install binary to system
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	sudo cp $(BINARY_NAME) /usr/local/bin/
	sudo chmod +x /usr/local/bin/$(BINARY_NAME)

install-service-linux: install ## Install as systemd service
	@echo "Installing systemd service..."
	sudo ./packaging/systemd/install.sh

# Release targets
release: ## Create release with GoReleaser
	@if [ -z "$(VERSION)" ]; then \
		echo "Error: VERSION is not set"; \
		exit 1; \
	fi
	goreleaser release --clean

release-snapshot: ## Create snapshot release
	goreleaser release --snapshot --clean --skip=publish

release-msi: release-snapshot build-msi ## Create snapshot release with MSI packages

# Windows MSI targets
download-winsw: ## Download latest WinSW release
	@echo "Downloading latest WinSW..."
	./scripts/download-winsw.sh

build-msi: ## Build Windows MSI packages (requires wixl)
	@echo "Building MSI packages..."
	@which wixl > /dev/null 2>&1 || { echo "Error: wixl is not installed. Install with: sudo apt-get install wixl"; exit 1; }
	./scripts/build-msi.sh

# Utility targets
clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_NAME)-*
	rm -rf dist/
	rm -f coverage.out coverage.html

deps: ## Download dependencies
	go mod download
	go mod verify

tidy: ## Tidy dependencies
	go mod tidy

update-deps: ## Update dependencies
	go get -u ./...
	go mod tidy

help: ## Show this help
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

# Default target
.DEFAULT_GOAL := help

