package types

import (
	"context"
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

func TestIDSetLaunch(t *testing.T) {
	x := ID("x")
	y := ID("y")
	z := ID("z")

	p := NewIDSetProducer()
	c := p.NewConsumer()

	const goroutines = 10
	done := make(chan any)
	want := []ID{}
	got := make([][]ID, goroutines)

	new := []ID{x}
	want = append(want, new...)
	p.Add(new)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			ctx, cancel := context.WithCancel(context.Background())

			ch := make(chan ID, 3)
			c.Launch(ctx, func(id ID) {
				ch <- id
				if len(ch) == cap(ch) {
					cancel()
				}
			})

			ids := []ID{}
			for len(ch) > 0 {
				ids = append(ids, <-ch)
			}
			sort.Slice(ids, func (i, j int) bool {
				return ids[i] < ids[j]
			})
			got[i] = ids
			done <- struct{}{}
		}(i)
	}

	new = []ID{y, z}
	want = append(want, new...)
	p.Add(new)

	for i := 0; i < goroutines; i++ {
		<-done
	}

	c.Close()

	for i := 0; i < goroutines; i++ {
		if !reflect.DeepEqual(want, got[i]) {
			t.Errorf("listen %d: wanted %q, got %q\n", i, want, got[i])
		}
	}
}
