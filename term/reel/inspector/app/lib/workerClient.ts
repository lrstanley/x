import type { Operation, Operations, Response, Rpc } from './workerProtocol'

export function createWorkerClient(base: string, callbacks: {
  ready: () => void
  progress: (value: { loaded: number, total: number }) => void
  fatal: (error: Error) => void
}): { rpc: Rpc } {
  const pending = new Map<number, { resolve: (message: Response) => void, reject: (error: Error) => void }>()
  let nextId = 0
  let failure: Error | undefined
  let markReady: () => void
  let rejectReady: (error: Error) => void
  const ready = new Promise<void>((resolve, reject) => {
    markReady = resolve
    rejectReady = reject
  })
  // Startup can fail before the first caller needs the worker.
  ready.catch(() => {})
  const fail = (error: Error) => {
    if (failure)
      return
    failure = error
    rejectReady(error)
    for (const p of pending.values())
      p.reject(error)
    pending.clear()
    worker?.terminate()
    callbacks.fatal(error)
  }
  let worker: Worker | undefined
  try {
    worker = new Worker(`${base}reel-worker.js`)
    worker.onerror = e => fail(new Error(e.message || 'Worker failed'))
    worker.onmessageerror = () => fail(new Error('Could not decode a worker response'))
    worker.onmessage = (e: MessageEvent<Response>) => {
      const m = e.data
      switch (m.kind) {
        case 'progress':
          callbacks.progress(m.progress)
          break
        case 'ready':
          markReady()
          callbacks.ready()
          break
        case 'fatal':
          fail(new Error(m.error))
          break
        case 'error':
        case 'result': {
          const p = pending.get(m.id)
          pending.delete(m.id)
          if (m.kind === 'error')
            p?.reject(new Error(m.error))
          else if (p)
            p.resolve(m)
          else if (m.op === 'frame')
            m.result.bitmap.close()
          break
        }
      }
    }
  }
  catch (err) {
    fail(err as Error)
  }
  async function rpc<K extends Operation>(op: K, args: Operations[K]['args']): Promise<Operations[K]['result']> {
    await ready
    if (failure)
      throw failure
    return new Promise((resolve, reject) => {
      const id = nextId++
      pending.set(id, {
        resolve: (m) => {
          if (m.kind === 'result' && m.op === op)
            resolve(m.result as Operations[K]['result'])
          else
            reject(new Error('Unexpected worker response'))
        },
        reject,
      })
      try {
        worker!.postMessage({ id, op, args })
      }
      catch (err) {
        pending.delete(id)
        reject(err)
      }
    })
  }
  return { rpc }
}
