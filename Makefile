BUILD_DIR := bin
BINARY_NAME := orla

.PHONY: help
help: ## Show this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: test
test: ## Run tests (excluding integration tests)
	@if [ "$${VERBOSE:-0}" = "1" ]; then \
		go test -v ./...; \
	else \
		go test ./...; \
	fi

.PHONY: test-integration
test-integration: ## Run only integration tests (requires Ollama)
	@if [ "$${VERBOSE:-0}" = "1" ]; then \
		go test -tags=integration -run Integration -v -count=1 ./...; \
	else \
		go test -tags=integration -run Integration -count=1 ./...; \
	fi

.PHONY: test-e2e
test-e2e: ## Run end-to-end tests for all examples
	@./scripts/e2e-test.sh

.PHONY: coverage
coverage: ## Generate coverage report (coverage.html, excludes integration tests)
	@# Tests all packages (excluding integration tests); codecov.yml excludes cmd/ and examples/ in CI/CD
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

.PHONY: lint
lint: ## Run go vet and golangci-lint
	go vet ./...
	golangci-lint run ./...

.PHONY: format
format: ## Format code and tidy go.mod
	go fmt ./...
	go mod tidy

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

.PHONY: build
build: ## Build the orla binaries
	mkdir -p $(BUILD_DIR)
	go build -ldflags "-X main.version=$(VERSION) -X main.buildDate=$(BUILD_DATE)" \
		-o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/orla
	go build -ldflags "-X main.version=$(VERSION) -X main.buildDate=$(BUILD_DATE)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-test ./cmd/orla-test

.PHONY: install-test
install-test: ## Install the orla-test binary
	go install -ldflags "-X main.version=$(VERSION) -X main.buildDate=$(BUILD_DATE)" ./cmd/orla-test

.PHONY: install
install: ## Install the orla binary
	go install -ldflags "-X main.version=$(VERSION) -X main.buildDate=$(BUILD_DATE)" ./cmd/$(BINARY_NAME)

.PHONY: run
run: ## Run the orla binary
	./$(BUILD_DIR)/$(BINARY_NAME) serve

.PHONY: deps
deps: ## Download Go dependencies
	go mod download