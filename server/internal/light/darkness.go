package light

import (
	"context"

	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/types"
)

// darkness makes no light.
type darkness struct{}

func init() {
	effect.RegisterLight("darkness", &darkness{}, nil)
}

func (d *darkness) Run(ctx context.Context, ids types.IDSetConsumer, params any, _ any) {
	select {
	case <-ctx.Done():
		return
	}
}
