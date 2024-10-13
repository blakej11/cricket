package lease

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/types"
)

// Config describes how many clients are needed for an effect.
type Config struct {
        Type		Type
        MinClients	int	// minimum number of clients needed
        MaxClients	int	// maximum number of clients allowed

	// could request specific IDs I guess
	// could request something w/r/t PhysLocation
}

type Type int
const (
	UnknownType Type = iota
	Sound
	Light
)

func ValidTypes() []Type {
	return []Type{Sound, Light}
}

func (t Type) String() string {
	switch (t) {
	default:
		return "unknown"
	case Sound:
		return "sound"
	case Light:
		return "light"
	}
}

// ---------------------------------------------------------------------

// Add allows the mDNS thread to add information about a newly
// discovered client. This also undoes a Suspend operation.
func Add(id types.ID, location types.PhysLocation) {
	enqueueMessage(&addMessage{id: id, location: location})
}

// Suspend allows a client to be marked as "should not be used".
func Suspend(id types.ID) {
	enqueueMessage(&suspendMessage{id: id})
}

// Resume allows a client to be marked as "can be used once again".
func Resume(id types.ID) {
	enqueueMessage(&resumeMessage{id: id})
}

// Request allows an effect to get a collection of clients.
func Request(config Config, targetFrac float64) ([]types.ID, error) {
	clientCh := make(chan []types.ID)
	errorCh := make(chan error)

	enqueueMessage(&requestMessage{
		config: config,
		targetFrac: targetFrac,
		clientResponse: clientCh,
		errorResponse: errorCh,
	})

	select {
	case clients := <-clientCh:
		return clients, nil
	case err := <-errorCh:
		return []types.ID{}, err
	}
}

// Return allows an effect to return a collection of clients.
// Clients leased for sound should have their sound queue drained before
// being returned here; similarly for clients leased for light.
func Return(ids []types.ID, ty Type) {
	enqueueMessage(&returnMessage{ids: ids, ty: ty})
}

// AwaitAvail allows a player to wait for at least one lease of the
// specified type to be available. The lease may disappear by the time
// this function returns.
func AwaitAvail(t Type) {
	a := &awaitMessage{t: t, response: make(chan struct{})}
	enqueueMessage(a)
	<- a.response
}

// ---------------------------------------------------------------------

// All API calls turn into messages sent over this channel, to be serialized.
func enqueueMessage(m message) {
	data.ch <- m
}

type message interface {
	handle()
}

type lease struct {
	id		types.ID
	location	types.PhysLocation
	leased		map[Type]bool
	suspended	bool
}

var data struct {
	leases	map[types.ID]*lease
	avail	map[Type]int
	waiting	map[Type][]*awaitMessage
	index	[]types.ID
	next	int
	ch	chan message
}

func init() {
	data.leases = make(map[types.ID]*lease)
	data.avail = make(map[Type]int)
	data.waiting = make(map[Type][]*awaitMessage)
	data.ch = make(chan message)
	for _, t := range ValidTypes() {
		data.avail[t] = 0
		data.waiting[t] = []*awaitMessage{}
	}

	go func() {
		for msg := range data.ch {
			msg.handle()
		}
	}()
}

func addAvail(t Type) {
	data.avail[t]++
	if data.avail[t] > 1 {
		return
	}
	waiters := data.waiting[t]
	data.waiting[t] = nil
	for _, w := range waiters {
		w.response <- struct{}{}
	}
}

func subAvail(t Type) {
	data.avail[t]--
}

// ---------------------------------------------------------------------

type addMessage struct {
	id types.ID
	location types.PhysLocation
}

func (r *addMessage) handle() {
	if _, ok := data.leases[r.id]; ok {
		log.Fatalf("duplicate request to add client %q", r.id)
	}
	l := &lease{
		id:		r.id,
		location:	r.location,
		leased:		make(map[Type]bool),
		suspended:	false,
	}
	for _, t := range ValidTypes() {
		l.leased[t] = false
		addAvail(t)
	}
	data.leases[r.id] = l
	data.index = append(data.index, r.id)
}

type suspendMessage struct {
	id types.ID
}

func (r *suspendMessage) handle() {
	if _, ok := data.leases[r.id]; !ok {
		log.Fatalf("can't suspend nonexistent client %q", r.id)
	}
	l := data.leases[r.id]
	if l.suspended {
		log.Fatalf("suspending already suspended client %q", r.id)
	}
	l.suspended = true
	for _, t := range ValidTypes() {
		if l.leased[t] {
			subAvail(t)
		}
	}
}

type resumeMessage struct {
	id types.ID
}

func (r *resumeMessage) handle() {
	if _, ok := data.leases[r.id]; !ok {
		log.Fatalf("can't resume nonexistent client %q", r.id)
	}
	l := data.leases[r.id]
	if !l.suspended {
		log.Fatalf("resuming non-suspended client %q", r.id)
	}
	l.suspended = false
	for _, t := range ValidTypes() {
		if !l.leased[t] {
			addAvail(t)
		}
	}
}

type requestMessage struct {
	config		Config
	targetFrac	float64
	clientResponse	chan []types.ID
	errorResponse	chan error
}

func (r *requestMessage) handle() {
	config := r.config
	desired := int(math.Round(r.targetFrac * float64(len(data.index))))
	if config.MaxClients > 0 {
		desired = min(config.MaxClients, desired)
	}
	desired = max(config.MinClients, desired)
	t := config.Type

	results := []types.ID{}
	for i := range data.index {
		index := (data.next + i) % len(data.index)
		if len(results) == desired {
			data.next = index
			r.clientResponse <- results
			return
		}
		id := data.index[i]
		l := data.leases[id]
		if l.leased[t] || l.suspended {
			continue
		}
		l.leased[t] = true
		subAvail(t)
		results = append(results, id)
	}

	// We got all the way through but haven't succeeded. What do?
	num := len(results)
	if num >= config.MinClients {
		r.clientResponse <- results
		return
	}

	err := fmt.Errorf("not enough clients available (%d, wanted at least %d)", num, config.MinClients)
	r.errorResponse <- err
	ret := &returnMessage{
		ids: results,
		ty:  config.Type,
	}
	ret.handle()
}

type returnMessage struct {
	ids	[]types.ID
	ty	Type
}

func (r *returnMessage) handle() {
	t := r.ty
	for _, id := range r.ids {
		if _, ok := data.leases[id]; !ok {
			log.Fatalf("returnClient: can't find client %q", id)
		}

		l := data.leases[id]
		if !l.leased[t] {
			log.Fatalf("returnClient: returning invalid lease on %q", id)
		}
		l.leased[t] = false
		if !l.suspended {
			addAvail(t)
		}
	}
}

type awaitMessage struct {
	t		Type
	response	chan struct{}
}

func (r *awaitMessage) handle() {
	t := r.t
	if data.avail[t] > 0 {
		r.response <- struct{}{}
		return
	}
	data.waiting[t] = append(data.waiting[t], r)
}

// ---------------------------------------------------------------------

func (t *Type) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch strings.ToLower(s) {
	default:
		*t = UnknownType
	case "sound":
		*t = Sound
	case "light":
		*t = Light
	}

	return nil
}

// needed to unmarshal a type as a map key
func (t *Type) UnmarshalText(b []byte) error {
        return t.UnmarshalJSON(b)
}

func (t Type) MarshalJSON() ([]byte, error) {
	var s string
	switch t {
	default:
		s = "unknown"
	case Sound:
		s = "sound"
	case Light:
		s = "light"
	}

	return json.Marshal(s)
}
