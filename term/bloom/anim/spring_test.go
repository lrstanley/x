// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package anim

import (
	"fmt"
	"math"
	"testing"
)

// integrate solves the spring's equation of motion, where acceleration is
// -ω²·(x-target) - 2ζω·v, with small fourth-order Runge-Kutta steps, as a
// reference for springStep.
func integrate(pos, vel, target, dt, omega, zeta float64) (newPos, newVel float64) {
	accel := func(x, v float64) float64 { return -omega*omega*(x-target) - 2*zeta*omega*v }
	const n = 10000
	h := dt / n
	for range n {
		k1x, k1v := vel, accel(pos, vel)
		k2x, k2v := vel+h/2*k1v, accel(pos+h/2*k1x, vel+h/2*k1v)
		k3x, k3v := vel+h/2*k2v, accel(pos+h/2*k2x, vel+h/2*k2v)
		k4x, k4v := vel+h*k3v, accel(pos+h*k3x, vel+h*k3v)
		pos += h / 6 * (k1x + 2*k2x + 2*k3x + k4x)
		vel += h / 6 * (k1v + 2*k2v + 2*k3v + k4v)
	}
	return pos, vel
}

func TestSpringStep(t *testing.T) {
	t.Parallel()

	for _, zeta := range []float64{0, 0.2, 0.75, 1 - 1e-12, 1, 1 + 1e-12, 1.00005, 1.5, 3} {
		for _, omega := range []float64{1, 8, 30} {
			for _, dt := range []float64{1.0 / 120, 0.1} {
				for _, st := range [][3]float64{{0, 0, 1}, {5, -3, 20}, {-2, 7, -2}} {
					t.Run(fmt.Sprintf("zeta=%v/omega=%v/dt=%v/%v", zeta, omega, dt, st), func(t *testing.T) {
						t.Parallel()
						p, v := springStep(st[0], st[1], st[2], dt, omega, zeta)
						wp, wv := integrate(st[0], st[1], st[2], dt, omega, zeta)
						// Written so that NaN, which compares false, fails.
						if ok := math.Abs(p-wp) <= 1e-6 && math.Abs(v-wv) <= 1e-6; !ok {
							t.Fatalf("got %v, %v, want %v, %v", p, v, wp, wv)
						}
					})
				}
			}
		}
	}
}

func TestSpringStepWithoutFrequency(t *testing.T) {
	t.Parallel()

	// A spring with no frequency does not pull, so it stays where it is.
	for _, omega := range []float64{0, -1} {
		if p, v := springStep(3, 2, 10, 0.1, omega, 1); p != 3 || v != 2 {
			t.Fatalf("omega=%v moved the spring to %v, %v", omega, p, v)
		}
	}
}
