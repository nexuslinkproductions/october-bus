import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import {
  OctoberBusAdminClient,
  OctoberBusAgentSession,
  OctoberBusClient,
  OctoberBusScopeClient,
  newIdempotencyKey,
  pollInbox
} from '../dist/index.js'

const binary = process.env.OCTOBER_BUS_BINARY
if (!binary) throw new Error('OCTOBER_BUS_BINARY is required')

const root = await mkdtemp(join(tmpdir(), 'october-bus-ready-edge-'))
const dataDir = join(root, 'data')
const runtimeDir = join(root, 'run')
const runFile = join(runtimeDir, 'bus.json')
const child = spawn(binary, ['start'], {
  env: {
    ...process.env,
    OCTOBER_BUS_DATA_DIR: dataDir,
    OCTOBER_BUS_RUNTIME_DIR: runtimeDir
  },
  stdio: ['ignore', 'pipe', 'pipe']
})

let stderr = ''
child.stderr.setEncoding('utf8')
child.stderr.on('data', (value) => {
  stderr += value
})

async function readRunFile() {
  const deadline = Date.now() + 10_000
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw new Error(`October Bus exited early: ${stderr}`)
    try {
      return JSON.parse(await readFile(runFile, 'utf8'))
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
  }
  throw new Error(`October Bus did not start: ${stderr}`)
}

let session

try {
  const run = await readRunFile()
  const admin = new OctoberBusAdminClient(run.address, run.adminToken)
  const scope = await admin.createScope({ id: 'ready-edge' })
  const owner = new OctoberBusScopeClient(run.address, scope.scopeToken)

  // Host starts NOT ready. Its inbox consumer blocks in a long-poll reserve.
  session = await OctoberBusAgentSession.start({
    address: run.address,
    scopeToken: scope.scopeToken,
    registration: { id: 'host', displayName: 'Host', leaseMs: 30_000 },
    heartbeatIntervalMs: 100,
    initialReady: false
  })

  const peer = await owner.registerAgent({ id: 'peer', displayName: 'Peer', connectTo: ['host'] })
  const peerClient = new OctoberBusClient(run.address, peer.agentToken)
  await peerClient.heartbeat('ready', true, 30_000)

  // Start the consumer loop first — it blocks in the server-side wait because
  // the inbox is empty. Then queue a delivery while the host is still NOT ready.
  const inbox = pollInbox(session.client, { waitMs: 25_000 })
  const first = inbox.next()

  // Give the loop time to block inside its first waitMs reserve.
  await new Promise((resolve) => setTimeout(resolve, 200))

  // Queue the delivery while the host is still not ready and blocked in reserve.
  const receipt = await peerClient.sendMessage({
    to: 'host',
    body: 'queued before ready',
    idempotencyKey: newIdempotencyKey()
  })

  // Now transition to ready. The server wakes the blocked reserve, delivering
  // the queued message through the in-flight long-poll.
  const startedAt = Date.now()
  await session.setState('ready', true)
  const result = await first
  assert.equal(result.done, false)
  assert.equal(result.value.length, 1)
  assert.equal(result.value[0].id, receipt.messageId)
  assert.equal(Date.now() - startedAt < 3_000, true, `ready-edge wake should resume promptly, took ${Date.now() - startedAt}ms`)
  assert.equal(await session.client.acknowledgeMessages([receipt.messageId]), 1)
  await inbox.return()

  // The successful false→true edge aborted the old wake signal and replaced it
  // with a live one.
  assert.equal(session.wake.aborted, false, 'wake signal must be live after a successful transition')

  // Failed false→true transition must NOT commit ready locally and must NOT
  // fire the wake signal. The local state stays false so the edge can be retried.
  // Kill the server to force a heartbeat failure.
  child.kill('SIGTERM')
  await Promise.race([
    new Promise((resolve) => child.once('exit', resolve)),
    new Promise((resolve) => setTimeout(resolve, 5_000))
  ])
  if (child.exitCode === null) child.kill('SIGKILL')

  const oldWake = session.wake
  // This is a false→true attempt that must fail (server is dead).
  await assert.rejects(
    () => session.setState('ready', true),
    (error) => {
      assert(error instanceof Error)
      return true
    }
  )
  // A failed heartbeat must not fire the wake signal — the false→true edge
  // was not committed, so no consumer should wake.
  assert.equal(oldWake.aborted, false, 'a failed heartbeat must not fire the wake signal')
} finally {
  await session?.close().catch(() => {})
  if (child.exitCode === null) child.kill('SIGKILL')
  await rm(root, { recursive: true, force: true })
}
