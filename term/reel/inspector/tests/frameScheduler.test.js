import { describe, expect, test } from 'bun:test'
import { createFrameScheduler } from '../app/lib/frameScheduler'

const display = { chrome: false, scrollbar: false, progress: false, status: false, prompts: false, links: false, bell: false }
function deferred() {
  let resolve, reject
  const promise = new Promise((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
function frame() {
  let closed = false
  return { bitmap: { close: () => closed = true }, isClosed: () => closed }
}

describe('frame targets', () => {
  for (const change of ['scale', 'display', 'generation', 'index']) {
    test(`discards an in-flight frame after changing ${change}`, async () => {
      let target = { generation: 1, index: 0, scale: 1, display }
      const first = deferred()
      const old = frame()
      const next = frame()
      const requests = []
      const commits = []
      const scheduler = createFrameScheduler((op, args) => {
        requests.push({ op, args })
        return requests.length === 1 ? first.promise : Promise.resolve(next)
      }, () => target, value => commits.push(value), err => { throw err })
      const rendering = scheduler.render()
      target = { ...target, [change]: change === 'display' ? { ...display, chrome: true } : 2 }
      first.resolve(old)
      await rendering
      expect(old.isClosed()).toBe(true)
      expect(commits).toEqual([next])
      expect(requests[1].args).toEqual([target.generation, target.index, { scale: target.scale, display: target.display }])
    })
  }
  test('shows a frame the mouse has moved on from, then the latest', async () => {
    let target = { generation: 1, index: 0, scale: 1, display: { ...display, mouse: true }, at: 100 }
    const first = deferred()
    const old = frame()
    const next = frame()
    const requests = []
    const commits = []
    const scheduler = createFrameScheduler((op, args) => {
      requests.push(args)
      return requests.length === 1 ? first.promise : Promise.resolve(next)
    }, () => target, value => commits.push(value), err => { throw err })
    const rendering = scheduler.render()
    target = { ...target, at: 120 }
    first.resolve(old)
    await rendering
    expect(old.isClosed()).toBe(false)
    expect(commits).toEqual([old, next])
    expect(requests[1][2].at).toBe(120)
  })
  test('close discards the pending bitmap without requesting another frame', async () => {
    let target = { generation: 1, index: 0, scale: 1, display }
    const pending = deferred()
    const old = frame()
    const commits = []
    const scheduler = createFrameScheduler(() => pending.promise, () => target, value => commits.push(value), err => { throw err })
    const rendering = scheduler.render()
    target = null
    pending.resolve(old)
    await rendering
    expect(old.isClosed()).toBe(true)
    expect(commits).toEqual([])
  })
  test('a stale failure does not stop the next target', async () => {
    let target = { generation: 1, index: 0, scale: 1, display }
    const pending = deferred()
    let calls = 0
    const commits = []
    const scheduler = createFrameScheduler(() => ++calls === 1 ? pending.promise : Promise.resolve(frame()), () => target, value => commits.push(value), () => {})
    const rendering = scheduler.render()
    target = { ...target, scale: 2 }
    pending.reject(new Error('obsolete'))
    await rendering
    await Promise.resolve()
    expect(commits).toHaveLength(1)
  })
})
