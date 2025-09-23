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
	"github.com/blakej11/cricket/internal/wander"
)

// "storm" simulates a storm.

type stormParams struct {
	VolumeMin    *random.Variable
	VolumeMax    *random.Variable
	Intensity    *random.Variable
	Acceleration *random.Variable
	Noise        *random.Variable
}

type stormFileSets struct {
	Intensity1 *fileset.Set
	Intensity2 *fileset.Set
	Intensity3 *fileset.Set
	Intensity4 *fileset.Set
	Intensity5 *fileset.Set
}

func init() {
	effect.RegisterSound[storm, stormParams, stormFileSets]("storm")
}

func (s storm) Run(ctx context.Context, ids types.IDSetConsumer, params any, fileSets any) {
	const qThresh = time.Duration(1.5 * float64(time.Second))

	deadline, ok := ctx.Deadline()
	if !ok {
		// It could, but I don't feel like dealing with this error handling.
		log.Fatalf("storm cannot run forever")
	}

	s.init(deadline, params, fileSets)

	for ctx.Err() == nil {
		volume, fs, delay := s.getParams()

		for _, c := range ids.Snapshot() {
			for client.HasSoundUntil(c).Sub(time.Now()) < qThresh {
				cmd := &request.Play{
					Volume: volume,
					Play: fileset.Play {
						File: fs.Pick(),
						Reps: 1,
					},
				}
				client.EnqueueAfterSoundEnds([]types.ID{c}, ctx, cmd, delay)
			}
		}
		time.Sleep(time.Second) // XXX
	}
}

type storm struct {
	filesets    []*fileset.Set
	intensity   *wander.Wander
	random      *random.Variable

	volumeMin   float64
	volumeHalf  float64
	volumeMax   float64
}

func (s *storm) init(deadline time.Time, params any, fileSets any) {
	p := params.(*stormParams)
	fs := fileSets.(*stormFileSets)

	if fs.Intensity1 != nil {
		s.filesets = append(s.filesets, fs.Intensity1)
	}
	if fs.Intensity2 != nil {
		s.filesets = append(s.filesets, fs.Intensity2)
	}
	if fs.Intensity3 != nil {
		s.filesets = append(s.filesets, fs.Intensity3)
	}
	if fs.Intensity4 != nil {
		s.filesets = append(s.filesets, fs.Intensity4)
	}
	if fs.Intensity5 != nil {
		s.filesets = append(s.filesets, fs.Intensity5)
	}
	if s.filesets == nil {
		log.Fatalf("storm has no filesets")
	}

	total := 0.0
	for _, f := range s.filesets {
		total += f.AverageDuration().Seconds()
	}
        average := total / float64(len(s.filesets))
        accelScale := time.Duration(float64(time.Second) * average)
	s.intensity = wander.New(wander.Config{
		Intensity:    p.Intensity,
		Acceleration: p.Acceleration,
		Noise:        p.Noise,
		AccelScale:   accelScale,
		Deadline:     deadline,
	})

	s.random = random.New(random.Config{
		Mean:         0.5,
		Variance:     0.5,
		Distribution: random.Uniform,
	})

	s.volumeMin = p.VolumeMin.Float64()
	s.volumeMax = p.VolumeMax.Float64()
	s.volumeHalf = (s.volumeMin + s.volumeMax) / 2
}

func (s *storm) getParams() (volume int, fs *fileset.Set, delay time.Duration) {
	i := s.intensity.Value()

	if i < 0.2 {
		volume = int(scale(i, 0, 0.2, s.volumeMin, s.volumeHalf))
	} else {
		volume = int(scale(i, 0.2, 1, s.volumeHalf, s.volumeMax))
	}

	// Currently not doing anything with delay.
	delay = time.Duration(0)

	// Example with four filesets:
	//   i in [0.00, 0.25): region = 0, choose from fileset 0
	//   i in [0.25, 0.50): region = 1, choose from fileset 0 -> 1
	//   i in [0.50, 0.75): region = 2, choose from fileset 1 -> 2
	//   i in [0.75, 1.00]: region = 3, choose from fileset 2 -> 3
	numSets := float64(len(s.filesets))
	region := int(i * numSets)
	if region == 0 {
		fs = s.filesets[0]
		return
	}
	if region == int(numSets) {  // happens if i == 1.0
		fs = s.filesets[region - 1]
		return
	}

	// The likelihood of using the fileset for this region is proportional
	// to how far into this region we are.
	regionFraction := i * numSets - float64(region)
	if regionFraction < s.random.Float64() {
		region--
	}
	fs = s.filesets[region]
	return
}

func scale(val, minDomain, maxDomain, minRange, maxRange float64) float64 {
	slope := (maxRange - minRange) / (maxDomain - minDomain)
	return (val - minDomain) * slope + minRange
}
