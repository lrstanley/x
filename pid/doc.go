// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package pid manages PID files for daemons and long-running processes: it
// records the current process ID, detects stale instances, and can signal a
// previous instance when a new one starts.
package pid
