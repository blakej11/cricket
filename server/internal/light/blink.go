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

// blink causes crickets to blink out of sync with each other.
type blink struct {}

type blinkParams struct {
	BlinkSpeed *random.Variable
	BlinkDelay *random.Variable
}

func init() {
	effect.RegisterLight[blink, blinkParams]("blink")
}

func (b blink) Run(ctx context.Context, ids types.IDSetConsumer, params any, _ any) {
	p := params.(*blinkParams)

	ids.Launch(ctx, func(id types.ID) {
		// The blink delay might be a changing variable,
		// and the changes aren't thread safe.
		delay := *p.BlinkDelay
		delay.Reset()
		clients := []types.ID{id}

		for ctx.Err() == nil {
			dur := delay.Duration()
			time.Sleep(dur)
			cmd := &request.Blink{
				Speed:	p.BlinkSpeed.Float64(),
				Delay:	0,
				Jitter:	0,
				Reps:	1,
			}
			client.EnqueueAfterDelay(clients, ctx, cmd, 0)
			time.Sleep(cmd.Duration())
		}
	})
}
