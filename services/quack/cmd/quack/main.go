package main

import (
	"log"

	"github.com/omneval/omneval/services/quack/internal/server"
)

func main() {
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
