package sound

import (
	"context"

	"github.com/blakej11/cricket/internal/effect"
	"github.com/blakej11/cricket/internal/log"
	"github.com/blakej11/cricket/internal/types"
)

// shuffle plays one of a set of sounds out of a set of clients, but
// with no file-level synchronization between clients.
type shuffle struct {}

func init() {
	effect.RegisterSound[shuffle, loopParams, loopFileSets]("shuffle")
}

func (s shuffle) Run(ctx context.Context, ids types.IDSetConsumer, params any, fileSets any) {
	ids.Launch(ctx, func(id types.ID) {
		l := &loop{}
		l.Run(ctx, types.NewFixedIDSet(id), params, fileSets)

		// Getting here means that the fileset doesn't have any more
		// files that can be played before the context expires. That
		// implies that would be a waste to add any more clients at
		// this point, so close the fileset.
		log.Infof("shuffle: client [ %s ] closing and draining early", string(id))
		ids.Close()
		effect.DrainQueue(types.Sound, []types.ID{id})
	})

	// ids.Launch guarantees that any ID added to the set will have the
	// Launch goroutine called on it exactly once. Since that goroutine
	// performs the queue drain operation, we remove all IDs from the set
	// here to ensure that effect.Run doesn't try to do it as well.
	ids.Remove(ids.Snapshot())

	// Don't actually return before the context expires, since that would
	// cause the context to be cancelled, thus waking up any loop threads
	// that were waiting for a playback to be finished.
	<-ctx.Done()
}

