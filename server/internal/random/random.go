package random

import (
	"math"
	"math/rand/v2"
	"strings"
	"time"
)

// ---------------------------------------------------------------------

type Distribution int
const (
	Unknown		Distribution = iota
	Normal
	Uniform
)

func (d *Distribution) unmarshalString(s string) {
	switch strings.ToLower(s) {
	default:
		*d = Unknown
	case "normal":
		*d = Normal
	case "uniform":
		*d = Uniform
	}
}

func (d Distribution) String() string {
	switch (d) {
	default:
		return "unknown"
	case Normal:
		return "normal"
	case Uniform:
		return "uniform"
	}
}

func (d *Distribution) UnmarshalText(b []byte) error {
	d.unmarshalString(string(b))
	return nil
}

// ---------------------------------------------------------------------

// Config describes how to choose the value of a parameter.
type Config struct {
	Mean		float64
	Variance	float64
	Distribution	Distribution
	Changes		[]Delta
	RepeatChanges	bool
}

type Delta struct {
	MeanDeltaRate	float64	// change in mean, per second
	VarDeltaRate	float64	// change in variance, per second
	Duration	float64	// duration of these changes, in seconds
}

// ---------------------------------------------------------------------

// Variable holds the runtime data for how to generate a random value.
type Variable struct {
	config		Config
	mean		float64
	variance	float64

	// these are only used if config.Changes is non-nil
	lastUpdateTime	time.Time
	curChangeIndex	int
	curDelta	Delta
}

func New(c Config) *Variable {
	var curDelta Delta
	if len(c.Changes) > 0 {
		curDelta = c.Changes[0]
	}
	return &Variable{
		config:		c,
		mean:		c.Mean,
		variance:	c.Variance,
		lastUpdateTime:	time.Time{},
		curChangeIndex:	0,
		curDelta:	curDelta,
	}
}

// Reset resets the random variable to its initial state.
func (v *Variable) Reset() {
	*v = *New(v.config)
}

// Float64 calculates a new concrete float64 value from the given Variable.
//
// - For Uniform distributions, the value will be in the range
//   [Mean - Uniform / 2, Mean + Uniform / 2), uniformly distributed.
//
// - For Normal distributions, the value will be given by a normal
//   distribution with mean = Mean and stdev = sqrt(Variance). A
//   negative variance is treated as zero.
//
// In all cases, the value returned will always be non-negative.
func (v *Variable) Float64() float64 {
	if v.lastUpdateTime.IsZero() {
		v.lastUpdateTime = time.Now()
	}
	if v.curChangeIndex < len(v.config.Changes) {
		idx := v.curChangeIndex
		t := time.Now()
		// How much time has elapsed since the last update?
		d := t.Sub(v.lastUpdateTime).Seconds()

		for {
			// Use the current Delta until it runs out.
			delta := &v.curDelta
			dt := max(min(d, delta.Duration), 0.0)
			delta.Duration -= dt
			d -= dt

			// Perform updates from this Delta.
			v.mean += dt * delta.MeanDeltaRate
			v.variance += dt * delta.VarDeltaRate

			if d == 0 {
				break
			}

			// Pick a new Delta.
			idx += 1
			if idx == len(v.config.Changes) {
				if !v.config.RepeatChanges {
					break
				}
				idx = 0
			}
			v.curDelta = v.config.Changes[idx]
		}
		v.curChangeIndex = idx
		v.lastUpdateTime = t
	}

	value := v.mean
	switch (v.config.Distribution) {
	default:
		break
	case Normal:
		value += rand.NormFloat64() * math.Sqrt(max(v.variance, 0.0))
	case Uniform:
		value += v.variance * rand.Float64() - v.variance / 2.0
	}
	return max(value, 0.0)
}

func (v *Variable) Int() int {
	return int(v.Float64())
}

func (v *Variable) Duration() time.Duration {
	return time.Duration(v.Float64() * float64(time.Second))
}

func (v *Variable) MeanDuration() time.Duration {
	return time.Duration(v.mean * float64(time.Second))
}

func (v *Variable) VarianceDuration() time.Duration {
	return time.Duration(v.variance * float64(time.Second))
}
