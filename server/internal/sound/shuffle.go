package sound

import (
	"context"

	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/types"
)

// shuffle plays one of a set of sounds out of a set of clients, but
// with no file-level synchronization between clients.
type shuffle struct {}

func init() {
	effect.RegisterSound("shuffle", &shuffle{}, &loopParams{}, &loopFileSets{})
}

func (s *shuffle) Run(ctx context.Context, ids types.IDSetConsumer, params any, fileSets any) {
	ids.Launch(ctx, func(id types.ID) {
		l := &loop{}
		l.Run(ctx, types.NewFixedIDSet(id), params, fileSets)
	})
}

