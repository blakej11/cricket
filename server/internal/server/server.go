package server

import (
	"fmt"

        "github.com/blakej11/cricket/internal/client"
        "github.com/blakej11/cricket/internal/effect"
        "github.com/blakej11/cricket/internal/fileset"
        "github.com/blakej11/cricket/internal/lease"
	_ "github.com/blakej11/cricket/internal/light"
	"github.com/blakej11/cricket/internal/mdns"
        "github.com/blakej11/cricket/internal/player"
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
	Players		map[lease.Type]player.Config
}

// ---------------------------------------------------------------------

// ConfigImpl is the runtime version of Config.
type ConfigImpl struct {
	defaultVolume	int
	clients		map[types.ID]types.Client
	players		map[lease.Type]*player.Player
}

func (c *Config) New() (*ConfigImpl, error) {
	fileSets := make(map[string]*fileset.Set)
	for name, fs := range c.FileSets {
		set, err := fileset.New(name, fs, c.Files)
		if err != nil {
			return nil, fmt.Errorf("failed to parse fileset %q: %w", name, err)
		}
		fileSets[name] = set
	}
	effects := make(map[lease.Type]map[string]*effect.Effect)
	for _, t := range lease.ValidTypes() {
		effects[t] = make(map[string]*effect.Effect)
	}
	for name, e := range c.Effects {
		effect, err := effect.New(name, e, fileSets)
		if err != nil {
			return nil, fmt.Errorf("failed to parse effect %q: %w", name, err)
		}
		effects[e.Lease.Type][name] = effect
	}
	players := make(map[lease.Type]*player.Player)
	for _, t := range lease.ValidTypes() {
		player, err := player.New(t, c.Players[t], effects[t])
		if err != nil {
			return nil, fmt.Errorf("failed to parse %v weights: %w", t, err)
		}
		players[t] = player
	}

	return &ConfigImpl{
		defaultVolume:	c.DefaultVolume,
		clients:	c.Clients,
		players:	players,
	}, nil
}

func (c *ConfigImpl) Run() { 
	client.Configure(c.defaultVolume, c.clients)

	mdns.Start()
	for _, p := range c.players {
		p.Start()
	}
}
