// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"os/user"
	"strings"
)

func expandUser(path string) string {
	if strings.HasPrefix(path, "~") {
		if u, err := user.Current(); err == nil {
			return strings.Replace(path, "~", u.HomeDir, 1)
		}
	}
	return path
}
