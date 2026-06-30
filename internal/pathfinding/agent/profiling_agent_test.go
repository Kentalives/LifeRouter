package agent

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/mock"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

// Agent benchmarks isolate D* Lite planning, pooling, sparse value storage,
// replanning, and path-following costs without the public NATS API layer.

type agentBenchFixture struct {
	world    *grid.World
	floor    *grid.Grid
	agent    *mock.Element
	geoTruth *mock.GeoTruth
	ex       *mock.ExternalSystem
	names    []string
	goal     grid.GlobalCoords
	origin   grid.Coords
}

func newAgentBenchFixture(b testing.TB, rows, cols uint) *agentBenchFixture {
	b.Helper()

	g := grid.NewFilled(rows, cols, grid.EMPTY_SPACE_COST)
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		b.Fatal(err)
	}

	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		b.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(8, ex)
	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	origin := grid.Coords{X: cols / 12, Y: rows / 12}
	goal := grid.GlobalCoords{Coords: grid.Coords{X: cols - cols/12 - 1, Y: rows - rows/12 - 1}, Layer: 0}
	x, y := origin.ToFloat64()
	agent := &mock.Element{IdName: "Agent_bench", X: x, Y: y, Z: 0, Width: pubdomain.CELL_SIZE_M, Height: pubdomain.CELL_SIZE_M}
	if err := geoTruth.AddObject(agent); err != nil {
		b.Fatal(err)
	}

	return &agentBenchFixture{
		world:    w,
		floor:    g,
		agent:    agent,
		geoTruth: geoTruth,
		ex:       ex,
		names:    names,
		goal:     goal,
		origin:   origin,
	}
}

func newAgentMultiFloorBenchFixture(b testing.TB, rows, cols uint) *agentBenchFixture {
	b.Helper()

	g0 := grid.NewFilled(rows, cols, grid.EMPTY_SPACE_COST)
	g1 := grid.NewFilled(rows, cols, grid.EMPTY_SPACE_COST)
	g2 := grid.NewFilled(rows, cols, grid.EMPTY_SPACE_COST)
	names := []string{"0", "1", "2"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0, g1, g2}, names)
	if err != nil {
		b.Fatal(err)
	}
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: cols / 4, Y: rows / 4}, Layer: 0}, grid.GlobalCoords{Coords: grid.Coords{X: cols / 4, Y: rows / 4}, Layer: 1}, 1)
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: cols * 3 / 4, Y: rows * 3 / 4}, Layer: 1}, grid.GlobalCoords{Coords: grid.Coords{X: cols * 3 / 4, Y: rows * 3 / 4}, Layer: 2}, 1)

	ex, err := mock.NewExternalSystem(names, []float64{0, 4.3, 8.6}, nil)
	if err != nil {
		b.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(8, ex)
	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	origin := grid.Coords{X: cols / 12, Y: rows / 12}
	goal := grid.GlobalCoords{Coords: grid.Coords{X: cols - cols/12 - 1, Y: rows - rows/12 - 1}, Layer: 2}
	x, y := origin.ToFloat64()
	agent := &mock.Element{IdName: "Agent_multifloor_bench", X: x, Y: y, Z: 0, Width: pubdomain.CELL_SIZE_M, Height: pubdomain.CELL_SIZE_M}
	if err := geoTruth.AddObject(agent); err != nil {
		b.Fatal(err)
	}

	return &agentBenchFixture{
		world:    w,
		floor:    g0,
		agent:    agent,
		geoTruth: geoTruth,
		ex:       ex,
		names:    names,
		goal:     goal,
		origin:   origin,
	}
}

func deterministicAgentChanges(rows, cols uint, n int) []grid.Coords {
	changes := make([]grid.Coords, n)
	for i := range n {
		changes[i] = grid.Coords{
			X: uint(3 + (i*17)%max(1, int(cols)-6)),
			Y: uint(3 + (i*29)%max(1, int(rows)-6)),
		}
	}
	return changes
}

func newPlannedAgentDStar(b testing.TB, f *agentBenchFixture) *agentDStarLite {
	b.Helper()

	x, y := f.origin.ToFloat64()
	f.agent.Move(x, y, 0, 0)
	d, err := newAgentDStarLite(f.world, f.goal, f.agent.Id())
	if err != nil {
		b.Fatal(err)
	}
	d.computeShortestPath()
	if grid.IsUnreachable(d.getRhs(d.startIdx)) {
		d.QuitWorldSub.Close()
		b.Fatal("benchmark fixture produced no path")
	}
	return d
}

func BenchmarkDStarLite_Plan(b *testing.B) {
	cases := []struct {
		name       string
		rows, cols uint
		heavy      bool
	}{
		{name: "Small", rows: 40, cols: 60},
		{name: "Medium", rows: 80, cols: 120},
		{name: "Large", rows: 400, cols: 875, heavy: true},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			if tc.heavy && os.Getenv("PATHFINDING_BENCH_HEAVY") != "1" {
				b.Skip("set PATHFINDING_BENCH_HEAVY=1 to run large pathfinding benchmark")
			}
			f := newAgentBenchFixture(b, tc.rows, tc.cols)

			b.ReportAllocs()
			for b.Loop() {
				d, err := newAgentDStarLite(f.world, f.goal, f.agent.Id())
				if err != nil {
					b.Fatal(err)
				}
				d.computeShortestPath()
				d.QuitWorldSub.Close()
			}
			b.ReportMetric(float64(tc.rows*tc.cols), "cells")
		})
	}
}

func BenchmarkDStarLite_PlanPooled(b *testing.B) {
	cases := []struct {
		name       string
		rows, cols uint
		heavy      bool
	}{
		{name: "Small", rows: 40, cols: 60},
		{name: "Medium", rows: 80, cols: 120},
		{name: "Large", rows: 400, cols: 875, heavy: true},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			if tc.heavy && os.Getenv("PATHFINDING_BENCH_HEAVY") != "1" {
				b.Skip("set PATHFINDING_BENCH_HEAVY=1 to run large pathfinding benchmark")
			}
			f := newAgentBenchFixture(b, tc.rows, tc.cols)

			b.ReportAllocs()
			for b.Loop() {
				d, err := acquireAgentDStarLite(f.world, f.goal, f.agent.Id())
				if err != nil {
					b.Fatal(err)
				}
				d.computeShortestPath()
				releaseAgentDStarLite(d)
			}
			b.ReportMetric(float64(tc.rows*tc.cols), "cells")
		})
	}

}

func BenchmarkDStarLite_PlanPooledPageDirectory(b *testing.B) {
	for _, directory := range []struct {
		name   string
		sparse bool
	}{
		{name: "Dense"},
		{name: "Sparse", sparse: true},
	} {
		cases := []struct {
			name       string
			rows, cols uint
			heavy      bool
		}{
			{name: "Small", rows: 40, cols: 60},
			{name: "Medium", rows: 80, cols: 120},
			{name: "Large", rows: 400, cols: 875, heavy: true},
		}

		for _, tc := range cases {
			b.Run(directory.name+"_"+tc.name, func(b *testing.B) {
				if tc.heavy && os.Getenv("PATHFINDING_BENCH_HEAVY") != "1" {
					b.Skip("set PATHFINDING_BENCH_HEAVY=1 to run large pathfinding benchmark")
				}
				f := newAgentBenchFixture(b, tc.rows, tc.cols)
				config.Cfg.Pathfinding.Agent.SparseValuePageDirectory = directory.sparse

				b.ReportAllocs()
				for b.Loop() {
					d, err := acquireAgentDStarLite(f.world, f.goal, f.agent.Id())
					if err != nil {
						b.Fatal(err)
					}
					d.computeShortestPath()
					releaseAgentDStarLite(d)
				}
				b.ReportMetric(float64(f.world.Size()), "cells")
			})
		}
	}
}

func BenchmarkDStarLite_PlanMultiFloor(b *testing.B) {
	f := newAgentMultiFloorBenchFixture(b, 60, 90)

	b.ReportAllocs()
	for b.Loop() {
		d, err := newAgentDStarLite(f.world, f.goal, f.agent.Id())
		if err != nil {
			b.Fatal(err)
		}
		d.computeShortestPath()
		d.QuitWorldSub.Close()
	}
	b.ReportMetric(3, "floors")
}

func BenchmarkDStarLite_ReplanByChanges(b *testing.B) {
	for _, numChanges := range []int{1, 5, 10, 50} {
		b.Run(fmt.Sprintf("Changes=%d", numChanges), func(b *testing.B) {
			changes := deterministicAgentChanges(80, 120, numChanges)

			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				f := newAgentBenchFixture(b, 80, 120)
				d := newPlannedAgentDStar(b, f)
				for j, c := range changes {
					f.floor.SetValue(c, grid.Cost(5+j%11))
				}
				b.StartTimer()

				d.applyChanges()

				d.QuitWorldSub.Close()
			}
			b.ReportMetric(float64(numChanges), "changes")
		})
	}
}

func BenchmarkDStarLite_CalculateKey(b *testing.B) {
	f := newAgentBenchFixture(b, 80, 120)
	d := newPlannedAgentDStar(b, f)
	defer d.QuitWorldSub.Close()

	cell := grid.GlobalCoords{Coords: grid.Coords{X: f.origin.X + 5, Y: f.origin.Y + 7}, Layer: 0}
	cellIdx := cell.ToIdx(f.world)

	b.ReportAllocs()
	for b.Loop() {
		d.calcKey(cell, cellIdx)
	}
}

func BenchmarkDStarLite_LocalVisionReplanPipeline(b *testing.B) {

	objects := []*mock.Element{
		{IdName: "wall-1", X: 9 * pubdomain.CELL_SIZE_M, Y: 8 * pubdomain.CELL_SIZE_M, Z: 0, Width: 4 * pubdomain.CELL_SIZE_M, Height: 3 * pubdomain.CELL_SIZE_M},
		{IdName: "wall-2", X: 11 * pubdomain.CELL_SIZE_M, Y: 13 * pubdomain.CELL_SIZE_M, Z: 0, Width: 3 * pubdomain.CELL_SIZE_M, Height: 5 * pubdomain.CELL_SIZE_M},
		{IdName: "wall-3", X: 15 * pubdomain.CELL_SIZE_M, Y: 10 * pubdomain.CELL_SIZE_M, Z: 0, Width: 4 * pubdomain.CELL_SIZE_M, Height: 4 * pubdomain.CELL_SIZE_M},
	}

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		f := newAgentBenchFixture(b, 80, 120)
		for _, object := range objects {
			if err := f.geoTruth.AddObject(object); err != nil {
				b.Fatal(err)
			}
		}
		d := newPlannedAgentDStar(b, f)
		b.StartTimer()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := d.applyLocalVision(ctx, f.agent.Id()); err != nil {
			cancel()
			b.Fatal(err)
		}
		cancel()
		if !d.EmptyChangedEdges() {
			d.applyChanges()
		}

		d.QuitWorldSub.Close()
	}
	b.ReportMetric(float64(len(objects)), "objects")
}

func BenchmarkDStarLite_PathFollowing(b *testing.B) {
	f := newAgentBenchFixture(b, 80, 120)

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		d := newPlannedAgentDStar(b, f)
		b.StartTimer()

		steps := 0
		for d.startIdx != d.Goal {
			next, cost := d.minCostSucc(d.start, d.startIdx)
			if grid.IsUnreachable(cost) {
				b.Fatal("path broke while following benchmark path")
			}
			d.start = next
			d.startIdx = d.start.ToIdx(d.World)
			steps++
			if steps > f.world.Size() {
				b.Fatal("path following exceeded world size")
			}
		}

		d.QuitWorldSub.Close()
	}
}
