import type { EventDetail, Frame, Screen } from './types'
import type { FrameOptions, Rpc } from './workerProtocol'

export interface FrameTarget extends FrameOptions {
  readonly generation: number
  readonly index: number
}

export function targetKey(target: FrameTarget): string {
  return JSON.stringify(target)
}

// sameScreen reports whether a and b show the same event drawn the same
// way, if perhaps with the mouse as of another time. A frame is still
// shown once the playhead has moved the mouse on, or one that takes longer
// than an animation frame would never be.
function sameScreen(a: FrameTarget, b: FrameTarget): boolean {
  return targetKey({ ...a, at: undefined }) === targetKey({ ...b, at: undefined })
}

export function createFrameScheduler(rpc: Rpc, target: () => FrameTarget | null, commit: (frame: Frame) => void, fail: (error: Error) => void) {
  let shown = ''
  let running = false
  async function render() {
    if (running)
      return
    running = true
    let requested: FrameTarget | null = null
    try {
      while ((requested = target()) && shown !== targetKey(requested)) {
        const want = requested
        const frame = await rpc('frame', [want.generation, want.index, { scale: want.scale, display: want.display, at: want.at }])
        const current = target()
        if (current && sameScreen(current, want)) {
          shown = targetKey(want)
          commit(frame)
        }
        else {
          frame.bitmap.close()
        }
      }
    }
    catch (err) {
      const current = target()
      if (current && requested && targetKey(current) === targetKey(requested))
        fail(err as Error)
    }
    finally {
      running = false
      const current = target()
      // A newer target may have arrived while an obsolete request failed.
      if (current && requested && targetKey(current) !== targetKey(requested))
        void render()
    }
  }
  return { render }
}

export class InspectorSession {
  readonly details = new Map<number, Promise<EventDetail>>()
  cells: { key: string, screen: Promise<Screen | undefined> } | null = null
  constructor(readonly generation: number) {}
}
