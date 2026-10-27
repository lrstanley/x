// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package draw //nolint:revive // name mirrors image/draw responsibilities

// ScaleMode selects fallback glyph scaling behavior.
type ScaleMode uint8

const (
	ScaleDefault ScaleMode = iota
	ScaleStretch
	ScaleFitCover1
)
