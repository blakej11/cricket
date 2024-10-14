package effect

import (
	"context"
	"fmt"
	"strings"
	"time"

        "github.com/blakej11/cricket/internal/client"
        "github.com/blakej11/cricket/internal/fileset"
        "github.com/blakej11/cricket/internal/lease"
        "github.com/blakej11/cricket/internal/log"
        "github.com/blakej11/cricket/internal/random"
        "github.com/blakej11/cricket/internal/types"
)

// Config describes the configuration of a single sound or light effect.
type Config struct {
	Algorithm	string			// the name of the algorithm
	FileSets	map[string]string	// names of fileset(s) to use
	Parameters	map[string]random.Config// how to define parameters
	Lease		lease.Config
}

// ---------------------------------------------------------------------

// Effect is the instantiation of a Config.
type Effect struct {
	name		string
	lease		lease.Params
	alg		Algorithm
	fileSets	map[string]*fileset.Set
	parameters	map[string]*random.Variable
	duration	*random.Variable
}

const (
	durationParameter = "duration"
)

func New(name string, c Config, fileSets map[string]*fileset.Set) (*Effect, error) {
	if _, ok := c.Parameters[durationParameter]; !ok {
		return nil, fmt.Errorf("effect %q is missing the required %q parameter", name, durationParameter)
	}
	duration := random.New(c.Parameters[durationParameter])

	alg, err := lookupAlgorithm(c.Lease.Type, c.Algorithm)
	if err != nil {
		return nil, err
	}
	reqs := alg.GetRequirements()

	fss := make(map[string]*fileset.Set)
	for _, fsName := range reqs.FileSets {
		if _, ok := c.FileSets[fsName]; !ok {
			return nil, fmt.Errorf("failed to find effect %q's %q fileset", name, fsName)
		}
		n := c.FileSets[fsName]
		if _, ok := fileSets[n]; !ok {
			return nil, fmt.Errorf("failed to find a fileset named %q for effect %q", n, name)
		}
		fss[fsName] = fileSets[n]
	}

	parameters := make(map[string]*random.Variable)
	for _, paramName := range reqs.Parameters {
		if _, ok := c.Parameters[paramName]; !ok {
			return nil, fmt.Errorf("failed to find effect %q's %q parameter", name, paramName)
		}
		parameters[paramName] = random.New(c.Parameters[paramName])
	}

	return &Effect{
		name:		name,
		lease:		lease.New(c.Lease),
		alg:		alg,
		fileSets:	fss,
		parameters:	parameters,
		duration:	duration,
	}, nil
}

// Run leases some clients and instantiates an effect on them.
// It spawns a thread to run the algorithm, and that thread hangs around
// until all of the client leases are returned.
// It returns an error if the lease could not be satisfied.
func (e *Effect) Run() error {
        dur := e.duration.Duration()
        ctx, cancel := context.WithTimeout(context.Background(), dur)

	clients, err := lease.Request(e.lease)
	if err != nil {
		return err
	}

	algParams := AlgParams {
		FileSets:	e.fileSets,
		Parameters:	e.parameters,
		Clients:	clients,
	}
	for _, p := range algParams.Parameters {
		p.Reset()
	}

	go func() {
		defer cancel()

		log.Infof("Start  effect %q: duration %v, params %s", e.name, dur, algParams)
		e.alg.Run(ctx, algParams)
		log.Infof("Finish effect %q: params %s", e.name, algParams)

		e.drainQueue(clients)
	}()

	return nil
}

// Drain the queue on each client.
// We will hang around as long as necessary to do so.
func (e *Effect) drainQueue(clients []types.ID) {
	drained := make(map[types.ID]bool)
	for _, id := range clients {
		drained[id] = false
	}
	acks := make(chan types.ID)
	drain := client.DrainQueue {
		Ack:	acks,
		Type:	e.lease.Type,
	}
	client.Action(clients, context.Background(), &drain, time.Now())

	start := time.Now()
	now := start
	ticker := time.Tick(time.Second)
	draining := []types.ID{}
	toDrain := len(clients)
	for toDrain > 0 {
		select {
		case id := <-acks:
			draining = append(draining, id)
			continue
		case now = <-ticker:
		}

		lease.Return(draining, e.lease.Type)
		for _, id := range draining {
			drained[id] = true
		}
		toDrain -= len(draining)
		draining = nil

		if now.Sub(start) <= 10 * time.Second {
			continue
		}
		stillDraining := []types.ID{}
		for id, done := range drained {
			if done {
				continue
			}
			stillDraining = append(stillDraining, id)
		}
		log.Infof("%d clients still draining after %v: %v",
		    toDrain, now.Sub(start), stillDraining)
	}
}

// ---------------------------------------------------------------------

type AlgRequirements struct {
	FileSets	[]string
	Parameters	[]string
}

type AlgParams struct {
	FileSets	map[string]*fileset.Set
	Parameters	map[string]*random.Variable
	Clients		[]types.ID
}

func (a AlgParams) String() string {
	fss := []string{}
	for n := range a.FileSets {
		fss = append(fss, n)
	}
	params := []string{}
	for n := range a.Parameters {
		params = append(params, n)
	}
	clients := []string{}
	for _, n := range a.Clients {
		clients = append(clients, string(n))
	}
	return fmt.Sprintf("<filesets [ %s ], params [ %s ], clients [ %s ]>",
	    strings.Join(fss, ","), strings.Join(params, ","), strings.Join(clients, ","))
}

type Algorithm interface {
	// GetRequirements allows the algorithm to specify what it needs.
	GetRequirements() AlgRequirements

	// Run is called after Setup and SetClients has been called.
	// It should do the actual thing.
	Run(context.Context, AlgParams)
}

// this can be called from module init functions
func RegisterAlgorithm(ty lease.Type, name string, alg Algorithm) {
	if algs == nil {
		algs = make(map[lease.Type]map[string]Algorithm)
		for _, t := range lease.ValidTypes() {
			algs[t] = make(map[string]Algorithm)
		}
	}
	algs[ty][name] = alg
}

func lookupAlgorithm(ty lease.Type, name string) (Algorithm, error) {
	if _, ok := algs[ty]; !ok {
		return nil, fmt.Errorf("failed to find any %v-type algorithms", ty)
	}
	if _, ok := algs[ty][name]; !ok {
		return nil, fmt.Errorf("failed to find %v-type algorithm %q", ty, name)
	}
	return algs[ty][name], nil
}

var algs map[lease.Type]map[string]Algorithm
