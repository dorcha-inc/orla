# registry example

This example demonstrates how to populate the tool registry directly from the configuration file, without using directory scanning.


1. Build orla (if you haven't already):
   ```bash
   cd ../..
   make build
   ```

   This creates `bin/orla` which the Makefile uses by default.

2. Start orla with config:
   ```bash
   make run
   ```
   
   Or manually:
   ```bash
   ../../bin/orla serve --config orla.yaml
   ```

3. Test the tools:
   ```bash
   make test
   ```

## configuration

The `orla.yaml` file defines tools directly in the `tools_registry` field:

```yaml
tools_registry:
  tools:
    echo:
      name: echo
      description: Echo a message
      path: ./tools/echo.sh
      interpreter: /bin/bash
    date:
      name: date
      description: Get current date and time
      path: ./tools/date.sh
      interpreter: /bin/bash
port: 8080
timeout: 30
log_format: pretty
log_level: info
```

Some key points here:

- No `tools_dir` field, tools are defined directly in `tools_registry`.
- Each tool has explicit `name`, `description`, `path`, and `interpreter`.
- Paths are relative to the config file directory.
- You can specify custom descriptions (not just inferred from filenames).
- Tools can be anywhere on the filesystem (not just in one directory).

## when to use registry mode

Use `tools_registry` directly when you want to:

1. Scatter tools across directories
2. Provide custom descriptions that override auto-generated descriptions
3. Have explicit control to specify exact paths and interpreters
4. Avoid filesystem scanning overhead
5. Have dynamic tool registration, so tools can be added/removed via config changes

## path resolution

Tool paths in the registry are resolved relative to the config file directory:
- `./tools/echo.sh` -> relative to `orla.yaml` location
- `/absolute/path/to/tool` -> absolute path (used as-is)