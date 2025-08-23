package wander

import (
	"math"
	"time"

	"github.com/blakej11/cricket/internal/random"
)

type Config struct {
	Intensity    *random.Variable
	Acceleration *random.Variable
	Noise        *random.Variable
	AccelScale   time.Duration
	Deadline     time.Time
}

type Wander struct {
	c            Config
	oldTarget    float64
	newTarget    float64
	oldTime      time.Time
	newTime      time.Time
	slope        float64
	ramping      bool
}

func New(c Config) *Wander {
	now := time.Now()
	return &Wander{
		c:         c,
		oldTime:   now,
		newTime:   now,
		ramping:   true,
	}
}

// only gives values in [0.0, 1.0]
func (w *Wander) Value() float64 {
	return w.value(time.Now())
}

func (w *Wander) value(now time.Time) float64 {
	if now.After(w.c.Deadline) {
		return 0.0
	}
	if now.After(w.newTime) {
		w.oldTarget = w.newTarget
		w.oldTime = w.newTime

		// accel = 1.0 -> rate =  1 * w.AccelScale
		// accel = 0.5 -> rate =  4 * w.AccelScale
		// accel = 0.0 -> rate = 16 * w.AccelScale
		accel := max(0.0, min(w.c.Acceleration.Float64(), 1.0))
		rate := math.Pow(4.0, (1.0 - accel) * 2.0)
		w.newTime = w.oldTime.Add(scaleDuration(w.c.AccelScale, rate))

		// Alternate between ramping the intensity and holding it.
		if w.ramping {
			w.newTarget = max(0.0, min(w.c.Intensity.Float64(), 1.0))
		}
		w.ramping = !w.ramping

		// Always aim to finish at min intensity.
		if w.newTime.After(w.c.Deadline) {
			w.newTime = w.c.Deadline
			w.newTarget = 0.0
		}

		// w.slope holds the new rate of change of intensity.
		delta := w.newTarget - w.oldTarget
		duration := w.newTime.Sub(w.oldTime).Seconds()
		w.slope = delta / duration
	}

	deltaT := now.Sub(w.oldTime).Seconds()
	intensity := w.oldTarget + deltaT * w.slope + w.c.Noise.Float64All()
	return max(0.0, min(intensity, 1.0))
}

func scaleDuration(d time.Duration, scale float64) time.Duration {
	return time.Duration(float64(time.Second) * (scale * d.Seconds()))
}
