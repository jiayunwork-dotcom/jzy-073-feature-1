// Command isa-service serves the International Standard Atmosphere model
// over HTTP. All values are computed per request; nothing is persisted.
package main

import (
	"log"
	"os"

	"isa-service/internal/httpapi"
)

func main() {
	addr := ":" + port()
	log.Printf("ISA atmosphere service listening on %s (model domain: 0..20000 m)", addr)
	if err := httpapi.NewRouter().Run(addr); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}
