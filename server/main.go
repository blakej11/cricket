package main

import (
	"context"
	"flag"
	"os"

	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/server"

	"github.com/pelletier/go-toml/v2"
)

var configFile = flag.String("config", "", "path to config file")

var virtualCricketAddr = flag.String("virtual", "", "if an [IP address]:[port] pair, connect all crickets to that address; if \"builtin\", start a simple virtual cricket server")

func main() {
	var config server.Config

	flag.Parse()

	if *configFile == "" {
		log.Fatalf("must specify configuration via \"-config=/path/to/config.json\"")
	}
	blob, err := os.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("could not open config file %q: %w", *configFile, err)
	}
	if err := toml.Unmarshal(blob, &config); err != nil {
		log.Fatalf("failed to unmarshal server config: %w", err)
	}
	s, err := server.New(config, *virtualCricketAddr)
	if err != nil {
		log.Fatalf("%v", err)
	}
	s.Start()

	log.Infof("Cricket server ready.")
	ctx := context.Background()
	<-ctx.Done()
}
