package types

import (
	"context"
	"strings"
	"sync"

	"github.com/blakej11/cricket/internal/log"
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
	String() string
	Close()
	Remove([]ID)
}

// For each ID in the idSet, call "go f(id)" on it.
// This is also done for any new IDs added to the set.
//
// "go f(id)" will be called exactly once for every ID
// that is successfully added by a call to is.Add().
//
// If the passed-in context expires, the idSet is closed.
//
// This returns after the idSet is closed.
func (is *idSet) Launch(ctx context.Context, f func(ID)) {
	ctxDone := ctx.Done()

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
		case id, ok := <-ch:
			if !ok {
				// The set has been closed.
				return
			}
			go f(id)
		case <-ctxDone:
			// Stop adding any new IDs to the set.
			// This must be done asynchronously,
			// in case this is racing with is.Add().
			go is.Close()

			// Only take the first select arm
			// from now on.
			ctxDone = nil
		}
	}
}

func (is *idSet) Snapshot() []ID {
	is.clientMu.Lock()
	defer is.clientMu.Unlock()
	return is.snapshotLocked()
}

func (is *idSet) String() string {
	var result []string
	for _, i := range is.Snapshot() {
		result = append(result, string(i))
	}
	return strings.Join(result, ", ")
}

func (is *idSet) snapshotLocked() []ID {
	ids := make([]ID, len(is.ids))
	copy(ids, is.ids)
	return ids
}

func (is *idSet) Close() {
	is.clientMu.Lock()
	defer is.clientMu.Unlock()

	if is.closed {
		return
	}
	is.closed = true
	for _, ch := range is.listeners {
		close(ch)
	}
}

func (is *idSet) Remove(ids []ID) {
	is.clientMu.Lock()
	defer is.clientMu.Unlock()

	if !is.closed {
		log.Panicf("idset.Remove: trying to remove IDs from non-closed set %v", is)
	}

	for _, id := range ids {
		found := false
		for idx, setID := range is.ids {
			if id == setID {
				is.ids = append(is.ids[:idx], is.ids[idx+1:]...)
				found = true
				break
			}
		}
		if !found {
			log.Panicf("idset.Remove: failed to remove ID %q from %v", id, is)
		}
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

func (fis *fixedIDSet) String() string {
	return string(fis.id)
}

func (fis *fixedIDSet) Close() {
}

func (fis *fixedIDSet) Remove(ids []ID) {
	for _, id := range ids {
		if fis.id == id {
			fis.id = ID("")
		} else {
			log.Panicf("idset.Remove: failed to remove ID %q from %v", id, fis)
		}
	}
}
