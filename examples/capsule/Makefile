.PHONY: help run test clean

ORLA_BIN ?= ../../bin/orla
ORLA_TEST_BIN ?= ../../bin/orla-test
PORT ?= 8080

help:
	@echo "Available commands:"
	@echo "  make run    - Start orla server"
	@echo "  make test   - Test the capsule tool"
	@echo "  make clean  - Clean up temporary files"
	@echo "  make help   - Show this help"

run:
	@echo "Starting orla on port $(PORT)..."
	@echo "Tools directory: $(PWD)/tools"
	@$(ORLA_BIN) serve --port $(PORT) --config orla.yaml

test:
	@echo "testing HTTP transport"; \
	echo ""; \
	echo "testing echo-capsule tool (HTTP)"; \
	$(ORLA_TEST_BIN) call --transport http --port $(PORT) --tool echo-capsule --args '{}' || true; \
	echo ""; \
	echo "testing stdio transport"; \
	echo ""; \
	echo "testing echo-capsule tool (stdio)"; \
	$(ORLA_TEST_BIN) call --transport stdio --orla-bin $(ORLA_BIN) --tool echo-capsule --args '{}' || true

clean:
	@echo "Nothing to clean"

