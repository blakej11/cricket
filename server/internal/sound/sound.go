package sound

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/blakej11/cricket/internal/client"
	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/fileset"
	"github.com/blakej11/cricket/internal/lease"
	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/random"
	"github.com/blakej11/cricket/internal/types"
)

func init() {
	effect.RegisterAlgorithm(lease.Sound, "silence", &silence{})
	effect.RegisterAlgorithm(lease.Sound, "nonrandom", &nonrandom{})
	effect.RegisterAlgorithm(lease.Sound, "loop", &loop{})
	effect.RegisterAlgorithm(lease.Sound, "shuffle", &shuffle{})
	effect.RegisterAlgorithm(lease.Sound, "storm", &storm{})
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

// "nonrandom" loops through all songs in the fileset and plays them,
// from all clients, in order, at the same time.
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
		client.EnqueueAfterDelay(params.Clients, ctx, cmd, 0)
		time.Sleep(cmd.Duration())
		time.Sleep(groupDelay.Duration())
	}
}

// ---------------------------------------------------------------------

// "loop" picks random songs from the fileset and plays them,
// from all clients, at the same time.
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
		client.EnqueueAfterDelay(clients, ctx, cmd, 0)

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

// ---------------------------------------------------------------------

// "storm" simulates a storm.
type storm struct {
	p                    stormParams
	f                    stormFilesets
	intensity            *random.Variable
	intensityLastUpdated time.Time
	delay                *random.Variable
	volume               *random.Variable
}

type stormParams struct {
	VolumeMin            *random.Variable
	VolumeMax            *random.Variable
	IntensityDelta       *random.Variable
	IntensityUpdateFreq  *random.Variable
	IntensityThreshold   *random.Variable
	InterDropDelayMin    *random.Variable
	InterDropDelayMax    *random.Variable
	InterDropDelayVarMin *random.Variable
	InterDropDelayVarMax *random.Variable

	QueueRefillThresh    *random.Variable
	QueueRefillFreq      *random.Variable
}

type stormFilesets struct {
	Main *fileset.Set
}

func (s *storm) GetRequirements() effect.AlgRequirements {
	return effect.AlgRequirements{
		FileSets:	[]string{"main"},
		Parameters:	[]string{
			"volumeMin",
			"volumeMax",
			"intensityDelta",
			"intensityUpdateFreq",
			"intensityThreshold",
			"interDropDelayMin",
			"interDropDelayMax",
			"interDropDelayVarMin",
			"interDropDelayVarMax",
			"queueRefillThresh",
			"queueRefillFreq",
		},
	}
}

func (s *storm) Run(ctx context.Context, params effect.AlgParams) {
	p := stormParams{
		VolumeMin:            params.Parameters["volumeMin"],
		VolumeMax:            params.Parameters["volumeMax"],
		IntensityDelta:       params.Parameters["intensityDelta"],
		IntensityUpdateFreq:  params.Parameters["intensityUpdateFreq"],
		IntensityThreshold:   params.Parameters["intensityThreshold"],
		InterDropDelayMin:    params.Parameters["interDropDelayMin"],
		InterDropDelayMax:    params.Parameters["interDropDelayMax"],
		InterDropDelayVarMin: params.Parameters["interDropDelayVarMin"],
		InterDropDelayVarMax: params.Parameters["interDropDelayVarMax"],
		QueueRefillThresh:    params.Parameters["queueRefillThresh"],
		QueueRefillFreq:      params.Parameters["queueRefillFreq"],
	}
	f := stormFilesets{
		Main: params.FileSets["main"],
	}

	s.realRun(ctx, p, f, params.Clients)
}

func (s *storm) realRun(ctx context.Context, p stormParams, f stormFilesets, clients []types.ID) {
	s.p = p
	s.f = f
	s.initIntensity()

	qThresh := s.p.QueueRefillThresh.Duration()
	for ctx.Err() == nil {
		s.maybeUpdateIntensity()

		for _, c := range clients {
			for client.HasSoundUntil(c).Sub(time.Now()) < qThresh {
				cmd := &client.Play{
					File: s.f.Main.Pick(),
					Volume: s.volume.Int(),
					Reps: 1,
					Delay: 0,
					Jitter: 0,
				}
				delay := s.delay.Duration()
				client.EnqueueAfterSoundEnds([]types.ID{c}, ctx, cmd, delay)
			}
		}
		time.Sleep(time.Second) // XXX
	}
}

const (
	stormIntensityMin = 0
	stormIntensityMax = 100
)

func (s *storm) initIntensity() {
	s.intensity = random.New(random.Config{
		Mean:         stormIntensityMin,
		Variance:     1,
		Distribution: random.Normal,
	})
}

func (s *storm) maybeUpdateIntensity() {
	boundIntensity := func(val float64) float64 {
		if val < stormIntensityMin {
			return stormIntensityMin
		} else if val >= stormIntensityMax {
			return stormIntensityMax
		} else {
			return val
		}
	}

	if time.Now().Sub(s.intensityLastUpdated) < s.p.IntensityUpdateFreq.Duration() {
		return
	}
	s.intensityLastUpdated = time.Now()
	s.intensity.AdjustMean(s.p.IntensityDelta.Float64())

	i := boundIntensity(s.intensity.Float64())
	it := boundIntensity(s.p.IntensityThreshold.Float64())

	if it > stormIntensityMin && i < it {
		scale := (i - stormIntensityMin) / (it - stormIntensityMin)
		varMin := s.p.InterDropDelayVarMin.Float64()
		varMax := s.p.InterDropDelayVarMax.Float64()
		s.delay = random.New(random.Config{
			Mean:         s.p.InterDropDelayMax.Float64(),
			Variance:     varMax - (varMax - varMin) * scale,
			Distribution: random.Normal,
		})
		s.volume = random.New(random.Config{
			Mean:         s.p.VolumeMin.Float64(),
		})
	} else if it < stormIntensityMax {
		scale := (i - it) / (stormIntensityMax - it)
		meanMin := s.p.InterDropDelayMin.Float64()
		meanMax := s.p.InterDropDelayMax.Float64()
		s.delay = random.New(random.Config{
			Mean:         meanMax - (meanMax - meanMin) * scale,
			Variance:     s.p.InterDropDelayVarMin.Float64(),
			Distribution: random.Normal,
		})
		volMin := s.p.VolumeMin.Float64()
		volMax := s.p.VolumeMax.Float64()
		s.volume = random.New(random.Config{
			Mean:         volMin + (volMax - volMin) * scale,
		})
	}
}
