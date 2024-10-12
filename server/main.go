package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/blakej11/cricket/internal/config"
)

var configFile = flag.String("config", "", "path to config file")

func main() {
	flag.Parse()

	if *configFile == "" {
		log.Fatal("must specify configuration via \"-config=/path/to/config.json\"")
	}
	jsonBlob, err := os.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("could not open config file %q: %w", *configFile, err)
	}
	cfg, err := config.ParseJSON(jsonBlob)
	if err != nil {
		log.Fatal(err)
	}

	cfg.Run()

	ctx := context.Background()
	<-ctx.Done()
}
