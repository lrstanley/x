// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"math"
	"math/rand/v2"
)

// telemetry generates the fake cluster metrics the dashboard plots. It is
// seeded, so every run (and every recording) draws the same curves.
type telemetry struct {
	rng  *rand.Rand
	tick int
	// load is extra cpu load, which deploys raise while they run.
	load float64
	// spike is extra p99 latency, which decays each tick.
	spike float64
}

type sample struct {
	cpu      float64 // 0-1
	rps      float64 // requests per second
	p50, p99 float64 // milliseconds
}

func newTelemetry() *telemetry {
	return &telemetry{rng: rand.New(rand.NewPCG(7, 42))} //nolint:gosec // demo data.
}

func (t *telemetry) noise(scale float64) float64 {
	return (t.rng.Float64()*2 - 1) * scale
}

func (t *telemetry) next() sample {
	t.tick++
	x := float64(t.tick)
	t.spike *= 0.82
	if t.rng.Float64() < 0.025 {
		t.spike += 40 + t.rng.Float64()*60
	}

	cpu := 0.42 + 0.16*math.Sin(x*0.09) + 0.07*math.Sin(x*0.41+1) + t.noise(0.045) + t.load
	rps := 2150 + 420*math.Sin(x*0.05) + 160*math.Sin(x*0.23) + t.noise(90) + t.load*900
	p50 := 31 + 6*math.Sin(x*0.13) + t.noise(2.5) + t.load*14
	p99 := 96 + 18*math.Sin(x*0.07+2) + t.noise(8) + t.spike + t.load*60

	return sample{
		cpu: min(max(cpu, 0.04), 0.98),
		rps: max(rps, 0),
		p50: max(p50, 5),
		p99: max(p99, p50+10),
	}
}

type serviceHealth int

const (
	healthy serviceHealth = iota
	degraded
	deploying
	failing
)

type service struct {
	name    string
	version string
	region  string
	rps     float64
	p99     float64
	health  serviceHealth
	// fails makes a deploy of this service fail its test stage.
	fails bool
	trend []float64
}

func newServices() []service {
	svcs := []service{
		{name: "api-gateway", version: "v2.41.0", region: "us-east", rps: 1240, p99: 88},
		{name: "auth", version: "v1.18.3", region: "us-east", rps: 610, p99: 54},
		{name: "billing", version: "v3.7.1", region: "us-west", rps: 182, p99: 131, fails: true},
		{name: "search", version: "v5.2.0", region: "eu-west", rps: 938, p99: 117},
		{name: "notifications", version: "v0.9.12", region: "us-east", rps: 344, p99: 72},
		{name: "media-proxy", version: "v4.0.6", region: "ap-south", rps: 1515, p99: 149, health: degraded},
		{name: "ledger", version: "v2.2.9", region: "us-west", rps: 97, p99: 61},
	}
	for i := range svcs {
		svcs[i].trend = make([]float64, 0, trendLen)
	}
	return svcs
}

const trendLen = 64

// step jitters each service's numbers and appends to its trend.
func (t *telemetry) step(svcs []service) {
	for i := range svcs {
		s := &svcs[i]
		s.rps = max(s.rps*(1+t.noise(0.035))+t.noise(4), 10)
		target := 40 + float64(len(s.name))*7
		if s.health == degraded {
			target += 60
		}
		s.p99 += (target-s.p99)*0.05 + t.noise(6)
		s.p99 = max(s.p99, 12)
		s.trend = appendCapped(s.trend, s.rps, trendLen)
	}
}

type logLevel int

const (
	levelDebug logLevel = iota
	levelInfo
	levelWarn
	levelError
)

type logLine struct {
	at      float64 // seconds since the dashboard opened
	level   logLevel
	service string
	msg     string
}

var (
	logPaths = []string{"/v1/orders", "/v1/session", "/v2/search", "/v1/invoices", "/healthz", "/v1/media/thumb", "/v1/users/me"}
	logVerbs = []string{"GET", "GET", "GET", "POST", "PUT", "DELETE"}
)

// ambientLog returns a plausible line of background traffic.
func (t *telemetry) ambientLog(svcs []service, at float64) logLine {
	s := svcs[t.rng.IntN(len(svcs))]
	roll := t.rng.Float64()
	switch {
	case roll < 0.07:
		return logLine{at: at, level: levelWarn, service: s.name, msg: fmt.Sprintf("slow upstream response  upstream=%s dur=%dms", svcs[t.rng.IntN(len(svcs))].name, 240+t.rng.IntN(500))}
	case roll < 0.16:
		return logLine{at: at, level: levelDebug, service: s.name, msg: fmt.Sprintf("cache refresh  keys=%d hit_ratio=0.%02d", 200+t.rng.IntN(4000), 80+t.rng.IntN(19))}
	default:
		status := 200
		if t.rng.Float64() < 0.1 {
			status = 201
		}
		return logLine{
			at: at, level: levelInfo, service: s.name,
			msg: fmt.Sprintf("%-6s %-16s status=%d dur=%dms", logVerbs[t.rng.IntN(len(logVerbs))], logPaths[t.rng.IntN(len(logPaths))], status, 3+t.rng.IntN(60)),
		}
	}
}
