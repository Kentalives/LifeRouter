package core

import (
	"testing"

	"github.com/Kentalives/LifeRouter/internal/grid"
)

func testKey(k1, k2 grid.Cost) Key {
	return Key{K1: k1, K2: k2}
}

func assertQueuePositions(t *testing.T, q *PrioQueue[grid.GlobalIdx, Key]) {
	t.Helper()

	if q.pos.Len() != len(q.heapElem) {
		t.Fatalf("position map length = %d, heap length = %d", q.pos.Len(), len(q.heapElem))
	}

	for idx, elem := range q.heapElem {
		got, ok := q.pos.Get(elem)
		if !ok {
			t.Fatalf("missing position for elem %d at heap index %d", elem, idx)
		}
		if got != idx {
			t.Fatalf("position for elem %d = %d, want %d", elem, got, idx)
		}
	}
}

func assertHeapOrdered(t *testing.T, q *PrioQueue[grid.GlobalIdx, Key]) {
	t.Helper()

	for idx := 1; idx < q.Len(); idx++ {
		parent := (idx - 1) / 2
		if q.cmp.Less(q.heapKeys[idx], q.heapKeys[parent]) {
			t.Fatalf("heap child at %d with key %+v is less than parent at %d with key %+v", idx, q.heapKeys[idx], parent, q.heapKeys[parent])
		}
	}
}

func TestPrioQueueInsertFindKeyOfWithZeroKey(t *testing.T) {
	q := NewPrioQueue[grid.GlobalIdx, Key](4, &Key_comparator)

	q.Insert(0, testKey(5, 0))
	q.Insert(1, testKey(3, 0))
	q.Insert(2, testKey(4, 0))

	if idx := q.Find(0); idx == -1 {
		t.Fatal("zero key element was not found")
	}

	key, ok := q.KeyOf(0)
	if !ok {
		t.Fatal("zero key element key was not found")
	}
	if key != testKey(5, 0) {
		t.Fatalf("zero key element key = %+v, want %+v", key, testKey(5, 0))
	}

	if _, ok := q.KeyOf(99); ok {
		t.Fatal("missing element key was reported as present")
	}

	assertQueuePositions(t, q)
	assertHeapOrdered(t, q)
}

func TestPrioQueueTopAndTopKeyOrdering(t *testing.T) {
	q := NewPrioQueue[grid.GlobalIdx, Key](4, &Key_comparator)

	q.Insert(10, testKey(8, 0))
	q.Insert(11, testKey(1, 3))
	q.Insert(12, testKey(1, 2))
	q.Insert(13, testKey(4, 0))

	top, ok := q.Top()
	if !ok {
		t.Fatal("top not found")
	}
	if top != 12 {
		t.Fatalf("top = %d, want 12", top)
	}

	topKey, ok := q.TopKey()
	if !ok {
		t.Fatal("top key not found")
	}
	if topKey != testKey(1, 2) {
		t.Fatalf("top key = %+v, want %+v", topKey, testKey(1, 2))
	}

	assertQueuePositions(t, q)
	assertHeapOrdered(t, q)
}

func TestPrioQueueUpdatePriorityDecreaseAndIncrease(t *testing.T) {
	q := NewPrioQueue[grid.GlobalIdx, Key](5, &Key_comparator)

	q.Insert(20, testKey(2, 0))
	q.Insert(21, testKey(5, 0))
	q.Insert(22, testKey(8, 0))
	q.Insert(23, testKey(9, 0))

	idx := q.Find(23)
	if idx == -1 {
		t.Fatal("element 23 not found before decrease")
	}
	q.Update(idx, 23, testKey(1, 0))

	if top, _ := q.Top(); top != 23 {
		t.Fatalf("top after priority decrease = %d, want 23", top)
	}

	idx = q.Find(23)
	if idx == -1 {
		t.Fatal("element 23 not found before increase")
	}
	q.Update(idx, 23, testKey(10, 0))

	if top, _ := q.Top(); top != 20 {
		t.Fatalf("top after priority increase = %d, want 20", top)
	}

	assertQueuePositions(t, q)
	assertHeapOrdered(t, q)
}

func TestPrioQueueRemoveRootMiddleAndLast(t *testing.T) {
	q := NewPrioQueue[grid.GlobalIdx, Key](6, &Key_comparator)

	entries := []struct {
		elem grid.GlobalIdx
		key  Key
	}{
		{elem: 30, key: testKey(1, 0)},
		{elem: 31, key: testKey(6, 0)},
		{elem: 32, key: testKey(2, 0)},
		{elem: 33, key: testKey(7, 0)},
		{elem: 34, key: testKey(3, 0)},
		{elem: 35, key: testKey(8, 0)},
	}
	for _, entry := range entries {
		q.Insert(entry.elem, entry.key)
	}

	removeElem := func(elem grid.GlobalIdx) {
		t.Helper()

		idx := q.Find(elem)
		if idx == -1 {
			t.Fatalf("element %d not found before remove", elem)
		}
		q.Remove(idx, elem)
		if idx := q.Find(elem); idx != -1 {
			t.Fatalf("removed element %d still found at index %d", elem, idx)
		}
		assertQueuePositions(t, q)
		assertHeapOrdered(t, q)
	}

	root, ok := q.Top()
	if !ok {
		t.Fatal("top not found before root remove")
	}
	removeElem(root)
	removeElem(34)
	last := q.heapElem[q.Len()-1]
	removeElem(last)
}

func TestPrioQueueClearAllowsReuse(t *testing.T) {
	q := NewPrioQueue[grid.GlobalIdx, Key](4, &Key_comparator)
	q.Insert(1, testKey(3, 0))
	q.Insert(2, testKey(1, 0))

	elemCapacity := cap(q.heapElem)
	keyCapacity := cap(q.heapKeys)
	q.Clear()

	if q.Len() != 0 || q.pos.Len() != 0 {
		t.Fatalf("clear left queue state: len=%d positions=%d", q.Len(), q.pos.Len())
	}
	if _, ok := q.Top(); ok {
		t.Fatal("clear left a top element")
	}
	if cap(q.heapElem) != elemCapacity || cap(q.heapKeys) != keyCapacity {
		t.Fatal("clear discarded queue capacity")
	}

	q.Insert(3, testKey(2, 0))
	if top, ok := q.Top(); !ok || top != 3 {
		t.Fatalf("top after reuse = %d, %v; want 3, true", top, ok)
	}
	assertQueuePositions(t, q)
}
