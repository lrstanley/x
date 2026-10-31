import type { CellsResponse, Display, EventDetail, RenderedFrame, Summary } from './types'

export interface FrameOptions {
  readonly scale: number
  readonly display: Readonly<Display>
}

export interface Operations {
  load: { args: [generation: number, bytes: Uint8Array, scale: number], result: Summary }
  unload: { args: [generation: number], result: null }
  detail: { args: [generation: number, index: number], result: EventDetail }
  frame: { args: [generation: number, index: number, options: FrameOptions], result: RenderedFrame }
  cells: { args: [generation: number, index: number, options: FrameOptions], result: CellsResponse }
}

export type Operation = keyof Operations
export type Request = { [K in Operation]: { id: number, op: K, args: Operations[K]['args'] } }[Operation]
export type Response =
  | { kind: 'ready' }
  | { kind: 'progress', progress: { loaded: number, total: number } }
  | { kind: 'fatal', error: string }
  | { kind: 'error', id: number, error: string }
  | { [K in Operation]: { kind: 'result', id: number, op: K, result: Operations[K]['result'] } }[Operation]

export type Rpc = <K extends Operation>(op: K, args: Operations[K]['args']) => Promise<Operations[K]['result']>
