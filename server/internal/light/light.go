package light

import (
	"context"
	"time"

	"github.com/blakej11/cricket/internal/client"
	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/lease"
	_ "github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/types"
)

func init() {
	effect.RegisterAlgorithm(lease.Light, "darkness", &darkness{})
	effect.RegisterAlgorithm(lease.Light, "blink", &blink{})
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

func (b *blink) GetRequirements() effect.AlgRequirements {
	return effect.AlgRequirements{
		Parameters:	[]string{"blinkSpeed", "blinkDelay"},
	}
}

func (b *blink) Run(ctx context.Context, params effect.AlgParams) {
	blinkSpeed := params.Parameters["blinkSpeed"]
	blinkDelay := params.Parameters["blinkDelay"]

	for _, c := range params.Clients {
		go func() {
			// The blink delay might be a changing variable,
			// and the changes aren't thread safe.
			delay := *blinkDelay
			delay.Reset()
			clients := []types.ID{c}

			for ctx.Err() == nil {
				dur := delay.Duration()
				time.Sleep(dur)
				cmd := &client.Blink{
					Speed:	blinkSpeed.Float64(),
					Delay:	0,
					Jitter:	0,
					Reps:	1,
				}
				client.EnqueueAfterDelay(clients, ctx, cmd, 0)
				time.Sleep(cmd.Duration())
			}
		}()
	}
	<-ctx.Done()
}

// ---------------------------------------------------------------------

// unison causes all crickets to flash in unison.
type unison struct {}

func (u *unison) GetRequirements() effect.AlgRequirements {
	return effect.AlgRequirements{
		Parameters:	[]string{"blinkSpeed", "blinkDelay", "blinkReps", "groupDelay", "groupReps"},
	}
}

func (u *unison) Run(ctx context.Context, params effect.AlgParams) {
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
		client.EnqueueAfterDelay(params.Clients, ctx, cmd, 0)
		time.Sleep(cmd.Duration())
		time.Sleep(groupDelay.Duration())
		groupReps--
	}
}

