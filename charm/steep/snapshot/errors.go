// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package snapshot

import "testing"

func errNotExist(tb testing.TB, path string) {
	tb.Helper()
	tb.Errorf("snapshot %q does not exist; set %s=true to create it", path, envUpdateSnapshots)
}
