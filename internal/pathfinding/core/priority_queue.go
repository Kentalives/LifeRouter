package core

import (
	"github.com/kamstrup/intmap"
)

type comparator[T any] struct {
	Less   func(x, y T) bool
	More   func(x, y T) bool
	Equals func(x, y T) bool
}

// PrioQueue is a binary heap backed by parallel element/key slices and an index
// map. D* Lite relies on Find, Update, and Remove being cheap for known vertices.
type PrioQueue[T intmap.IntKey, U any] struct {
	heapElem []T
	heapKeys []U
	pos      *intmap.Map[T, int]
	cmp      *comparator[U]
}

// NewPrioQueue creates an empty priority queue with the given initial capacity.
func NewPrioQueue[T intmap.IntKey, U any](size int, cmp *comparator[U]) *PrioQueue[T, U] {
	return &PrioQueue[T, U]{
		heapElem: make([]T, 0, size),
		heapKeys: make([]U, 0, size),
		pos:      intmap.New[T, int](size),
		cmp:      cmp,
	}
}

// Clear removes all elements while retaining allocated storage.
func (q *PrioQueue[T, U]) Clear() {
	q.heapElem = q.heapElem[:0]
	q.heapKeys = q.heapKeys[:0]
	q.pos.Clear()
}

// Insert adds elem with key and restores heap ordering.
func (q *PrioQueue[T, U]) Insert(elem T, key U) {

	//q.heap = append(q.heap, heapEntry[T, U]{elem, key})
	//idx := len(q.heap) - 1
	//q.pos[elem] = idx
	//q.bubbleUp(idx)
	q.heapElem = append(q.heapElem, elem)
	q.heapKeys = append(q.heapKeys, key)
	idx := len(q.heapKeys) - 1
	q.pos.Put(elem, idx)
	q.bubbleUp(idx)
}

// Update replaces the key at idx and restores heap ordering.
func (q *PrioQueue[T, U]) Update(idx int, elem T, key U) {

	q.heapKeys[idx] = key
	// key could be smaller or larger, try both directions
	q.bubbleUp(idx)
	q.bubbleDown(idx)
}

// Remove deletes elem at idx and restores heap ordering.
func (q *PrioQueue[T, U]) Remove(idx int, elem T) {

	//q.swapAt(idx, len(q.heap)-1)
	//q.heap = q.heap[:len(q.heap)-1]
	//delete(q.pos, elem)
	//if idx < len(q.heap) {
	//	q.bubbleUp(idx)
	//	q.bubbleDown(idx)
	//}

	//q.swapAt(idx, len(q.heapKeys)-1)
	q.swapAtFast(idx, len(q.heapKeys)-1)
	q.heapKeys = q.heapKeys[:len(q.heapKeys)-1]
	q.heapElem = q.heapElem[:len(q.heapElem)-1]
	q.pos.Del(elem)
	if idx < len(q.heapKeys) {
		q.bubbleUp(idx)
		q.bubbleDown(idx)
	}
}

// Top returns the minimum-priority element.
func (q *PrioQueue[T, U]) Top() (T, bool) {
	if len(q.heapElem) == 0 {
		var zero T
		return zero, false
	}
	return q.heapElem[0], true
}

// TopKey returns the minimum priority key.
func (q *PrioQueue[T, U]) TopKey() (U, bool) {
	if len(q.heapKeys) == 0 {
		var zero U
		return zero, false
	}
	return q.heapKeys[0], true
}

// KeyOf returns the current key for elem.
func (q *PrioQueue[T, U]) KeyOf(elem T) (U, bool) {
	idx, exists := q.pos.Get(elem)
	if !exists {
		var zero U
		return zero, false
	}
	return q.heapKeys[idx], true
}

// Find returns elem's heap index, or -1 if elem is absent.
func (q *PrioQueue[T, U]) Find(elem T) int {
	i, exists := q.pos.Get(elem)
	if exists {
		return i
	}
	return -1
}

// Len returns the number of queued elements.
func (q *PrioQueue[T, U]) Len() int {
	return len(q.heapKeys)
}

// --- internal helpers ---

func (q *PrioQueue[T, U]) bubbleUp(idx int) {
	cachedIdx := idx
	for idx > 0 {
		//parent := (idx - 1) / 2
		//if q.cmp.Less(q.heap[idx].key, q.heap[parent].key) {
		//	q.swapAt(idx, parent)
		//	idx = parent
		//} else {
		//	break
		//}
		parent := (idx - 1) / 2
		if q.cmp.Less(q.heapKeys[idx], q.heapKeys[parent]) {
			//q.swapAt(idx, parent)
			q.swapAtFast(idx, parent)
			idx = parent
		} else {
			break
		}
	}
	if cachedIdx != idx {
		q.pos.Put(q.heapElem[idx], idx)
	}
}

func (q *PrioQueue[T, U]) bubbleDown(idx int) {
	//n := len(q.heap)
	//for {
	//	smallest := idx
	//	left := 2*idx + 1
	//	right := 2*idx + 2
	//	if left < n && q.cmp.Less(q.heap[left].key, q.heap[smallest].key) {
	//		smallest = left
	//	}
	//	if right < n && q.cmp.Less(q.heap[right].key, q.heap[smallest].key) {
	//		smallest = right
	//	}
	//	if smallest == idx {
	//		break
	//	}
	//	q.swapAt(idx, smallest)
	//	idx = smallest
	//}

	cachedIdx := idx
	n := len(q.heapKeys)
	for {
		smallest := idx
		left := 2*idx + 1
		right := 2*idx + 2
		if left < n && q.cmp.Less(q.heapKeys[left], q.heapKeys[smallest]) {
			smallest = left
		}
		if right < n && q.cmp.Less(q.heapKeys[right], q.heapKeys[smallest]) {
			smallest = right
		}
		if smallest == idx {
			break
		}
		//q.swapAt(idx, smallest)
		q.swapAtFast(idx, smallest)
		idx = smallest
	}
	if cachedIdx != idx {
		q.pos.Put(q.heapElem[idx], idx)
	}
}

func (q *PrioQueue[T, U]) swapAt(i, j int) {
	//q.heap[i], q.heap[j] = q.heap[j], q.heap[i]
	//q.pos[q.heap[i].elem] = i
	//q.pos[q.heap[j].elem] = j
	q.heapKeys[i], q.heapKeys[j] = q.heapKeys[j], q.heapKeys[i]
	q.heapElem[i], q.heapElem[j] = q.heapElem[j], q.heapElem[i]

	q.pos.Put(q.heapElem[i], i)
	q.pos.Put(q.heapElem[j], j)
}

func (q *PrioQueue[T, U]) swapAtFast(cachedOutside, j int) {
	//q.heap[i], q.heap[j] = q.heap[j], q.heap[i]
	//q.pos[q.heap[i].elem] = i
	//q.pos[q.heap[j].elem] = j
	q.heapKeys[cachedOutside], q.heapKeys[j] = q.heapKeys[j], q.heapKeys[cachedOutside]
	q.heapElem[cachedOutside], q.heapElem[j] = q.heapElem[j], q.heapElem[cachedOutside]

	q.pos.Put(q.heapElem[cachedOutside], cachedOutside)
	//q.pos[q.heapElem[j]] = j
}
