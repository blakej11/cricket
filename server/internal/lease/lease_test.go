package lease

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/blakej11/cricket/internal/random"
	"github.com/blakej11/cricket/internal/types"
)

// ---------------------------------------------------------------------

// launchEffect implements the lease.Holder interface.
type launchEffect struct{
	ctx     context.Context
	cancel	func()
	await   chan []types.ID
	ch	chan types.ID
}

func newLaunchEffect(targetClients int) *launchEffect {
	ctx, cancel := context.WithCancel(context.Background())
	return &launchEffect{
		ctx:    ctx,
		cancel: cancel,
		await:  make(chan []types.ID),
		ch:     make(chan types.ID, targetClients),
	}
}

func (le *launchEffect) Run(is types.IDSetConsumer) {
	go le.run(is)
}

func (le *launchEffect) run(is types.IDSetConsumer) {
	is.Launch(le.ctx, func(id types.ID) {
		le.ch <- id
		if len(le.ch) == cap(le.ch) {
			le.cancel() // causes Launch to return
		}
	})

	ids := []types.ID{}
	for len(le.ch) > 0 {
		ids = append(ids, <-le.ch)
	}
	sort.Slice(ids, func (i, j int) bool {
		return ids[i] < ids[j]
	})
	is.Close()
	le.await <- ids
}

// This will wait until "targetClients" clients have been received
// before returning. There isn't a great way to synchronize with it
// otherwise, since the function called from Launch runs async.
func (le *launchEffect) getClients() []types.ID {
	return <-le.await
}

// ---------------------------------------------------------------------

// fakeEffect implements the lease.Holder interface.
type fakeEffect struct{
	ctx     context.Context
	cancel	func()
	stop    chan any
	clients chan []types.ID
	runs    int
}

func newFakeEffect() *fakeEffect {
	ctx, cancel := context.WithCancel(context.Background())
	return &fakeEffect{
		ctx:     ctx,
		cancel:  cancel,
		stop:    make(chan any),
		clients: make(chan []types.ID),
	}
}

func (fe *fakeEffect) Run(is types.IDSetConsumer) {
	fe.runs++
	go fe.run(is)
}

func (fe *fakeEffect) run(is types.IDSetConsumer) {
	<-fe.stop
	fe.cancel()
	is.Close()
	fe.clients <- is.Snapshot()
}

func (fe *fakeEffect) getClients() []types.ID {
	fe.stop <- struct{}{}
	return <-fe.clients
}

// ---------------------------------------------------------------------

func TestLeaseConfigErrors(t *testing.T) {
	for _, test := range []struct {
		name	string
		c	Config
	}{
		{
			name: "min > max",
			c: Config{
				Type: types.Sound,
				Weight: 1.0,
				FleetFraction: random.Config{},
				MinClients: 3,
				MaxClients: 2,
			},
		},
		{
			name: "two maxes",
			c: Config{
				Type: types.Sound,
				Weight: 1.0,
				FleetFraction: random.Config{},
				MaxClients: 2,
				MaxFleetFraction: 0.75,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			l, _ := New(test.c, test.name)
			if l != nil {
				t.Errorf("LeaseConfig: want nil, got %v\n", l)
			}
		})
	}
}

// ---------------------------------------------------------------------

func newLease(t *testing.T, name string, weight, fleetFraction float64) *Lease {
	l, err := New(Config{
		Type:          types.Sound,
		Weight:        weight,
		FleetFraction: random.FixedConfig(fleetFraction),
	}, name)
	if err != nil {
		t.Fatalf("Lease generation failed: %v\n", err)
	}
	return l
}

// A helper function for doing reflect.DeepEqual() on a broker.
func pushHolders(b *broker) ([]*holder, func()) {
	holders := b.holders
	b.holders = nil
	return holders, func() { b.holders = holders }
}

// Disable the randomness that's used to "fairly" allocate clients.
func fixAllocationRandomness() func() {
	oldRandomizer := leaseRandomizer
	leaseRandomizer = func() float64 { return 0.0 }
	return func() { leaseRandomizer = oldRandomizer }
}

func TestAddClient(t *testing.T) {
	fe := newFakeEffect()
	b := newBroker()
	b.assign(newLease(t, "addClient lease", 1.0, 1.0), fe)

	wantClients := []types.ID {
		types.ID("c00"),
		types.ID("c01"),
		types.ID("c02"),
		types.ID("c03"),
		types.ID("c04"),
	}
	resume := fixAllocationRandomness()
	for i := range wantClients {
		b.addClient(wantClients[i], types.PhysLocation{})
	}
	resume()

	holders, doneWithHolders := pushHolders(b)
	wantBroker := &broker{
		started: true,
		locations: func() map[types.ID]types.PhysLocation {
			m := make(map[types.ID]types.PhysLocation)
			for _, c := range wantClients {
				m[c] = types.PhysLocation{}
			}
			return m
		}(),
		leased: func() map[types.ID]bool {
			m := make(map[types.ID]bool)
			for _, c := range wantClients {
				m[c] = true
			}
			return m
		}(),
		unallocated:    []types.ID{},
		fleetSize:      len(wantClients),
		leasedCount:    len(wantClients),
		leasedFraction: 1.0,
	}
	if !reflect.DeepEqual(wantBroker, b) {
		t.Errorf("want broker %v, got %v\n", wantBroker, b)
	}

	if len(holders) != 1 {
		t.Fatalf("broker had %d holders, wanted %d\n", len(holders), 1)
	}
	h := holders[0]
	if h.targetFraction != 1.0 {
		t.Errorf("want target fraction %f, got %f\n", 1.0, h.targetFraction)
	}
	if h.targetClientCount != len(wantClients) {
		t.Errorf("want target count %d, got %d\n", len(wantClients), h.targetClientCount)
	}
	if !h.started {
		t.Errorf("target was not started\n")
	}
	doneWithHolders()

	gotClients := fe.getClients()
	if !reflect.DeepEqual(wantClients, gotClients) {
		t.Errorf("want client list %q, got %q\n", wantClients, gotClients)
	}
}

func TestAddClients(t *testing.T) {
	type effect struct {
		fe	*fakeEffect
		name	string
		weight	float64
		fleet	float64
		runs	int
		clients	int
	}

	for _, test := range []struct {
		name	string
		effects	[]*effect
		clients	[]types.ID
	}{
		{
			name: "two leases",
			effects: []*effect {
				{ name: "l0", weight: 1.0, fleet: 0.6, runs: 1, clients: 3 },
				{ name: "l1", weight: 1.0, fleet: 0.4, runs: 1, clients: 2 },
			},
			clients: []types.ID{
				types.ID("c00"),
				types.ID("c01"),
				types.ID("c02"),
				types.ID("c03"),
				types.ID("c04"),
			},
		},
		{
			name: "imbalanced leases",
			effects: []*effect {
				{ name: "l0", weight: 0.1, fleet: 0.1, runs: 0, clients: 0 },
				{ name: "l1", weight: 0.9, fleet: 0.9, runs: 1, clients: 5 },
			},
			clients: []types.ID{
				types.ID("c00"),
				types.ID("c01"),
				types.ID("c02"),
				types.ID("c03"),
				types.ID("c04"),
			},
		},
		{
			// The weights will cause the last effect to come first and get everything.
			name: "weighted",
			effects: []*effect {
				{ name: "l0", weight: 1.0, fleet: 1.0, runs: 0, clients: 0 },
				{ name: "l1", weight: 1.0, fleet: 1.0, runs: 0, clients: 0 },
				{ name: "l2", weight: 1.0, fleet: 1.0, runs: 0, clients: 0 },
				{ name: "l3", weight: 9.0, fleet: 1.0, runs: 1, clients: 5 },
			},
			clients: []types.ID{
				types.ID("c00"),
				types.ID("c01"),
				types.ID("c02"),
				types.ID("c03"),
				types.ID("c04"),
			},
		},
		{
			name: "beyond full fleet",
			effects: []*effect {
				{ name: "l0", weight: 1.0, fleet: 0.3, runs: 1, clients: 3 },
				{ name: "l1", weight: 1.0, fleet: 0.3, runs: 1, clients: 3 },
				{ name: "l2", weight: 1.0, fleet: 1.0, runs: 1, clients: 4 },
				{ name: "l3", weight: 1.0, fleet: 0.5, runs: 0, clients: 0 },
			},
			clients: []types.ID{
				types.ID("c00"),
				types.ID("c01"),
				types.ID("c02"),
				types.ID("c03"),
				types.ID("c04"),
				types.ID("c05"),
				types.ID("c06"),
				types.ID("c07"),
				types.ID("c08"),
				types.ID("c09"),
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			b := newBroker()
			for _, e := range test.effects {
				e.fe = newFakeEffect()
				n := fmt.Sprintf("%q %s", test.name, e.name)
				b.assign(newLease(t, n, e.weight, e.fleet), e.fe)
			}
			resume := fixAllocationRandomness()
			for _, c := range test.clients {
				b.addClient(c, types.PhysLocation{})
			}
			resume()
			for i, e := range test.effects {
				if e.fe.runs != e.runs {
					t.Errorf("effect %d want run count %d, got %d\n", i, e.runs, e.fe.runs)
					if e.fe.runs == 0 {
						// This will cause the test to hang in getClients().
						t.Fatalf("effect %d should have run and didn't\n", i)
					}
				}
				if e.clients > 0 {
					c := e.fe.getClients()
					if len(c) != e.clients {
						t.Errorf("effect %d want %d clients, got %d (%v)\n", i, e.clients, len(c), c)
					}
				}
			}
		})
	}
}

func TestReturnClients(t *testing.T) {
	fe := newFakeEffect()

	b := newBroker()
	b.assign(newLease(t, "ReturnClients lease", 1.0, 1.0), fe)

	clients := []types.ID {
		types.ID("c00"),
		types.ID("c01"),
	}
	resume := fixAllocationRandomness()
	for i := range clients {
		b.addClient(clients[i], types.PhysLocation{})
	}
	resume()

	if fe.runs != 1 {
		t.Fatalf("effect 0 want run count %d, got %d\n", 1, fe.runs)
	}
	gotClients := fe.getClients()
	if len(gotClients) != 2 {
		t.Errorf("effect %d wanted %d clients, got %d (%v)\n", 0, 2, len(gotClients), gotClients)
	}

	// Since there is just one leaseholder, it will get the new clients
	// that the old copy of it returned.
	resume = fixAllocationRandomness()
	b.returnClients(gotClients)
	resume()

	if fe.runs != 2 {
		t.Fatalf("effect 0 want run count %d, got %d\n", 2, fe.runs)
	}
	gotClients = fe.getClients()
	sort.Slice(gotClients, func (i, j int) bool {
		return gotClients[i] < gotClients[j]
	})
	if !reflect.DeepEqual(clients, gotClients) {
		t.Errorf("want client list %q, got %q\n", clients, gotClients)
	}
}

func TestReturnClientsToAnother(t *testing.T) {
	fe0 := newFakeEffect()
	fe1 := newFakeEffect()

	b := newBroker()
	b.assign(newLease(t, "fe0", 1.0, 0.4), fe0)
	b.assign(newLease(t, "fe1", 1.0, 1.0), fe1)

	clients := []types.ID {
		types.ID("c00"),
		types.ID("c01"),
		types.ID("c02"),
		types.ID("c03"),
		types.ID("c04"),
	}
	resume := fixAllocationRandomness()
	for i := range clients {
		b.addClient(clients[i], types.PhysLocation{})
	}
	resume()

	runs := fe0.runs
	if runs != 1 {
		t.Fatalf("effect %d want run count %d, got %d\n", 0, 1, runs)
	}
	runs = fe1.runs
	if runs != 1 {
		t.Fatalf("effect %d want run count %d, got %d\n", 1, 1, runs)
	}
	gotClients := fe0.getClients()
	if len(gotClients) != 2 {
		t.Errorf("effect %d wanted %d clients, got %d (%v)\n", 0, 2, len(gotClients), gotClients)
	}

	// Since effect 1 still has a claim on the whole fleet, all of the
	// clients should flow to it when they're returned from effect 0.
	resume = fixAllocationRandomness()
	b.returnClients(gotClients)
	resume()

	// fe0 should not have been restarted, and fe1 should have been
	// running the whole time.
	runs = fe0.runs
	if runs != 1 {
		t.Fatalf("effect %d want run count %d, got %d\n", 0, 1, runs)
	}
	runs = fe1.runs
	if runs != 1 {
		t.Fatalf("effect %d want run count %d, got %d\n", 1, 1, runs)
	}

	// fe1 should have wound up with all of the clients.
	gotClients = fe1.getClients()
	sort.Slice(gotClients, func (i, j int) bool {
		return gotClients[i] < gotClients[j]
	})
	if !reflect.DeepEqual(clients, gotClients) {
		t.Errorf("want client list %q, got %q\n", clients, gotClients)
	}
}

func TestReturnClientsWhileFleetGrows(t *testing.T) {
	fe0 := newFakeEffect()
	fe1 := newFakeEffect()

	b := newBroker()
	b.assign(newLease(t, "fe0", 1.0, 0.4), fe0)
	b.assign(newLease(t, "fe1", 1.0, 1.0), fe1)

	clients := []types.ID {
		types.ID("c00"),
		types.ID("c01"),
		types.ID("c02"),
		types.ID("c03"),
		types.ID("c04"),
	}
	resume := fixAllocationRandomness()
	for i := range clients {
		b.addClient(clients[i], types.PhysLocation{})
	}
	resume()

	runs := fe0.runs
	if runs != 1 {
		t.Fatalf("effect %d want run count %d, got %d\n", 0, 1, runs)
	}
	runs = fe1.runs
	if runs != 1 {
		t.Fatalf("effect %d want run count %d, got %d\n", 1, 1, runs)
	}
	gotClients := fe0.getClients()
	if len(gotClients) != 2 {
		t.Errorf("effect %d wanted %d clients, got %d (%v)\n", 0, 2, len(gotClients), gotClients)
	}

	// This grows the fleet. All of the new clients should go to fe1,
	// since fe0 is closed (from the fe0.getClients() call).
	newClients := []types.ID {
		types.ID("c05"),
		types.ID("c06"),
		types.ID("c07"),
		types.ID("c08"),
		types.ID("c09"),
	}
	resume = fixAllocationRandomness()
	for i := range clients {
		b.addClient(newClients[i], types.PhysLocation{})
	}
	resume()

	// Since fe1 still has a claim on the whole fleet, all of the
	// clients should flow to it when they're returned from effect 0.
	resume = fixAllocationRandomness()
	b.returnClients(gotClients)
	resume()

	// fe0 should not have been restarted, and fe1 should have been
	// running the whole time.
	runs = fe0.runs
	if runs != 1 {
		t.Fatalf("effect %d want run count %d, got %d\n", 0, 1, runs)
	}
	runs = fe1.runs
	if runs != 1 {
		t.Fatalf("effect %d want run count %d, got %d\n", 1, 1, runs)
	}

	// fe1 should have wound up with all of the clients.
	gotClients = fe1.getClients()
	wantClients := append(clients, newClients...)
	sort.Slice(gotClients, func (i, j int) bool {
		return gotClients[i] < gotClients[j]
	})
	sort.Slice(wantClients, func (i, j int) bool {
		return wantClients[i] < wantClients[j]
	})
	if !reflect.DeepEqual(wantClients, gotClients) {
		t.Errorf("want client list %q, got %q\n", wantClients, gotClients)
	}
}

// ---------------------------------------------------------------------

func TestHolder(t *testing.T) {
	fe := newFakeEffect()

	h := newHolder(fe, Lease{
		Type:          types.Sound,
		weight:        1.0,
		fleetFraction: random.New(random.Config{}),
		minClients:    3,
	})

	if !h.isDormant() {
		t.Errorf("newly created holder is not dormant, should be\n")
	}
	if h.isClosed() {
		t.Errorf("newly created holder is closed, shouldn't be\n")
	}
	if h.clientsWanted() != 0 {
		t.Errorf("newly created holder wants clients already\n")
	}

	const fleetFrac = 0.3
	const targetClients = 5
	h.init(fleetFrac)
	h.setTargetCount(targetClients)

	if h.isDormant() {
		t.Errorf("initialized holder is dormant, shouldn't be\n")
	}
	if h.isClosed() {
		t.Errorf("initialized holder is closed, shouldn't be\n")
	}
	if h.clientsWanted() != targetClients {
		t.Errorf("initialized holder wants %d clients, expected %d\n",
		    h.clientsWanted(), targetClients)
	}

	setOne := []types.ID{types.ID("a"), types.ID("b"), types.ID("c")}
	setTwo := []types.ID{types.ID("d"), types.ID("e")}
	h.addClients(setOne)
	h.addClients(setTwo)

	want := append(setOne, setTwo...)
	got := fe.getClients()

	if !reflect.DeepEqual(want, got) {
		t.Errorf("TestHolder: wanted %q, got %q\n", want, got)
	}
	if h.isDormant() {
		t.Errorf("finished holder is dormant, shouldn't be\n")
	}
	if !h.isClosed() {
		t.Errorf("finished holder isn't closed, should be\n")
	}

	h.reset()
	if !h.isDormant() {
		t.Errorf("resetted holder is not dormant, should be\n")
	}
	if h.isClosed() {
		t.Errorf("resetted holder is closed, shouldn't be\n")
	}
}
