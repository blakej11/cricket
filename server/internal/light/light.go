package light

import (
	"context"
	"time"

	"github.com/blakej11/cricket/internal/client"
	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/lease"
)

func init() {
	effect.RegisterAlgorithm(lease.Light, "darkness", &darkness{})
	effect.RegisterAlgorithm(lease.Light, "unison", &unison{})
}

// ---------------------------------------------------------------------

// darkness makes no light.
type darkness struct {}

func (d *darkness) GetRequirements() effect.AlgRequirements {
	return effect.AlgRequirements{}
}

func (d *darkness) Run(ctx context.Context, params effect.AlgParams) {
	select {
	case <-ctx.Done():
		return
	}
}

// ---------------------------------------------------------------------

// blink causes crickets to blink out of sync with each other.
type blink struct {}

// for each cricket in the set:
// - choose a random amount of time, wait that amount, then blink once
// - slowly decrease the max delay, so they're lighting up more often

// ---------------------------------------------------------------------

// unison causes all crickets to flash in unison.
type unison struct {}

func (b *unison) GetRequirements() effect.AlgRequirements {
	return effect.AlgRequirements{
		Parameters:	[]string{"blinkSpeed", "blinkDelay", "blinkReps", "groupDelay", "groupReps"},
	}
}

func (b *unison) Run(ctx context.Context, params effect.AlgParams) {
	blinkSpeed := params.Parameters["blinkSpeed"]
	blinkDelay := params.Parameters["blinkDelay"]
	blinkReps := params.Parameters["blinkReps"]
	groupDelay := params.Parameters["groupDelay"]
	groupReps := params.Parameters["groupReps"].Int()
	if groupReps == 0 {
		groupReps = 1
	}

	for ctx.Err() == nil && groupReps > 0 {
		cmd := &client.Blink{
			Speed:	blinkSpeed.Float64(),
			Delay:	blinkDelay.MeanDuration(),
			Jitter:	blinkDelay.VarianceDuration(),
			Reps:	blinkReps.Int(),
		}
		client.Action(params.Clients, ctx, cmd, time.Now())
		cmd.SleepForDuration()
		time.Sleep(groupDelay.Duration())
		groupReps--
	}
}

