package sound

import (
	"context"

	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/types"
)

// silence plays no sound.
type silence struct {}
type silenceParams struct {}
type silenceFileSets struct {}

func init() {
	effect.RegisterSound[silence, silenceParams, silenceFileSets]("silence")
}

func (s silence) Run(ctx context.Context, _ types.IDSetConsumer, _ any, _ any) {
	select {
	case <-ctx.Done():
		return
	}
}

