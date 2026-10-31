import { afterEach, expect, test } from 'bun:test'
import { createWorkerClient } from '../app/lib/workerClient'

const original = globalThis.Worker
let worker
class FakeWorker {
  constructor() { worker = this }
  requests = []
  postMessage(value) { this.requests.push(value) }
  terminate() {}
  send(data) { this.onmessage({ data }) }
}
afterEach(() => { globalThis.Worker = original })
function client() {
  globalThis.Worker = FakeWorker
  const fatal = []
  return { ...createWorkerClient('/', { ready() {}, progress() {}, fatal: error => fatal.push(error) }), fatal }
}

test('startup failure rejects requests waiting for readiness and future requests', async () => {
  const c = client()
  const result = c.rpc('unload', [1])
  worker.onerror({ message: 'startup failed' })
  await expect(result).rejects.toThrow('startup failed')
  await expect(c.rpc('unload', [2])).rejects.toThrow('startup failed')
  expect(c.fatal).toHaveLength(1)
})

test('message errors reject every pending request', async () => {
  const c = client()
  worker.send({ kind: 'ready' })
  const first = c.rpc('unload', [1])
  const second = c.rpc('unload', [2])
  await Promise.resolve()
  expect(worker.requests).toHaveLength(2)
  const failures = Promise.allSettled([first, second])
  worker.onmessageerror()
  for (const result of await failures) {
    expect(result.status).toBe('rejected')
    expect(result.reason.message).toContain('decode')
  }
  expect(c.fatal).toHaveLength(1)
})
