# config example

This example demonstrates how to use orla with a simple configuration file.

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
   ../../bin/orla --config orla.json
   ```

3. Test the tools:
   ```bash
   make test
   ```

## configuration

The `orla.json` file customizes orla's behavior:

```json
{
  "tools_dir": "./tools",
  "port": 9090,
  "timeout": 60,
  "log_format": "pretty",
  "log_level": "info"
}
```

## why use a config file?

Configuration files are useful when you want to:
- Use a custom tools directory
- Change the default port
- Adjust timeout settings
- Use pretty-printed logs for development
- Share configuration across team members
