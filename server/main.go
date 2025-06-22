package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/blakej11/cricket/internal/server"

	"github.com/pelletier/go-toml/v2"
)

var configFile = flag.String("config", "", "path to config file")

func main() {
	var cfg server.Config

	flag.Parse()

	if *configFile == "" {
		log.Fatal("must specify configuration via \"-config=/path/to/config.json\"")
	}
	blob, err := os.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("could not open config file %q: %w", *configFile, err)
	}
	if err := toml.Unmarshal(blob, &cfg); err != nil {
		log.Fatalf("failed to unmarshal server config: %w", err)
	}
	cfgi, err := cfg.New()
	if err != nil {
		log.Fatal(err)
	}
	cfgi.Run()

	ctx := context.Background()
	<-ctx.Done()
}
