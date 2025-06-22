package lease

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/random"
	"github.com/blakej11/cricket/internal/types"
)

type Type int
const (
	UnknownType Type = iota
	Sound
	Light
)

func ValidTypes() []Type {
	return []Type{Sound, Light}
}

func (ty *Type) unmarshalString(s string) {
	switch strings.ToLower(s) {
	default:
		*ty = UnknownType
	case "sound":
		*ty = Sound
	case "light":
		*ty = Light
	}
}

func (ty Type) String() string {
	switch (ty) {
	default:
		return "unknown"
	case Sound:
		return "sound"
	case Light:
		return "light"
	}
}

func (ty *Type) UnmarshalText(b []byte) error {
	ty.unmarshalString(string(b))
	return nil
}

// ------------------------------------------------------------------

// Config describes how many clients are needed/desired for an effect.
type Config struct {
        Type		Type
        MinClients	int		// minimum number of clients needed
        MaxClients	int		// maximum number of clients allowed
	FleetFraction	random.Config	// desired fraction of fleet
	MaxWait		random.Config

	// could request specific IDs I guess
	// could request something w/r/t PhysLocation
}

// Params is the instantiation of a Config.
type Params struct {
        Type		Type
        minClients	int
        maxClients	int
	fleetFraction	*random.Variable
	maxWait		*random.Variable
}

func New(c Config) Params {
	return Params{
		Type:          c.Type,
		minClients:    c.MinClients,
		maxClients:    c.MaxClients,
		fleetFraction: random.New(c.FleetFraction),
		maxWait:       random.New(c.MaxWait),
	}
}

// ---------------------------------------------------------------------

// Add allows the mDNS thread to add information about a newly
// discovered client. This also undoes a Suspend operation.
func Add(id types.ID, location types.PhysLocation) {
	for _, ty := range ValidTypes() {
		enqueueReturnMessage(ty, &addMessage{id: id, location: location})
	}
}

// Request allows an effect to get a collection of clients.
func Request(p Params) ([]types.ID, error) {
	clientCh := make(chan []types.ID)
	errorCh := make(chan error)

	enqueueNormalMessage(p.Type, &requestMessage{
		params: p,
		clientResponse: clientCh,
		errorResponse: errorCh,
	})

	select {
	case clients := <-clientCh:
		return clients, nil
	case err := <-errorCh:
		return nil, err
	}
}

// Return allows an effect to return a collection of clients.
// Clients leased for sound should have their sound queue drained before
// being returned here; similarly for clients leased for light.
func Return(ids []types.ID, ty Type) {
	enqueueReturnMessage(ty, &returnMessage{ids: ids})
}

// ---------------------------------------------------------------------

// All API calls turn into messages sent over these channels, to be serialized.
func enqueueNormalMessage(ty Type, m message) {
	data[ty].normalCh <- m
}
func enqueueReturnMessage(ty Type, m message) {
	data[ty].returnCh <- m
}

type message interface {
	handle(Type)
}

type leaseData struct {
	locations	map[types.ID]types.PhysLocation
	leased		map[types.ID]bool
	idSlice		[]types.ID
	next		int
	normalCh	chan message // for request messages
	returnCh	chan message // for add and return messages
}

var data map[Type]*leaseData

func init() {
	data = make(map[Type]*leaseData)
	for _, ty := range ValidTypes() {
		data[ty] = &leaseData{
			locations:	make(map[types.ID]types.PhysLocation),
			leased:		make(map[types.ID]bool),
			normalCh:	make(chan message),
			returnCh:	make(chan message),
		}

		go func() {
			for {
				select {
				case msg := <-data[ty].normalCh:
					msg.handle(ty)
				case msg := <-data[ty].returnCh:
					msg.handle(ty)
				}
			}
		}()
	}
}

// ---------------------------------------------------------------------

type addMessage struct {
	id types.ID
	location types.PhysLocation
}

func (r *addMessage) handle(ty Type) {
	d := data[ty]

	if _, ok := d.leased[r.id]; ok {
		log.Fatalf("duplicate request to add client %q", r.id)
	}
	d.locations[r.id] = r.location
	d.leased[r.id] = false
	d.idSlice = append(d.idSlice, r.id)
}

type requestMessage struct {
	params		Params
	clientResponse	chan []types.ID
	errorResponse	chan error
}

func (r *requestMessage) handle(ty Type) {
	d := data[ty]
	params := r.params

	ctx, cancel := context.WithTimeout(context.Background(), params.maxWait.Duration())
	defer cancel()

	desired := int(math.Round(params.fleetFraction.Float64() * float64(len(d.idSlice))))
	if params.maxClients > 0 {
		desired = min(params.maxClients, desired)
	}
	desired = max(params.minClients, desired)
	if desired == 0 {
		r.clientResponse <- nil
		return
	}

	results := []types.ID{}

waitLoop:
	for {
		for i := range d.idSlice {
			index := (d.next + i) % len(d.idSlice)
			id := d.idSlice[index]
			if d.leased[id] {
				continue
			}
			d.leased[id] = true
			results = append(results, id)
			if len(results) == desired {
				d.next = index
				r.clientResponse <- results
				return
			}
		}

		// Didn't find enough clients. Wait for some to be returned
		// (and try to grab them), or for the timeout to be reached.
		select {
		case msg := <-d.returnCh:
			msg.handle(ty)
		case <-ctx.Done():
			break waitLoop
		}
	}

	// We got all the way through but haven't succeeded. What do?
	num := len(results)
	if num >= params.minClients {
		r.clientResponse <- results
		return
	}

	err := fmt.Errorf("not enough clients available (%d, wanted at least %d)", num, params.minClients)
	r.errorResponse <- err
	ret := &returnMessage{ids: results}
	ret.handle(ty)
}

type returnMessage struct {
	ids	[]types.ID
}

func (r *returnMessage) handle(ty Type) {
	d := data[ty]
	for _, id := range r.ids {
		if _, ok := d.leased[id]; !ok {
			log.Fatalf("returnClient: can't find client %q", id)
		}
		if !d.leased[id] {
			log.Fatalf("returnClient: returning invalid lease on %q", id)
		}
		d.leased[id] = false
	}
}

