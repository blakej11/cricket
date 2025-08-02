package lease

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/random"
	"github.com/blakej11/cricket/internal/types"
)

// A hook that can te changed for testing.
var rotationAmount = rand.Float64

// ------------------------------------------------------------------

// Config describes how many clients are needed/desired for an effect.
type Config struct {
	Type		 types.LeaseType
	Weight		 float64	// likelihood of being chosen
	FleetFraction	 random.Config	// desired fraction of fleet
	MinClients	 int		// minimum number of clients needed

	// At most one of these may be non-zero.
	MaxClients	 int		// maximum number of clients allowed
	MaxFleetFraction float64	// maximum fleet percent allowed

	// could request specific IDs I guess
	// could request something w/r/t PhysLocation
}

// Lease is the instantiation of a Config.
type Lease struct {
	Type		 types.LeaseType
	name		 string
	weight		 float64
	fleetFraction	 *random.Variable
	minClients	 int
	maxClients	 int
	maxFleetFraction float64
}

func New(c Config, name string) (*Lease, error) {
	ff := c.FleetFraction
	if ff.IsDefault() {
		ff = random.Config{
			Mean:         50.0,
			Variance:     20.0,
			Distribution: random.Normal,
		}
	}

	return validate(&Lease{
		Type:             c.Type,
		name:		  name,
		weight:           c.Weight,
		fleetFraction:    random.New(ff),
		minClients:       c.MinClients,
		maxClients:       c.MaxClients,
		maxFleetFraction: c.MaxFleetFraction,
	})
}

func validate(l *Lease) (*Lease, error) {
	if l.maxClients > 0 && l.maxFleetFraction > 0 {
		return nil, fmt.Errorf("lease has maxClients %d and maxFleetFraction %f; one must be zero", l.maxClients, l.maxFleetFraction)
	}
	if l.maxClients > 0 && l.minClients > l.maxClients {
		return nil, fmt.Errorf("lease with minClients %d and maxClients %d cannot be satisfied", l.minClients, l.maxClients)
	}

	return l, nil
}

// These interfaces are for testing.
// If you fail validation, you get to keep both pieces.

func (l *Lease) setMinClients(c int) *Lease {
	l.minClients = c
	if _, err := validate(l); err != nil {
		log.Fatalf("setMinClients: %v\n", err)
	}
	return l
}

func (l *Lease) setMaxClients(c int) *Lease {
	l.maxClients = c
	if _, err := validate(l); err != nil {
		log.Fatalf("setMaxClients: %v\n", err)
	}
	return l
}

func (l *Lease) setMaxFleetFraction(ff float64) *Lease {
	l.maxFleetFraction = ff
	if _, err := validate(l); err != nil {
		log.Fatalf("setMaxFleetFraction: %v\n", err)
	}
	return l
}

// ---------------------------------------------------------------------
// The interfaces to the lease subsystem.

// AddClient allows the mDNS thread to add information about a newly
// discovered client.
func AddClient(id types.ID, location types.PhysLocation) {
	for _, lt := range types.ValidLeaseTypes() {
		channels[lt] <- addMessage{id: id, location: location}
	}
}

type addMessage struct {
	id       types.ID
	location types.PhysLocation
}

// ReturnClients allows an effect to return a collection of clients.
// Clients leased for sound should have their sound queue drained before
// being returned here; similarly for clients leased for light.
func ReturnClients(ids []types.ID, lt types.LeaseType) {
	channels[lt] <- returnMessage{ids: ids}
}

type returnMessage struct {
	ids []types.ID
}

var brokers map[types.LeaseType]*broker
var channels map[types.LeaseType]chan any

func init() {
	brokers = make(map[types.LeaseType]*broker)
	channels = make(map[types.LeaseType]chan any)
	for _, lt := range types.ValidLeaseTypes() {
		brokers[lt] = newBroker()
		channels[lt] = make(chan any)
	}
}

func Start() {
	for _, lt := range types.ValidLeaseTypes() {
		go brokerThread(brokers[lt], channels[lt])
	}
}

func brokerThread(b *broker, ch chan any) {
	for msg := range ch {
		switch m := msg.(type) {
		case addMessage:
			b.addClient(m.id, m.location)
		case returnMessage:
			b.returnClients(m.ids)
		default:
			log.Fatalf("unknown message type")
		}
	}
}

// Associate a lease with a leaseholder.
func Assign(l *Lease, h Holder) {
	brokers[l.Type].assign(l, h)
}

// Holder describes how to control entities that receive leases.
type Holder interface {
	// Run is called once a leaseholder has received at least
	// minClients clients in its IDSet. It should not block for
	// an extended period of time, since it's run from the
	// lease broker thread.
	Run(types.IDSetConsumer)
}

// ---------------------------------------------------------------------

// The broker holds all of the information about what's available to be
// leased, and keeps track of any outstanding leases.
type broker struct {
	started		bool
	locations	map[types.ID]types.PhysLocation
	leased		map[types.ID]bool
	unallocated	[]types.ID

	fleetSize	int	// actual number of clients known about
	leasedCount	int	// actual number of clients assigned

	leasedFraction	float64	// may be >1.0

	holders		[]*holder
}

func newBroker() *broker {
	return &broker{
		locations:	make(map[types.ID]types.PhysLocation),
		leased:		make(map[types.ID]bool),
	}
}

func (b *broker) assign(l *Lease, h Holder) {
	if b.started {
		// This function assumes it is not racing with calls to
		// addClient()/returnClient().
		log.Fatalf("lease.Assign() must not be called after startup")
	}
	b.holders = append(b.holders, newHolder(h, *l))
}

func (b *broker) addClient(id types.ID, location types.PhysLocation) {
	b.started = true
	if _, ok := b.leased[id]; ok {
		log.Fatalf("handleAdd: duplicate request to add %q", id)
	}
	b.locations[id] = location
	b.unallocated = append(b.unallocated, id)

	b.increaseFleetSize(1)
	b.updateLeasedFraction()
	b.assignClients()
}

func (b *broker) returnClients(ids []types.ID) {
	for _, id := range ids {
		if _, ok := b.leased[id]; !ok {
			log.Fatalf("handleReturn: can't find client %q", id)
		}
		if !b.leased[id] {
			log.Fatalf("handleReturn: returning invalid lease on %q", id)
		}

		b.leasedCount--
		b.unallocated = append(b.unallocated, id)
	}

	b.cleanHolders()
	b.updateLeasedFraction()
	b.assignClients()
}

// Adjust b.fleetSize, and update the targetClientCount of any holders that
// have been assigned a fraction of the fleet.
func (b *broker) increaseFleetSize(newClients int) {
	b.fleetSize += newClients

	frac := 0.0
	count := 0
	for _, h := range b.holders {
		if h.targetFraction == 0 {
			continue
		}

		// This shares a consistent rounding behavior with
		// tryAllocateFleetFraction.
		newFrac := frac + h.targetFraction
		newCount := int(newFrac * float64(b.fleetSize))
		h.setTargetCount(newCount - count)
		frac = newFrac
		count = newCount
	}
}

// This is a best-effort attempt to clear out any holders that have been
// marked closed or that are too small to get any clients. It might race
// with an update of a Close() of that holder (since this code isn't holding
// the mutex protecting it), but we'll do another more careful pass later.
func (b *broker) cleanHolders() {
	for _, h := range b.holders {
		if h.isClosed() || h.isStale() {
			b.leasedFraction -= h.targetFraction
			h.reset()
		}
	}
}

// If there is any fraction of the fleet that is not currently assigned
// to a leaseholder, hand it out.
//
// This can update b.leasedFraction, but no other fields in "broker".
// It can also update h.targetFraction and h.targetClientCount.
func (b *broker) updateLeasedFraction() {
	const fullyLeasedFraction = 0.9999

	if b.leasedFraction >= fullyLeasedFraction {
		return
	}

	// Select the holders that haven't been allocated anything.
	var toPick []*holder
	sum := 0.0
	for _, h := range b.holders {
		if h.isDormant() && h.Lease.weight > 0 {
			toPick = append(toPick, h)
			sum += h.Lease.weight
		}
	}
	if sum == 0.0 {
		return
	}

	// Reorder the above slice, so a specific element picked out by the
	// weighted random selection will get first dibs at any allocation.
	var toActivate []*holder
	pick := rotationAmount() * sum
	sum = 0.0
	for idx, h := range toPick {
		sum += h.Lease.weight
		if sum < pick {
			continue
		}
		toActivate = append(toPick[idx:], toPick[:idx]...)
		break
	}

	for _, h := range toActivate {
		l := h.Lease
		frac := h.Lease.fleetFraction.Float64()
		if l.maxFleetFraction > 0.0 {
			frac = min(frac, l.maxFleetFraction)
		}
		oldFrac := b.leasedFraction
		newFrac := oldFrac + frac
		oldTargetCount := int(oldFrac * float64(b.fleetSize))
		newTargetCount := int(newFrac * float64(b.fleetSize))
		count := newTargetCount - oldTargetCount
		if l.maxClients > 0 {
			count = min(count, l.maxClients)
		}
		// "count" may start off at 0, e.g. when the fleet is small.
		h.init(frac, count)
		b.leasedFraction = newFrac
		if b.leasedFraction >= fullyLeasedFraction {
			break
		}
	}
}

// If there are any unassigned clients in the fleet, hand them out.
// This can update b.leased, b.unallocated, and b.leasedCount.
func (b *broker) assignClients() {
	available := len(b.unallocated)
	if b.fleetSize - b.leasedCount != available {
		log.Fatalf("%d available + %d leased != %d total",
		    available, b.leasedCount, b.fleetSize)
	}
	if available == 0 {
		return
	}

	// Select any non-dormant holders that still want some clients.
	var toPick []*holder
	sum := 0
	for _, h := range b.holders {
		if cw := h.clientsWanted(); cw > 0 {
			toPick = append(toPick, h)
			sum += cw
		}
	}
	if sum == 0 {
		log.Infof("assignClients: no clients wanted\n")
		return
	}

	// Rotate the above slice, so a specific element picked out by the
	// weighted random selection will get first dibs at any allocation.
	var toActivate []*holder
	pick := int(rotationAmount() * float64(sum))
	sum = 0
	for idx, h := range toPick {
		sum += h.clientsWanted()
		if sum < pick {
			continue
		}
		toActivate = append(toPick[idx:], toPick[:idx]...)
		break
	}

	for _, h := range toActivate {
		count := min(h.clientsWanted(), available)
		clients := b.unallocated[:count]

		if ok := h.addClients(clients); !ok {
			// This client is closed. Ignore it.
			b.leasedFraction -= h.targetFraction
			h.reset()
			continue
		}

		available -= count
		b.leasedCount += count
		b.unallocated = b.unallocated[count:]
		for _, c := range clients {
			b.leased[c] = true
		}

		if available == 0 {
			break
		}
	}
}

// ---------------------------------------------------------------------

type holder struct {
	Holder
	Lease
	is                types.IDSetProducer
	targetFraction    float64
	targetClientCount int
	initTime          time.Time
	started	          bool
}

// If a holder has been around for this long and hasn't received any
// clients, that probably means its targetFraction is too small.
const staleHolderTime = 5 * time.Minute

func newHolder(h Holder, l Lease) *holder {
	return &holder{
		Holder: h,
		Lease:  l,
	}
}

func (h *holder) init(frac float64, count int) {
	h.is = types.NewIDSetProducer()
	h.setTargetFraction(frac)
	h.setTargetCount(count)
	h.initTime = time.Now()
	h.started = false
}

func (h *holder) setTargetFraction(newFrac float64) {
	h.targetFraction = newFrac
	log.Infof("%s (%v) targeting %.6f of fleet\n", h.Lease.name, h.Lease.Type, newFrac)
}

func (h *holder) setTargetCount(newCount int) {
	h.targetClientCount = newCount
	log.Infof("%s (%v) targeting %d clients\n", h.Lease.name, h.Lease.Type, newCount)
}

// This holder has not received any clients, so it's not even trying to start.
func (h *holder) isDormant() bool {
	return h.targetClientCount == 0
}

func (h *holder) isStale() bool {
	return h.isDormant() && time.Now().Sub(h.initTime) > staleHolderTime
}

func (h *holder) isClosed() bool {
	return h.is != nil && h.is.Closed()
}

func (h *holder) clientsWanted() int {
	if h.is == nil {
		return 0
	}
	return h.targetClientCount - h.is.Size()
}

func (h *holder) addClients(clients []types.ID) bool {
	if ok := h.is.Add(clients); !ok {
		// This holder is closed. Ignore it.
		return false
	}
	h.logAssignment(clients)

	if !h.started && h.is.Size() >= h.Lease.minClients {
		h.started = true
		h.Holder.Run(h.is.NewConsumer())
	}
	return true
}

func (h *holder) logAssignment(clients []types.ID) {
	strs := make([]string, len(clients))
	for i := range clients {
		strs[i] = string(clients[i])
	}
	log.Infof("assigning clients [ %s ] to %s (%v)\n", strings.Join(strs, ", "), h.Lease.name, h.Lease.Type)
}

func (h *holder) reset() {
	h.is = nil
	h.targetFraction = 0
	h.targetClientCount = 0
	h.initTime = time.Now()
	h.started = false
}

