package timedheap

import (
	"testing"
	"time"
)

func T(i int) time.Time {
	return time.Unix(0, int64(i))
}

func unT(t time.Time) int {
	return int(t.UnixNano())
}

// ---------------------------------------------------------------------

// A Lamport clock that can be used to hijack the heap's "waitFor" function.
type clock struct {
	now    int
	target int

	nowCh    chan int
	targetCh chan int
	stopCh   chan any
	output   chan time.Time
}

func newClock() *clock {
	return &clock{
		nowCh:    make(chan int),
		targetCh: make(chan int),
		stopCh:   make(chan any),
		output:   make(chan time.Time),
	}
}

func (f *clock) start() {
	go f.thread()
}

func (f *clock) stop() {
	f.stopCh <- struct{}{}
}

func (f *clock) getWaitForFunc() func(t time.Time) <-chan time.Time {
	return func(t time.Time) <-chan time.Time {
		f.targetCh <- unT(t)
		return f.output
	}
}

func (f *clock) thread() {
	for {
		if f.now < f.target {
			select {
			case now := <-f.nowCh:
				f.now = now
			case target := <-f.targetCh:
				f.target = target
			case <-f.stopCh:
				return
			}
		} else {
			select {
			case now := <-f.nowCh:
				f.now = now
			case target := <-f.targetCh:
				f.target = target
			case <-f.stopCh:
				return
			case f.output <- T(f.target):
			}
		}
	}
}

func (f *clock) setNow(t int) {
	f.nowCh <- t
}

// ---------------------------------------------------------------------

func dequeue(t *testing.T, ch <-chan int, want int) {
	got := <-ch
	if got != want {
		t.Errorf("dequeue: want %d, got %d\n", want, got)
	}
}

func noDequeue(t *testing.T, ch <-chan int) {
	select {
	case got := <-ch:
		t.Errorf("dequeue: want empty queue, got %v\n", got)
	default:
	}
}

// ---------------------------------------------------------------------

func TestHeap(t *testing.T) {
	for _, test := range []struct {
		name	string
		fn	func(*clock, *TimedHeap[int], <-chan int)
	}{
		{
			name: "single",
			fn: func(c *clock, th *TimedHeap[int], ch <-chan int) {
				th.Add(1, T(1))
				noDequeue(t, ch)

				c.setNow(1)
				dequeue(t, ch, 1)
				noDequeue(t, ch)
			},
		},
		{
			name: "past",
			fn: func(c *clock, th *TimedHeap[int], ch <-chan int) {
				th.Add(1, T(1))
				noDequeue(t, ch)

				c.setNow(2)
				dequeue(t, ch, 1)
				noDequeue(t, ch)
			},
		},
		{
			name: "in order",
			fn: func(c *clock, th *TimedHeap[int], ch <-chan int) {
				th.Add(1, T(1))
				th.Add(2, T(2))
				noDequeue(t, ch)

				c.setNow(1)
				dequeue(t, ch, 1)
				noDequeue(t, ch)

				c.setNow(2)
				dequeue(t, ch, 2)
				noDequeue(t, ch)
			},
		},
		{
			name: "dups",
			fn: func(c *clock, th *TimedHeap[int], ch <-chan int) {
				th.Add(1, T(1))
				th.Add(1, T(1))
				noDequeue(t, ch)

				c.setNow(1)
				dequeue(t, ch, 1)
				dequeue(t, ch, 1)
				noDequeue(t, ch)
			},
		},
		{
			name: "out of order",
			fn: func(c *clock, th *TimedHeap[int], ch <-chan int) {
				th.Add(2, T(2))
				th.Add(1, T(1))
				noDequeue(t, ch)

				c.setNow(1)
				dequeue(t, ch, 1)
				noDequeue(t, ch)

				c.setNow(2)
				dequeue(t, ch, 2)
				noDequeue(t, ch)
			},
		},
		{
			name: "batches",
			fn: func(c *clock, th *TimedHeap[int], ch <-chan int) {
				th.Add(3, T(3))
				th.Add(1, T(1))
				th.Add(4, T(4))
				th.Add(1, T(1))
				th.Add(5, T(5))
				th.Add(9, T(9))
				noDequeue(t, ch)

				c.setNow(1)
				dequeue(t, ch, 1)
				dequeue(t, ch, 1)
				noDequeue(t, ch)

				c.setNow(5)
				dequeue(t, ch, 3)
				dequeue(t, ch, 4)
				dequeue(t, ch, 5)
				noDequeue(t, ch)

				c.setNow(9)
				dequeue(t, ch, 9)
				noDequeue(t, ch)
			},
		},
		{
			name: "listener blocked",
			fn: func(c *clock, th *TimedHeap[int], ch <-chan int) {
				th.Add(1, T(1))
				noDequeue(t, ch)

				c.setNow(1)
				th.Add(3, T(3))
				th.Add(2, T(2))
				dequeue(t, ch, 1)
				noDequeue(t, ch)

				c.setNow(5)
				th.Add(5, T(5))
				th.Add(4, T(4))
				dequeue(t, ch, 2)
				dequeue(t, ch, 3)
				dequeue(t, ch, 4)
				dequeue(t, ch, 5)
				noDequeue(t, ch)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := newClock()
			c.start()
			defer c.stop()

			ch := make(chan int)
			th := New[int]()
			th.waitFor = c.getWaitForFunc()
			th.Start(ch)
			defer th.Stop()

			test.fn(c, th, ch)
		})
	}
}

// ---------------------------------------------------------------------

