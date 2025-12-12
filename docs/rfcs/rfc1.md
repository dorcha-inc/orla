# RFC 1: Orla Runtime

status: complete

The vision behind orla's single-server runtime is to build something
that can be meaningfully used without having to set up a configuration.

orla must support a single port MCP server that can be
hot reloaded via `SIGHUP`.

```bash
orla run
```

should start an MCP server on a default port or stdio.

orla should automatically search for a `./tools/` directory
and load each executable file as a tool. It should default
to using the filename as a tool name. We can infer the 
interpreter via shebang if needed.

For example, the followign directory:

```bash
tools/
  ls.sh
  summarize.py
  convert
```

gives us three tools: ls, summarize, and convert. No configuration is required.

orla should execute the tools using `exec.CommandContext(...)` or something
similar. It should pipe stdin/stdout to MCP request/response. It should
enforce a a suitable timeout per request (30s).

This provides reliability, correct cancellation propagation, and
correct stdout/stderr handling.

If a user *does* want to specify a config, orla should support it. Following 
what [Anthropic does](https://www.anthropic.com/engineering/desktop-extensions), 
we are specifying orla's conifg in JSON.

```bash
orla run --config orla.json
```

With regards to hot reloading, orla should be able to support

```bash
kill -HUP $(orla_pid)
```

which should make orla reload configuration (if provided), re-scan the tools 
directory, and apply changes without a restart.

orla should also do robust logging in orla, with an optional
pretty-print in terminal mode. orla should log in json by default. orla's
logs should include tool executions, errors, request timings, failures, 
and panic recovery.

the scope of orla's runtime is the following capabilities:

1. invoking a request
2. listing tools
3. prompt to tool execution flow
4. cancellation tokens
5. error messages

The following will be added later:

1. routing
2. policies
3. support for plugins
4. queues
5. multi-backend support