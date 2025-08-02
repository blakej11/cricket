package server

import (
	"fmt"

        "github.com/blakej11/cricket/internal/client"
        "github.com/blakej11/cricket/internal/effect"
        "github.com/blakej11/cricket/internal/fileset"
        "github.com/blakej11/cricket/internal/lease"
	_ "github.com/blakej11/cricket/internal/light"
	_ "github.com/blakej11/cricket/internal/sound"
        "github.com/blakej11/cricket/internal/types"
)

// Config holds the configuration for the server.
type Config struct {
	DefaultVolume	int
	Clients		map[types.ID]types.Client
	Files		map[string]fileset.File
	FileSets	map[string]fileset.Config
	Effects		map[string]effect.Config
}

// ---------------------------------------------------------------------

// Server is the runtime version of Config.
type Server struct {
	defaultVolume	int
	clients		map[types.ID]types.Client
}

func New(c Config) (*Server, error) {
	fileSets := make(map[string]*fileset.Set)
	for name, fs := range c.FileSets {
		set, err := fileset.New(name, fs, c.Files)
		if err != nil {
			return nil, fmt.Errorf("failed to parse fileset %q: %w", name, err)
		}
		fileSets[name] = set
	}
	for name, e := range c.Effects {
		effect, l, err := effect.New(name, e, fileSets)
		if err != nil {
			return nil, fmt.Errorf("failed to parse effect %q: %w", name, err)
		}
		lease.Assign(l, effect)
	}

	return &Server{
		defaultVolume:	c.DefaultVolume,
		clients:	c.Clients,
	}, nil
}

func (s *Server) Start() { 
	client.Configure(s.defaultVolume, s.clients)

	lease.Start()
	client.Start()
}
