package player

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/lease"
	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/random"
)

type Config struct {
	StartupDelay	random.Config
	Delay		random.Config
	Weights		map[string]float64
}

// ---------------------------------------------------------------------

type weightedEffect struct {
	name		string
	baseWeight	float64
	weight		float64
	effect		*effect.Effect
}

type Player struct {
	ty		lease.Type
	startupDelay	*random.Variable
	delay		*random.Variable
	effects		[]*weightedEffect
}

func New(ty lease.Type, config Config, effects map[string]*effect.Effect) (*Player, error) {
	player := &Player{
		ty:		ty,
		startupDelay:	random.New(config.StartupDelay),
		delay:		random.New(config.Delay),
		effects:	[]*weightedEffect{},
	}

	for name, weight := range config.Weights {
		if _, ok := effects[name]; !ok {
			return nil, fmt.Errorf("player couldn't find effect named %q", name)
		}
		player.effects = append(player.effects, &weightedEffect{
			name:		name,
			baseWeight:	weight,
			weight:		weight,
			effect:		effects[name],
		})
	}

	return player, nil
}

func (p *Player) Start() {
	go p.start()
}

func (p *Player) pickEffect() *weightedEffect {
	sum := 0.0
	for _, e := range p.effects {
		sum += e.weight
	}
	target := rand.Float64() * sum
	for _, e := range p.effects {
		target -= e.weight
		if target <= 0.0 {
			return e
		}
	}
	return nil
}

func (p *Player) start() {
	startupDelay := p.startupDelay.Float64()
	if startupDelay > 0 {
		log.Infof("%v player sleeping for %.2f seconds before starting", p.ty, startupDelay)
		time.Sleep(time.Duration(startupDelay * float64(time.Second)))
	}

	for {
		eff := p.pickEffect()

		if eff != nil {
			err := eff.effect.Run()
			log.Infof("running %v effect %q returned %v", p.ty, eff.name, err)
			if err == nil {
				eff.weight = eff.baseWeight
			} else {
				eff.weight++
			}
		}

		// don't just spin-loop if no delay is configured
		dur := max(p.delay.Duration(), time.Second)
		time.Sleep(dur)
	}
}

// - have some bags of Effects (non-partial, partial, "use 'the rest'"), fully
//   specified
// - allow algs to say "only do one of me at a time" (e.g. owls)
