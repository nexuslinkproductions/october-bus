# October Bus TypeScript Client

Typed clients and protocol definitions for connecting Node.js applications and harness adapters to October Bus.

The Bus daemon is distributed separately as a native executable.

The client is in active development and has not reached a stable release. Before 1.0, its API, schemas, and protocol behavior may change between releases.

## Install

When a prerelease is available, install it from the `next` tag:

```sh
npm install october-bus@next
```

## Example

Start the daemon, create a scope with the October Bus CLI, and set the returned token as `OCTOBER_BUS_SCOPE_TOKEN`.

```ts
import { OctoberBusClient, OctoberBusScopeClient } from 'october-bus'

const address = 'http://127.0.0.1:4765'
const scope = new OctoberBusScopeClient(address, process.env.OCTOBER_BUS_SCOPE_TOKEN!)

const plannerRegistration = await scope.registerAgent({
  id: 'planner',
  displayName: 'Planner'
})
const reviewerRegistration = await scope.registerAgent({
  id: 'reviewer',
  displayName: 'Reviewer',
  connectTo: ['planner']
})

const planner = new OctoberBusClient(address, plannerRegistration.agentToken)
const reviewer = new OctoberBusClient(address, reviewerRegistration.agentToken)

const receipt = await planner.sendMessage({
  to: 'reviewer',
  mode: 'request',
  body: 'Review the retry path',
  idempotencyKey: crypto.randomUUID()
})

const messages = await reviewer.pullInbox()
await reviewer.acknowledgeMessages(messages.map((message) => message.id))
console.log(receipt.messageId, messages)
```

Scope credentials create agents and handle human escalations. Agent credentials discover peers, exchange messages, coordinate tasks, and ask for human input.

Generate a new idempotency key for each logical send. Keys remain bound to their original message. Keep heartbeats running while an execution holds a task claim, or the claim may be released for another agent.
