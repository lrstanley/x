// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"sync"
	"testing"

	"golang.org/x/image/font/opentype"
)

func BenchmarkLoad(b *testing.B) {
	norm := normalizeFontName(embeddedFixture)
	src, err := locateMiss(norm)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("sequential_warm_Load", func(b *testing.B) {
		resetFontCachesForTest()
		if _, loadErr := Load(embeddedFixture); loadErr != nil {
			b.Fatal(loadErr)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, loadErr := Load(embeddedFixture); loadErr != nil {
				b.Fatal(loadErr)
			}
		}
	})

	b.Run("parallel_warm_Load", func(b *testing.B) {
		resetFontCachesForTest()
		if _, loadErr := Load(embeddedFixture); loadErr != nil {
			b.Fatal(loadErr)
		}
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, loadErr := Load(embeddedFixture); loadErr != nil {
					b.Fatal(loadErr)
				}
			}
		})
	})

	b.Run("sequential_parseFont_baseline_no_cache", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, parseErr := parseFont(src); parseErr != nil {
				b.Fatal(parseErr)
			}
		}
	})
}

// BenchmarkParsedFontMapContention compares read-side contention for two cache
// implementations holding the same *opentype.Font (lookup only; no NewFace).
func BenchmarkParsedFontMapContention(b *testing.B) {
	norm := normalizeFontName(embeddedFixture)
	src, err := locateMiss(norm)
	if err != nil {
		b.Fatal(err)
	}
	tf, err := parseFont(src)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("RWMutex_map", func(b *testing.B) {
		resetFontCachesForTest()
		parsedMu.Lock()
		parsedFonts = map[string]*opentype.Font{norm: tf}
		parsedMu.Unlock()
		defer resetFontCachesForTest()

		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				parsedMu.RLock()
				if parsedFonts[norm] == nil {
					b.Fatal("missing font")
				}
				parsedMu.RUnlock()
			}
		})
	})

	b.Run("syncMap", func(b *testing.B) {
		var m sync.Map
		m.Store(norm, tf)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				v, ok := m.Load(norm)
				if !ok {
					b.Fatal("missing font")
				}
				font, ok := v.(*opentype.Font)
				if !ok || font == nil {
					b.Fatal("missing font")
				}
			}
		})
	})
}
