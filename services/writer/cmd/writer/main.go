package main

import (
	"log"
	"os"

	"github.com/omneval/omneval/services/writer/internal/backfill"
	"github.com/omneval/omneval/services/writer/internal/server"
)

func main() {
	// `writer backfill` is the one-off legacy→Lake data migration
	// (ADR-0004); everything else runs the writer service.
	if len(os.Args) > 1 && os.Args[1] == "backfill" {
		os.Exit(backfill.Main(os.Args[2:]))
	}

	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
