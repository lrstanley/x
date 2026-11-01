// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command dashboard is a service dashboard redrawn at up to 120 frames a
// second: a sidebar that springs open and shut, live sparklines, a log
// stream, a scrolling table, a confirmation dialog that slides up over a
// dimmed backdrop, and a toast for each cell effect. The status bar shows
// how long the layout engine took to draw the last frame.
//
// Keys: up/down select, enter restarts the selected service, tab moves
// focus, ctrl+b toggles the sidebar, 1-6 show a toast with each effect, q
// quits. Everything can be clicked.
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

func main() {
	if _, err := tea.NewProgram(newModel(), tea.WithFPS(120)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
