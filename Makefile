BUILD_DIR := build
BINARY_NAME := orla

.PHONY: test
test:
	@if [ "$${VERBOSE:-0}" = "1" ]; then \
		go test -v -cover ./...; \
	else \
		go test -cover ./...; \
	fi

.PHONY: coverage
coverage:
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
	go build -o $(BUILD_DIR)/$(BINARY_NAME) cmd/orla/main.go

.PHONY: install
install:
	go install ./cmd/$(BINARY_NAME)

.PHONY: run
run:
	./$(BUILD_DIR)/$(BINARY_NAME)

.PHONY: deps
deps:
	go mod download