// Package timedheap maintains a heap of messages that are to be delivered
// at or after a specified time.
package timedheap

import (
	"container/heap"
	"time"
)

type TimedHeap[T any] struct {
	impl	*heapImpl[T]
	in	chan timedMessage[T]
	stopCh	chan any

	// A hook that can be overridden for testing.
	waitFor	func (time.Time) <-chan time.Time
}

type timedMessage[T any] struct {
	msg		T
	earliest	time.Time
}

func New[T any]() *TimedHeap[T] {
	return &TimedHeap[T]{
		impl:	&heapImpl[T]{},
		in:	make(chan timedMessage[T]),
		stopCh:	make(chan any),
		waitFor: func(t time.Time) <-chan time.Time {
			return time.After(time.Until(t))
		},
	}
}

func (h *TimedHeap[T]) Start(out chan<- T) {
	go h.thread(h.in, out)
}

func (h *TimedHeap[T]) Stop() {
	h.stopCh <- struct{}{}
}

func (h *TimedHeap[T]) Add(msg T, earliest time.Time) {
	h.in <- timedMessage[T]{
		msg:      msg,
		earliest: earliest,
	}
}

// This thread maintains a heap of messages that are received on "in".
// It sends them to "out" when both of the following are true:
// - "out" is ready to receive a message.
// - there's a message that's ready to send.
func (h *TimedHeap[T]) thread(in <-chan timedMessage[T], out chan<- T) {
	for {
		select {
		case msg := <-in:
			h.push(msg)
			continue
		case <-h.ready():
			// there's at least one message ready to dequeue
		case <-h.stopCh:
			return
		}

		poppedMsg := h.pop()

		select {
		case msg := <-in:
			// "out" was blocked while we were trying to send
			// the popped message to it, and another message
			// arrived on "in" in the meantime. Put both
			// messages back on the heap and try again.  This
			// ensures that a stuck client won't block any
			// effect that's trying to communicate with it.
			h.push(msg)
			h.push(poppedMsg)
		case out <- poppedMsg.msg:
			// Successfully sent the popped message.
		case <-h.stopCh:
			return
		}
	}
}

func (h *TimedHeap[T]) push(msg timedMessage[T]) {
	heap.Push(h.impl, msg)
}

func (h *TimedHeap[T]) pop() timedMessage[T] {
	return heap.Pop(h.impl).(timedMessage[T])
}

// Returns a channel that will have a message sent when the
// heap's earliest message is ready to go.
func (h *TimedHeap[T]) ready() <-chan time.Time {
	if t, ok := h.impl.earliest(); ok {
		return h.waitFor(t)
	}
	return make(chan time.Time)
}

type heapImpl[T any] []timedMessage[T]

// https://pkg.go.dev/container/heap
func (h heapImpl[T]) Len() int {
	return len(h)
}

func (h heapImpl[T]) Less(i, j int) bool {
	return h[i].earliest.Before(h[j].earliest)
}

func (h heapImpl[T]) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *heapImpl[T]) Push(x any) {
	*h = append(*h, x.(timedMessage[T]))
}

func (h *heapImpl[T]) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// Returns the time when the heap's earliest message is ready to go.
func (h *heapImpl[T]) earliest() (time.Time, bool) {
	if len(*h) > 0 {
		return (*h)[0].earliest, true
	}
	return time.Time{}, false
}
