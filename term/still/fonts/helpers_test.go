// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import "sync"

// resetFontCachesForTest drops the system font index and parsed faces. It must
// not run concurrently with lookups.
func resetFontCachesForTest() {
	systemFamilyIndex = sync.OnceValue(buildSystemIndex)
	systemFallbackIndex = sync.OnceValue(buildSystemFallbackIndex)
	faceCache.Clear()
	fallbackFonts.Clear()
}
