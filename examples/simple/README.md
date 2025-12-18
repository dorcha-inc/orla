# simple example

This example demonstrates the simplest way to use orla: zero configuration, just tools!

1. Build orla (if you haven't already):
   ```bash
   cd ../..
   make build
   ```
   
   This creates `bin/orla` which the Makefile uses by default.

2. Start orla:
   ```bash
   make run
   ```
   
   Or manually:
   ```bash
   ../../bin/orla serve
   ```

3. Test the tools:
   ```bash
   make test
   ```

orla automatically discovers executable files in the `tools/` directory. No configuration needed!

Some notes:

1. Tools are named after their filename (without extension)
2. Shebangs are automatically detected
3. Arguments are passed as `--key value` pairs
