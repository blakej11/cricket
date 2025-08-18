package sound

import (
	"context"
	"time"

	"github.com/blakej11/cricket/internal/client"
	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/fileset"
	"github.com/blakej11/cricket/internal/random"
	"github.com/blakej11/cricket/internal/request"
	"github.com/blakej11/cricket/internal/types"
)

// "storm" simulates a storm.
type storm struct {
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

type stormFileSets struct {
	Main *fileset.Set
}

func init() {
	effect.RegisterSound[storm, stormParams, stormFileSets]("storm")
}

func (s storm) Run(ctx context.Context, ids types.IDSetConsumer, params any, fileSets any) {
	p := params.(*stormParams)
	fs := fileSets.(*stormFileSets)

	s.initIntensity()

	qThresh := p.QueueRefillThresh.Duration()
	for ctx.Err() == nil {
		s.maybeUpdateIntensity(p)

		for _, c := range ids.Snapshot() {
			for client.HasSoundUntil(c).Sub(time.Now()) < qThresh {
				cmd := &request.Play{
					Volume: s.volume.Int(),
					Play: fileset.Play {
						File: fs.Main.Pick(),
						Reps: 1,
					},
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

func (s *storm) maybeUpdateIntensity(p *stormParams) {
	boundIntensity := func(val float64) float64 {
		if val < stormIntensityMin {
			return stormIntensityMin
		} else if val >= stormIntensityMax {
			return stormIntensityMax
		} else {
			return val
		}
	}

	if time.Now().Sub(s.intensityLastUpdated) < p.IntensityUpdateFreq.Duration() {
		return
	}
	s.intensityLastUpdated = time.Now()
	s.intensity.AdjustMean(p.IntensityDelta.Float64())

	i := boundIntensity(s.intensity.Float64())
	it := boundIntensity(p.IntensityThreshold.Float64())

	if it > stormIntensityMin && i < it {
		scale := (i - stormIntensityMin) / (it - stormIntensityMin)
		varMin := p.InterDropDelayVarMin.Float64()
		varMax := p.InterDropDelayVarMax.Float64()
		s.delay = random.New(random.Config{
			Mean:         p.InterDropDelayMax.Float64(),
			Variance:     varMax - (varMax - varMin) * scale,
			Distribution: random.Normal,
		})
		s.volume = random.New(random.Config{
			Mean:         p.VolumeMin.Float64(),
		})
	} else if it < stormIntensityMax {
		scale := (i - it) / (stormIntensityMax - it)
		meanMin := p.InterDropDelayMin.Float64()
		meanMax := p.InterDropDelayMax.Float64()
		s.delay = random.New(random.Config{
			Mean:         meanMax - (meanMax - meanMin) * scale,
			Variance:     p.InterDropDelayVarMin.Float64(),
			Distribution: random.Normal,
		})
		volMin := p.VolumeMin.Float64()
		volMax := p.VolumeMax.Float64()
		s.volume = random.New(random.Config{
			Mean:         volMin + (volMax - volMin) * scale,
		})
	}
}
