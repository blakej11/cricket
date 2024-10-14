package sound

import (
	"context"
	"sort"
	"time"

	"github.com/blakej11/cricket/internal/client"
	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/lease"
)

func init() {
	effect.RegisterAlgorithm(lease.Sound, "silence", &silence{})
	effect.RegisterAlgorithm(lease.Sound, "nonrandom", &nonrandom{})
	effect.RegisterAlgorithm(lease.Sound, "loop", &loop{})
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
			Reps: 8,
			Delay: 0,
			Jitter: 0,
		}
		client.Action(params.Clients, ctx, cmd, time.Now())
		cmd.SleepForDuration()
		time.Sleep(groupDelay.Duration())
	}
}

// ---------------------------------------------------------------------

// loop plays one of a set of sounds.
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

	for ctx.Err() == nil {
		cmd := &client.Play{
			File:   fileSet.Pick(),
			Volume: 0, // use default
			Reps:   fileReps.Int(),
			Delay:	fileDelay.MeanDuration(),
			Jitter:	fileDelay.VarianceDuration(),
		}
		client.Action(params.Clients, ctx, cmd, time.Now())
		cmd.SleepForDuration()
		time.Sleep(groupDelay.Duration())
	}
}

