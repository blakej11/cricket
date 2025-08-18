package sound

import (
	"context"
	"sort"
	"time"

	"github.com/blakej11/cricket/internal/client"
	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/fileset"
	"github.com/blakej11/cricket/internal/random"
	"github.com/blakej11/cricket/internal/request"
	"github.com/blakej11/cricket/internal/types"
)

// "nonrandom" loops through all songs in the fileset and plays them,
// from all clients, in order, at the same time.
type nonrandom struct {}

type nonrandomParams struct {
	GroupDelay *random.Variable
}

type nonrandomFileSets struct {
	Main *fileset.Set
}

func init() {
	effect.RegisterSound[nonrandom, nonrandomParams, nonrandomFileSets]("nonrandom")
}

func (n nonrandom) Run(ctx context.Context, ids types.IDSetConsumer, params any, fileSets any) {
	p := params.(*nonrandomParams)
	fs := fileSets.(*nonrandomFileSets)

	set := fs.Main.Set()
	sort.Slice(set, func (i, j int) bool {
		if set[i].Folder < set[j].Folder {
			return true
		}
		return set[i].File < set[j].File
	})

	for _, f := range set {
		cmd := &request.Play{
			Play: fileset.Play {
				File: f,
				Reps: 1,
			},
		}
		client.EnqueueAfterDelay(ids.Snapshot(), ctx, cmd, 0)

		sleepTimer := time.NewTimer(cmd.Duration() + p.GroupDelay.Duration())
		select {
			case <-sleepTimer.C:
			case <-ctx.Done():
		}
	}
}
