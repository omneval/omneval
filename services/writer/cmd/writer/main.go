package main

import (
	"log"

	"github.com/zbloss/lantern/services/writer/internal/server"
)

func main() {
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
