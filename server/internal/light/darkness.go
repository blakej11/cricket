package light

import (
	"context"

	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/types"
)

// darkness makes no light.
type darkness struct{}
type darknessParams struct{}

func init() {
	effect.RegisterLight[darkness, darknessParams]("darkness")
}

func (d darkness) Run(ctx context.Context, _ types.IDSetConsumer, _ any, _ any) {
	select {
	case <-ctx.Done():
		return
	}
}
