// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package fonts

import "testing"

func BenchmarkLoadFace(b *testing.B) {
	b.Run("warm_sequential", func(b *testing.B) {
		resetFontCachesForTest()
		if _, err := LoadFace(embeddedFixture, StyleRegular); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := LoadFace(embeddedFixture, StyleRegular); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("warm_parallel", func(b *testing.B) {
		resetFontCachesForTest()
		if _, err := LoadFace(embeddedFixture, StyleRegular); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := LoadFace(embeddedFixture, StyleRegular); err != nil {
					b.Fatal(err)
				}
			}
		})
	})

	b.Run("cold", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			resetFontCachesForTest()
			if _, err := LoadFace(embeddedFixture, StyleRegular); err != nil {
				b.Fatal(err)
			}
		}
	})
}
