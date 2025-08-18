package types

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func TestIDSetSnapshot(t *testing.T) {
	x := ID("x")
	y := ID("y")
	z := ID("z")

	p := NewIDSetProducer()
	c := p.NewConsumer()
	want := []ID{}

	want = append(want, x)
	p.Add([]ID{x})
	got := c.Snapshot()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("added 1: wanted %q, got %q\n", want, got)
	}

	want = append(append(want, y), z)
	p.Add([]ID{y, z})
	got = c.Snapshot()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("added 2: wanted %q, got %q\n", want, got)
	}

	c.Close()
	got = c.Snapshot()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("close: wanted %q, got %q\n", want, got)
	}

	p.Add([]ID{ID("bogus")})
	got = c.Snapshot()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("after close: wanted %q, got %q\n", want, got)
	}

}

func TestIDSetLaunchWithClose(t *testing.T) {
	const numIDs = 100

	p := NewIDSetProducer()
	c := p.NewConsumer()

	chs := make([]chan ID, 10)
	for i := range chs {
		chs[i] = make(chan ID, numIDs)
	}

	for _, ch := range chs {
		go func() {
			c.Launch(context.Background(), func(id ID) {
				ch <- id
				if len(ch) == cap(ch) / 2 {
					c.Close()
				}
			})
		}()
	}

	var want int
	for want = 0; want < numIDs; want++ {
		if !p.Add([]ID{ID(fmt.Sprintf("%02d", want))}) {
			break
		}
	}

	for chID, ch := range chs {
		ids := []ID{}
		for i := 0; i < want; i++ {
			ids = append(ids, <-ch)
		}
		sort.Slice(ids, func (i, j int) bool {
			return ids[i] < ids[j]
		})
		for idx, got := range ids {
			want := ID(fmt.Sprintf("%02d", idx))
			if want != got {
				t.Errorf("#%d: wanted %q, got %q; bad ID set %v\n",
				    chID, want, got, ids)
			}
		}
	}
}

func TestIDSetLaunchWithCancel(t *testing.T) {
	const numIDs = 100

	p := NewIDSetProducer()
	c := p.NewConsumer()

	chs := make([]chan ID, 10)
	for i := range chs {
		chs[i] = make(chan ID, numIDs)
	}

	for _, ch := range chs {
		go func() {
			ctx, cancel := context.WithCancel(context.Background())

			c.Launch(ctx, func(id ID) {
				ch <- id
				if len(ch) == cap(ch) / 2 {
					cancel()
				}
			})
		}()
	}

	var want int
	for want = 0; want < numIDs; want++ {
		if !p.Add([]ID{ID(fmt.Sprintf("%02d", want))}) {
			break
		}
	}

	for chID, ch := range chs {
		ids := []ID{}
		for i := 0; i < want; i++ {
			ids = append(ids, <-ch)
		}
		sort.Slice(ids, func (i, j int) bool {
			return ids[i] < ids[j]
		})
		for idx, got := range ids {
			want := ID(fmt.Sprintf("%02d", idx))
			if want != got {
				t.Errorf("#%d: wanted %q, got %q; bad ID set %v\n",
				    chID, want, got, ids)
			}
		}
	}
}

func TestIDSetRemove(t *testing.T) {
	x := ID("x")
	y := ID("y")

	p := NewIDSetProducer()
	c := p.NewConsumer()

	want := []ID{x, y}
	p.Add(want)
	c.Close()
	c.Remove([]ID{x})
	c.Remove([]ID{y})
	got := c.Snapshot()

	if len(got) > 0 {
		t.Errorf("remove: wanted empty snapshot, got %q\n", got)
	}
}
