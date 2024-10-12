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
	delay		*random.Variable
	effects		[]*weightedEffect
}

func New(ty lease.Type, config Config, effects map[string]*effect.Effect) (*Player, error) {
	player := &Player{
		ty:		ty,
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

func (p *Player) start() {
	for {
		var eff *weightedEffect

		sum := 0.0
		for _, e := range p.effects {
			sum += e.weight
		}
		target := rand.Float64() * sum
		for _, e := range p.effects {
			target -= e.weight
			if target <= 0.0 {
				eff = e
				break
			}
		}

		if eff != nil {
			err := eff.effect.Run()
			log.Infof("running %v effect %q returned %v", p.ty, eff.name, err)
			if err == nil {
				eff.weight = eff.baseWeight
			} else {
				eff.weight++
			}
		}

		time.Sleep(p.delay.Duration())
	}
}

// - have some bags of Effects (non-partial, partial, "use 'the rest'"), fully
//   specified
// - allow algs to say "only do one of me at a time" (e.g. owls)
