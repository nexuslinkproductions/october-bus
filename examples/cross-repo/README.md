# Cross-repository coordination example

This example demonstrates two agents coordinating work across separate repositories
without sharing full workspaces — they exchange only the bounded context (contract spec)
needed for shared work.

**Flow**: discovery → bounded-context delegation → dependent tasks → reply → ack → receipt verification.

## Run

```sh
go run ./examples/cross-repo
```

No daemon, no Desktop, no cloud service required. The example spins up an in-memory
October Bus runtime for the duration of the demo.

## What it shows

- Two agents (`frontend` and `backend`) registered in separate conceptual working directories.
- Agent discovery via `ListPeers` after linking.
- Bounded-context message: the frontend sends only the API contract spec, not its full workspace.
- Dependent tasks: the frontend integration task depends on the backend change task.
- Backend receives, replies, acknowledges, and completes its task.
- Frontend receives the reply and acknowledges.
- Final delivery receipts and task states printed.

## Test

```sh
go test ./examples/cross-repo/
```

The test asserts the ack count is exactly one for each agent and that messages
round-trip from send through delivery to acknowledgement.
