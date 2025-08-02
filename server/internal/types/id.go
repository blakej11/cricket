package types

import (
	"context"
	"sync"
)

// ID is the main way that clients are referred to.
type ID string

type idSet struct {
	clientMu    sync.Mutex

	// updated by producer side
	ids	    []ID
	clientCount int

	// updated by consumer side
	closed      bool
	listeners   []chan ID
}

type IDSetProducer interface {
	Size() int
	Closed() bool
	Add(ids []ID) bool
	NewConsumer() IDSetConsumer
}

func NewIDSetProducer() IDSetProducer {
	return &idSet{}
}

func (is *idSet) Size() int {
	return len(is.ids)
}

func (is *idSet) Closed() bool {
	return is.closed
}

// Returns true if the clients were added successfully.
// Returns false if this idSet has been marked "closed",
//   indicating that it should not receive new clients.
func (is *idSet) Add(ids []ID) bool {
	is.clientMu.Lock()
	defer is.clientMu.Unlock()

	if is.closed {
		return false
	}
	is.ids = append(is.ids, ids...)
	for _, ch := range is.listeners {
		for _, c := range ids {
			ch <- c
		}
	}
	return true
}

func (is *idSet) NewConsumer() IDSetConsumer {
	return is
}

type IDSetConsumer interface {
	Launch(ctx context.Context, f func(ID))
	Snapshot() []ID
	Close()
}

// For each ID in the idSet, call "go f(id)" on it.
// This is also done for any new IDs added to the set.
// This returns only once the passed-in context expires.
func (is *idSet) Launch(ctx context.Context, f func(ID)) {
	is.clientMu.Lock()
	ch := make(chan ID)
	is.listeners = append(is.listeners, ch)
	snap := is.snapshotLocked()
	is.clientMu.Unlock()

	for _, id := range snap {
		go f(id)
	}
	for {
		select {
			case id := <-ch:
				go f(id)
			case <-ctx.Done():
				return
		}
	}
}

func (is *idSet) Snapshot() []ID {
	is.clientMu.Lock()
	defer is.clientMu.Unlock()
	return is.snapshotLocked()
}

func (is *idSet) snapshotLocked() []ID {
	ids := make([]ID, len(is.ids))
	copy(ids, is.ids)
	return ids
}

func (is *idSet) Close() {
	is.clientMu.Lock()
	defer is.clientMu.Unlock()

	is.closed = true
	for _, ch := range is.listeners {
		close(ch)
	}
}

// - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -

// This allows an idSet to be split up into many individual sets.
// The "close" operation doesn't work on this one - it's assumed that
// the parent will perform the close operation and clean up all of these.
type fixedIDSet struct {
	id	    ID
}

func NewFixedIDSet(id ID) IDSetConsumer {
	return &fixedIDSet{id: id}
}

func (fis *fixedIDSet) Launch(ctx context.Context, f func(ID)) {
	go f(fis.id)
	<-ctx.Done()
}

func (fis *fixedIDSet) Snapshot() []ID {
	return []ID{fis.id}
}

func (fis *fixedIDSet) Close() {
}

