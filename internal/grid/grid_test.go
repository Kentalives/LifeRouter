package grid

import (
	"testing"

	"github.com/Kentalives/LifeRouter/internal/config"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
)

func defaultTestConfig() *pubconfig.Config {
	return &pubconfig.Config{
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
	}
}

func setupTestConfig() {
	config.Setup(defaultTestConfig(), nil)
}

func TestGrid_CreationEmpty(t *testing.T) {

	g := newGrid(5, 3)

	if len(g.base) != 3*5 || len(g.objects) != 3*5 || len(g.final) != 3*5 {
		t.Errorf("Expected internal buffers size to be 15, but got (base: %d, objects: %d, final: %d)\n", len(g.base), len(g.objects), len(g.final))
	}
	for i := 0; i < 15; i++ {
		if g.final[i] != 0 {
			t.Errorf("Expected cells to be 0, but got otherwise: %#v\n", g.final)
		}
	}
}

func TestGrid_CreationPopulated(t *testing.T) {
	base := []int32{
		0, 0, 1,
		1, 1, 1,
		1, 1, 1,
		0, 0, 1,
		0, 1, 1,
	}
	g := FromSlice(5, 3, base)

	for i := 0; i < 15; i++ {
		if base[i] != g.base[i] || base[i] != g.final[i] {
			t.Errorf("Expected base and final layer to be %d, but got (base: %d, final: %d)\nEXPECTED: %#v\nBASE: %#v\nFINAL: %#v\n", base[i], g.base[i], g.final[i], base, g.base, g.final)
			return
		}
	}
}

func TestCoordsUseConfiguredCellSize(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Grid.CellSizeM = 0.5
	config.Setup(cfg, nil)
	defer setupTestConfig()

	x, y := (Coords{X: 1, Y: 2}).ToFloat64()
	if x != 0.75 || y != 1.25 {
		t.Fatalf("expected configured cell size coordinates (0.75, 1.25), got (%f, %f)", x, y)
	}
}

func TestGridConfigValidationPanics(t *testing.T) {
	config.Setup(&pubconfig.Config{}, nil)
	defer setupTestConfig()

	defer func() {
		if recover() == nil {
			t.Fatal("expected missing grid config to panic")
		}
	}()
	_ = CellSizeM()
}

func TestGrid_NewFromImage(t *testing.T) {
	setupTestConfig()
	g, err := FromImg(62, 114, "../../data/map.png", 10)
	if err != nil {
		t.Errorf("Failure in creation: %s\n", err)
	}

	t.Log(g)
}

func TestGrid_ReplaceObjectRegion(t *testing.T) {
	rows, cols := 5, 5
	g := NewFilled(uint(rows), uint(cols), 1)
	r := &RegionGrid{
		Origin: Coords{X: 1, Y: 1},
		Rows:   2,
		Cols:   3,
		Cells: []int32{
			3, 0, 0,
			0, 0, 4,
		},
	}

	g.ReplaceObjectRegion(r)
	change1Idx := cols + 1
	change2Idx := 2*cols + 3
	if g.objects[change1Idx] != 3 {
		t.Errorf("Expected Objects (X:1,Y:1) to be 3, got %d\n", g.objects[change1Idx])
	}
	if g.final[change1Idx] != 4 {
		t.Errorf("Expected Final (X:1,Y:1) to be 4, got %d\n", g.objects[change1Idx])
	}

	if g.objects[change2Idx] != 4 {
		t.Errorf("Expected Objects (X:3,Y:2) to be 4, got %d\n", g.objects[change2Idx])
	}
	if g.final[change2Idx] != 5 {
		t.Errorf("Expected Final (X:3,Y:2) to be 5, got %d\n", g.objects[change2Idx])
	}

	t.Log(g)
}

func TestGrid_ReplaceObjectRegionClearsObjectCost(t *testing.T) {
	g := NewFilled(3, 3, 1)
	object := &RegionGrid{
		Origin: Coords{X: 1, Y: 1},
		Rows:   1,
		Cols:   1,
		Cells:  []int32{3},
	}
	clearObject := &RegionGrid{
		Origin: Coords{X: 1, Y: 1},
		Rows:   1,
		Cols:   1,
		Cells:  []int32{0},
	}

	t.Log(g)

	g.ReplaceObjectRegion(object)
	if got := g.GetValue(Coords{X: 1, Y: 1}); got != 4 {
		t.Fatalf("expected object cost to raise final value to 4, got %d", got)
	}

	t.Log(g)

	g.ReplaceObjectRegion(clearObject)
	if got := g.GetValue(Coords{X: 1, Y: 1}); got != 1 {
		t.Fatalf("expected cleared object region to restore base cost 1, got %d", got)
	}

	t.Log(g)
}

func TestGrid_GetValueOutOfBoundsIsUnreachable(t *testing.T) {
	g := NewFilled(2, 2, EMPTY_SPACE_COST)

	got := g.GetValue(Coords{X: 2, Y: 0})
	if got != UNREACHABLE_COST {
		t.Fatalf("out-of-bounds GetValue = %d, want %d", got, UNREACHABLE_COST)
	}
	if !IsBlocked(got) {
		t.Fatal("out-of-bounds GetValue was not blocked")
	}
}

func TestGrid_TraversalCost(t *testing.T) {
	g := FromSlice(3, 3, []Cost{
		1, 2, 3,
		4, 5, 6,
		7, 8, 9,
	})

	tests := []struct {
		name     string
		from, to Coords
		want     Cost
	}{
		{
			name: "orthogonal uses destination cost",
			from: Coords{X: 0, Y: 0},
			to:   Coords{X: 1, Y: 0},
			want: 2,
		},
		{
			name: "diagonal uses destination diagonal cost",
			from: Coords{X: 0, Y: 0},
			to:   Coords{X: 1, Y: 1},
			want: DiagonalCost(5),
		},
		{
			name: "out of bounds source is unreachable",
			from: Coords{X: 3, Y: 0},
			to:   Coords{X: 1, Y: 1},
			want: UNREACHABLE_COST,
		},
		{
			name: "out of bounds destination is unreachable",
			from: Coords{X: 1, Y: 1},
			to:   Coords{X: 3, Y: 0},
			want: UNREACHABLE_COST,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.TraversalCost(tt.from, tt.to); got != tt.want {
				t.Fatalf("TraversalCost() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGrid_TraversalCostBlockedEndpoint(t *testing.T) {
	g := FromSlice(2, 2, []Cost{
		1, 0,
		UNREACHABLE_COST, 1,
	})

	if got := g.TraversalCost(Coords{X: 0, Y: 0}, Coords{X: 1, Y: 0}); got != UNREACHABLE_COST {
		t.Fatalf("blocked destination TraversalCost() = %d, want %d", got, UNREACHABLE_COST)
	}
	if got := g.TraversalCost(Coords{X: 0, Y: 1}, Coords{X: 1, Y: 1}); got != UNREACHABLE_COST {
		t.Fatalf("blocked source TraversalCost() = %d, want %d", got, UNREACHABLE_COST)
	}
}
