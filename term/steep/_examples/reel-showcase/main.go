// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Command reel-showcase is a fake deploy control room: a full screen Bubble
// Tea program with a boot animation, live charts, theme cross-fades, and a
// deploy pipeline that reports its progress to the terminal through OSC 9;4
// and OSC 7501. Its test records it with steep and reel.
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

func main() {
	if _, err := tea.NewProgram(newModel()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
