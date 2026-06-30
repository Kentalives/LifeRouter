package core

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/Kentalives/LifeRouter/internal/grid"
)

// Core benchmarks measure priority-queue operations with D* Lite-like key
// patterns and mixed workloads.

func benchmarkKey(r *rand.Rand) Key {
	minimum := grid.Cost(r.IntN(5000))
	return Key{
		K1: minimum + grid.Cost(r.IntN(5000)),
		K2: minimum,
	}
}

func benchmarkQueueEntry(i int) grid.GlobalIdx {
	return grid.GlobalIdx(i)
}

func buildBenchmarkQueue(size int) (*PrioQueue[grid.GlobalIdx, Key], []grid.GlobalIdx) {
	return buildBenchmarkQueueWithCapacity(size, size)
}

func buildBenchmarkQueueWithCapacity(size, capacity int) (*PrioQueue[grid.GlobalIdx, Key], []grid.GlobalIdx) {
	r := rand.New(rand.NewPCG(1, 2))
	pq := NewPrioQueue[grid.GlobalIdx, Key](capacity, &Key_comparator)
	entries := make([]grid.GlobalIdx, size, capacity)
	for i := range size {
		entry := benchmarkQueueEntry(i)
		entries[i] = entry
		pq.Insert(entry, benchmarkKey(r))
	}
	return pq, entries
}

func BenchmarkCore_PriorityQueueInsert(b *testing.B) {
	for _, size := range []int{0, 1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("InitialSize=%d", size), func(b *testing.B) {
			pq, _ := buildBenchmarkQueueWithCapacity(size, size+1)
			r := rand.New(rand.NewPCG(3, 4))
			next := size

			b.ReportAllocs()
			for b.Loop() {
				entry := benchmarkQueueEntry(next)
				pq.Insert(entry, benchmarkKey(r))
				idx := pq.Find(entry)
				if idx == -1 {
					b.Fatal("inserted queue entry not found")
				}
				pq.Remove(idx, entry)
				next++
			}
			b.ReportMetric(float64(size), "initial_entries")
		})
	}
}

func BenchmarkCore_PriorityQueueUpdate(b *testing.B) {
	for _, size := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("Size=%d", size), func(b *testing.B) {
			pq, entries := buildBenchmarkQueue(size)
			r := rand.New(rand.NewPCG(5, 6))

			b.ReportAllocs()
			i := 0
			for b.Loop() {
				entry := entries[i%len(entries)]
				idx := pq.Find(entry)
				if idx == -1 {
					b.Fatalf("queue entry %v not found", entry)
				}
				pq.Update(idx, entry, benchmarkKey(r))
				i++
			}
			b.ReportMetric(float64(size), "entries")
		})
	}
}

func BenchmarkCore_PriorityQueuePop(b *testing.B) {
	for _, size := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("Size=%d", size), func(b *testing.B) {
			pq, _ := buildBenchmarkQueue(size)
			r := rand.New(rand.NewPCG(9, 10))
			next := size

			b.ReportAllocs()
			for b.Loop() {
				u, ok := pq.Top()
				if !ok {
					b.Fatal("priority queue unexpectedly empty")
				}
				pq.Remove(0, u)
				pq.Insert(benchmarkQueueEntry(next), benchmarkKey(r))
				next++
			}
			b.ReportMetric(float64(size), "initial_entries")
		})
	}
}

func BenchmarkCore_PriorityQueueRemoveRoot(b *testing.B) {
	for _, size := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("Size=%d", size), func(b *testing.B) {
			pq, _ := buildBenchmarkQueue(size)
			r := rand.New(rand.NewPCG(11, 12))
			next := size

			b.ReportAllocs()
			for b.Loop() {
				u, ok := pq.Top()
				if !ok {
					b.Fatal("priority queue unexpectedly empty")
				}
				pq.Remove(0, u)
				pq.Insert(benchmarkQueueEntry(next), benchmarkKey(r))
				next++
			}
			b.ReportMetric(float64(size), "initial_entries")
		})
	}
}

func BenchmarkCore_PriorityQueueMixedDStarWorkload(b *testing.B) {
	const initialSize = 10_000

	pq, entries := buildBenchmarkQueueWithCapacity(initialSize, initialSize+1)
	r := rand.New(rand.NewPCG(7, 8))
	next := initialSize
	i := 0

	b.ReportAllocs()
	for b.Loop() {
		switch i % 10 {
		case 0, 1:
			entry := benchmarkQueueEntry(next)
			pq.Insert(entry, benchmarkKey(r))
			removed, ok := pq.Top()
			if !ok {
				b.Fatal("priority queue unexpectedly empty")
			}
			pq.Remove(0, removed)
			next++
		case 2, 3, 4, 5, 6, 7:
			entry := entries[i%len(entries)]
			if idx := pq.Find(entry); idx != -1 {
				pq.Update(idx, entry, benchmarkKey(r))
			}
		default:
			if u, ok := pq.Top(); ok {
				pq.Remove(0, u)
				pq.Insert(benchmarkQueueEntry(next), benchmarkKey(r))
				next++
			}
		}
		i++
	}

	b.ReportMetric(float64(initialSize), "initial_entries")
}

func BenchmarkCore_CostSameFloor(b *testing.B) {
	floor := grid.NewFilled(80, 120, grid.EMPTY_SPACE_COST)
	w, err := grid.NewWorldFromGrids([]*grid.Grid{floor}, []string{"0"})
	if err != nil {
		b.Fatal(err)
	}
	d := &DStarLiteCore{World: w}
	moves := []struct {
		from    grid.GlobalCoords
		to      grid.GlobalCoords
		fromIdx grid.GlobalIdx
	}{
		{
			from: grid.GlobalCoords{Coords: grid.Coords{X: 10, Y: 10}, Layer: 0},
			to:   grid.GlobalCoords{Coords: grid.Coords{X: 11, Y: 10}, Layer: 0},
		},
		{
			from: grid.GlobalCoords{Coords: grid.Coords{X: 11, Y: 10}, Layer: 0},
			to:   grid.GlobalCoords{Coords: grid.Coords{X: 12, Y: 11}, Layer: 0},
		},
		{
			from: grid.GlobalCoords{Coords: grid.Coords{X: 12, Y: 11}, Layer: 0},
			to:   grid.GlobalCoords{Coords: grid.Coords{X: 12, Y: 12}, Layer: 0},
		},
		{
			from: grid.GlobalCoords{Coords: grid.Coords{X: 12, Y: 12}, Layer: 0},
			to:   grid.GlobalCoords{Coords: grid.Coords{X: 11, Y: 11}, Layer: 0},
		},
	}
	for i := range moves {
		moves[i].fromIdx = moves[i].from.ToIdx(w)
	}

	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		move := moves[i%len(moves)]
		if got := d.Cost(move.from, move.to, move.fromIdx); got == grid.UNREACHABLE_COST {
			b.Fatal("Cost unexpectedly unreachable")
		}
	}
}

func BenchmarkCore_NeighborCostGathering(b *testing.B) {
	floor := grid.NewFilled(80, 120, grid.EMPTY_SPACE_COST)
	portalFloor := grid.NewFilled(80, 120, grid.EMPTY_SPACE_COST)
	w, err := grid.NewWorldFromGrids([]*grid.Grid{floor, portalFloor}, []string{"0", "1"})
	if err != nil {
		b.Fatal(err)
	}
	center := grid.GlobalCoords{Coords: grid.Coords{X: 40, Y: 40}, Layer: 0}
	w.AddBidirectionalPortal(center, grid.GlobalCoords{Coords: grid.Coords{X: 40, Y: 40}, Layer: 1}, 3)
	d := &DStarLiteCore{World: w, NeighboursCache: NewNeighboursCache()}
	centerIdx := center.ToIdx(w)

	b.Run("SuccPlusCost", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			succs, depth := d.Succ(center, centerIdx)
			var total grid.Cost
			for _, succ := range succs {
				total = grid.AddCost(total, d.Cost(center, succ, centerIdx))
			}
			d.NeighboursCache.Release(depth)
			if grid.IsUnreachable(total) {
				b.Fatal("unexpected unreachable total")
			}
		}
	})

	b.Run("SuccCosts", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			succs, depth := d.SuccCosts(center, centerIdx)
			var total grid.Cost
			for _, succ := range succs {
				total = grid.AddCost(total, succ.Cost)
			}
			d.NeighboursCache.Release(depth)
			if grid.IsUnreachable(total) {
				b.Fatal("unexpected unreachable total")
			}
		}
	})

	b.Run("PredPlusCost", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			preds, depth := d.Pred(center, centerIdx)
			var total grid.Cost
			for _, pred := range preds {
				total = grid.AddCost(total, d.Cost(pred, center, pred.ToIdx(w)))
			}
			d.NeighboursCache.Release(depth)
			if grid.IsUnreachable(total) {
				b.Fatal("unexpected unreachable total")
			}
		}
	})

	b.Run("PredCosts", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			preds, depth := d.PredCosts(center, centerIdx)
			var total grid.Cost
			for _, pred := range preds {
				total = grid.AddCost(total, pred.Cost)
			}
			d.NeighboursCache.Release(depth)
			if grid.IsUnreachable(total) {
				b.Fatal("unexpected unreachable total")
			}
		}
	})
}
