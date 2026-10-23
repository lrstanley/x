// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package still

import "github.com/lrstanley/x/term/still/types"

type (
	Metrics       = types.Metrics
	Palette       = types.Palette
	EmulatorState = types.EmulatorState
	Progress      = types.Progress
	ProgressState = types.ProgressState
)

// Progress states reported by OSC 9;4. See [types.ProgressState].
const (
	ProgressRemove        = types.ProgressRemove
	ProgressSet           = types.ProgressSet
	ProgressError         = types.ProgressError
	ProgressIndeterminate = types.ProgressIndeterminate
	ProgressPause         = types.ProgressPause
)
