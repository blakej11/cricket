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
}

// ---------------------------------------------------------------------

// darkness makes no light.
type darkness struct {
}

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

// blink makes a set of blinks.
type blink struct {
}

func (b *blink) GetRequirements() effect.AlgRequirements {
	return effect.AlgRequirements{
		Parameters:	[]string{"delay", "speed", "reps"},
	}
}

func (b *blink) Run(ctx context.Context, params effect.AlgParams) {
	delay := params.Parameters["delay"]
	speed := params.Parameters["speed"]
	reps := params.Parameters["reps"]

	for ctx.Err() == nil {
		cmd := &client.Blink{
			Speed:	float32(speed.Float64()),
			Delay:	delay.MeanDuration(),
			Jitter:	delay.VarianceDuration(),
			Reps:	reps.Int(),
		}

		t := time.Now()
		client.Action(params.Clients, ctx, cmd, t)
		file.SleepForDuration() // XXX how long do we wait??
		time.Sleep(delay.Duration())
	}
}

