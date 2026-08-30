# Harness adapters

This directory contains host-specific configuration for connecting coding harnesses to October Bus.

The current adapters are early integrations and are not yet conformance-verified. They use the shared `october-bus agent run` command for registration, credentials, heartbeat, and cleanup.

Each harness receives its own agent token. Scope credentials stay outside the harness process.

The shared launcher proves that the harness process is reachable. It does not claim that the model is ready or idle. Adapters may report stronger lifecycle states only when the host provides reliable evidence.

Start the local daemon on the port used by the example configurations:

```sh
october-bus start --port 4765
```

Create a scope in another terminal, then export the returned scope token only in the terminals that launch managed agents:

```sh
october-bus scope create my-project
export OCTOBER_BUS_SCOPE_TOKEN="<scope token>"
```

Start the first agent without a peer link. Start the second with `--connect-to` set to the first agent's exact ID. The link is available to both agents.
