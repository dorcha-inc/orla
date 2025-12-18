.PHONY: help run test clean

ORLA_BIN ?= ../../bin/orla
ORLA_TEST_BIN ?= ../../bin/orla-test
PORT ?= 8080

help:
	@echo "Available commands:"
	@echo "  make run    - Start orla server"
	@echo "  make test   - Test the tools"
	@echo "  make clean  - Clean up temporary files"
	@echo "  make help   - Show this help"

run:
	@echo "Starting orla on port $(PORT)..."
	@echo "Tools directory: $(PWD)/tools"
	@$(ORLA_BIN) serve --port $(PORT) --pretty

test:
	@echo "testing HTTP transport"; \
	echo ""; \
	echo "testing hello tool (HTTP)"; \
	$(ORLA_TEST_BIN) call --transport http --port $(PORT) --tool hello --args '{"name":"World"}' || true; \
	echo ""; \
	echo "testing calculator tool (HTTP)"; \
	$(ORLA_TEST_BIN) call --transport http --port $(PORT) --tool calculator --args '{"operation":"multiply","a":7,"b":6}' || true; \
	echo ""; \
	echo "testing stdio transport"; \
	echo ""; \
	echo "testing hello tool (stdio)"; \
	$(ORLA_TEST_BIN) call --transport stdio --orla-bin $(ORLA_BIN) --tool hello --args '{"name":"World"}' || true; \
	echo ""; \
	echo "testing calculator tool (stdio)"; \
	$(ORLA_TEST_BIN) call --transport stdio --orla-bin $(ORLA_BIN) --tool calculator --args '{"operation":"multiply","a":7,"b":6}' || true

clean:
	@echo "Nothing to clean"
