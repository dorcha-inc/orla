BUILD_DIR := bin
BINARY_NAME := orla

.PHONY: test
test:
	@if [ "$${VERBOSE:-0}" = "1" ]; then \
		go test -v -cover ./...; \
	else \
		go test -cover ./...; \
	fi

.PHONY: e2e-test
e2e-test:
	@# Run end-to-end tests for all examples
	@./scripts/e2e-test.sh

.PHONY: coverage
coverage:
	@# Coverage exclusions are configured in .codecov.yml
	@# Note: Local HTML report may include excluded paths, but Codecov will filter them
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

.PHONY: lint
lint:
	go vet ./...
	golangci-lint run ./...

.PHONY: format
format:
	go fmt ./...
	go mod tidy

.PHONY: build
build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/orla
	go build -o $(BUILD_DIR)/$(BINARY_NAME)-test ./cmd/orla-test

.PHONY: install-test
install-test:
	go install ./cmd/orla-test

.PHONY: install
install:
	go install ./cmd/$(BINARY_NAME)

.PHONY: run
run:
	./$(BUILD_DIR)/$(BINARY_NAME)

.PHONY: deps
deps:
	go mod download