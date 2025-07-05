package sound

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/blakej11/cricket/internal/client"
	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/lease"
	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/types"
)

func init() {
	effect.RegisterAlgorithm(lease.Sound, "silence", &silence{})
	effect.RegisterAlgorithm(lease.Sound, "nonrandom", &nonrandom{})
	effect.RegisterAlgorithm(lease.Sound, "loop", &loop{})
	effect.RegisterAlgorithm(lease.Sound, "shuffle", &shuffle{})
}

// ---------------------------------------------------------------------

// silence plays no sound.
type silence struct {
}

func (s *silence) GetRequirements() effect.AlgRequirements {
	return effect.AlgRequirements{}
}

func (s *silence) Run(ctx context.Context, params effect.AlgParams) {
	select {
	case <-ctx.Done():
		return
	}
}

// ---------------------------------------------------------------------

// nonrandom plays one of a set of sounds.
type nonrandom struct {}

func (n *nonrandom) GetRequirements() effect.AlgRequirements {
	return effect.AlgRequirements{
		FileSets:	[]string{"main"},
		Parameters:	[]string{"groupDelay"},
	}
}

func (n *nonrandom) Run(ctx context.Context, params effect.AlgParams) {
	set := params.FileSets["main"].Set()
	groupDelay := params.Parameters["groupDelay"]

	sort.Slice(set, func (i, j int) bool {
		if set[i].Folder < set[j].Folder {
			return true
		}
		return set[i].File < set[j].File
	})

	for _, f := range set {
		cmd := &client.Play{
			File: f,
			Volume: 0, // default
			Reps: 1,
			Delay: 0,
			Jitter: 0,
		}
		client.Action(params.Clients, ctx, cmd, 0)
		time.Sleep(cmd.Duration())
		time.Sleep(groupDelay.Duration())
	}
}

// ---------------------------------------------------------------------

// loop plays one of a set of sounds out of all clients at ~the same time.
type loop struct {}

func (l *loop) GetRequirements() effect.AlgRequirements {
	return effect.AlgRequirements{
		FileSets:	[]string{"main"},
		Parameters:	[]string{"fileReps", "fileDelay", "groupDelay"},
	}
}

func (l *loop) Run(ctx context.Context, params effect.AlgParams) {
	fileSet := params.FileSets["main"]
	fileReps := params.Parameters["fileReps"]
	fileDelay := params.Parameters["fileDelay"]
	groupDelay := params.Parameters["groupDelay"]

	clients := params.Clients

	for ctx.Err() == nil {
		file := fileSet.Pick()
		reps := fileReps.Int()

		fileDur := file.Duration + fileDelay.MeanDuration().Seconds()
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
			reps = 1
		}

		cmd := &client.Play{
			File:   file,
			Volume: 0, // use default
			Reps:   reps,
			Delay:	fileDelay.MeanDuration(),
			Jitter:	fileDelay.VarianceDuration(),
		}
		client.Action(clients, ctx, cmd, 0)

		dur := time.Duration(cmd.Duration() + groupDelay.Duration())
		sleepTimer := time.NewTimer(dur)
		select {
			case <-sleepTimer.C:
		}
	}
}

// ---------------------------------------------------------------------

// shuffle plays one of a set of sounds out of a set of clients, but
// with no file-level synchronization between clients.
type shuffle struct {}

func (s *shuffle) GetRequirements() effect.AlgRequirements {
	l := &loop{}
	return l.GetRequirements()
}

func (s *shuffle) Run(ctx context.Context, params effect.AlgParams) {
	l := &loop{}
	for _, c := range params.Clients {
		go func() {
			p := params
			p.Clients = []types.ID{c}
			l.Run(ctx, p)
		}()
	}
	<-ctx.Done()
}

