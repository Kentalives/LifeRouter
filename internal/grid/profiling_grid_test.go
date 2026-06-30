package grid

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Kentalives/LifeRouter/internal/config"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
)

func setupBenchmarkGridConfig() {
	config.Setup(&pubconfig.Config{
		Grid: pubconfig.GridConfig{
			CellSizeM:     0.2,
			WallAuraCells: 1,
		},
		Pathfinding: pubconfig.PathfindingConfig{
			FreeMovementHeight: 2,
			Emergency: pubconfig.EmergencyConfig{
				LightingHystheresis:    10,
				PriorityPathCellsWidth: 1,
			},
			Agent: pubconfig.AgentConfig{
				VisionRadiusCells:  10,
				CellsForRealUpdate: 5,
			},
		},
	}, nil)
}

func BenchmarkGrid_FromImg(b *testing.B) {
	setupBenchmarkGridConfig()

	cases := []struct {
		name          string
		path          string
		rows, cols    uint
		pixelsPerCell float64
	}{
		{name: "Map", path: "../../data/map.png", rows: 61, cols: 114, pixelsPerCell: 10},
		{name: "Floor1", path: "../../data/floor1.png", rows: 61, cols: 114, pixelsPerCell: 10},
		{name: "Floor2", path: "../../data/floor2.png", rows: 61, cols: 114, pixelsPerCell: 10},
		{name: "RealisticMap", path: "../../data/realisticMap.png", rows: 30, cols: 110, pixelsPerCell: 4.2286},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := FromImg(tc.rows, tc.cols, tc.path, tc.pixelsPerCell); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(tc.rows*tc.cols), "cells")
		})
	}
}

func BenchmarkGrid_NewWorldFromImages(b *testing.B) {
	setupBenchmarkGridConfig()

	layers := []pubconfig.ConfigLayer{
		{Name: "0", ImgPath: "../../data/map.png"},
		{Name: "1", ImgPath: "../../data/floor1.png"},
		{Name: "2", ImgPath: "../../data/floor2.png"},
	}

	cfg := pubconfig.GridConfig{
		FloorLayers:   layers,
		Rows:          61,
		Cols:          114,
		PxPerCell:     10,
		WallAuraCells: 1,
		CellSizeM:     0.2,
		Portals: []pubconfig.ConfigPortal{
			{From: [2]float64{15.5 * 0.2, 20.5 * 0.2}, FromLayer: 0, To: [2]float64{15.5 * 0.2, 20.5 * 0.2}, ToLayer: 1, TraversalCost: 1, Bidirectional: true},
			{From: [2]float64{15.5 * 0.2, 20.5 * 0.2}, FromLayer: 1, To: [2]float64{15.5 * 0.2, 20.5 * 0.2}, ToLayer: 2, TraversalCost: 1, Bidirectional: true},
		},
	}

	b.ReportAllocs()
	for b.Loop() {
		w, err := NewWorld(cfg.FloorLayers, cfg.Rows, cfg.Cols, cfg.PxPerCell)
		if err != nil {
			b.Fatal(err)
		}
		for _, p := range cfg.Portals {
			from := GlobalCoords{Coords: CoordsFromFloat64(p.From[0], p.From[1]), Layer: p.FromLayer}
			to := GlobalCoords{Coords: CoordsFromFloat64(p.To[0], p.To[1]), Layer: p.ToLayer}
			w.AddBidirectionalPortal(from, to, p.TraversalCost)
		}
	}
	b.ReportMetric(float64(len(layers)), "floors")
}

func BenchmarkGrid_VirtualWorldLinkedFloors(b *testing.B) {
	for _, pooled := range []bool{false, true} {
		for _, linkedFloors := range []int{1, 2, 3} {
			b.Run(fmt.Sprintf("Pooled=%t/Floors=%d", pooled, linkedFloors), func(b *testing.B) {
				setupBenchmarkGridConfig()
				if pooled {
					config.Cfg.Pathfinding.Agent.MaxReusableWorldLinkedFloors = 2
				}
				virtualWorldPool = sync.Pool{}

				floors := []*Grid{NewFilled(80, 120, 1), NewFilled(80, 120, 1), NewFilled(80, 120, 1)}
				if _, err := NewWorldFromGrids(floors, []string{"0", "1", "2"}); err != nil {
					b.Fatal(err)
				}

				b.ReportAllocs()
				for b.Loop() {
					w := NewVirtualWorld()
					for floor := range linkedFloors {
						w.Floor(floor)
					}
					ReleaseVirtualWorld(w)
				}
			})
		}
	}
}

func BenchmarkGrid_GetValue(b *testing.B) {
	g := NewFilled(80, 120, EMPTY_SPACE_COST)
	cells := []Coords{
		{X: 10, Y: 10},
		{X: 11, Y: 10},
		{X: 11, Y: 11},
		{X: 10, Y: 11},
	}

	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if got := g.GetValue(cells[i%len(cells)]); got != EMPTY_SPACE_COST {
			b.Fatalf("GetValue() = %d, want %d", got, EMPTY_SPACE_COST)
		}
	}
}

func BenchmarkGrid_TraversalCost(b *testing.B) {
	g := NewFilled(80, 120, EMPTY_SPACE_COST)
	moves := []struct {
		from Coords
		to   Coords
	}{
		{from: Coords{X: 10, Y: 10}, to: Coords{X: 11, Y: 10}},
		{from: Coords{X: 11, Y: 10}, to: Coords{X: 12, Y: 11}},
		{from: Coords{X: 12, Y: 11}, to: Coords{X: 12, Y: 12}},
		{from: Coords{X: 12, Y: 12}, to: Coords{X: 11, Y: 11}},
	}

	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		move := moves[i%len(moves)]
		if got := g.TraversalCost(move.from, move.to); got == UNREACHABLE_COST {
			b.Fatal("TraversalCost unexpectedly unreachable")
		}
	}
}

func BenchmarkGrid_PortalsTo(b *testing.B) {
	setupBenchmarkGridConfig()

	g0 := NewFilled(80, 120, EMPTY_SPACE_COST)
	g1 := NewFilled(80, 120, EMPTY_SPACE_COST)
	w, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"})
	if err != nil {
		b.Fatal(err)
	}

	target := GlobalCoords{Coords: Coords{X: 10, Y: 10}, Layer: 1}
	w.AddPortal(GlobalCoords{Coords: Coords{X: 10, Y: 9}, Layer: 0}, target, 3)
	w.AddLocalPortal(GlobalCoords{Coords: Coords{X: 11, Y: 9}, Layer: 0}, target, 4)

	const unrelatedPortals = 5000
	for i := range unrelatedPortals {
		from := GlobalCoords{Coords: Coords{X: uint(i % 120), Y: uint((i / 120) % 80)}, Layer: 0}
		to := GlobalCoords{Coords: Coords{X: uint((i*17 + 3) % 120), Y: uint((i*31 + 7) % 80)}, Layer: 1}
		if to == target {
			to = GlobalCoords{Coords: Coords{X: target.X + 1, Y: target.Y}, Layer: target.Layer}
		}
		w.AddPortal(from, to, 5)
	}

	b.ReportAllocs()
	for b.Loop() {
		portals := w.PortalsTo(target.ToIdx(w))
		if len(portals) != 2 {
			b.Fatalf("PortalsTo returned %d portals, want 2", len(portals))
		}
	}
	b.ReportMetric(unrelatedPortals, "unrelated_portals")
}
