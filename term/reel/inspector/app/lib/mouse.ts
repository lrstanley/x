// mouseAt is the time in ms to draw the mouse as of, with the playhead at
// t: t while the mouse moves, in one of spans (the summary's mouse_spans),
// or else the end of the last span before t, as the mouse has held still
// since, so the frame is not drawn again until it moves.
export function mouseAt(spans: readonly (readonly [number, number])[], t: number): number {
  let still = 0
  for (const [start, end] of spans) {
    if (t < start)
      break
    if (t < end)
      return t
    still = end
  }
  return still
}
