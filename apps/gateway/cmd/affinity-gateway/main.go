package main

import (
	"log"

	"github.com/powercess/affinity-gateway/apps/gateway/internal/server"
)

// Set via -ldflags at build time.
var (
	version  = "dev"
	revision = "unknown"
)

func main() {
	log.Printf("affinity-gateway %s (%s)", version, revision)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
