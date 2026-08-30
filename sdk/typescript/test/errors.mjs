import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { once } from 'node:events'
import { BusError, OctoberBusClient } from '../dist/index.js'

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
