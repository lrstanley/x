// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package showcase holds a recording of steep's reel-showcase example for
// the examples that export it with effects.
//
// showcase.cast.gz is the end of the showcase's recording, from the mark of
// its sixth input on: a dashboard of services, where pressing the deploy key
// ("input-8") deploys billing, whose pipeline then builds and fails its
// tests, which have failed by "wait-13". It was recorded by the showcase
// test with STEEP_EXPORT_MODE=on, which writes showcase.cast, cut with
// reel.Timeline.Between("input-6", ""), and saved with
// reel.Timeline.WriteTo, so it is asciicast v2 and holds the annotations
// steep recorded.
package showcase

import (
	"bytes"
	"compress/gzip"
	_ "embed"

	"github.com/lrstanley/x/term/reel"
)

//go:embed showcase.cast.gz
var showcase []byte

// Timeline returns the recording.
func Timeline() (*reel.Timeline, error) {
	zr, err := gzip.NewReader(bytes.NewReader(showcase))
	if err != nil {
		return nil, err
	}
	return reel.ReadTimeline(zr)
}
