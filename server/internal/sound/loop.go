package sound

import (
	"context"
	"math"
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
	effect.RegisterSound("loop", &loop{}, &loopParams{}, &loopFileSets{})
}

func (l *loop) Run(ctx context.Context, ids types.IDSetConsumer, params any, fileSets any) {
	p := params.(*loopParams)
	fs := fileSets.(*loopFileSets)

	for ctx.Err() == nil {
		file := fs.Main.Pick()
		reps := p.FileReps.Int()
		if reps == 0 {
			reps = 1
		}

		fileDur := file.Duration + p.FileDelay.MeanDuration().Seconds()
		if deadline, ok := ctx.Deadline(); ok {
			remaining := max(deadline.Sub(time.Now()).Seconds(), 0.0)
			newReps := min(reps, int(math.Floor(remaining / fileDur)))
			if reps != newReps {
				log.Infof("cutting short %d/%d play: %d reps rather than %d",
				    file.Folder, file.File, newReps, reps)
				reps = newReps
			}
		}
		if reps == 0 {
			return
		}

		cmd := &request.Play{
			File:   file,
			Volume: 0, // use default
			Reps:   reps,
			Delay:	p.FileDelay.MeanDuration(),
			Jitter:	p.FileDelay.VarianceDuration(),
		}
		client.EnqueueAfterDelay(ids.Snapshot(), ctx, cmd, 0)

		dur := time.Duration(cmd.Duration() + p.GroupDelay.Duration())
		sleepTimer := time.NewTimer(dur)
		select {
			case <-sleepTimer.C:
			case <-ctx.Done():
		}
	}
}
