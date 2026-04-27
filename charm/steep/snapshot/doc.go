// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package snapshot provides snapshot assertions for tests.
//
// Snapshots are stored under testdata. Set UPDATE_SNAPSHOTS=true to create or
// update snapshots.
//
//	snapshot.AssertEqual(t, model.View())
//	snapshot.RequireEqual(t, model.View())
//	snapshot.RequireEqual(t, model.View(), snapshot.WithSuffix("empty-state"))
package snapshot
