// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package anim

import "math"

// criticalBand is how close to 1 a damping ratio is treated as critically
// damped, where the over- and under-damped solutions divide by nearly zero.
const criticalBand = 1e-9

// springStep advances a damped spring pulling pos toward target by dt
// seconds, with angular frequency omega and damping ratio zeta, returning
// the new position and velocity. It solves the spring exactly rather than
// integrating it, so it is stable for any dt.
func springStep(pos, vel, target, dt, omega, zeta float64) (newPos, newVel float64) {
	if omega <= 0 {
		return pos, vel
	}
	zeta = max(zeta, 0)
	x := pos - target

	switch {
	case zeta > 1+criticalBand:
		// Overdamped: x(t) = c1·e^(r1·t) + c2·e^(r2·t).
		d := omega * math.Sqrt(zeta*zeta-1)
		r1, r2 := -omega*zeta-d, -omega*zeta+d
		c1 := (vel - x*r2) / (r1 - r2)
		c2 := x - c1
		e1, e2 := math.Exp(r1*dt), math.Exp(r2*dt)
		return target + c1*e1 + c2*e2, c1*r1*e1 + c2*r2*e2
	case zeta > 1-criticalBand:
		// Critically damped: x(t) = (c1 + c2·t)·e^(-ω·t).
		c2 := vel + omega*x
		e := math.Exp(-omega * dt)
		p := x + c2*dt
		return target + p*e, (c2 - omega*p) * e
	default:
		// Underdamped: x(t) = e^(-ζω·t)·(c1·cos(ωd·t) + c2·sin(ωd·t)).
		a := omega * zeta
		wd := omega * math.Sqrt(1-zeta*zeta)
		c2 := (vel + a*x) / wd
		e := math.Exp(-a * dt)
		sin, cos := math.Sincos(wd * dt)
		return target + e*(x*cos+c2*sin), e * ((c2*wd-a*x)*cos - (x*wd+a*c2)*sin)
	}
}
