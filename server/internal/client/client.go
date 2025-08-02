// Package client provides the interface to interacting with the cricket
// devices. It also listens for mDNS messages advertising the arrival of
// new devices.
package client

import (
	"context"
	"strings"
	"time"

	"github.com/blakej11/cricket/internal/device"
	"github.com/blakej11/cricket/internal/lease"
	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/request"
	"github.com/blakej11/cricket/internal/types"

	zeroconf "github.com/libp2p/zeroconf/v2"
)

var data struct {
	devices		map[types.ID]*device.Device

	// Client information from startup configuration.
	defaultVolume	int
	config		map[types.ID]types.Client
}

type adminMessage interface {
	handle()
}

func init() {
	data.devices = make(map[types.ID]*device.Device)
	data.defaultVolume = 24 // midway between min (0) and max (48)
	data.config = make(map[types.ID]types.Client)
}

func Configure(defaultVolume int, config map[types.ID]types.Client) { 
	data.defaultVolume = defaultVolume
	data.config = config
}

func Start() {
	ch := make(chan adminMessage)
	go mDNSListener(ch)
	go func() {
		for msg := range ch {
			msg.handle()
		}
	}()
}

// ---------------------------------------------------------------------

// Enqueue an action for some devices to perform at some delay from now.
func EnqueueAfterDelay(ids []types.ID, ctx context.Context, req device.Request, delay time.Duration) {
	for _, id := range ids {
		mustGetDevice(id).Enqueue(ctx, req, delay, device.FromNow)
	}
}

// Enqueue an action for some devices to perform, at some delay from the
// end of the last enqueued sound play request.
func EnqueueAfterSoundEnds(ids []types.ID, ctx context.Context, req device.Request, delay time.Duration) {
	for _, id := range ids {
		mustGetDevice(id).Enqueue(ctx, req, delay, device.FromEnd)
	}
}

// Returns the current time when the (server-side) sound queue will be
// drained. This is not synchronized, so it's only a best guess.
func HasSoundUntil(id types.ID) time.Time {
	return mustGetDevice(id).SoundEndsTime()
}

func mustGetDevice(id types.ID) *device.Device {
	d, ok := data.devices[id]
	if !ok {
		log.Fatalf("can't execute request on nonexistent client %q", id)
	}
	return d
}

// ---------------------------------------------------------------------

func mDNSListener(out chan<- adminMessage) {
	entries := make(chan *zeroconf.ServiceEntry)

	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			if len(entry.AddrIPv4) < 1 {
				continue
			}
			s := strings.Split(entry.Instance, " ")
			if len(s) < 2 || !strings.HasPrefix(s[0], "Cricket") {
				continue
			}
			out <- addClientMessage{
				id:       types.ID(s[1]),
				location: types.NetLocation{
					Address: entry.AddrIPv4[0],
					Port:    entry.Port,
				},
			}
		}
	}(entries)

	ctx := context.Background()
	err := zeroconf.Browse(ctx, "_http._tcp", "local.", entries)
	if err != nil {
		log.Fatalf("failed to browse mDNS: %v", err.Error())
	}
	<-ctx.Done()	// should not be reached
}

type addClientMessage struct {
	id		types.ID
	location	types.NetLocation
}

func (r addClientMessage) handle() {
	if d, ok := data.devices[r.id]; ok {
		log.Infof("got new add from existing client: %v", *d)
		d.SetNetLocation(r.location)
		return
	}

	physLocation := types.PhysLocation{}
	name := ""
	if conf, ok := data.config[r.id]; ok {
		physLocation = conf.PhysLocation
		name = conf.Name
	}

	d := device.New(r.id, name, r.location, physLocation, data.defaultVolume)
	log.Infof("adding new client: %v", *d)

	data.devices[r.id] = d
	d.Start()

	s := &request.Stop{}
	d.Enqueue(context.Background(), s, 0, device.FromNow)

	v := &request.SetVolume{Volume: data.defaultVolume}
	d.Enqueue(context.Background(), v, 0, device.FromNow)

	k := &request.KeepVoltageUpdated{}
	d.Enqueue(context.Background(), k, 0, device.FromNow)

	lease.AddClient(r.id, physLocation)
}
