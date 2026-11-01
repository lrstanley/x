// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build windows

package main

// onResize does nothing on Windows, which has no resize signal: a
// recording keeps the size it started at.
func onResize(func()) (stop func()) { return func() {} }
