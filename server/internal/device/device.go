package device

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/timedheap"
	"github.com/blakej11/cricket/internal/types"
)

// The device thread executes commands that implement this interface.
type Request interface {
	// Execute the request on the given device.
	Execute(ctx context.Context, d *Device) error

	// How long will this request take, in expectation,
	// once it starts running on the device?
	Duration() time.Duration

	// What type of request is this?
	Type() RequestType
}               

type RequestType int
const (
	Sound RequestType = iota
	Light
	Admin
)

// ---------------------------------------------------------------------

const (
	// Names of timestamps.
	creation    = "first registration"
	endOfAdmin  = "end of last enqueued admin request"
	endOfLight  = "end of last enqueued light request"
	endOfSound  = "end of last enqueued sound request"
	lastFailure = "last failed client call"
	lastSuccess = "last successful client call"
	nextExecute = "next time Execute() can proceed"
)

type Device struct {
	id		types.ID
	name		string
	physLocation	types.PhysLocation

	netLocation	types.NetLocation
	locMu		sync.Mutex

	timedHeap	*timedheap.TimedHeap[requestWithContext]
	heapMu		sync.Mutex

	timestamps	map[string]time.Time
	statistics	map[string]float32
	targetVolume    int
	statsMu		sync.Mutex
}

type requestWithContext struct {
	req	Request
	ctx	context.Context
}

func New(id types.ID, name string, netLocation types.NetLocation, physLocation types.PhysLocation, volume int) *Device {
	d := &Device{
		id:		id,
		name:		name,
		physLocation:	physLocation,

		timedHeap:	timedheap.New[requestWithContext](),
		timestamps:	make(map[string]time.Time),
		statistics:	make(map[string]float32),
	}
	d.SetNetLocation(netLocation)
	d.SetTargetVolume(volume)
	d.SetTimestamp(creation, time.Now())
	return d
}

func (d *Device) Start() {
	ch := make(chan requestWithContext)
	go d.timedHeap.Start(ch)
	go func(in <-chan requestWithContext) {
		for rwc := range in {
			if rwc.ctx.Err() != nil {
				log.Infof("%v: discarding expired request: %v",
				    rwc, rwc.ctx.Err())
				continue
			}
			if err := rwc.req.Execute(rwc.ctx, d); err != nil {
				log.Errorf("%v request failed: %v", *d, err)
			}
		}
	}(ch)
}

type EnqueueDeadline int
const (
	FromNow EnqueueDeadline = iota
	FromEnd
)

// Enqueue a request for a device to perform.  The caller must have already
// obtained an appropriate lease for this device.  Errors are logged, but not
// returned.
func (d *Device) Enqueue(ctx context.Context, req Request, delay time.Duration, fromWhen EnqueueDeadline) {
	// This code is called both from the sound/light code ("the top half")
	// as well as from specific request types ("the bottom half").
	d.heapMu.Lock()
	defer d.heapMu.Unlock()

	endStampName := ""
	switch req.Type() {
	case Sound:
		endStampName = endOfSound
	case Light:
		endStampName = endOfLight
	case Admin:
		endStampName = endOfAdmin
	default:
		log.Fatalf("device.Enqueue: unknown type %v\n", req.Type())
	}

	earliest := time.Now()
	endTime := d.GetTimestamp(endStampName)
	if endTime.IsZero() {
		endTime = earliest
	}

	switch fromWhen {
	case FromNow:
		earliest = earliest.Add(delay)
	case FromEnd:
		earliest = endTime.Add(delay)
	}

	thisEndTime := earliest.Add(req.Duration())
	if endTime.Before(thisEndTime) {
		d.SetTimestamp(endStampName, endTime)
	}

	d.timedHeap.Add(requestWithContext{req: req, ctx: ctx}, earliest)
}

func (d Device) SoundEndsTime() time.Time {
	return d.GetTimestamp(endOfSound)
}

func (d Device) String() string {
	return fmt.Sprintf("[%s (%q, %s, %v)]", d.id, d.name, d.netLocation.String(), d.physLocation)
}

// ------------------------------------------------------------------

func (d *Device) Execute(ctx context.Context, endpoint string, args map[string]string) (string, error) {
	// Time between Execute() calls to a given device, to avoid
	// "connection reset by peer" errors.
	const postGetURLDelay = 30 * time.Millisecond

	url := fmt.Sprintf("http://%s/%s", d.GetNetLocation(), endpoint)
	desc := fmt.Sprintf("%q", endpoint)

	if args != nil {
		var equalArgs []string
		for k, v := range args {
			equalArgs = append(equalArgs, fmt.Sprintf("%s=%s", k, v))
		}
		url = url + "?" + strings.Join(equalArgs, "&")
		desc = desc + " (" + strings.Join(equalArgs, ",") + ")"
	}

	// Wait until this is allowed to proceed.
	// (A workaround for flaky webserver software on the device.)
	now := time.Now()
	next := d.GetTimestamp(nextExecute)
	if now.Before(next) {
		<-time.After(next.Sub(now))
	}

	getURLFailure := func(err error, message string) (string, error) {
		ls := d.GetTimestamp(lastSuccess)
		lf := d.GetTimestamp(lastFailure)
		now := time.Now()

		times := fmt.Sprintf("[last success %v, last fail %v, now %v]", ls, lf, now)
		if ctx.Err() == nil {
			d.SetTimestamp(lastFailure, now)
			d.SetTimestamp(nextExecute, ls.Add(postGetURLDelay))
		}
		return "", fmt.Errorf("%s %s: err = %v", times, message, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return getURLFailure(err, fmt.Sprintf("NewRequest(%s) returned error", desc))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return getURLFailure(err, fmt.Sprintf("Do(%s) returned error", desc))
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return getURLFailure(err, fmt.Sprintf("error while reading body from %s", desc))
	}
	if resp.StatusCode > 299 {
		return getURLFailure(err, fmt.Sprintf("got failure status code (%d) from %s: %q", resp.StatusCode, desc, body))
	}

	t := time.Now()
	d.SetTimestamp(lastSuccess, t)
	d.SetTimestamp(nextExecute, t.Add(postGetURLDelay))
	return string(body), nil
}

func (d *Device) SetTimestamp(name string, t time.Time) {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()

	d.timestamps[name] = t
}

func (d *Device) SetStatistic(name string, v float32) {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()

	d.statistics[name] = v
}

func (d *Device) SetTargetVolume(v int) {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()

	d.targetVolume = v
}

func (d *Device) GetTimestamp(name string) time.Time {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()

	return d.timestamps[name]
}

func (d *Device) GetStatistic(name string) float32 {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()

	return d.statistics[name]
}

func (d *Device) GetTargetVolume() int {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()

	return d.targetVolume
}

func (d *Device) GetID() types.ID {
	return d.id
}

func (d *Device) GetTimestamps() map[string]time.Time {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()

	return d.timestamps
}

func (d *Device) GetStatistics() map[string]float32 {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()

	return d.statistics
}

func (d *Device) GetNetLocation() string {
	d.locMu.Lock()
	defer d.locMu.Unlock()

	return d.netLocation.String()
}

func (d *Device) SetNetLocation(newLoc types.NetLocation) {
	d.locMu.Lock()
	defer d.locMu.Unlock()

	if !d.netLocation.Equal(newLoc) {
		log.Infof("%v updating IP to %s", *d, newLoc.String())
		d.netLocation = newLoc
	}
}

