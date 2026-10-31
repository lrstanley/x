// @ts-check
/// <reference lib="webworker" />
/** @typedef {import('../app/lib/workerProtocol').Request} WorkerRequest */
/** @typedef {import('../app/lib/workerProtocol').Response} WorkerResponse */
/** @typedef {{error?: string, json: string, pixels?: Uint8Array, width: number, height: number, more?: boolean}} WasmResult */
/** @typedef {{Go: new () => {importObject: WebAssembly.Imports, run: (instance: WebAssembly.Instance) => Promise<void>}, reel: Record<string, (...args: unknown[]) => WasmResult>}} WasmRuntime */
const runtime = /** @type {WasmRuntime} */ (/** @type {unknown} */ (globalThis))
/** @param {WorkerResponse} message @param {Transferable[]} [transfer] */
function send(message, transfer = []) { postMessage(message, transfer) }
// Runs reel, compiled to WebAssembly from ../wasm, off the main thread. The
// page posts {id, op, args} and gets back {id, result} or {id, error}; while
// the module downloads, it also gets {progress: {loaded, total}}.
importScripts('wasm_exec.js')

async function fetchModule() {
  const res = await fetch('reel.wasm')
  if (!res.ok) throw new Error(`fetching reel.wasm: ${res.status} ${res.statusText}`)
  if (!res.body) throw new Error('Missing WebAssembly response body')
  const total = Number(res.headers.get('content-length')) || 0
  const reader = res.body.getReader()
  const chunks = []
  let loaded = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    chunks.push(value)
    loaded += value.length
    send({ kind: 'progress', progress: { loaded, total } })
  }
  return new Blob(chunks).arrayBuffer()
}

const ready = (async () => {
  const go = new runtime.Go()
  const { instance } = await WebAssembly.instantiate(await fetchModule(), go.importObject)
  go.run(instance).catch(err => send({ kind: 'fatal', error: String(err) }))
})()

/** @param {string} op @param {unknown[]} args */
function call(op, args) {
  const out = runtime.reel[op](...args)
  if (out.error) throw new Error(out.error)
  return out
}

let generation = -1

/** @param {WorkerRequest} request */
async function handle(request) {
  const { id, op, args } = request
  if (op === 'unload') {
    generation = args[0]
    call('unload', [])
    send({ kind: 'result', id, op, result: null })
    return
  }
  if (op === 'load') {
    generation = args[0]
    send({ kind: 'result', id, op, result: JSON.parse(call('load', [args[1], args[2]]).json) })
    return
  }
  if (args[0] !== generation) throw new Error('Recording was replaced')
  if (op === 'detail') {
    send({ kind: 'result', id, op, result: JSON.parse(call('detail', [args[1]]).json) })
    return
  }
  const out = call('frame', [args[1], JSON.stringify({ ...args[2], image: op === 'frame', cells: op === 'cells' })])
  const result = JSON.parse(out.json)
  if (op === 'cells') {
    send({ kind: 'result', id, op, result: { screen: result.screen } })
    return
  }
  if (!out.pixels) throw new Error('Missing frame pixels')
  const bitmap = await createImageBitmap(new ImageData(new Uint8ClampedArray(out.pixels), out.width, out.height))
  try {
    send({ kind: 'result', id, op, result: { ...result, width: out.width, height: out.height, bitmap, scale: args[2].scale } }, [bitmap])
  }
  catch (err) {
    bitmap.close()
    throw err
  }
}

// Between requests, the worker builds the checkpoints that keep seeking back
// quick, in slices short enough that a request waits at most one slice. It
// starts once requests have stopped for a moment, so playback and scrubbing
// come first.
let idleTimer = 0
function scheduleIdle() {
  clearTimeout(idleTimer)
  idleTimer = setTimeout(idle, 100)
}
function idle() {
  try {
    if (call('prepare', [8]).more)
      idleTimer = setTimeout(idle, 0)
  }
  catch {
    // Checkpoints only make seeking quicker; without them it still works.
  }
}

// Bitmap creation is asynchronous; serialize messages so load/unload cannot
// replace a player halfway through a request.
let queue = Promise.resolve()
onmessage = (/** @type {MessageEvent<WorkerRequest>} */ e) => {
  clearTimeout(idleTimer)
  queue = queue.then(async () => {
    try {
      await ready
      await handle(e.data)
    }
    catch (err) {
      send({ kind: 'error', id: e.data.id, error: err instanceof Error ? err.message : String(err) })
    }
  })
  const last = queue
  queue.then(() => {
    if (last === queue) scheduleIdle()
  })
}

ready.then(() => send({ kind: 'ready' }), err => send({ kind: 'fatal', error: String(err?.message ?? err) }))
