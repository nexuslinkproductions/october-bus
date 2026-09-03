# Proposed fix: `bus-not-ready` never recovers

## Bug

Terminal nodes that aren't ready when a message arrives are deferred with `bus-not-ready`, their retry timer is cleared, and nothing re-attempts delivery when the node becomes ready.

The ready-edge signal (`signalReady`) only persists the readiness flag — it never kicks the delivery engine.

## Evidence from shipped `october-core.js` (line refs are stable)

### 1. `bus-not-ready` clears the retry timer

In `DeliveryEngine.finish`:

```
if (result.status === "deferred" && result.retryAfterMs !== void 0)
  this.schedule(canvasId, nodeId, result.retryAfterMs);
else if (result.status !== "deferred" || result.reason !== "in-flight")
  this.clearRetry(nodeId);
```

`bus-not-ready` is deferred but has no `retryAfterMs`. The `else if` branch fires → any existing retry timer for that node is **deleted** and never re-armed.

### 2. `kick` returns `bus-not-ready` and stops

In `DeliveryEngine.kick`:

```
if (!binding.ready && !bootstrap)
  return this.finish(canvasId, nodeId, { status: "deferred", reason: "bus-not-ready" }, reason);
```

No `retryAfterMs` is set on this deferral. The only recovery path is the 20s sweep (line 4955), but that sweep only fires when `activePolicy` is set and `nodeAllowed` passes — if policy is off or the node is excluded, the message sits in the queue indefinitely.

### 3. `signalReady` never kicks delivery

```
async signalReady(canvasId, nodeId, bindingId, signal, at) {
  current.readySignals = { ...current.readySignals, [signal]: at };
  await this.persistMcp(key2, current);
  return this.isReady(current);
}
```

Both call sites (mcp-initialize at line 8574, adapter-ready at line 8793) call `signalReady` and nothing else. The transition to ready is silent to `DeliveryEngine`.

## Proposed fix (two changes in `october-core.js`)

### Change 1: Kick on the ready edge

In `CapabilityRegistry.signalReady`, after line 4103, add a delivery kick:

```js
async signalReady(canvasId, nodeId, bindingId, signal, at) {
  const key = this.key(canvasId, nodeId);
  const current = this.mcp.get(key);
  if (!current || current.bindingId !== bindingId) return false;
  current.readySignals = { ...current.readySignals, [signal]: at };
  current.updatedAt = Date.now();
  await this.persistMcp(key, current);
  const ready = this.isReady(current);
  if (ready) this.delivery.kick(canvasId, nodeId, "ready");  // <-- add
  return ready;
}
```

This is the root-cause fix. It mirrors the contract defined in october-bus PR #90 (spec clause) and verified in PR #91 (conformance `drain-on-ready` check).

### Change 2: Give `bus-not-ready` a bounded retry (belt-and-suspenders)

In `DeliveryEngine.kick`, where `bus-not-ready` is returned, add a one-second retry:

```js
return this.finish(canvasId, nodeId, { status: "deferred", reason: "bus-not-ready", retryAfterMs: 1000 }, reason);
```

Change 1 is the primary fix. Change 2 ensures a node that *briefly* isn't ready will retry naturally without waiting for a message arrival or sweep. Without it, a one-frame gap between the kick reading `binding.ready` and the ready signal landing can still leave the queue stuck.

## Contract reference

- october-bus PR #90: spec clause requiring hosts to resume inbox reservation on ready; TS SDK `drainOnReady`; Go runtime wake-on-ready.
- october-bus PR #91: conformance check `drain-on-ready` in the mcp-adapter profile that asserts inbox resumes after the ready edge.

Change 1 fulfills this contract in the desktop core: when an agent signals ready, the delivery engine kicks and resumes inbox reservation. Change 2 is a bounded retry fallback.

## Verification (conformance profile with a real adapter)

```
go build ./...
october-bus-conformance --profile mcp-adapter --start-runtime \
  --adapter-command october-bus --adapter-arg mcp --adapter-arg stdio

=> 14/14 passed, including "drain-on-ready"
```

The conformance check `drain-on-ready` verifies the exact scenario: register a host-controlled execution, queue a message while not ready, heartbeat ready=true, assert `ReserveInbox` returns the queued message. This proves the Go runtime + adapter stack are correct; the desktop core change 1 is the same contract on the other side.

## Files changed

| File | Change |
|------|--------|
| `october-core.js:4104` | Add `this.delivery.kick(...)` after ready edge |
| `october-core.js:4470` | Add `retryAfterMs: 1000` to bus-not-ready deferral |