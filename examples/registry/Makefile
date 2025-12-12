.PHONY: help run test clean

ORLA_BIN ?= ../../bin/orla
ORLA_TEST_BIN ?= ../../bin/orla-test
CONFIG ?= orla.json
PORT ?= 8080

help:
	@echo "Available commands:"
	@echo "  make run    - Start orla server with registry config"
	@echo "  make test   - Test the tools (requires orla-test binary)"
	@echo "  make clean  - Clean up temporary files"
	@echo "  make help   - Show this help"

run:
	@echo "Starting orla with registry config: $(CONFIG)"
	@echo "Tools are defined directly in the config file"
	@$(ORLA_BIN) --config $(CONFIG)

test:
	@echo "testing HTTP transport"; \
	echo ""; \
	echo "testing echo tool (HTTP)"; \
	$(ORLA_TEST_BIN) call --transport http --port $(PORT) --tool echo --args '{"message":"Hello from registry!"}' || true; \
	echo ""; \
	echo "testing date tool (HTTP)"; \
	$(ORLA_TEST_BIN) call --transport http --port $(PORT) --tool date --args '{"format":"iso8601"}' || true; \
	echo ""; \
	echo "testing stdio transport"; \
	echo ""; \
	echo "testing echo tool (stdio)"; \
	$(ORLA_TEST_BIN) call --transport stdio --orla-bin $(ORLA_BIN) --tool echo --args '{"message":"Hello from registry!"}' || true; \
	echo ""; \
	echo "testing date tool (stdio)"; \
	$(ORLA_TEST_BIN) call --transport stdio --orla-bin $(ORLA_BIN) --tool date --args '{"format":"iso8601"}' || true

clean:
	@echo "Nothing to clean"
