import { expect, test } from 'bun:test'
import { hitMarker, sceneRows, unitAt, xAt } from '../app/lib/timelineGeometry'

for (const axis of ['time', 'index']) {
  test(`nested waits pick their own visible depth on the ${axis} axis`, () => {
    const outer = { id: 'outer', from: 0, to: 10, fromAt: 0, toAt: 100, depth: 0 }
    const inner = { id: 'inner', from: 2, to: 8, fromAt: 20, toAt: 80, depth: 1 }
    const lane = { kind: 'waits', id: 'waits', group: 'steep', waits: [outer, inner] }
    const scene = sceneRows([{ kind: 'lane', lane }], [], { axis, zoom: axis === 'time' ? 2 : 20, scrollX: 10 }, 0, 1000, 300)
    for (const marker of scene[0].markers) {
      const { x, y, width, height } = marker.rect
      expect(hitMarker(scene, x + width / 2, y + height / 2)?.wait.id).toBe(marker.wait.id)
    }
    const gap = scene[0].markers[0].rect
    expect(hitMarker(scene, gap.x + gap.width / 2, gap.y + gap.height + 1)).toBeUndefined()
  })
  test(`viewport transformations round trip on the ${axis} axis`, () => {
    const view = { axis, zoom: 2.5, scrollX: 471 }
    expect(unitAt(xAt(140, view), view)).toBe(140)
  })
}
