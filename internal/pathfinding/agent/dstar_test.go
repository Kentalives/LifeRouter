package agent

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/mock"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/core"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
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

func setupTestConfig(dep *pubconfig.Dependencies) {
	config.Setup(defaultTestConfig(), dep)
}

func TestDStarLite_StraightLine(t *testing.T) {
	g := grid.FromSlice(1, 5, []int32{
		1, 1, 1, 1, 1,
	})
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 0}, Layer: 0}
	e := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}

	d.computeShortestPath()

	t.Log(d)

	if grid.IsUnreachable(d.getRhs(d.startIdx)) {
		t.Fatal("no path found, expected straight line path")
	}
	if float64(d.getRhs(d.startIdx)) > 4.001 {
		t.Errorf("expected cost ~4, got %v", d.getRhs(d.startIdx))
	}
}

func TestDStarLite_StraightLineSparseValuePageDirectory(t *testing.T) {
	g := grid.FromSlice(1, 5, []int32{
		1, 1, 1, 1, 1,
	})
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	e := &mock.Element{}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	if err := geoTruth.AddObject(e); err != nil {
		t.Fatal(err)
	}
	cfg := defaultTestConfig()
	cfg.Pathfinding.Agent.SparseValuePageDirectory = true
	config.Setup(cfg, &pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 0}, Layer: 0}
	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	if !d.values.sparse {
		t.Fatal("solver did not configure sparse value page directory")
	}
	if grid.IsUnreachable(d.getRhs(d.startIdx)) {
		t.Fatal("no path found, expected straight line path")
	}
}

func TestDStarLiteStateValuesHandleZeroAndUnreachable(t *testing.T) {
	d := &agentDStarLite{}
	d.values.configure(8, false)

	if got := d.getG(0); got != grid.UNREACHABLE_COST {
		t.Fatalf("initial g(0) = %d, want %d", got, grid.UNREACHABLE_COST)
	}
	if got := d.getRhs(7); got != grid.UNREACHABLE_COST {
		t.Fatalf("initial rhs(7) = %d, want %d", got, grid.UNREACHABLE_COST)
	}

	d.setG(0, 12)
	d.setRhs(0, 13)
	d.setG(7, 21)
	d.setRhs(7, 22)

	if got := d.getG(0); got != 12 {
		t.Fatalf("g(0) = %d, want 12", got)
	}
	if got := d.getRhs(0); got != 13 {
		t.Fatalf("rhs(0) = %d, want 13", got)
	}
	if got := d.getG(7); got != 21 {
		t.Fatalf("g(7) = %d, want 21", got)
	}
	if got := d.getRhs(7); got != 22 {
		t.Fatalf("rhs(7) = %d, want 22", got)
	}

	d.setG(0, grid.UNREACHABLE_COST)
	d.setRhs(7, grid.UNREACHABLE_COST)

	if got := d.getG(0); got != grid.UNREACHABLE_COST {
		t.Fatalf("deleted g(0) = %d, want %d", got, grid.UNREACHABLE_COST)
	}
	if d.values.hasG(0) {
		t.Fatal("gVal still has zero key after unreachable set")
	}
	if got := d.getRhs(7); got != grid.UNREACHABLE_COST {
		t.Fatalf("deleted rhs(7) = %d, want %d", got, grid.UNREACHABLE_COST)
	}
	if got := d.values.getRhs(7); !grid.IsUnreachable(got) {
		t.Fatal("rhsVal still has key 7 after unreachable set")
	}
}

func TestDStarLiteResetClearsStateAndRetainsBuffers(t *testing.T) {
	q := core.NewPrioQueue[grid.GlobalIdx, core.Key](4, &core.Key_comparator)
	q.Insert(2, core.Key{K1: 1})
	cache := core.NewNeighboursCache()
	cache.GetFreeCache()
	cache.GetFreeCache()
	visionCells := make([]grid.Cost, 4)

	d := &agentDStarLite{
		DStarLiteCore: core.DStarLiteCore{
			Queue:           q,
			Changed:         map[grid.GlobalCoords]grid.Cost{{Layer: 1}: 2},
			Goal:            8,
			NeighboursCache: cache,
		},
		km:       4,
		startIdx: 5,
		vision:   &grid.RegionGrid{Rows: 2, Cols: 2, Cells: visionCells},
	}
	d.values.configure(4, false)
	d.setG(2, 3)
	d.setRhs(3, 4)

	d.Reset()

	if d.Queue.Len() != 0 || len(d.Changed) != 0 || d.values.gLen != 0 || d.values.rhsLen != 0 {
		t.Fatal("reset left algorithm state populated")
	}
	if d.Goal != -1 || d.startIdx != -1 || d.km != 0 || d.World != nil || d.QuitWorldSub != nil {
		t.Fatal("reset left request-specific scalar state populated")
	}
	if _, depth := d.NeighboursCache.GetFreeCache(); depth != 0 {
		t.Fatalf("neighbor cache depth after reset = %d, want 0", depth)
	}
}

func TestAcquireAgentDStarLiteReturnsResetState(t *testing.T) {
	names := []string{"g"}
	w1, err := grid.NewWorldFromGrids([]*grid.Grid{grid.NewFilled(4, 4, grid.EMPTY_SPACE_COST)}, names)
	if err != nil {
		t.Fatal(err)
	}
	w2, err := grid.NewWorldFromGrids([]*grid.Grid{grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)}, names)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	e := &mock.Element{IdName: "agent"}
	if err := geoTruth.AddObject(e); err != nil {
		t.Fatal(err)
	}
	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d1, err := acquireAgentDStarLite(w1, grid.GlobalCoords{Coords: grid.Coords{X: 3, Y: 3}}, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d1.computeShortestPath()
	if d1.values.gLen == 0 {
		t.Fatal("first pathfinding run did not populate reusable state")
	}
	releaseAgentDStarLite(d1)

	goal2 := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 0}}
	d2, err := acquireAgentDStarLite(w2, goal2, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseAgentDStarLite(d2)

	if d2.World != w2 || d2.Goal != goal2.ToIdx(w2) {
		t.Fatal("pooled D* state did not receive the new request world and goal")
	}
	if d2.values.gLen != 0 || d2.values.rhsLen != 1 || d2.Queue.Len() != 1 {
		t.Fatal("pooled D* state leaked algorithm data from the previous run")
	}
}

func TestDStarLite_RejectsOutOfBoundsStartAndGoal(t *testing.T) {
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{grid.NewFilled(2, 2, grid.EMPTY_SPACE_COST)}, names)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("start", func(t *testing.T) {
		geoTruth := mock.NewGeoTruth(1, ex)
		e := &mock.Element{X: 2.0, Y: 0, Z: 0, IdName: "agent-start"}
		if err := geoTruth.AddObject(e); err != nil {
			t.Fatal(err)
		}
		setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

		_, err := newAgentDStarLite(w, grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 1}, Layer: 0}, e.Id())
		if err == nil {
			t.Fatal("expected out-of-bounds start to fail")
		}
	})

	t.Run("goal", func(t *testing.T) {
		geoTruth := mock.NewGeoTruth(1, ex)
		e := &mock.Element{X: 0, Y: 0, Z: 0, IdName: "agent-goal"}
		if err := geoTruth.AddObject(e); err != nil {
			t.Fatal(err)
		}
		setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

		_, err := newAgentDStarLite(w, grid.GlobalCoords{Coords: grid.Coords{X: 9, Y: 1}, Layer: 0}, e.Id())
		if err == nil {
			t.Fatal("expected out-of-bounds goal to fail")
		}
	})
}

func TestDStarLite_VisionRadiusUsesConfig(t *testing.T) {
	g := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	e := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	cfg := defaultTestConfig()
	cfg.Pathfinding.Agent.VisionRadiusCells = 2
	config.Setup(cfg, &pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w, grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 0}, Layer: 0}, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.QuitWorldSub.Close()

	if d.vision.Rows != 5 || d.vision.Cols != 5 || len(d.vision.Cells) != 25 {
		t.Fatalf("expected 5x5 vision from configured radius 2, got %dx%d (%d cells)", d.vision.Rows, d.vision.Cols, len(d.vision.Cells))
	}
}

func TestDStarLite_ThreeFloorsPathFollowing(t *testing.T) {
	g0 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	g1 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	g2 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	names := []string{"0", "1", "2"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0, g1, g2}, names)
	if err != nil {
		t.Fatal(err)
	}
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 1}, Layer: 0}, grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 1}, Layer: 1}, 1)
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 3, Y: 3}, Layer: 1}, grid.GlobalCoords{Coords: grid.Coords{X: 3, Y: 3}, Layer: 2}, 1)

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 4}, Layer: 2}
	e := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0, IdName: "agent"}
	ex, err := mock.NewExternalSystem(names, []float64{0, 4, 8}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	if grid.IsUnreachable(d.getRhs(d.startIdx)) {
		t.Fatal("no path found, expected path across three floors")
	}

	t.Log(d)

	current := d.start
	currentIdx := d.startIdx
	visited := make(map[grid.GlobalCoords]bool)
	visitedLayers := []int{current.Layer}
	for current != goal {
		if visited[current] {
			t.Fatalf("cycle detected at %v", current)
		}
		visited[current] = true
		next, cost := d.minCostSucc(current, currentIdx)
		if grid.IsUnreachable(cost) {
			t.Fatalf("path broke at %v before reaching goal", current)
		}
		current = next
		currentIdx = current.ToIdx(d.World)
		if !slices.Contains(visitedLayers, current.Layer) {
			visitedLayers = append(visitedLayers, current.Layer)
		}
		if len(visited) > w.Size() {
			t.Fatal("exceeded world size while following path")
		}
	}

	for _, layer := range []int{0, 1, 2} {
		if !slices.Contains(visitedLayers, layer) {
			t.Fatalf("expected path to visit layer %d, visited %v", layer, visitedLayers)
		}
	}
}

func TestInternalAgentFindPath_UpdatesRealPositionOnGoalFloor(t *testing.T) {
	g0 := grid.NewFilled(3, 3, grid.EMPTY_SPACE_COST)
	g1 := grid.NewFilled(3, 3, grid.EMPTY_SPACE_COST)
	g2 := grid.NewFilled(3, 3, grid.EMPTY_SPACE_COST)
	names := []string{"0", "1", "2"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0, g1, g2}, names)
	if err != nil {
		t.Fatal(err)
	}
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 0}, Layer: 0}, grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 0}, Layer: 1}, 1)
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 1}, Layer: 1}, grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 1}, Layer: 2}, 1)

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 2}, Layer: 2}
	e := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0, IdName: "agent"}
	ex, err := mock.NewExternalSystem(names, []float64{0, 4, 8}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	comm, err := InternalAgentFindPath(w, goal, e.Id(), 1000, 0, testing.Verbose())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(comm.Terminate)

	select {
	case err := <-comm.ErrorCh():
		t.Fatal(err)
	case <-comm.BlockingWait():
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not finish")
	}

	_, _, z, _ := e.Position()
	if z != 8 {
		t.Fatalf("expected final z to be goal floor height 8, got %v", z)
	}
}

func TestDStarLite_DiagonalPath(t *testing.T) {
	g := grid.FromSlice(4, 4, []int32{
		1, 1, 1, 1,
		1, 1, 1, 1,
		1, 1, 1, 1,
		1, 1, 1, 1,
	})
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 3, Y: 3}, Layer: 0}

	e := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	if grid.IsUnreachable(d.getRhs(d.startIdx)) {
		t.Fatal("no path found, expected diagonal path")
	}
	expected := 6.0
	if float64(d.getRhs(d.startIdx)) > expected+0.001 {
		t.Errorf("expected cost ~%v, got %v", expected, d.getRhs(d.startIdx))
	}
}

func TestDStarLite_SameStartAndGoal(t *testing.T) {
	g := grid.FromSlice(3, 3, []int32{
		1, 1, 1,
		1, 1, 1,
		1, 1, 1,
	})
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 1}, Layer: 0}
	e := &mock.Element{X: 1 * pubdomain.CELL_SIZE_M, Y: 1 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	// when start==goal, rhs(goal)=0 by initialize, and start==goal so rhs(start)=0
	if float64(d.getRhs(d.startIdx)) > 0.001 {
		t.Errorf("expected cost 0 for same start and goal, got %v", d.getRhs(d.startIdx))
	}
}

func TestDStarLite_WallForceDetour(t *testing.T) {
	g := grid.FromSlice(3, 5, []int32{
		1, 1, 0, 1, 1,
		1, 1, 0, 1, 1,
		1, 1, 1, 1, 1,
	})
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 0}, Layer: 0}
	e := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	if grid.IsUnreachable(d.getRhs(d.startIdx)) {
		t.Fatal("no path found, expected detour around wall")
	}
	// path must go around the wall via row 2 Ã¢â‚¬â€ minimum is 2 diag + 1 ortho + 2 diag = 4*sqrt2 + 1
	// be generous with the upper bound, just verify it found something and is not trivially wrong
	if float64(d.getRhs(d.startIdx)) < 4.0 {
		t.Errorf("cost suspiciously low for a detour: %v", d.getRhs(d.startIdx))
	}
}

func TestDStarLite_NoPath(t *testing.T) {
	g := grid.FromSlice(5, 5, []int32{
		1, 1, 1, 1, 1,
		1, 0, 0, 0, 1,
		1, 0, 1, 0, 1,
		1, 0, 0, 0, 1,
		1, 1, 1, 1, 1,
	})
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 2}, Layer: 0}
	e := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	if !grid.IsUnreachable(d.getRhs(d.startIdx)) {
		t.Errorf("expected no path, but got cost %v", d.getRhs(d.startIdx))
	}
}

func TestInternalAgentFindPath_NoPathExitError(t *testing.T) {
	g := grid.FromSlice(5, 5, []int32{
		1, 1, 1, 1, 1,
		1, 0, 0, 0, 1,
		1, 0, 1, 0, 1,
		1, 0, 0, 0, 1,
		1, 1, 1, 1, 1,
	})
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 2}, Layer: 0}
	e := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	comm, err := InternalAgentFindPath(w, goal, e.Id(), 1000, 0, testing.Verbose())
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-comm.BlockingWait():
	case <-time.After(5 * time.Second):
		comm.Terminate()
		t.Fatal("agent did not finish")
	}

	if err := comm.ExitError(); !errors.Is(err, pubdomain.ErrAgentNoPath) {
		t.Fatalf("expected no-path exit error, got %v", err)
	}
}

func TestDStarLite_HighCostCorridor(t *testing.T) {
	g := grid.FromSlice(5, 3, []int32{
		1, 9, 1,
		1, 9, 1,
		1, 9, 1,
		1, 9, 1,
		1, 9, 1,
	})
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 4}, Layer: 0}
	e := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	if grid.IsUnreachable(d.getRhs(d.startIdx)) {
		t.Fatal("no path found")
	}
	// straight down the left column costs 4, going via center costs more
	if float64(d.getRhs(d.startIdx)) > 4.001 {
		t.Errorf("expected cost ~4 going down left column, got %v", d.getRhs(d.startIdx))
	}
}

func TestDStarLite_Replan_ObstacleAppears(t *testing.T) {
	g := grid.FromSlice(3, 5, []int32{
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
	})
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 1}, Layer: 0}
	e := &mock.Element{X: 0, Y: 1 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log("=== Before obstacle ===")
	t.Log(d)

	if grid.IsUnreachable(d.getRhs(d.startIdx)) {
		t.Fatal("no initial path found")
	}

	// move one step
	d.start, _ = d.minCostSucc(d.start, d.startIdx)
	d.startIdx = d.start.ToIdx(d.World)

	//d.setChangedEdge(grid.Coords{X: 2, Y: 1}, 0)
	g.SetValue(grid.Coords{X: 2, Y: 1}, grid.UNREACHABLE_COST)

	d.km += heuristic(d.prevPos, d.start)
	d.prevPos = d.start
	d.applyChanges()

	t.Log("=== After obstacle appears at (2,1) ===")
	t.Log(d)

	// path must still exist
	if grid.IsUnreachable(d.getRhs(d.startIdx)) {
		t.Fatal("no path found after replanning around obstacle")
	}

	// path must not pass through the obstacle
	current := d.start
	currentIdx := d.startIdx
	visited := make(map[grid.GlobalCoords]bool)
	obstacle := grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 1}, Layer: 0}
	for current != goal {
		if current == obstacle {
			t.Fatalf("path passes through obstacle at %v", obstacle)
		}
		if visited[current] {
			t.Fatalf("cycle detected at %v", current)
		}
		visited[current] = true
		next, cost := d.minCostSucc(current, currentIdx)

		if grid.IsUnreachable(cost) {
			t.Fatalf("path broke at %v before reaching goal", current)
		}
		current = next
		currentIdx = current.ToIdx(d.World)
	}
	t.Logf("path successfully avoids obstacle, cost from new start=%v", d.getRhs(d.startIdx))
}

func TestDStarLite_Replan_ObstacleDisappears(t *testing.T) {
	g := grid.FromSlice(3, 5, []int32{
		1, 1, 2, 1, 1,
		1, 1, 0, 1, 1,
		1, 1, 2, 1, 1,
	})
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 1}, Layer: 0}
	e := &mock.Element{X: 0, Y: 1 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	g.SetValue(grid.Coords{X: 2, Y: 0}, 2)
	g.SetValue(grid.Coords{X: 2, Y: 2}, 2)

	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log("=== With obstacle ===")
	t.Log(d)

	detourCost := d.getRhs(d.startIdx)
	if grid.IsUnreachable(detourCost) {
		t.Fatal("no initial path found with obstacle")
	}

	// setChangedEdge captures old value (0) AND updates grid to 1 atomically
	//d.setChangedEdge(grid.Coords{X: 2, Y: 1}, 1)
	g.SetValue(grid.Coords{X: 2, Y: 0}, 1)

	d.km += heuristic(d.prevPos, d.start)
	d.prevPos = d.start

	d.applyChanges()

	t.Log("=== After obstacle removed ===")
	t.Log(d)

	newCost := d.getRhs(d.startIdx)
	if grid.IsUnreachable(newCost) {
		t.Fatal("no path found after obstacle removed")
	}
	// direct path through (2,1) costs 4, detour costs more Ã¢â‚¬â€ new cost must be cheaper
	if newCost >= detourCost {
		t.Errorf("expected cheaper path after obstacle removed, got %v vs detour %v",
			newCost, detourCost)
	}
}

func TestDStarLite_PathFollowing(t *testing.T) {
	// verify that following minCostSucc from start actually reaches goal
	g := grid.FromSlice(5, 5, []int32{
		1, 1, 1, 1, 1,
		1, 1, 0, 1, 1,
		1, 1, 0, 1, 1,
		1, 1, 0, 1, 1,
		1, 1, 1, 1, 1,
	})
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 2}, Layer: 0}
	e := &mock.Element{X: 0, Y: 2 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	if grid.IsUnreachable(d.getRhs(d.startIdx)) {
		t.Fatal("no path found")
	}

	// walk the path and verify we reach goal without cycles
	current := d.start
	currentIdx := d.startIdx
	visited := make(map[grid.GlobalCoords]bool)
	steps := 0
	maxSteps := int(g.Rows * g.Cols)

	for current != goal {
		if visited[current] {
			t.Fatalf("cycle detected at %v after %d steps", current, steps)
		}
		visited[current] = true
		next, cost := d.minCostSucc(current, currentIdx)
		if grid.IsUnreachable(cost) {
			t.Fatalf("path broke at %v after %d steps", current, steps)
		}
		current = next
		currentIdx = current.ToIdx(d.World)
		steps++
		if steps > maxSteps {
			t.Fatalf("exceeded max steps, possible cycle")
		}
	}
	t.Logf("reached goal in %d steps", steps)
}

func TestDStarLite_LocalVision(t *testing.T) {
	for _, tc := range []struct {
		name               string
		stepsForRealUpdate int
	}{
		{name: "every_step", stepsForRealUpdate: 1},
		{name: "every_four_steps", stepsForRealUpdate: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := grid.NewFilled(50, 50, 1)
			names := []string{"g"}
			w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
			if err != nil {
				t.Fatal(err)
			}

			goal := grid.GlobalCoords{Coords: grid.Coords{X: 49, Y: 49}, Layer: 0}
			e := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0, IdName: "agent"}
			ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
			if err != nil {
				t.Fatal(err)
			}
			geoTruth := mock.NewGeoTruth(4, ex)
			geoTruth.AddObject(e)

			w1 := &mock.Element{X: 25 * pubdomain.CELL_SIZE_M, Y: 25 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, Width: 5 * pubdomain.CELL_SIZE_M, Height: 5 * pubdomain.CELL_SIZE_M, IdName: "w1"}
			w2 := &mock.Element{X: 25 * pubdomain.CELL_SIZE_M, Y: 35 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, Width: 5 * pubdomain.CELL_SIZE_M, Height: 7 * pubdomain.CELL_SIZE_M, IdName: "w2"}
			w3 := &mock.Element{X: 35 * pubdomain.CELL_SIZE_M, Y: 25 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, Width: 7 * pubdomain.CELL_SIZE_M, Height: 5 * pubdomain.CELL_SIZE_M, IdName: "w3"}
			geoTruth.AddObject(w1)
			geoTruth.AddObject(w2)
			geoTruth.AddObject(w3)

			cfg := defaultTestConfig()
			cfg.Pathfinding.Agent.CellsForRealUpdate = tc.stepsForRealUpdate
			config.Setup(cfg, &pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

			comm, err := InternalAgentFindPath(w, goal, e.Id(), 1000, 0, testing.Verbose())
			if err != nil {
				t.Fatal(err)
			}

			select {
			case err := <-comm.ErrorCh():
				t.Fatal(err)
			case <-comm.BlockingWait():
			case <-time.After(10 * time.Second):
				comm.Terminate()
				t.Fatal("local vision pathfinding did not finish")
			}
		})
	}
}

func TestDStarLite_DifferentFloor(t *testing.T) {
	g0 := grid.FromSlice(5, 5, []int32{
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
	})
	g1 := grid.FromSlice(5, 5, []int32{
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
	})
	names := []string{"g0", "g1"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0, g1}, names)
	if err != nil {
		t.Fatal(err)
	}
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 0}, Layer: 0}, grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 0}, Layer: 1}, 1)

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 4}, Layer: 1}
	e := &mock.Element{X: 0, Y: 4 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0, 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	if grid.IsUnreachable(d.getRhs(d.startIdx)) {
		t.Fatal("no path found, expected path across floors")
	}
	expected := 13.0
	if float64(d.getRhs(d.startIdx)) > expected+0.001 {
		t.Errorf("expected cost ~%v, got %v", expected, d.getRhs(d.startIdx))
	}
}

func TestDStarLite_MultiplePortals(t *testing.T) {
	g0 := grid.FromSlice(5, 5, []int32{
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
	})
	g1 := grid.FromSlice(5, 5, []int32{
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
	})
	names := []string{"g0", "g1"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0, g1}, names)
	if err != nil {
		t.Fatal(err)
	}
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 0}, Layer: 0}, grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 0}, Layer: 1}, 1)
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 2}, Layer: 0}, grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 2}, Layer: 1}, 1)

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 4}, Layer: 1}
	e := &mock.Element{X: 0, Y: 4 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0, 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	if grid.IsUnreachable(d.getRhs(d.startIdx)) {
		t.Fatal("no path found, expected path across floors")
	}
	expected := 9.0
	if float64(d.getRhs(d.startIdx)) > expected+0.001 {
		t.Errorf("expected cost ~%v, got %v", expected, d.getRhs(d.startIdx))
	}
}

func TestDStarLite_FloorDiscovery(t *testing.T) {
	g0 := grid.FromSlice(5, 5, []int32{
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
	})
	g1 := grid.FromSlice(5, 5, []int32{
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
		1, 1, 1, 1, 1,
	})
	names := []string{"g0", "g1"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0, g1}, names)
	if err != nil {
		t.Fatal(err)
	}
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 0}, Layer: 0}, grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 0}, Layer: 1}, 1)
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 2}, Layer: 0}, grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 2}, Layer: 1}, 1)
	w2 := grid.NewVirtualWorld()

	r := &grid.RegionGrid{
		Origin: grid.Coords{X: 1, Y: 0},
		Rows:   5,
		Cols:   1,
		Cells: []int32{
			50,
			50,
			50,
			50,
			50,
		},
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 4}, Layer: 0}
	e := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0, 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(e)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newAgentDStarLite(w2, goal, e.Id())
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	//Diagonal same floor
	expected := 8.0
	if float64(d.getRhs(d.startIdx)) > expected+0.001 {
		t.Errorf("expected diagonal path cost ~%v, got %v", expected, d.getRhs(d.startIdx))
	}

	//Apply complete vertical wall (column 1)
	f0 := w2.Floor(0)
	f0.ReplaceObjectRegion(r)
	d.applyChanges()
	t.Log(d)

	//Change floor to avoid wall and go down again for a diagonal final path
	expected = 14.0
	if float64(d.getRhs(d.startIdx)) > expected+0.001 {
		t.Errorf("expected change of floor into diagonal path cost ~%v, got %v", expected, d.getRhs(d.startIdx))
	}

	//Apply 3-cell horizontal wall in floor 1 (row 1)
	r.Cols = 3
	r.Rows = 1
	r.Origin = grid.Coords{X: 0, Y: 1}
	f1 := w2.Floor(1)
	f1.ReplaceObjectRegion(r)
	d.applyChanges()
	t.Log(d)

	//Change floor to avoid wall and go down again for a diagonal final path
	expected = 16.0
	if float64(d.getRhs(d.startIdx)) > expected+0.001 {
		t.Errorf("expected floor 1 wall avoidance path cost ~%v, got %v", expected, d.getRhs(d.startIdx))
	}
}
