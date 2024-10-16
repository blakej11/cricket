package client

import (
	"container/heap"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/blakej11/cricket/internal/fileset"
	"github.com/blakej11/cricket/internal/lease"
	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/types"
)

// Add allows the mDNS thread to add information about a newly discovered
// client.
func Add(id types.ID, loc types.NetLocation) {
	enqueueAdminMessage(&addClientMessage{id: id, location: loc})
}

// Request that some clients perform an action.
func Action(ids []types.ID, ctx context.Context, req clientRequest, earliest time.Time) {
	for _, id := range ids {
		action(id, ctx, req, earliest)
	}
}

// Request that a single client perform some action.
// The caller must have already obtained an appropriate lease for this client.
// Errors are logged in the client, but not returned.
func action(id types.ID, ctx context.Context, req clientRequest, earliest time.Time) {
	c, ok := data.clients[id]
	if !ok {
		log.Fatalf("can't execute request on nonexistent client %q", id)
	}
	c.heapChannel <- clientMessage{
		ctx:		ctx,
		clientRequest:	req,
		earliest:	earliest,
	}
}

// ---------------------------------------------------------------------

func Configure(defaultVolume int, clients map[types.ID]types.Client) { 
	data.defaultVolume = defaultVolume
	data.config = clients
}

func enqueueAdminMessage(m adminMessage) {
	data.ch <- m
}

type adminMessage interface {
	handle()
}

const (
	// Time between attempts to DrainQueue in case of network failure.
	transientDelay = 5 * time.Second

	// Time between voltage updates.
	voltageUpdateDelay = 60 * time.Second

	// Time between getURL() calls to a given client, to avoid "connection reset by peer".
	postGetURLDelay = 30 * time.Millisecond
)

func init() {
	data.clients = make(map[types.ID]*client)
	data.ch = make(chan adminMessage)
	data.config = make(map[types.ID]types.Client)
	data.defaultVolume = 24 // midway between min (0) and max (48)

	go func() {	// The admin thread.
		for msg := range data.ch {
			msg.handle()
		}
	}()
}

var data struct {
	clients		map[types.ID]*client
	ch		chan adminMessage

	// Client information from startup configuration.
	defaultVolume	int
	config		map[types.ID]types.Client
}

// ---------------------------------------------------------------------
// Admin message handling - performed by the admin thread.

type addClientMessage struct {
	id		types.ID
	location	types.NetLocation
}

func (r *addClientMessage) handle() {
	if _, ok := data.clients[r.id]; ok {
		c := data.clients[r.id]
		log.Infof("%v got new add from existing client", *c)
		if !c.netLocation.Address.Equal(r.location.Address) ||
		   c.netLocation.Port != r.location.Port {
			log.Infof("%v updating net to %v", *c, r.location)
			c.netLocation = r.location
		}
		return
	}

	physLocation := types.PhysLocation{}
	name := ""
	if conf, ok := data.config[r.id]; ok {
		physLocation = conf.PhysLocation
		name = conf.Name
	}

	c := &client{
		id:		r.id,
		netLocation:	r.location,
		physLocation:	physLocation,
		name:		name,

		heapChannel:	make(chan clientMessage),
		deviceChannel:	make(chan clientMessage),
		heap:		&clientMessageHeap{},

		creation:	time.Now(),

		targetVolume:	data.defaultVolume,
	}
	data.clients[r.id] = c
	log.Infof("%v adding new client", *c)

	c.start()

	lease.Add(r.id, physLocation)
}

// ---------------------------------------------------------------------

// client represents a single client.
type client struct {
	id		types.ID
        name		string
        netLocation	types.NetLocation
	physLocation	types.PhysLocation

	heap		*clientMessageHeap

	// messages from API clients to the heap manager
	heapChannel	chan clientMessage

	// messages from the heap manager to the device thread
	deviceChannel	chan clientMessage

        creation        time.Time
        lastPing        time.Time
	nextGetURL	time.Time
        lastSuccessCmd  time.Time
        lastFailureCmd  time.Time
        lastVoltageUpdate	time.Time
        voltage		float32

        targetVolume    int
}

func (c client) String() string {
	return fmt.Sprintf("[%s (%q, %v, %v)]", c.id, c.name, c.netLocation, c.physLocation)
}

type clientMessage struct {
	ctx		context.Context
	clientRequest
	earliest	time.Time
}

type clientMessageHeap []clientMessage

// https://pkg.go.dev/container/heap
func (h clientMessageHeap) Len() int {
	return len(h)
}

func (h clientMessageHeap) Less(i, j int) bool {
	return h[i].earliest.Before(h[j].earliest)
}

func (h clientMessageHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *clientMessageHeap) Push(x any) {
	*h = append(*h, x.(clientMessage))
}

func (h *clientMessageHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// not part of the containers/heap interface
func (h *clientMessageHeap) nextDeadline() time.Time {
	if len(*h) == 0 {
		// an arbitrary timeout; if nothing happens between now and
		// then, we'll just keep waiting for this period of time
		return time.Now().Add(3600 * time.Second)
	}
	return (*h)[0].earliest
}

func (c *client) start() {
	go c.heapThread()
	go c.deviceThread()

	s := &Stop{}
	action(c.id, context.Background(), s, time.Now())

	v := &SetVolume{Volume: c.targetVolume}
	action(c.id, context.Background(), v, time.Now())

	k := &KeepVoltageUpdated{}
	action(c.id, context.Background(), k, time.Now().Add(voltageUpdateDelay))
}

func (c *client) heapThread() {
	for {
		select {
		case msg := <-c.heapChannel:
			heap.Push(c.heap, msg)
			continue
		case <-time.After(time.Until(c.heap.nextDeadline())):
			// there's at least one message ready to dequeue
		}

		poppedMsg := heap.Pop(c.heap).(clientMessage)
		if poppedMsg.ctx.Err() != nil {
			log.Infof("%v: discarding expired message: %v", *c, poppedMsg.ctx.Err())
			continue
		}

		select {
		case msg := <-c.heapChannel:
			// We got another incoming message before we were
			// able to push this one to the device channel.
			// Try again.
			heap.Push(c.heap, msg)
			heap.Push(c.heap, poppedMsg)
		case c.deviceChannel <- poppedMsg:
			// Successfully sent the popped message.
		}
	}
}

func (c *client) deviceThread() {
	for {
		select {
		case msg := <-c.deviceChannel:
			err := msg.clientRequest.handle(msg.ctx, c)
			if err != nil {
				log.Errorf("%v request failed: %v", *c, err)
			}
		}
	}
}

// ------------------------------------------------------------------
// The following code is only run from the deviceThread.

// The commands that a client can handle implement this interface.
type clientRequest interface {
	handle(ctx context.Context, c *client) error
}

type Ping struct {}

func (r *Ping) handle(ctx context.Context, c *client) error {
	_, err := c.getURL(ctx, "ping")
	if err != nil {
		return err
	}
	c.lastPing = time.Now()
	return nil
}

type Play struct {
	File	fileset.File
	Volume	int
	Reps	int
	Delay	time.Duration
	Jitter	time.Duration
}

// The expected duration of this command.
// This is an unfortunate hack given the synchronous web server on the client.
func (r *Play) Duration() time.Duration {
	reps := r.Reps
	if reps == 0 {
		reps = 1
	}
	d := (r.File.Duration + r.Delay.Seconds()) * float64(reps)
	return time.Duration(d * float64(time.Second))
}

func (r *Play) handle(ctx context.Context, c *client) error {
	log.Infof("%s playing %2d/%2d (%d reps, %d delay, %d jitter, expected time %.2f sec)",
            *c, r.File.Folder, r.File.File, r.Reps, r.Delay.Milliseconds(), r.Jitter.Milliseconds(),
            r.Duration().Seconds())

	if r.Reps == 0 {
		return nil
	}
	volume := r.Volume
	if volume == 0 {
		volume = c.targetVolume
	}

	_, err := c.getURL(ctx, "play",
		fmt.Sprintf("folder=%d", r.File.Folder),
		fmt.Sprintf("file=%d", r.File.File),
		fmt.Sprintf("volume=%d", volume),
		fmt.Sprintf("reps=%d", r.Reps),
		fmt.Sprintf("delay=%d", r.Delay.Milliseconds()),
		fmt.Sprintf("jitter=%d", r.Jitter.Milliseconds()))
	return err
}

type SetVolume struct {
	Volume int
}

func (r *SetVolume) handle(ctx context.Context, c *client) error {
	arg1 := fmt.Sprintf("volume=%d", r.Volume)
	_, err := c.getURL(ctx, "setvolume", arg1, "persist=true")

	// set this regardless of whether the set-volume action succeeded
	c.targetVolume = r.Volume

	return err
}

type Blink struct {
	Speed  float64
	Delay  time.Duration
	Jitter time.Duration
	Reps   int
}

// The expected duration of this command.
// This is an unfortunate hack given the synchronous web server on the client.
func (r *Blink) Duration() time.Duration {
	pause := ((256.0 / r.Speed) * 2.0) + float64(r.Delay.Milliseconds())
	pause *= float64(r.Reps)
	return time.Duration(pause * float64(time.Millisecond))
}

func (r *Blink) handle(ctx context.Context, c *client) error {
	_, err := c.getURL(ctx, "blink",
		fmt.Sprintf("speed=%.3f", r.Speed),
		fmt.Sprintf("delay=%d", r.Delay.Milliseconds()),
		fmt.Sprintf("jitter=%d", r.Jitter.Milliseconds()),
		fmt.Sprintf("reps=%d", r.Reps))
	return err
}

type Pause struct {}

func (r *Pause) handle(ctx context.Context, c *client) error {
	_, err := c.getURL(ctx, "pause")
	return err
}

type Unpause struct {}

func (r *Unpause) handle(ctx context.Context, c *client) error {
	_, err := c.getURL(ctx, "unpause")
	return err
}

type Stop struct {}

func (r *Stop) handle(ctx context.Context, c *client) error {
	_, err := c.getURL(ctx, "stop")
	return err
}

type KeepVoltageUpdated struct {}

func (r *KeepVoltageUpdated) handle(ctx context.Context, c *client) error {
	retryTime := time.Now().Add(voltageUpdateDelay)
	body, err := c.getURL(ctx, "battery")
	if err != nil {
		action(c.id, ctx, r, retryTime)
		return err
	}
	p, err := strconv.ParseFloat(strings.TrimSpace(body), 32)
	if err != nil {
		action(c.id, ctx, r, retryTime)
		return err
	}

	c.voltage = float32(p)
	c.lastVoltageUpdate = time.Now()
	log.Infof("%v voltage is %.2f", c, p)

	action(c.id, ctx, r, retryTime)
	return nil
}

type DrainQueue struct {
	Ack	chan types.ID
	Type	lease.Type
}

func (r *DrainQueue) handle(ctx context.Context, c *client) error {
	url := "unknown"
	switch r.Type {
	case lease.Sound:
		url = "soundpending"
	case lease.Light:
		url = "lightpending"
	}

	retryTime := time.Now().Add(transientDelay)
	body, err := c.getURL(ctx, url)
	if err != nil {
		action(c.id, ctx, r, retryTime)
		return err
	}
	p, err := strconv.ParseInt(strings.TrimSpace(body), 10, 32)
	if err != nil {
		action(c.id, ctx, r, retryTime)
		return err
	}
	if int(p) == 0 {
		r.Ack <- c.id
		return nil
	}

	action(c.id, ctx, r, retryTime)
	return nil
}

func (c *client) getURL(ctx context.Context, command string, args ...string) (string, error) {
	url := fmt.Sprintf("http://%s:%d/%s", c.netLocation.Address, c.netLocation.Port, command)
	urlArgs := strings.Join(args, "&")
	if urlArgs != "" {
		url = url + "?" + urlArgs
	}

	desc := fmt.Sprintf("%q", command)
	descArgs := strings.Join(args, ",")
	if descArgs != "" {
		desc = desc + " (" + descArgs + ")"
	}

	now := time.Now()
	if now.Before(c.nextGetURL) {
		dur := c.nextGetURL.Sub(now)
		<-time.After(dur)
	}

	getURLFailure := func(err error, message string) (string, error) {
		t := time.Now()
		times := fmt.Sprintf("[last success %v, last fail %v, now %v]", c.lastSuccessCmd, c.lastFailureCmd, t)
		if ctx.Err() == nil {
			c.lastFailureCmd = t
			c.nextGetURL = c.lastSuccessCmd.Add(postGetURLDelay)
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

	c.lastSuccessCmd = time.Now()
	c.nextGetURL = c.lastSuccessCmd.Add(postGetURLDelay)
	return string(body), nil
}
