.PHONY: help run test clean

ORLA_BIN ?= ../../bin/orla
ORLA_TEST_BIN ?= ../../bin/orla-test
CONFIG ?= orla.yaml
PORT ?= 9090

help:
	@echo "Available commands:"
	@echo "  make run    - Start orla server with config"
	@echo "  make test   - Test the tools (requires orla-test binary)"
	@echo "  make clean  - Clean up temporary files"
	@echo "  make help   - Show this help"

run:
	@echo "Starting orla with config: $(CONFIG)"
	@echo "Tools directory: $(PWD)/tools"
	@echo "Port: $(PORT)"
	@$(ORLA_BIN) --config $(CONFIG)

test:
	@echo "testing HTTP transport"; \
	echo ""; \
	echo "testing greet tool (HTTP)"; \
	$(ORLA_TEST_BIN) call --transport http --port $(PORT) --tool greet --args '{"language":"es"}' --stdin "María" || true; \
	echo ""; \
	echo "testing file-info tool (HTTP)"; \
	$(ORLA_TEST_BIN) call --transport http --port $(PORT) --tool file-info --args '{"path":"/etc/hosts","format":"json","include-size":"true"}' || true; \
	echo ""; \
	echo "testing stdio transport"; \
	echo ""; \
	echo "testing greet tool (stdio)"; \
	$(ORLA_TEST_BIN) call --transport stdio --orla-bin $(ORLA_BIN) --tool greet --args '{"language":"fr"}' --stdin "Pierre" || true; \
	echo ""; \
	echo "testing file-info tool (stdio)"; \
	$(ORLA_TEST_BIN) call --transport stdio --orla-bin $(ORLA_BIN) --tool file-info --args '{"path":"/etc/hosts","format":"json","include-size":"true"}' || true

clean:
	@echo "Nothing to clean"
