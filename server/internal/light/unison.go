package light

import (
	"context"
	"time"

	"github.com/blakej11/cricket/internal/client"
	"github.com/blakej11/cricket/internal/effect"
	_ "github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/random"
	"github.com/blakej11/cricket/internal/request"
	"github.com/blakej11/cricket/internal/types"
)

// unison causes all crickets to flash in unison.
type unison struct {}

type unisonParams struct {
	BlinkSpeed *random.Variable
	BlinkDelay *random.Variable
	BlinkReps  *random.Variable
	GroupDelay *random.Variable
	GroupReps  *random.Variable
}

func init() {
	effect.RegisterLight[unison, unisonParams]("unison")
}

func (u unison) Run(ctx context.Context, ids types.IDSetConsumer, params any, _ any) {
	p := params.(*unisonParams)

	groupReps := p.GroupReps.Int()
	if groupReps == 0 {
		groupReps = 1
	}

	for ctx.Err() == nil && groupReps > 0 {
		cmd := &request.Blink{
			Speed:	p.BlinkSpeed.Float64(),
			Delay:	p.BlinkDelay.MeanDuration(),
			Jitter:	p.BlinkDelay.VarianceDuration(),
			Reps:	p.BlinkReps.Int(),
		}
		client.EnqueueAfterDelay(ids.Snapshot(), ctx, cmd, 0)
		time.Sleep(cmd.Duration())
		time.Sleep(p.GroupDelay.Duration())
		groupReps--
	}
}

