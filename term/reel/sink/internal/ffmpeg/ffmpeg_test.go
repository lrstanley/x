// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package ffmpeg

import "testing"

func TestHasFPSMode(t *testing.T) {
	t.Parallel()

	for version, want := range map[string]bool{
		"ffmpeg version 4.4.2-0ubuntu0.22.04.1 Copyright": false,
		"ffmpeg version 5.0.1 Copyright":                  false,
		"ffmpeg version 5.1.2 Copyright":                  true,
		"ffmpeg version n6.1 Copyright":                   true,
		"ffmpeg version 7.1-full_build-www.gyan.dev":      true,
		"ffmpeg version N-113000-g0123456789 Copyright":   true,
		"ffmpeg version 2024-01-01-git-abc Copyright":     true,
		"something else entirely":                         true,
	} {
		if got := hasFPSMode([]byte(version + "\nbuilt with gcc")); got != want {
			t.Errorf("%q: fps_mode = %v, want %v", version, got, want)
		}
	}
}
