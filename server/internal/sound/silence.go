package sound

import (
	"context"

	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/types"
)

// silence plays no sound.
type silence struct {}

func init() {
	effect.RegisterSound("silence", &silence{}, nil, nil)
}

func (s *silence) Run(ctx context.Context, _ types.IDSetConsumer, _ any, _ any) {
	select {
	case <-ctx.Done():
		return
	}
}

