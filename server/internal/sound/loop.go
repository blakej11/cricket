package sound

import (
	"context"
	"time"

	"github.com/blakej11/cricket/internal/client"
	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/fileset"
	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/random"
	"github.com/blakej11/cricket/internal/request"
	"github.com/blakej11/cricket/internal/types"
)

// "loop" picks random songs from the fileset and plays them,
// from all clients, at the same time.
type loop struct {}

type loopParams struct {
	FileReps   *random.Variable
	FileDelay  *random.Variable
	GroupDelay *random.Variable
}

type loopFileSets struct {
	Main *fileset.Set
}

func init() {
	effect.RegisterSound[loop, loopParams, loopFileSets]("loop")
}

func (l loop) Run(ctx context.Context, ids types.IDSetConsumer, params any, fileSets any) {
	deadline, _ := ctx.Deadline()
	p := params.(*loopParams)
	fs := fileSets.(*loopFileSets)

	for ctx.Err() == nil {
		delay := p.FileDelay.MeanDuration()
		jitter := p.FileDelay.VarianceDuration()
		play := fs.Main.PickCarefully(deadline, p.FileReps.Int(), delay, jitter)
		if play.Reps == 0 {
			log.Infof("out of time to play anything on clients [ %s ]", ids.String())
			log.Infof("  remaining = %v, fs %s", deadline.Sub(time.Now()), fs.Main)
			return
		}

		cmd := &request.Play{Play: play}
		client.EnqueueAfterDelay(ids.Snapshot(), ctx, cmd, 0)

		sleepTimer := time.NewTimer(cmd.Duration() + p.GroupDelay.Duration())
		select {
			case <-sleepTimer.C:
			case <-ctx.Done():
		}
	}
}
