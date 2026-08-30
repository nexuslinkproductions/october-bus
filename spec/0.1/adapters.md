# Adapter contract

An adapter connects one harness execution to October Bus without changing protocol semantics.

## Required lifecycle

1. Receive a scope credential through a protected bootstrap channel.
2. Register a stable logical agent ID and a new execution.
3. Keep the scope credential outside the harness process.
4. Give the harness only its execution-bound agent credential.
5. Configure the public HTTP or MCP endpoint.
6. Renew the lease outside the model loop.
7. Stop the host if execution authority is replaced and safe continuation cannot be proven.
8. Mark the execution offline during clean shutdown.
9. Allow lease expiry to recover state after an unclean shutdown.

## Required behavior

An adapter MUST:

- use exact agent IDs for programmatic addressing;
- declare only capabilities it implements;
- preserve durable inbox and acknowledgement behavior;
- keep task claims tied to the current execution;
- keep heartbeat active while the execution holds a claim;
- preserve the harness's own permission system;
- keep tokens out of logs and shared configuration;
- report only lifecycle and readiness states supported by host evidence;
- document pull-only delivery when it cannot wake the host;
- cleanly identify its harness, adapter, and supported protocol version.

## Optional behavior

An adapter MAY provide native hooks for wake, working, idle, needs-input, completion, or bounded-context mapping. Every optional claim requires a matching conformance test.

## Manifest

Each adapter includes `adapter.json`, validated by [adapter-manifest.schema.json](schemas/adapter-manifest.schema.json).

An `experimental` manifest describes integration work but is never compatibility evidence. Only a `verified` manifest with current public evidence can appear in the compatibility registry.
