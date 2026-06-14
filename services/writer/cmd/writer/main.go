package main

import (
	"log"

	"github.com/omneval/omneval/services/writer/internal/server"
)

func main() {
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
