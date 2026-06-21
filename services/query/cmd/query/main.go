package main

import (
	"log"

	"github.com/omneval/omneval/services/query/internal/server"

	// Sets GOMAXPROCS to match the container's cgroup CPU limit instead of
	// the host's full core count. Without this, the Go runtime spawns as
	// many scheduler threads as the host has cores (e.g. 12 on a node whose
	// pods are capped to 2 CPUs), so goroutines — including the readiness/
	// liveness probe's own handler — compete for scheduling time against far
	// more runnable work than the cgroup can actually run in parallel. This
	// caused /healthz and /readyz to time out under heavy query load even
	// after CFS throttling itself (nr_throttled) was fixed via DuckDB's own
	// `threads` pragma (see internal/lake's Threads config) — that pragma
	// only bounds DuckDB's cgo worker threads, not the separate pool of OS
	// threads the Go runtime itself schedules goroutines onto.
	_ "go.uber.org/automaxprocs"
)

func main() {
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
