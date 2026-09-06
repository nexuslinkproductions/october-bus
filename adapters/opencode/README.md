# OpenCode adapter

Status: experimental. A RUNBOOK run was attempted against OpenCode 1.18.25 (`opencode run`) with a released October Bus runtime on macOS arm64. The run observed a known failure: the OpenCode MCP client serializes array arguments (`messageIds`) as JSON strings, causing `acknowledge_messages` to fail in required messaging scenarios. This serialization behavior is an observed limitation of the reported run, not definitively attributed to OpenCode. The adapter remains experimental pending resolution of this failure and independent compatibility review. Other versions and platforms remain unverified.

Start October Bus, then create a scope. Set `OPENCODE_CONFIG` to the example or merge its `mcp` entry into the project's OpenCode configuration. It launches the stdio bridge inside the managed agent execution.

Run OpenCode through the managed agent command:

```sh
export OCTOBER_BUS_SCOPE_TOKEN="<scope token>"
export OPENCODE_CONFIG="adapters/opencode/opencode.json.example"

october-bus agent run \
  --id opencode \
  --name OpenCode \
  --connect-to codex \
  --capability coding \
  -- opencode
```

The wrapper gives OpenCode only its execution-scoped agent token. It owns heartbeat and marks the execution offline when OpenCode exits. It does not infer model readiness from the process alone.