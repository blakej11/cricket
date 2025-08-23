// Package request has a number of types that implement specific operations
// on the cricket device. Each of them implements the device.Request
// interface.
package request

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/blakej11/cricket/internal/device"
	"github.com/blakej11/cricket/internal/fileset"
	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/types"
)

// ------------------------------------------------------------------

const (
	// Time between attempts to DrainQueue in case of network failure.
	transientDelay = 5 * time.Second

	// Time between voltage updates.
	voltageUpdateDelay = 60 * time.Second

	// Names of timestamps.
	lastPing    = "last ping"
	lastVoltage = "last update of client voltage"

	// Names of statistics.
	voltage     = "voltage"
)

// ------------------------------------------------------------------

type Ping struct {}

func (r *Ping) Execute(ctx context.Context, d *device.Device) error {
	_, err := d.Execute(ctx, "ping", nil)
	if err != nil {
		return err
	}
	d.SetTimestamp(lastPing, time.Now())
	return nil
}

func (r *Ping) Duration() time.Duration {
	return 0
}

func (r *Ping) Type() device.RequestType {
	return device.Admin
}

// ------------------------------------------------------------------

type Play struct {
	Volume	int
	fileset.Play
}

func (r *Play) Execute(ctx context.Context, d *device.Device) error {
	reps := r.Play.Reps
	delay := r.Play.Delay.Milliseconds()
	jitter := r.Play.Jitter.Milliseconds()

	log.Debugf("%s: playing %s", d.Name(), r.Play)

	if reps == 0 {
		return nil
	}
	volume := r.Volume
	if volume == 0 {
		volume = d.GetTargetVolume()
	}

	_, err := d.Execute(ctx, "play", map[string]string {
		"folder": fmt.Sprintf("%d", r.Play.File.Folder),
		"file":   fmt.Sprintf("%d", r.Play.File.File),
		"volume": fmt.Sprintf("%d", volume),
		"reps":   fmt.Sprintf("%d", reps),
		"delay":  fmt.Sprintf("%d", delay),
		"jitter": fmt.Sprintf("%d", jitter),
	})

	return err
}

func (r *Play) Duration() time.Duration {
	return r.Play.Duration()
}

func (r *Play) Type() device.RequestType {
	return device.Sound
}

// ------------------------------------------------------------------

type SetVolume struct {
	Volume int
}

func (r *SetVolume) Execute(ctx context.Context, d *device.Device) error {
	_, err := d.Execute(ctx, "setvolume", map[string]string {
		"volume":  fmt.Sprintf("%d", r.Volume),
		"persist": "true",
	})

	// set this regardless of whether the set-volume action succeeded
	d.SetTargetVolume(r.Volume)

	return err
}

func (r *SetVolume) Duration() time.Duration {
	return 0
}

func (r *SetVolume) Type() device.RequestType {
	return device.Sound
}

// ------------------------------------------------------------------

type Blink struct {
	Speed  float64
	Delay  time.Duration
	Jitter time.Duration
	Reps   int
}

func (r *Blink) Execute(ctx context.Context, d *device.Device) error {
	_, err := d.Execute(ctx, "blink", map[string]string {
		"speed":  fmt.Sprintf("%.3f", r.Speed),
		"delay":  fmt.Sprintf("%d", r.Delay.Milliseconds()),
		"jitter": fmt.Sprintf("%d", r.Jitter.Milliseconds()),
		"reps":   fmt.Sprintf("%d", r.Reps),
	})

	return err
}

func (r *Blink) Duration() time.Duration {
	pause := ((256.0 / r.Speed) * 2.0) + float64(r.Delay.Milliseconds())
	pause *= float64(r.Reps)
	return time.Duration(pause * float64(time.Millisecond))
}

func (r *Blink) Type() device.RequestType {
	return device.Light
}

// ------------------------------------------------------------------

type Pause struct {}

func (r *Pause) Execute(ctx context.Context, d *device.Device) error {
	_, err := d.Execute(ctx, "pause", nil)
	return err
}

func (r *Pause) Duration() time.Duration {
	return 0
}

func (r *Pause) Type() device.RequestType {
	return device.Sound
}

// ------------------------------------------------------------------

type Unpause struct {}

func (r *Unpause) Execute(ctx context.Context, d *device.Device) error {
	_, err := d.Execute(ctx, "unpause", nil)
	return err
}

func (r *Unpause) Duration() time.Duration {
	return 0
}

func (r *Unpause) Type() device.RequestType {
	return device.Sound
}

// ------------------------------------------------------------------

type Stop struct {}

func (r *Stop) Execute(ctx context.Context, d *device.Device) error {
	_, err := d.Execute(ctx, "stop", nil)
	return err
}

func (r *Stop) Duration() time.Duration {
	return 0
}

func (r *Stop) Type() device.RequestType {
	return device.Sound
}

// ------------------------------------------------------------------

type KeepVoltageUpdated struct {}

func (r *KeepVoltageUpdated) Execute(ctx context.Context, d *device.Device) error {
	results, err := d.Execute(ctx, "battery", nil)
	if err != nil {
		d.Enqueue(ctx, r, voltageUpdateDelay, device.FromNow)
		return err
	}
	p, err := strconv.ParseFloat(strings.TrimSpace(results), 32)
	if err != nil {
		d.Enqueue(ctx, r, voltageUpdateDelay, device.FromNow)
		return err
	}

	d.SetStatistic(voltage, float32(p))
	d.SetTimestamp(lastVoltage, time.Now())
	log.Debugf("%s: voltage is %.2f", d.Name(), p)

	d.Enqueue(ctx, r, voltageUpdateDelay, device.FromNow)
	return nil
}

func (r *KeepVoltageUpdated) Duration() time.Duration {
	return 0
}

func (r *KeepVoltageUpdated) Type() device.RequestType {
	return device.Admin
}

// ------------------------------------------------------------------

type DrainQueue struct {
	Ack		chan types.ID
	LeaseType	types.LeaseType
}

func (r *DrainQueue) Execute(ctx context.Context, d *device.Device) error {
	endpoint := "unknown"
	switch r.LeaseType {
	case types.Sound:
		endpoint = "soundpending"
	case types.Light:
		endpoint = "lightpending"
	}

	results, err := d.Execute(ctx, endpoint, nil)
	if err != nil {
		d.Enqueue(ctx, r, transientDelay, device.FromNow)
		return err
	}
	p, err := strconv.ParseInt(strings.TrimSpace(results), 10, 32)
	if err != nil {
		d.Enqueue(ctx, r, transientDelay, device.FromNow)
		return err
	}
	if int(p) == 0 {
		r.Ack <- d.GetID()
		return nil
	}

	d.Enqueue(ctx, r, transientDelay, device.FromNow)
	return nil
}

func (r *DrainQueue) Duration() time.Duration {
	return 0
}

func (r *DrainQueue) Type() device.RequestType {
	return device.Admin
}

