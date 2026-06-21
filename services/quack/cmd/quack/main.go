package main

import (
	"log"

	"github.com/omneval/omneval/services/quack/internal/server"

	// Matches the container's cgroup CPU limit instead of the host's full
	// core count — see services/query/cmd/query/main.go's import comment.
	_ "go.uber.org/automaxprocs"
)

func main() {
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
