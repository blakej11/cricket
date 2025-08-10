package weightedset

import (
	"cmp"
	"slices"
)

type weightedMember[T any] struct {
	member T
	weight float64
}

type WeightedSet[T any] struct {
	members	[]weightedMember[T]
	sum     float64
	random	func() float64
}

func New[T any](random func() float64) WeightedSet[T] {
	return WeightedSet[T]{random: random}
}

func (ws *WeightedSet[T]) Add(item T, weight float64) {
	if weight == 0.0 {
		return
	}
	ws.members = append(ws.members, weightedMember[T]{
		member: item,
		weight: weight,
	})
	ws.sum += weight
}

func (ws *WeightedSet[T]) Len() int {
	return len(ws.members)
}

// Reorder the slice, so a specific element picked out by the
// weighted random selection will get first dibs at any allocation.
func (ws *WeightedSet[T]) Slice() []T {
	// Sort by decreasing weight.
	unchosen := ws.members
	slices.SortStableFunc(unchosen, func(a, b weightedMember[T]) int {
		return -cmp.Compare(a.weight, b.weight)
	})
	chosen := make([]T, 0, len(ws.members))

	// This is O(n^2), but n is never gonna be all that big.
	sum := ws.sum
	for len(unchosen) > 0 {
		running := 0.0
		pick := ws.random() * sum
		for idx, wm := range unchosen {
			running += wm.weight
			if running < pick {
				continue
			}
			sum -= wm.weight
			chosen = append(chosen, wm.member)
			unchosen = append(unchosen[:idx], unchosen[idx+1:]...)
			break
		}
	}

	return chosen
}
