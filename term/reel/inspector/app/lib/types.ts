// Shapes of what the reel WebAssembly module returns; see ../../wasm.

export interface CastHeader {
  cols: number
  rows: number
  title?: string
  skip_pristine?: boolean
  scrollback: number
  cell_width?: number
  cell_height?: number
  kitty_storage?: number
  fg?: string
  bg?: string
  cursor?: string
  palette: string[]
}

export type EventType =
  | 'output' | 'input' | 'resize' | 'mark' | 'focus' | 'scroll'
  | 'scrollback' | 'theme' | 'exit' | 'annotation'

export interface CastEvent {
  i: number
  at: number
  type: EventType
  label?: string
  size?: number
  kind?: string
  data?: Record<string, unknown>
  tags?: Record<string, number>
  category?: string
  source?: Source
}

// Source is where in Go source a mark or annotation was made, when the
// recording has sources (reel.WithSource, steep's STEEP_SOURCE).
export interface Source {
  file: string
  line: number
  func?: string
}

export interface Summary {
  header: CastHeader
  duration: number
  events: CastEvent[]
  // display is what the cast's sequences turn on by default, and
  // display_reason why, for each part that is on.
  display: Display
  display_reason?: Record<string, string>
}

// Display is which parts reel draws around and over the grid.
export interface Display {
  chrome: boolean
  scrollbar: boolean
  progress: boolean
  status: boolean
  prompts: boolean
  links: boolean
  bell: boolean
}

export const displayParts: { key: keyof Display, label: string, about: string }[] = [
  { key: 'chrome', label: 'Window chrome', about: 'A title bar with the title and working directory' },
  { key: 'scrollbar', label: 'Scrollbar', about: 'Where the screen is in the scrollback' },
  { key: 'progress', label: 'Progress bar', about: 'OSC 9;4 progress reports' },
  { key: 'status', label: 'Program status', about: 'OSC 7501 records, in the title bar' },
  { key: 'prompts', label: 'Prompt marks', about: 'OSC 133 exit badges and scrollbar ticks' },
  { key: 'links', label: 'Link underlines', about: 'Underline OSC 8 hyperlinks' },
  { key: 'bell', label: 'Bell', about: 'Flash the window on BEL' },
]

export interface Token {
  continues?: boolean
  o: number
  len: number
  g: string
  t?: string
  n?: string
  d?: string
  r: string
  // f breaks the sequence out, such as an OSC payload's keys.
  f?: { k: string, v: string }[]
}

export interface EventDetail {
  i: number
  raw?: string
  tokens?: Token[]
}

export interface ScreenCell {
  content?: string
  style?: {
    fg?: string
    bg?: string
    underline_color?: string
    underline?: number
    attrs?: number
  }
  link?: { url?: string, params?: string }
  width?: number
}

export interface Screen {
  title?: string
  rows: number
  cols: number
  alt_screen?: boolean
  focused?: boolean
  cursor?: { position: { x: number, y: number }, visible: boolean, style: number, blink: boolean }
  cells: (ScreenCell | null)[][]
}

export interface ColorUse {
  // Defaults name their role: 'default-fg' and 'default-bg' resolve independently.
  spec: string
  hex: string
  fg: number
  bg: number
  ul: number
}

export interface FrameState {
  at: number
  applied: number
  title?: string
  working_directory?: string
  focused: boolean
  alt_screen: boolean
  reverse_colors: boolean
  cursor: { X: number, Y: number, Visible: boolean, Blink: boolean, Shape: number }
  fg: string
  bg: string
  cursor_color?: string
  scrollback_len: number
  scrollback_offset: number
  progress?: string
  bells: number
  notifications?: { At: number, Title: string, Body: string }[]
  clipboard?: string
  prompt?: string
  prompt_marks?: { Row: number, Ended: boolean, HasExitCode: boolean, ExitCode: number }[]
  program_status?: { id?: string, state: string, kind?: string, app?: string, title?: string, message?: string, progress?: number }[]
  palette_set?: Record<string, string>
  generation: number
}

export interface FrameData {
  layout: { x: number, y: number, cell_w: number, cell_h: number }
  state: FrameState
  cols: number
  rows: number
  colors: ColorUse[]
}

export interface RenderedFrame extends FrameData {
  width: number
  height: number
  bitmap: ImageBitmap
  // scale is what the frame was drawn at, set by the page.
  scale?: number
}

export interface CellsResponse {
  screen: Screen
}

export type Frame = RenderedFrame
