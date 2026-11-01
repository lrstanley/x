import { expect, test } from 'bun:test'
import { mouseAt } from '../app/lib/mouse'

test('the mouse is drawn as of the playhead only while it moves', () => {
  const spans = [[500, 1750], [3000, 4250]]
  expect(mouseAt(spans, 100)).toBe(0)
  expect(mouseAt(spans, 600)).toBe(600)
  expect(mouseAt(spans, 1750)).toBe(1750)
  expect(mouseAt(spans, 2500)).toBe(1750)
  expect(mouseAt(spans, 3100)).toBe(3100)
  expect(mouseAt(spans, 9000)).toBe(4250)
  expect(mouseAt([], 600)).toBe(0)
})
