package server


import (
	"fmt"
	"net"
	"strconv"
	"strings"

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
	virtualCricketLoc *types.NetLocation
}

func New(c Config, virtualCricketAddr string) (*Server, error) {
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

	var loc *types.NetLocation
	loc, err := parseVirtualCricketAddr(virtualCricketAddr, fileSets)
	if err != nil {
		return nil, err
	}

	return &Server{
		defaultVolume:	c.DefaultVolume,
		clients:	c.Clients,
		virtualCricketLoc: loc,
	}, nil
}

func parseVirtualCricketAddr(virtualCricketAddr string, fileSets map[string]*fileset.Set) (*types.NetLocation, error) {
	if virtualCricketAddr != "" {
		s := strings.Split(virtualCricketAddr, ":")
		if len(s) != 2 {
			return nil, fmt.Errorf("couldn't parse virtual cricket address %q\n", virtualCricketAddr)
		}
		addr := net.ParseIP(s[0])
		port, err := strconv.ParseInt(s[0], 10, 32)
		if addr == nil || err != nil {
			return nil, fmt.Errorf("couldn't parse virtual cricket address %q\n", virtualCricketAddr)
		}
		return &types.NetLocation {
			Address: addr,
			Port:    int(port),
		}, nil
	}
	return nil, nil
}

func (s *Server) Start() { 
	client.Configure(s.defaultVolume, s.clients, s.virtualCricketLoc)

	lease.Start()
	client.Start()
}
