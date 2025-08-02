package effect

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/maphash"
	"reflect"
	"strings"
	"time"

        "github.com/blakej11/cricket/internal/client"
        "github.com/blakej11/cricket/internal/fileset"
        "github.com/blakej11/cricket/internal/lease"
        "github.com/blakej11/cricket/internal/log"
        "github.com/blakej11/cricket/internal/random"
        "github.com/blakej11/cricket/internal/request"
        "github.com/blakej11/cricket/internal/types"
)

// Config describes the configuration of a single sound or light effect.
type Config struct {
	Algorithm	string			// the name of the algorithm
	FileSets	map[string]string	// names of fileset(s) to use
	Parameters	map[string]random.Config// how to define parameters
	Duration	random.Config
	Lease		lease.Config
}

// ---------------------------------------------------------------------

// Effect is the instantiation of a Config.
type Effect struct {
	name		string
	leaseType	types.LeaseType
	alg		Algorithm
	parameters	any
	fileSets	any
	duration	*random.Variable
	skipDrain	bool	// for testing
}

func New(name string, c Config, fileSets map[string]*fileset.Set) (lease.Holder, *lease.Lease, error) {
	e, err := newEffect(name, c, fileSets)
	if err != nil {
		return nil, nil, err
	}

	l, err := lease.New(c.Lease, name)
	if err != nil {
		return nil, nil, err
	}

	return e, l, nil
}

func newEffect(name string, c Config, fileSetMap map[string]*fileset.Set) (*Effect, error) {
	alg, params, fileSets, err := lookupAlgorithm(c.Lease.Type, c.Algorithm)
	if err != nil {
		return nil, err
	}

	if err := fillStructure(name, params, "Variable", func(field string) (any, error) {
		if _, ok := c.Parameters[field]; !ok {
			return nil, fmt.Errorf("failed to find parameter %q in %q config", field, name)
		}
		return random.New(c.Parameters[field]), nil
	}); err != nil {
		return nil, err
	}

	if err := fillStructure(name, fileSets, "Set", func(field string) (any, error) {
		if _, ok := c.FileSets[field]; !ok {
			return nil, fmt.Errorf("failed to find effect %q's %q fileset", name, field)
		}
		n := c.FileSets[field]
		if _, ok := fileSetMap[n]; !ok {
			return nil, fmt.Errorf("failed to find a fileset named %q for effect %q", n, name)
		}
		return fileSetMap[n], nil
	}); err != nil {
		return nil, err
	}

	return &Effect{
		name:		name,
		leaseType:	c.Lease.Type,
		alg:		alg,
		parameters:	params,
		fileSets:	fileSets,
		duration:	random.New(c.Duration),
	}, nil
}

func fillStructure(name string, structure any, typeName string, getValue func(string) (any, error)) error {
	if structure == nil {
		return nil
	}

	ptr := reflect.TypeOf(structure)
	if ptr.Kind() != reflect.Ptr {
		return fmt.Errorf("algorithm %q has non-pointer argument %v\n", name, structure)
	}
	t := ptr.Elem()
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("algorithm %q has non-struct argument %v\n", name, structure)
	}
	v := reflect.ValueOf(structure).Elem()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.Type.Kind() != reflect.Ptr {
			return fmt.Errorf("algorithm %s's param #%d is not a pointer\n", name, i)
		}
		if sf.Type.Elem().Name() != typeName {
			return fmt.Errorf("algorithm %s's param #%d is a %q, not a %q\n", name, i,
			    sf.Type.Elem().Name(), typeName)
		}
		val, err := getValue(string(sf.Name))
		if err != nil {
			return err
		}
		v.Field(i).Set(reflect.ValueOf(val))
	}
	return nil
}

func (e *Effect) setSkipDrain() {
	e.skipDrain = true
}

// ---------------------------------------------------------------------

type Algorithm interface {
	Run(context.Context, types.IDSetConsumer, any, any)
}

func RegisterSound(name string, alg Algorithm, params any, fileSets any) {
	registerAlgorithm(types.Sound, name, alg, params, fileSets)
}

func RegisterLight(name string, alg Algorithm, params any) {
	registerAlgorithm(types.Light, name, alg, params, nil)
}

// this can be called from module init functions
func registerAlgorithm(ty types.LeaseType, name string, alg Algorithm, params any, fileSets any) {
	if algs == nil {
		algs = make(map[types.LeaseType]map[string]algorithmImpl)
		for _, t := range types.ValidLeaseTypes() {
			algs[t] = make(map[string]algorithmImpl)
		}
	}
	algs[ty][name] = algorithmImpl{
		alg:		alg,
		params:		params,
		fileSets:	fileSets,
	}
}

func lookupAlgorithm(ty types.LeaseType, name string) (Algorithm, any, any, error) {
	if _, ok := algs[ty]; !ok {
		return nil, nil, nil, fmt.Errorf("failed to find any %v-type algorithms", ty)
	}
	if _, ok := algs[ty][name]; !ok {
		return nil, nil, nil, fmt.Errorf("failed to find %v-type algorithm %q", ty, name)
	}
	a := algs[ty][name]
	return a.alg, a.params, a.fileSets, nil
}

// For testing.
func resetAlgs() {
	algs = nil
}

type algorithmImpl struct {
	alg		Algorithm
	params		any
	fileSets	any
}

var algs map[types.LeaseType]map[string]algorithmImpl

// ---------------------------------------------------------------------

// Run receives a ClientSet, starts an algorithm running, then waits
// around until all of the client leases are returned.
//
// This implements the lease.Holder interface.
func (e *Effect) Run(is types.IDSetConsumer) {
	go e.run(is)
}

func (e *Effect) run(is types.IDSetConsumer) {
	desc := fmt.Sprintf("params %s", describeArg(e.parameters))
	if e.fileSets != nil {
		desc += fmt.Sprintf(", filesets %s", describeArg(e.fileSets))
	}

	resetParams(e.parameters)
        dur := e.duration.Duration()
        ctx, cancel := context.WithTimeout(context.Background(), dur)
	log.Infof("Start  effect %q: %s, duration %v", e.name, desc, dur)
	e.alg.Run(ctx, is, e.parameters, e.fileSets)
	log.Infof("Finish effect %q: %s", e.name, desc)
	cancel()

	is.Close()
	if !e.skipDrain {
		e.drainQueue(is.Snapshot())
	}
}

func describeArg(arg any) string {
	var names []string
	t := reflect.TypeOf(arg).Elem()
	for i := 0; i < t.NumField(); i++ {
		names = append(names, strings.ToLower(string(t.Field(i).Name)))
	}
	return fmt.Sprintf("[ %s ]", strings.Join(names, ","))
}

func resetParams(params any) {
	v := reflect.ValueOf(params).Elem()
	for i := 0; i < v.NumField(); i++ {
		v.Field(i).Interface().(*random.Variable).Reset()
	}
}

func (e *Effect) drainQueue(clients []types.ID) {
	var b []byte
	drained := make(map[types.ID]bool)
	for _, id := range clients {
		drained[id] = false
		b, _ = binary.Append(b, binary.NativeEndian, ([]byte)(id))
	}
	clientHash := maphash.Bytes(maphash.MakeSeed(), b)

	acks := make(chan types.ID)
	drain := request.DrainQueue {
		Ack:		acks,
		LeaseType:	e.leaseType,
	}
	client.EnqueueAfterDelay(clients, context.Background(), &drain, 0)

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

		lease.ReturnClients(draining, e.leaseType)
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
		log.Infof("[drain %016x] %d clients still draining after %.1f seconds: %v",
		    clientHash, toDrain, now.Sub(start).Seconds(), stillDraining)
	}
}
