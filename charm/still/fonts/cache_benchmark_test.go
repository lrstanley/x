// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import (
	"sync"
	"testing"

	"golang.org/x/image/font/opentype"
)

func TestLoad_concurrentSameEmbeddedFont(t *testing.T) {
	t.Parallel()

	const workers = 48
	const iters = 32

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for range iters {
				if _, err := Load(embeddedFixture); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestLoad_concurrentDistinctEmbeddedFonts(t *testing.T) {
	t.Parallel()

	stubs := readEmbeddedFonts()
	if len(stubs) < 2 {
		t.Skip("need at least 2 embedded fonts")
	}
	names := []string{stubs[0].stem, stubs[1].stem}
	if len(stubs) > 2 {
		names = append(names, stubs[2].stem)
	}

	var wg sync.WaitGroup
	for _, n := range names {
		for range 16 {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				for range 24 {
					if _, err := Load(name); err != nil {
						t.Error(err)
						return
					}
				}
			}(n)
		}
	}
	wg.Wait()
}

func BenchmarkLoad(b *testing.B) {
	norm := normalizeFontName(embeddedFixture)
	src, err := locateMiss(norm)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("sequential_warm_Load", func(b *testing.B) {
		resetFontCachesForTest()
		if _, err := Load(embeddedFixture); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := Load(embeddedFixture); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("parallel_warm_Load", func(b *testing.B) {
		resetFontCachesForTest()
		if _, err := Load(embeddedFixture); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := Load(embeddedFixture); err != nil {
					b.Fatal(err)
				}
			}
		})
	})

	b.Run("sequential_parseFont_baseline_no_cache", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := parseFont(src); err != nil {
				b.Fatal(err)
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
				_ = v.(*opentype.Font)
			}
		})
	})
}
