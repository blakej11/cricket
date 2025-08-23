package effect

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/maphash"
	"reflect"
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

	e := &Effect{
		name:		name,
		leaseType:	c.Lease.Type,
		alg:		alg,
		parameters:	params,
		fileSets:	fileSets,
		duration:	random.New(c.Duration),
	}
	return e, nil
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

func resetParams(params any) {
	v := reflect.ValueOf(params).Elem()
	for i := 0; i < v.NumField(); i++ {
		v.Field(i).Interface().(*random.Variable).Reset()
	}
}

func (e *Effect) setSkipDrain() {
	e.skipDrain = true
}

// ---------------------------------------------------------------------

type Algorithm interface {
	Run(context.Context, types.IDSetConsumer, any, any)
}

type algorithmCtor func() (Algorithm, /*params*/ any, /*fileSets*/ any)

func RegisterSound[T Algorithm, P any, F any](name string) {
	registerAlgorithm(types.Sound, name, func() (Algorithm, any, any) {
		var t T
		var p P
		var f F
		return t, &p, &f
	})
}

func RegisterLight[T Algorithm, P any](name string) {
	registerAlgorithm(types.Light, name, func() (Algorithm, any, any) {
		var t T
		var p P
		return t, &p, nil
	})
}

// this can be called from module init functions
func registerAlgorithm(ty types.LeaseType, name string, ctor algorithmCtor) {
	if algs == nil {
		algs = make(map[types.LeaseType]map[string]algorithmCtor)
		for _, t := range types.ValidLeaseTypes() {
			algs[t] = make(map[string]algorithmCtor)
		}
	}
	algs[ty][name] = ctor
}

func lookupAlgorithm(ty types.LeaseType, name string) (Algorithm, any, any, error) {
	if _, ok := algs[ty]; !ok {
		return nil, nil, nil, fmt.Errorf("failed to find any %v-type algorithms", ty)
	}
	if _, ok := algs[ty][name]; !ok {
		return nil, nil, nil, fmt.Errorf("failed to find %v-type algorithm %q", ty, name)
	}
	alg, params, fileSets := algs[ty][name]()
	return alg, params, fileSets, nil
}

// For testing.
func resetAlgs() {
	algs = nil
}

var algs map[types.LeaseType]map[string]algorithmCtor

// ---------------------------------------------------------------------

// Run receives a ClientSet, starts an algorithm running, then waits
// around until all of the client leases are returned.
//
// This implements the lease.Holder interface.
func (e *Effect) Run(is types.IDSetConsumer) {
	go e.run(is)
}

func (e *Effect) run(is types.IDSetConsumer) {
	resetParams(e.parameters)
        dur := e.duration.Duration()
        ctx, cancel := context.WithTimeout(context.Background(), dur)
	log.Debugf("Start  effect %q: target duration %v", e.name, dur)
	e.alg.Run(ctx, is, e.parameters, e.fileSets)
	log.Debugf("Finish effect %q", e.name)
	cancel()

	is.Close()
	if !e.skipDrain {
		DrainQueue(e.leaseType, is.Snapshot())
	}
}

func DrainQueue(lt types.LeaseType, clients []types.ID) {
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
		LeaseType:	lt,
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

		lease.ReturnClients(draining, lt)
		for _, id := range draining {
			drained[id] = true
		}
		toDrain -= len(draining)
		draining = nil

		if int(now.Sub(start) / time.Second) % 10 != 0 {
			continue
		}
		stillDraining := []types.ID{}
		for id, done := range drained {
			if done {
				continue
			}
			stillDraining = append(stillDraining, id)
		}
		log.Warningf("[drain %016x, %s] %d clients still draining after %.1f seconds: %v",
		    clientHash, lt, toDrain, now.Sub(start).Seconds(), stillDraining)
	}
}
