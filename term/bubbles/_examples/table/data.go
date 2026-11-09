// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"math/rand/v2"
	"time"
)

// Status is where a job is in its life.
type Status string

const (
	Running   Status = "running"
	Pending   Status = "pending"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

// Job is a row of mock data: a batch job on a cluster.
type Job struct {
	ID      int
	Name    string
	Owner   string
	Queue   string
	Status  Status
	CPU     float64 // percent of its request
	Memory  int     // MiB
	Age     time.Duration
	Node    string
	Image   string
	Command string
}

var (
	verbs   = []string{"nightly", "hourly", "adhoc", "backfill", "replay", "daily", "canary", "weekly"}
	nouns   = []string{"ingest", "rollup", "export", "reindex", "compact", "train", "vacuum", "snapshot", "audit", "sync", "render", "billing"}
	owners  = []string{"ava", "liam", "mei", "omar", "priya", "sam", "tomas", "zoe", "data-platform", "ml-infra", "billing-svc"}
	queues  = []string{"default", "batch", "gpu", "high-mem", "low-priority", "realtime"}
	repos   = []string{"platform/ingest-worker", "ml/trainer-cuda12", "data/rollup-engine", "infra/vacuum", "billing/exporter"}
	weights = []Status{Running, Running, Running, Running, Pending, Pending, Succeeded, Succeeded, Succeeded, Failed}
)

// mockJobs returns n jobs, the same ones for the same n.
func mockJobs(n int) []*Job {
	r := rand.New(rand.NewPCG(7, 42)) //nolint:gosec // Mock data.
	jobs := make([]*Job, n)
	for i := range jobs {
		hash := fmt.Sprintf("%07x", r.Uint32()&0xfffffff)
		noun := nouns[r.IntN(len(nouns))]
		repo := repos[r.IntN(len(repos))]
		j := &Job{
			ID:     i + 1,
			Name:   fmt.Sprintf("%s-%s-%s", verbs[r.IntN(len(verbs))], noun, hash[:5]),
			Owner:  owners[r.IntN(len(owners))],
			Queue:  queues[r.IntN(len(queues))],
			Status: weights[r.IntN(len(weights))],
			Memory: 128 << r.IntN(8),
			Age:    time.Duration(r.IntN(14*24*3600)) * time.Second,
			Node:   fmt.Sprintf("node-%s-%02d", []string{"a", "b", "c", "gpu"}[r.IntN(4)], r.IntN(40)),
			Image:  fmt.Sprintf("registry.internal.example.com/%s:v%d.%d.%d-%s", repo, 1+r.IntN(3), r.IntN(20), r.IntN(10), hash),
		}
		j.Command = fmt.Sprintf("/usr/local/bin/%s --shard=%d/%d --since=%dh --verbose", noun, r.IntN(64), 64, 1+r.IntN(72))
		if j.Status == Running {
			j.CPU = r.Float64() * 140
		}
		jobs[i] = j
	}
	return jobs
}

// age formats d as the largest unit or two, like kubectl does.
func age(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// memory formats MiB.
func memory(mib int) string {
	if mib >= 1024 {
		return fmt.Sprintf("%.1f GiB", float64(mib)/1024)
	}
	return fmt.Sprintf("%d MiB", mib)
}
