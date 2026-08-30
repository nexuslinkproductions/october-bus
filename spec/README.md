# October Bus protocol

This directory contains the public October Bus protocol specification.

| Version | Status | Documents |
| --- | --- | --- |
| 0.1 | Draft | [Overview](0.1/README.md) · [HTTP API](0.1/http.md) · [MCP mapping](0.1/mcp.md) · [Adapter contract](0.1/adapters.md) |

Protocol versions are separate from runtime and SDK versions. A runtime reports its protocol version from `GET /health`.

Before 1.0, a protocol release may make breaking changes. A breaking change must use a new protocol version and retain the previous specification in this directory.

## Language

The words MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY describe protocol requirements.

## Changes

Protocol changes should include:

1. the specification update;
2. matching JSON Schema changes;
3. reference-runtime tests;
4. conformance tests;
5. migration notes when existing clients are affected.

New extension or negotiation mechanisms should be added only after real integrations demonstrate the need.
