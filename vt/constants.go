// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package vt

const (
	ghosttySuccess      int32 = 0
	ghosttyOutOfMemory  int32 = -1
	ghosttyInvalidValue int32 = -2
	ghosttyOutOfSpace   int32 = -3
	ghosttyNoValue      int32 = -4
)

const (
	ghosttyTerminalDataCols            int32 = 1
	ghosttyTerminalDataRows            int32 = 2
	ghosttyTerminalDataCursorX         int32 = 3
	ghosttyTerminalDataCursorY         int32 = 4
	ghosttyTerminalDataActiveScreen    int32 = 6
	ghosttyTerminalDataScrollbackRows  int32 = 15
	ghosttyTerminalDataColorForeground int32 = 18
	ghosttyTerminalDataColorBackground int32 = 19
	ghosttyTerminalDataColorCursor     int32 = 20
)

const (
	ghosttyTerminalScreenPrimary   int32 = 0
	ghosttyTerminalScreenAlternate int32 = 1
)

const (
	ghosttyPointTagActive int32 = 0
)

const (
	ghosttyCellDataWide int32 = 3
)

const (
	ghosttyCellWideNarrow     int32 = 0
	ghosttyCellWideWide       int32 = 1
	ghosttyCellWideSpacerTail int32 = 2
	ghosttyCellWideSpacerHead int32 = 3
)

const (
	ghosttyStyleColorNone    int32 = 0
	ghosttyStyleColorPalette int32 = 1
	ghosttyStyleColorRGB     int32 = 2
)
