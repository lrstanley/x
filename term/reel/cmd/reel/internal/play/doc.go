// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package play runs VHS tapes (https://github.com/charmbracelet/vhs) with
// reel: [Parse] reads a tape, and [Tape.Run] types its commands into a shell
// or an in-process program on a [reel.Session], and writes its outputs with
// reel's renderer and effects. [WithLook] and [WithProfile] set how it looks
// and what it writes over the tape's own settings and outputs.
//
// A tape runs commands with the caller's privileges and writes files where
// its Output and Screenshot commands say, as a shell script does, so only
// run tapes you trust.
//
// # Grammar
//
// A tape is parsed by [syntax.Parse], which follows VHS's grammar and
// groups its tokens into commands, and compiled by [Compile], which gives
// them their meaning: [Parse] does both. Strings are quoted with ", ', or
// `, or are bare words; times are a number with an optional unit of ms, s,
// or m, and seconds without one. Errors give the line and column, as
// [*syntax.Error].
//
// [WithCommand] adds a command, or replaces a built-in one, with a [Step]
// that runs on the session, such as to click or resize, or to drive the
// program some other way. [WithCommands] skips or fails commands, such as
// to ignore Screenshot or forbid Env, and [WithReport] tells the caller of
// each.
//
// # Commands
//
//   - Output <path>: a file to write, as [reel.ParseFile] reads it, query
//     parameters included, such as "demo.gif?loop=once". The formats are
//     .gif, .mp4, .webm, .webp, and reel's .cast (asciicast), .snap (the
//     screen's text), and .json; .png only with ?marks=. ?match= is an
//     error, as the outputs are written as the tape runs, and so are
//     VHS's directory of frames, .txt, .ascii, and .test.
//   - Require <program>: the program must be on PATH, checked before the
//     run starts.
//   - Source <path.tape>: the commands of another tape, in place. As in
//     VHS, its Output commands are dropped, and it cannot Source another.
//     See [WithSourceRoot].
//   - Env <name> <value>: an environment variable for the shell.
//   - Set <setting> <value>: see Settings.
//   - Type[@<time>] <string>...: types the strings, joined by spaces,
//     pausing after each key for the time or Set TypingSpeed.
//   - Enter, Backspace, Delete, Insert, Tab, Escape, Space, Up, Down, Left,
//     Right, PageUp, PageDown, Home, End, each [@<time>] [count]: presses
//     the key count times, pausing as Type does.
//   - Ctrl[+Alt][+Shift]+<key>, Alt+<key>, Shift+<key>: presses a key with
//     modifiers. Alt+<string> presses each of its characters with alt.
//   - ScrollUp, ScrollDown, each [@<time>] [count]: scrolls the viewport
//     through the scrollback a row at a time, pausing as Type does.
//   - Sleep <time>: waits.
//   - Wait[+Line|+Screen][@<timeout>] [/<regexp>/|<string>]: waits until
//     the regexp, or (reel) the string as is (Set WaitPattern's without
//     either), matches the cursor's line (+Line,
//     the default) or the screen, each line without trailing spaces, for
//     up to the timeout (Set WaitTimeout's without one). A timeout is an
//     error that reports the screen.
//   - Hide, Show: Hide cuts what follows out of the recording, until Show
//     (see [effects.Hide]). A tape that is hidden from its start to its end
//     is an error, as is Hide with a .cast output, which holds the
//     recording uncut.
//   - Screenshot <path.png>: writes a PNG of the screen. As each is the
//     still of a mark labeled with its file name without ".png", file names
//     must be unique, and hold none of <>:"|?* or "{".
//   - Copy <string>..., Paste: Copy copies the strings, joined by spaces,
//     to the run's own clipboard (not the host's, as VHS uses), and Paste
//     sends them as a paste ([reel.Session.Paste]).
//   - Mark <label> (reel): appends a [reel.Mark], such as for an output's
//     marks or a [effects.Pause]. Labels starting with "tape:" are the
//     run's own.
//   - Highlight, Zoom, Redact <spec> (reel): the effect the spec describes
//     (see package spec), such as Zoom "re=ERROR.*;scale=2;between=a,b",
//     for the whole recording or the window of its between=, wherever the
//     command is in the tape.
//
// # Settings
//
// As in VHS, a Set after the first command other than Set, Output, or
// Require is ignored with a warning, except TypingSpeed, which applies from
// there on. Settings VHS has defaults for start at them, so a tape's screen
// is the size it is in VHS.
//
//	Shell <name>          bash (cmd on Windows), zsh, fish, powershell, pwsh,
//	                      cmd, nu, osh, or xonsh, without rc files and with
//	                      a "> " prompt, as VHS starts them
//	Width, Height <px>    the image size the screen is fit to; 1200 by 600
//	FontSize <px>         the text's size; 22 (see [render.ScaleForFontSize])
//	FontFamily <names>    the first installed of a comma-separated list
//	LetterSpacing <px>    pixels added to each cell's width
//	LineHeight <factor>   the cell height, as a multiple of the font's
//	TypingSpeed <time>    the pause after each typed key; 50ms
//	Theme <json|name>     the palette: xterm.js theme JSON, or a name from
//	                      the catalog (see package themes); also the
//	                      terminal's default colors
//	Padding <px>          60
//	Margin <px>           with MarginFill's color, or the theme background
//	MarginFill <#rrggbb>  an image file is an error
//	BorderRadius <px>
//	WindowBar <style>     reel's window chrome; styles other than Colorful
//	                      are drawn as Colorful, with a warning
//	WindowBarSize <px>    ignored with a warning
//	Framerate <fps>       [reel.Profile.FPS]
//	PlaybackSpeed <x>     [reel.Profile.Speed]
//	WaitTimeout <time>    15s
//	WaitPattern <regexp>  />$/
//	CursorBlink <bool>    false is ignored with a warning: the cursor blinks
//	                      as the program sets it
//	LoopOffset            an error
//
// reel adds these, which VHS does not read:
//
//	Columns, Rows <n>     the screen in cells, instead of fitting it to
//	                      Width and Height (see [Tape.Run])
//	Title <string>        the recording's title ([reel.WithTitle])
//	Scrollbar <bool>      [render.WithScrollbar]
//	PromptMarks <bool>    [render.WithPromptMarks]
//	LinkUnderline <bool>  [render.WithLinkUnderline]
//	Bell <bool>           [render.WithBell]
//	KeyOverlay <bool>     the keys pressed, as [effects.Keys] shows them
//	Toasts <bool>         notifications and clipboard writes, as
//	                      [effects.Toasts] shows them
package play
