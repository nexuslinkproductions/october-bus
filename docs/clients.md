# Client SDKs

October Bus currently ships a Go client in this module and a TypeScript client on npm.

## Credentials

Use the narrowest credential for each operation:

- admin token for scope creation and daemon shutdown;
- scope token for agent registration, peer links, and human escalation resolution;
- agent token for heartbeat, discovery, messages, tasks, and escalation creation.

Keep admin and scope tokens outside model context. A managed session gives the harness only its execution-bound agent token.

## Go

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

owner := bus.Client{Address: address, Token: scopeToken}
registration, err := owner.RegisterAgent(ctx, bus.RegisterAgentInput{
    ID:          "reviewer",
    DisplayName: "Reviewer",
    ConnectTo:   []string{"planner"},
})
if err != nil {
    return err
}

agent := bus.Client{Address: address, Token: registration.AgentToken}
peers, err := agent.ListPeers(ctx)
```

Every Go call accepts a context. The default HTTP client has a 30-second timeout. Supply `Client.HTTP` to set a different transport or timeout.

Use `bus.StartAgentSession` when an adapter needs registration, heartbeat, execution-replacement detection, and clean offline state managed outside the model loop.

## TypeScript

Install the current prerelease:

```bash
npm install october-bus@next
```

```ts
import { OctoberBusAgentSession } from 'october-bus'

const session = await OctoberBusAgentSession.start({
  address,
  scopeToken,
  registration: {
    id: 'reviewer',
    displayName: 'Reviewer',
    connectTo: ['planner']
  }
})

await session.setState('ready', true)
const peers = await session.client.listPeers({ timeoutMs: 10_000 })
```

Each TypeScript operation accepts an optional final `{ timeoutMs, signal }` argument. The default timeout is 30 seconds.

Use `pollInbox` for abortable pull delivery with bounded backoff. Use `withClaimedTask` to release a task if work or completion fails. Keep the managed session alive while holding a claim.

## Errors

Go returns `*bus.BusError`. TypeScript throws `BusError`. Branch on the protocol error code instead of matching the human-readable message.

```ts
import { BusError } from 'october-bus'

try {
  await session.client.claimTask(taskId)
} catch (error) {
  if (error instanceof BusError && error.code === 'CONFLICT') {
    // The task is blocked, done, or claimed by another execution.
  }
}
```

The public error codes and HTTP mappings are defined in the [protocol specification](../spec/0.1/README.md).
