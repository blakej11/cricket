package sound

import (
	"context"
	"time"

	"github.com/blakej11/cricket/internal/client"
	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/lease"
)

func init() {
	effect.RegisterAlgorithm(lease.Sound, "silence", &silence{})
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

// loop plays one of a set of sounds.
type loop struct {}

func (l *loop) GetRequirements() effect.AlgRequirements {
	return effect.AlgRequirements{
		FileSets:	[]string{"main"},
		Parameters:	[]string{"delay"},
	}
}

func (l *loop) Run(ctx context.Context, params effect.AlgParams) {
	fileSet := params.FileSets["main"]
	delay := params.Parameters["delay"]

	for ctx.Err() == nil {
		file := fileSet.Pick()
		client.Action(params.Clients, ctx, &client.Play{File: file}, time.Now())
		file.SleepForDuration()
		time.Sleep(delay.Duration())
	}
}

// Client delays:
// - 1000 + 30 msec for init
// - 30 msec for volume
// could call /setvolume (or /unpause or /stop) to ensure it's on
