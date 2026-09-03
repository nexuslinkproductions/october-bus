import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { once } from 'node:events'
import {
  BusError,
  OctoberBusAgentSession,
  OctoberBusClient,
  OctoberBusScopeClient,
  newIdempotencyKey,
  pollInbox,
  requiredEnvironmentValue,
  withClaimedTask
} from '../dist/index.js'

async function expectInternal(work) {
  try {
    await work()
    assert.fail('request unexpectedly succeeded')
  } catch (error) {
    assert(error instanceof BusError)
    assert.equal(error.code, 'INTERNAL')
  }
}

await expectInternal(() => new OctoberBusClient('not a url', 'token').listPeers())

assert.equal(requiredEnvironmentValue({ OCTOBER_BUS_TOKEN: 'token' }, 'OCTOBER_BUS_TOKEN'), 'token')
assert.throws(
  () => requiredEnvironmentValue({}, 'OCTOBER_BUS_TOKEN'),
  /OCTOBER_BUS_TOKEN is required/
)
assert.match(newIdempotencyKey(), /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)

const abortController = new AbortController()
abortController.abort(new Error('stopped'))
await assert.rejects(
  () =>
    OctoberBusAgentSession.start({
      address: 'not a url',
      scopeToken: 'token',
      registration: { id: 'aborted', displayName: 'Aborted' },
      signal: abortController.signal
    }),
  /stopped/
)
await assert.rejects(
  () =>
    OctoberBusAgentSession.start({
      address: 'not a url',
      scopeToken: 'token',
      registration: { id: 'offline-ready', displayName: 'Offline Ready' },
      initialLifecycle: 'offline',
      initialReady: true
    }),
  /offline agents cannot be ready/
)

let releasedTask
const failingTaskClient = {
  async claimTask(taskId) {
    return { id: taskId, status: 'claimed' }
  },
  async releaseTask(taskId) {
    releasedTask = taskId
  }
}
await assert.rejects(
  () =>
    withClaimedTask(failingTaskClient, 'task_failure', async () => {
      throw new Error('work failed')
    }),
  /work failed/
)
assert.equal(releasedTask, 'task_failure')

let inboxPolls = 0
const pollingClient = {
  async pullInbox(limit, options) {
    assert.equal(limit, 50)
    assert.equal(options.waitMs, 25_000)
    inboxPolls += 1
    return inboxPolls === 1 ? [] : [{ id: 'message_1' }]
  }
}
const inbox = pollInbox(pollingClient)
const polled = await inbox.next()
assert.equal(polled.done, false)
assert.equal(polled.value[0].id, 'message_1')
await inbox.return()
await assert.rejects(() => pollInbox(pollingClient, { waitMs: 0 }).next(), /waitMs must be an integer between 1 and 25000/)
await assert.rejects(
  () => new OctoberBusScopeClient('not a url', 'token').watchEvents({ waitMs: 0 }).next(),
  /waitMs must be an integer between 1 and 25000/
)

// drainOnReady: a setState that flips ready false->true must reserve the inbox
// once so queued deliveries drain. A stub bus records the reserve call.
{
  const agent = {
    id: 'drainer',
    displayName: 'Drainer',
    capabilities: [],
    lifecycle: 'ready',
    ready: true,
    executionId: 'exec_1',
    leaseExpiresAt: new Date(Date.now() + 300_000).toISOString(),
    registeredAt: new Date().toISOString(),
    updatedAt: new Date().toISOString()
  }
  let reserves = 0
  const stub = createServer((req, response) => {
    const reply = (value) => {
      response.writeHead(200, { 'content-type': 'application/json' })
      response.end(JSON.stringify({ ok: true, result: value }))
    }
    if (req.method === 'POST' && req.url === '/v1/agents') {
      return reply({ scopeId: 'scope_1', agentId: 'drainer', executionId: 'exec_1', agentToken: 'tok', leaseExpiresAt: agent.leaseExpiresAt })
    }
    if (req.method === 'PATCH' && req.url === '/v1/me/heartbeat') return reply(agent)
    if (req.method === 'POST' && req.url === '/v1/inbox/reserve') {
      reserves += 1
      return reply(null)
    }
    response.writeHead(404, { 'content-type': 'application/json' })
    response.end(JSON.stringify({ ok: false, error: { code: 'NOT_FOUND', message: 'nope' } }))
  })
  stub.listen(0, '127.0.0.1')
  await once(stub, 'listening')
  try {
    const address = `http://127.0.0.1:${stub.address().port}`
    const session = await OctoberBusAgentSession.start({
      address,
      scopeToken: 'scope-tok',
      registration: { id: 'drainer', displayName: 'Drainer' },
      drainOnReady: true,
      heartbeatIntervalMs: 60_000
    })
    const before = reserves
    await session.setState('ready', true) // false -> true: must drain
    assert.equal(reserves, before + 1, 'ready transition must reserve inbox once')
    await session.setState('ready', true) // already ready: no extra drain
    assert.equal(reserves, before + 1, 'repeat ready must not reserve again')
    await session.setState('working', true) // lifecycle change, still ready: no drain
    assert.equal(reserves, before + 1, 'lifecycle-only change must not reserve')
    await session.close()
  } finally {
    stub.closeAllConnections()
    stub.close()
    await once(stub, 'close')
  }
}

const server = createServer((_request, response) => {
  response.writeHead(502, { 'content-type': 'text/plain' })
  response.end('upstream unavailable')
})
server.listen(0, '127.0.0.1')
await once(server, 'listening')

try {
  const address = server.address()
  assert(address && typeof address === 'object')
  await expectInternal(() =>
    new OctoberBusClient(`http://127.0.0.1:${address.port}`, 'token').listPeers()
  )
} finally {
  server.close()
  await once(server, 'close')
}

const hangingServer = createServer(() => {})
hangingServer.listen(0, '127.0.0.1')
await once(hangingServer, 'listening')

try {
  const address = hangingServer.address()
  assert(address && typeof address === 'object')
  const client = new OctoberBusClient(`http://127.0.0.1:${address.port}`, 'token')
  await assert.rejects(() => client.listPeers({ timeoutMs: 10 }), /timed out after 10ms/)
  const cancelled = new AbortController()
  cancelled.abort(new Error('cancelled by caller'))
  await assert.rejects(() => client.listPeers({ signal: cancelled.signal }), /cancelled by caller/)
  await assert.rejects(() => client.listPeers({ timeoutMs: 0 }), (error) => {
    assert(error instanceof BusError)
    assert.equal(error.code, 'INVALID_ARGUMENT')
    return true
  })
} finally {
  hangingServer.closeAllConnections()
  hangingServer.close()
  await once(hangingServer, 'close')
}
