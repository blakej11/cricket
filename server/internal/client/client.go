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

// Request that some clients perform an action, at some delay from now.
func EnqueueAfterDelay(ids []types.ID, ctx context.Context, req clientRequest, delay time.Duration) {
	for _, id := range ids {
		action(mustGetClient(id), ctx, req, delay, fromNow)
	}
}

// Request that some clients perform an action, at some delay from the
// end of the last enqueued sound play request.
func EnqueueAfterSoundEnds(ids []types.ID, ctx context.Context, req clientRequest, delay time.Duration) {
	for _, id := range ids {
		action(mustGetClient(id), ctx, req, delay, fromEndOfSound)
	}
}

// Returns the current time when the (server-side) sound queue will be
// drained. This is not synchronized, so it's only a best guess.
func HasSoundUntil(id types.ID) time.Time {
	return mustGetClient(id).soundEndsTime
}

func mustGetClient(id types.ID) *client {
	c, ok := data.clients[id]
	if !ok {
		log.Fatalf("can't execute request on nonexistent client %q", id)
	}
	return c
}

// Request that a single client perform some action.
// The caller must have already obtained an appropriate lease for this client.
// Errors are logged in the client, but not returned.
func action(c *client, ctx context.Context, req clientRequest, delay time.Duration, fromWhen heapDeadline) {
	c.heapChannel <- clientMessage{
		ctx:		ctx,
		clientRequest:	req,
		delay:		delay,
		fromWhen:	fromWhen,
	}
}

type heapDeadline int
const (
	fromNow heapDeadline = iota
	fromEndOfSound
)

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
		soundEndsTime:	time.Now(),

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

	// these fields are updated by client API callers,
	// and thus need to be synchronized
        soundEndsTime	time.Time
}

func (c client) String() string {
	return fmt.Sprintf("[%s (%q, %v, %v)]", c.id, c.name, c.netLocation, c.physLocation)
}

type clientMessage struct {
	ctx		context.Context
	clientRequest
	delay		time.Duration
	fromWhen	heapDeadline

	// this field is only used by the heap.
	earliest	time.Time
}

func (c *client) start() {
	go c.heapThread()
	go c.deviceThread()

	s := &Stop{}
	action(c, context.Background(), s, 0, fromNow)

	v := &SetVolume{Volume: c.targetVolume}
	action(c, context.Background(), v, 0, fromNow)

	k := &KeepVoltageUpdated{}
	action(c, context.Background(), k, voltageUpdateDelay, fromNow)
}

// ---------------------------------------------------------------------
// The following code is only run from the heapThread.

// The heapThread maintains the heap of messages, and sends them along
// to the deviceThread when (a) there's a message that's ready to send,
// and (b) the deviceThread is ready to receive a message.
func (c *client) heapThread() {
	for {
		select {
		case msg := <-c.heapChannel:
			c.pushHeap(msg)
			continue
		case <-time.After(time.Until(c.heap.nextDeadline())):
			// there's at least one message ready to dequeue
		}

		poppedMsg := c.popHeap()
		if poppedMsg.ctx.Err() != nil {
			log.Infof("%v: discarding expired message: %v", *c, poppedMsg.ctx.Err())
			continue
		}

		select {
		case msg := <-c.heapChannel:
			// deviceThread was blocked while we were trying to
			// send the popped message to it, and another message
			// arrived on the heap channel in the meantime. Put
			// both messages back on the heap and try again. This
			// ensures that a stuck client won't block any effect
			// that's trying to communicate with it.
			c.pushHeap(msg)
			c.pushHeap(poppedMsg)
		case c.deviceChannel <- poppedMsg:
			// Successfully sent the popped message.
		}
	}
}

func (c *client) pushHeap(msg clientMessage) {
	if msg.earliest.IsZero() {
		switch msg.fromWhen {
		case fromNow:
			msg.earliest = time.Now().Add(msg.delay)
		case fromEndOfSound:
			msg.earliest = c.soundEndsTime.Add(msg.delay)
		}
		if play, ok := msg.clientRequest.(*Play); ok {
			thisEndTime := msg.earliest.Add(play.Duration())
			if c.soundEndsTime.Before(thisEndTime) {
				c.soundEndsTime = thisEndTime
			}
		}
	}

	heap.Push(c.heap, msg)
}

func (c *client) popHeap() clientMessage {
	return heap.Pop(c.heap).(clientMessage)
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

// ------------------------------------------------------------------
// The following code is only run from the deviceThread.

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
// Doesn't take jitter into account, because that happens on the client.
func (r *Play) Duration() time.Duration {
	delay := r.Delay.Seconds()
	d := (r.File.Duration + delay) * float64(r.Reps)
	if (r.Reps > 0) {
		d -= delay	// don't delay after the last one
	}
	return time.Duration(d * float64(time.Second))
}

func (r *Play) handle(ctx context.Context, c *client) error {
	delay := r.Delay.Milliseconds()
	jitter := r.Jitter.Milliseconds()

	log.Infof("%s playing %2d/%2d (%d reps, %d delay, %d jitter, expected time %.2f sec)",
            *c, r.File.Folder, r.File.File, r.Reps, delay, jitter, r.Duration().Seconds())

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
		fmt.Sprintf("delay=%d", delay),
		fmt.Sprintf("jitter=%d", jitter))
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
	body, err := c.getURL(ctx, "battery")
	if err != nil {
		action(c, ctx, r, voltageUpdateDelay, fromNow)
		return err
	}
	p, err := strconv.ParseFloat(strings.TrimSpace(body), 32)
	if err != nil {
		action(c, ctx, r, voltageUpdateDelay, fromNow)
		return err
	}

	c.voltage = float32(p)
	c.lastVoltageUpdate = time.Now()
	log.Infof("%v voltage is %.2f", c, p)

	action(c, ctx, r, voltageUpdateDelay, fromNow)
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

	body, err := c.getURL(ctx, url)
	if err != nil {
		action(c, ctx, r, transientDelay, fromNow)
		return err
	}
	p, err := strconv.ParseInt(strings.TrimSpace(body), 10, 32)
	if err != nil {
		action(c, ctx, r, transientDelay, fromNow)
		return err
	}
	if int(p) == 0 {
		r.Ack <- c.id
		return nil
	}

	action(c, ctx, r, transientDelay, fromNow)
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
