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

// Del allows the mDNS thread (... or something ...) to indicate that a client
// has gone away.
func Del(id types.ID) {
	enqueueAdminMessage(&delClientMessage{id: id})
}

// Request that a client perform some action.
// The caller must have already obtained an appropriate lease for this client.
// Errors are logged in the client, but not returned.
func Action(id types.ID, ctx context.Context, req clientRequest, earliest time.Time) {
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
	transientDelay = time.Second
	voltageUpdateDelay = 60 * time.Second
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
		if !c.suspended {
			log.Fatalf("duplicate request to add client %q", r.id)
		}
		c.suspended = false
		lease.Resume(r.id)
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

	c.start()

	lease.Add(r.id, physLocation)
}

type delClientMessage struct {
	id types.ID
}

func (r *delClientMessage) handle() {
	if _, ok := data.clients[r.id]; !ok {
		log.Fatalf("request to remove nonexistent client %q", r.id)
	}
	data.clients[r.id].suspended = true
	lease.Suspend(r.id)
}

// ---------------------------------------------------------------------

// client represents a single client.
type client struct {
	id		types.ID
        netLocation	types.NetLocation
	physLocation	types.PhysLocation
        name		string

	heap		*clientMessageHeap

	// messages from API clients to the heap manager
	heapChannel	chan clientMessage

	// messages from the heap manager to the device thread
	deviceChannel	chan clientMessage

        creation        time.Time
        lastPing        time.Time
        lastSuccessCmd  time.Time
        lastFailureCmd  time.Time
        lastVoltageUpdate	time.Time
        voltage		float32
	suspended	bool

        targetVolume    int
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

	r := &KeepVoltageUpdated{}
	Action(c.id, context.Background(), r, time.Now().Add(voltageUpdateDelay))
}

func (c *client) heapThread() {
	for {
		// XXX: some sort of delay in case of transient geturl fails?

		deadline := time.Until(c.heap.nextDeadline())
		select {
		case msg := <-c.heapChannel:
			heap.Push(c.heap, msg)
			continue
		case <-time.After(deadline):
			// there's at least one message ready to dequeue
		}

		poppedMsg := heap.Pop(c.heap).(clientMessage)
		if poppedMsg.ctx.Err() != nil {
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
				log.Errorf("request failed: %v", err)
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
	fileset.File
}

func (r *Play) handle(ctx context.Context, c *client) error {
	msg, err := c.getURL(ctx, "play",
		fmt.Sprintf("folder=%d", r.Folder),
		fmt.Sprintf("file=%d", r.File))
	if err != nil {
		return err
	}

	// this returns the current volume
	res := strings.Split(msg, ":")
	if len(res) == 2 {
		volume, err := strconv.Atoi(strings.TrimSpace(res[1]))
		// This can happen if a device resets.
		if err == nil && volume != c.targetVolume {
			Action(c.id, ctx, &SetVolume{Volume: c.targetVolume},
				time.Now().Add(transientDelay))
		}
	}
	return nil
}

type SetVolume struct {
	Volume int
}

func (r *SetVolume) handle(ctx context.Context, c *client) error {
	arg1 := fmt.Sprintf("volume=%d", r.Volume)
	_, err := c.getURL(ctx, "setvolume", arg1, "persist=true")

	// set this regardless of whether the set-volume action succeeded
	c.targetVolume = r.Volume

	if err != nil {
		return err
	}

	return nil
}

type Blink struct {
	Speed  float32
	Delay  int
	Jitter int
	Reps   int
}

func (r *Blink) handle(ctx context.Context, c *client) error {
	_, err := c.getURL(ctx, "blink",
		fmt.Sprintf("speed=%f", r.Speed),
		fmt.Sprintf("delay=%d", r.Delay),
		fmt.Sprintf("jitter=%d", r.Jitter),
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
	retryTime := time.Now().Add(transientDelay)
	body, err := c.getURL(ctx, "battery")
	if err != nil {
		Action(c.id, ctx, r, retryTime)
		return err
	}
	p, err := strconv.ParseFloat(strings.TrimSpace(body), 32)
	if err != nil {
		Action(c.id, ctx, r, retryTime)
		return err
	}

	c.voltage = float32(p)
	c.lastVoltageUpdate = time.Now()
	log.Infof("voltage is %v", p)

	Action(c.id, ctx, r, time.Now().Add(voltageUpdateDelay))
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
		// hopefully just a transient issue...
		Action(c.id, ctx, r, retryTime)
		return err
	}
	p, err := strconv.ParseInt(strings.TrimSpace(body), 10, 32)
	if err != nil {
		// hopefully just a transient issue...
		Action(c.id, ctx, r, retryTime)
		return err
	}
	if int(p) == 0 {
		r.Ack <- c.id
		return nil
	}

	log.Infof("%s queue length is %v", r.Type.String(), p) // XXX
	Action(c.id, ctx, r, retryTime)
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

	getURLFailure := func(err error, message string) (string, error) {
		if ctx.Err() == nil {
			c.lastFailureCmd = time.Now()
		}
		return "", fmt.Errorf("[%s] %s: %v", c.id, message, err)
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
		return getURLFailure(err, fmt.Sprintf("got failure status code (%d) from %s", resp.StatusCode, desc))
	}

	// Infof("[%s] %s returned success: %s", c.id, desc, body)
	c.lastSuccessCmd = time.Now()
	return string(body), nil
}
