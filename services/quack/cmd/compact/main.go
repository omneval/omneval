package main

import (
	"log"

	"github.com/omneval/omneval/services/quack/internal/compact"

	_ "go.uber.org/automaxprocs"
)

func main() {
	if err := compact.Run(); err != nil {
		log.Fatal(err)
	}
}
