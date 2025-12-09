BUILD_DIR := build
BINARY_NAME := orla

.PHONY: test
test:
	@if [ "$${VERBOSE:-0}" = "1" ]; then \
		go test -v -cover ./...; \
	else \
		go test -cover ./...; \
	fi

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