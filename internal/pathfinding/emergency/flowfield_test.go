package emergency

import (
	"testing"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/domain"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/mock"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"

	"github.com/google/uuid"
)

const fakeGoalPortalCost grid.Cost = 1

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

func TestFlowfield_Basic(t *testing.T) {
	g := grid.NewFilled(3, 3, grid.EMPTY_SPACE_COST)
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 2}, Layer: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newSysDStarLite([]grid.GlobalCoords{goal}, nil, w)
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	// straight down the left column costs 4, going via center costs more
	top_left := grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 0}, Layer: 0}
	expected := 2*grid.DiagonalCost(grid.EMPTY_SPACE_COST) + fakeGoalPortalCost
	if d.getRhs(top_left.ToIdx(d.World)) > expected {
		t.Errorf("expected cost ~%v from top-left corner, got %v", expected, d.getRhs(top_left.ToIdx(d.World)))
	}
}

func TestFlowfield_FakeGoalPortalsAreLocalAndCleanedUp(t *testing.T) {
	g := grid.NewFilled(3, 3, grid.EMPTY_SPACE_COST)
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 2}, Layer: 0}
	d, err := newSysDStarLite([]grid.GlobalCoords{goal}, nil, w)
	if err != nil {
		t.Fatal(err)
	}

	fakeGoalIdx := d.fakeGoal.ToIdx(w)

	if len(w.PortalsFrom(goal.ToIdx(w))) == 0 {
		t.Fatal("expected emergency world to see fake-goal local portal")
	}
	if d.fakeGoal != grid.SentinelGlobalCoords {
		t.Fatalf("fake goal = %v, want sentinel %v", d.fakeGoal, grid.SentinelGlobalCoords)
	}
	if d.Goal != -1 || d.fakeGoal.ToIdx(w) != -1 {
		t.Fatalf("fake goal index = %d/%d, want -1", d.Goal, d.fakeGoal.ToIdx(w))
	}
	if len(w.PortalsFrom(d.fakeGoal.ToIdx(w))) == 0 {
		t.Fatal("expected reverse fake-goal local portal")
	}

	virtual := grid.NewVirtualWorld()
	if len(virtual.PortalsFrom(goal.ToIdx(virtual))) != 0 {
		t.Fatal("fake-goal local portal leaked into virtual world")
	}

	d.cleanupFakeGoalPortals()
	if len(w.PortalsFrom(goal.ToIdx(w))) != 0 {
		t.Fatal("fake-goal local portal was not removed from emergency world")
	}
	if len(w.PortalsTo(fakeGoalIdx)) != 0 {
		t.Fatal("reverse fake-goal local portal was not removed from emergency world")
	}
}

func TestFlowfieldRejectsInvalidGoal(t *testing.T) {
	g := grid.NewFilled(3, 3, grid.EMPTY_SPACE_COST)
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 9, Y: 0}, Layer: 0}
	if _, err := newSysDStarLite([]grid.GlobalCoords{goal}, nil, w); err == nil {
		t.Fatal("expected invalid emergency goal to fail")
	}
	if len(w.PortalsFrom(grid.SentinelGlobalCoords.ToIdx(w))) != 0 {
		t.Fatal("invalid emergency goal left fake-goal portal state")
	}
}

func TestFlowfield_MultiExit(t *testing.T) {
	g := grid.NewFilled(3, 3, grid.EMPTY_SPACE_COST)
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newSysDStarLite(
		[]grid.GlobalCoords{
			{Coords: grid.Coords{X: 2, Y: 2}, Layer: 0},
			{Coords: grid.Coords{X: 0, Y: 0}, Layer: 0},
		},
		nil,
		w,
	)
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	top := grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 0}, Layer: 0}
	expected := grid.EMPTY_SPACE_COST + fakeGoalPortalCost
	if d.getRhs(top.ToIdx(d.World)) > expected {
		t.Errorf("expected cost ~%v from top, got %v", expected, d.getRhs(top.ToIdx(d.World)))
	}
	middle := grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 1}, Layer: 0}
	expected = grid.DiagonalCost(grid.EMPTY_SPACE_COST) + fakeGoalPortalCost
	if d.getRhs(middle.ToIdx(d.World)) > expected {
		t.Errorf("expected cost ~%v from middle, got %v", expected, d.getRhs(middle.ToIdx(d.World)))
	}
	bottom := grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 2}, Layer: 0}
	expected = grid.EMPTY_SPACE_COST + fakeGoalPortalCost
	if d.getRhs(bottom.ToIdx(d.World)) > expected {
		t.Errorf("expected cost ~%v from bottom, got %v", expected, d.getRhs(bottom.ToIdx(d.World)))
	}
}

func TestFlowfield_ObstacleAppears(t *testing.T) {
	g := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	names := []string{"g"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 3, Y: 4}, Layer: 0}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newSysDStarLite([]grid.GlobalCoords{goal}, nil, w)
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log("===OBSTACLE NOT THERE YET===", d)

	// straight down the left column costs 4, going via center costs more
	top_left := grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 0}, Layer: 0}
	expected := 3*grid.DiagonalCost(grid.EMPTY_SPACE_COST) + grid.EMPTY_SPACE_COST + fakeGoalPortalCost
	if d.getRhs(top_left.ToIdx(d.World)) > expected {
		t.Errorf("expected cost ~%v from top-left corner, got %v", expected, d.getRhs(top_left.ToIdx(d.World)))
	}

	g.SetValue(grid.Coords{X: 2, Y: 2}, grid.UNREACHABLE_COST)
	g.SetValue(grid.Coords{X: 3, Y: 2}, grid.UNREACHABLE_COST)
	g.SetValue(grid.Coords{X: 2, Y: 3}, grid.UNREACHABLE_COST)

	if !d.EmptyChangedEdges() {

		d.applyChanges()

	}

	t.Log("===OBSTACLE APPEARED===", d)

}

func TestFlowfield_DifferentFloor(t *testing.T) {
	g0 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	g1 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	names := []string{"g0", "g1"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0, g1}, names)
	if err != nil {
		t.Fatal(err)
	}
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 0}, Layer: 0}, grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 0}, Layer: 1}, grid.EMPTY_SPACE_COST)

	realGoals := []grid.GlobalCoords{{Coords: grid.Coords{X: 4, Y: 4}, Layer: 1}}
	ex, err := mock.NewExternalSystem(names, []float64{0, 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newSysDStarLite(realGoals, nil, w)
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	top_left_0 := grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 0}, Layer: 0}
	expected := 5*grid.EMPTY_SPACE_COST + 2*grid.DiagonalCost(grid.EMPTY_SPACE_COST) + fakeGoalPortalCost
	if d.getRhs(top_left_0.ToIdx(d.World)) > expected {
		t.Errorf("expected cost ~%v from top-left-0 corner, got %v", expected, d.getRhs(top_left_0.ToIdx(d.World)))
	}

	bottom_right_0 := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 4}, Layer: 0}
	expected = 5*grid.EMPTY_SPACE_COST + 4*grid.DiagonalCost(grid.EMPTY_SPACE_COST) + fakeGoalPortalCost
	if d.getRhs(bottom_right_0.ToIdx(d.World)) > expected {
		t.Errorf("expected cost ~%v from bottom-right-0 corner, got %v", expected, d.getRhs(bottom_right_0.ToIdx(d.World)))
	}

	top_right_1 := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 0}, Layer: 1}
	expected = 4*grid.EMPTY_SPACE_COST + fakeGoalPortalCost
	if d.getRhs(top_right_1.ToIdx(d.World)) > expected {
		t.Errorf("expected cost ~%v from top-right-1 corner, got %v", expected, d.getRhs(top_right_1.ToIdx(d.World)))
	}
}

func TestFlowfield_MultiplePortals(t *testing.T) {
	g0 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	g1 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	names := []string{"g0", "g1"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0, g1}, names)
	if err != nil {
		t.Fatal(err)
	}
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 0}, Layer: 0}, grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 0}, Layer: 1}, grid.EMPTY_SPACE_COST)
	w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 2}, Layer: 0}, grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 2}, Layer: 1}, 2*grid.EMPTY_SPACE_COST)

	realGoals := []grid.GlobalCoords{{Coords: grid.Coords{X: 4, Y: 4}, Layer: 1}}
	ex, err := mock.NewExternalSystem(names, []float64{0, 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newSysDStarLite(realGoals, nil, w)
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	t.Log(d)

	top_left_0 := grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 0}, Layer: 0}
	expected := 5*grid.EMPTY_SPACE_COST + 2*grid.DiagonalCost(grid.EMPTY_SPACE_COST) + fakeGoalPortalCost
	if d.getRhs(top_left_0.ToIdx(d.World)) > expected {
		t.Errorf("expected cost ~%v from top-left-0 corner, got %v", expected, d.getRhs(top_left_0.ToIdx(d.World)))
	}

	bottom_right_0 := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 4}, Layer: 0}
	expected = 5*grid.EMPTY_SPACE_COST + 4*grid.DiagonalCost(grid.EMPTY_SPACE_COST) + fakeGoalPortalCost
	if d.getRhs(bottom_right_0.ToIdx(d.World)) > expected {
		t.Errorf("expected cost ~%v from bottom-right-0 corner, got %v", expected, d.getRhs(bottom_right_0.ToIdx(d.World)))
	}

	top_right_1 := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 0}, Layer: 1}
	expected = 4*grid.EMPTY_SPACE_COST + fakeGoalPortalCost
	if d.getRhs(top_right_1.ToIdx(d.World)) > expected {
		t.Errorf("expected cost ~%v from top-right-1 corner, got %v", expected, d.getRhs(top_right_1.ToIdx(d.World)))
	}

	mid_right_of_portal_0 := grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 2}, Layer: 0}
	expected = 5*grid.EMPTY_SPACE_COST + 2*grid.DiagonalCost(grid.EMPTY_SPACE_COST) + fakeGoalPortalCost
	if d.getRhs(mid_right_of_portal_0.ToIdx(d.World)) > expected {
		t.Errorf("expected cost ~%v from mid-right-of-portal-0 corner, got %v", expected, d.getRhs(mid_right_of_portal_0.ToIdx(d.World)))
	}
}

func TestFlowfield_ThreeFloorsPortalDirections(t *testing.T) {
	g0 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	g1 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	g2 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	names := []string{"0", "1", "2"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0, g1, g2}, names)
	if err != nil {
		t.Fatal(err)
	}
	f0Portal := grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 1}, Layer: 0}
	f1PortalDown := grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 1}, Layer: 1}
	f1PortalUp := grid.GlobalCoords{Coords: grid.Coords{X: 3, Y: 3}, Layer: 1}
	f2Portal := grid.GlobalCoords{Coords: grid.Coords{X: 3, Y: 3}, Layer: 2}
	w.AddBidirectionalPortal(f0Portal, f1PortalDown, 1)
	w.AddBidirectionalPortal(f1PortalUp, f2Portal, 1)

	realGoals := []grid.GlobalCoords{{Coords: grid.Coords{X: 4, Y: 4}, Layer: 2}}
	ex, err := mock.NewExternalSystem(names, []float64{0, 4, 8}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newSysDStarLite(realGoals, nil, w)
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	if grid.IsUnreachable(d.getRhs(f0Portal.ToIdx(d.World))) {
		t.Fatal("expected floor 0 portal to reach floor 2 goal")
	}
	if got := d.getFlow(f0Portal, f0Portal.ToIdx(d.World)); got != pubdomain.DIR_OUT {
		t.Fatalf("expected floor 0 portal flow OUT, got %d", got)
	}
	if got := d.getFlow(f1PortalUp, f1PortalUp.ToIdx(d.World)); got != pubdomain.DIR_OUT {
		t.Fatalf("expected floor 1 upper portal flow OUT, got %d", got)
	}
	if got := d.getFlow(f2Portal, f2Portal.ToIdx(d.World)); got == pubdomain.DIR_IN || got == pubdomain.DIR_OUT {
		t.Fatalf("expected floor 2 portal to flow toward same-floor exit, got %d", got)
	}

	t.Log(d)
}

func TestFlowfield_EnvironmentObjectsDoNotLeakAcrossFloors(t *testing.T) {
	g0 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	g1 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	g2 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	names := []string{"0", "1", "2"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0, g1, g2}, names)
	if err != nil {
		t.Fatal(err)
	}

	ex, err := mock.NewExternalSystem(names, []float64{0, 4, 8}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(&mock.Element{
		IdName: "wall-floor-1",
		X:      2.5 * pubdomain.CELL_SIZE_M,
		Y:      2.5 * pubdomain.CELL_SIZE_M,
		Z:      4,
		Width:  1 * pubdomain.CELL_SIZE_M,
		Height: 1 * pubdomain.CELL_SIZE_M,
	})

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	d, err := newSysDStarLite([]grid.GlobalCoords{{Coords: grid.Coords{X: 4, Y: 4}, Layer: 2}}, nil, w)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.applyEnvironmentObjects(); err != nil {
		t.Fatal(err)
	}

	objectCell := grid.Coords{X: 2, Y: 2}
	if got := g0.GetValue(objectCell); got != grid.EMPTY_SPACE_COST {
		t.Fatalf("expected floor 0 object cell to remain empty cost, got %d", got)
	}
	if got := g1.GetValue(objectCell); got <= grid.EMPTY_SPACE_COST {
		t.Fatalf("expected floor 1 object cell to include object cost, got %d", got)
	}
	if got := g2.GetValue(objectCell); got != grid.EMPTY_SPACE_COST {
		t.Fatalf("expected floor 2 object cell to remain empty cost, got %d", got)
	}
}

func TestFlowfield_ApplyPreferencePath(t *testing.T) {
	//g0 := grid.FromSlice(5, 5, []int32{
	//	2, 2, 2, 2, 2,
	//	2, 2, 2, 2, 2,
	//	2, 2, 2, 2, 2,
	//	2, 2, 2, 2, 2,
	//	2, 2, 2, 2, 2,
	//})
	g0 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0}, []string{"g0"})
	if err != nil {
		t.Fatal(err)
	}

	uuid1, uuid2, uuid3 := uuid.New(), uuid.New(), uuid.New()
	weight1 := uint16(1)

	routeGraphs := []pubdomain.RouteGraph{
		{
			Floor: 0,
			Waypoints: []pubdomain.GraphWaypoint{
				{Id: &uuid1, X: 0.5 * pubdomain.CELL_SIZE_M, Y: 2.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid2, X: 3.6 * pubdomain.CELL_SIZE_M, Y: 2.6 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid3, X: 1.5 * pubdomain.CELL_SIZE_M, Y: 0.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
			},
			Edges: []pubdomain.GraphEdge{
				{FromWaypoint: &uuid1, ToWaypoint: &uuid2, Weight: &weight1},
				{FromWaypoint: &uuid2, ToWaypoint: &uuid3, Weight: &weight1},
			},
		},
	}
	ApplyPreferencePaths(routeGraphs, w)
	affectedCells := []grid.Coords{
		{X: 0, Y: 2},
		{X: 1, Y: 2},
		{X: 2, Y: 2},
		{X: 3, Y: 2},
		{X: 2, Y: 1},
		{X: 1, Y: 0},
	}
	for _, c := range affectedCells {
		if g0.GetValue(c) != 1 {
			t.Errorf("Not rasterized (x:%v,y:%v)! Expected priority path (1, got %d)", c.X, c.Y, g0.GetValue(c))
		}
	}
	t.Log(w)

}

func TestFlowfield_ApplyPreferencePathDefaultsNilWeightToOne(t *testing.T) {
	g0 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0}, []string{"g0"})
	if err != nil {
		t.Fatal(err)
	}

	uuid1, uuid2 := uuid.New(), uuid.New()
	routeGraphs := []pubdomain.RouteGraph{
		{
			Floor: 0,
			Waypoints: []pubdomain.GraphWaypoint{
				{Id: &uuid1, X: 0.5 * pubdomain.CELL_SIZE_M, Y: 2.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid2, X: 3.5 * pubdomain.CELL_SIZE_M, Y: 2.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
			},
			Edges: []pubdomain.GraphEdge{
				{FromWaypoint: &uuid1, ToWaypoint: &uuid2},
			},
		},
	}

	ApplyPreferencePaths(routeGraphs, w)

	affectedCells := []grid.Coords{
		{X: 0, Y: 2},
		{X: 1, Y: 2},
		{X: 2, Y: 2},
	}
	for _, c := range affectedCells {
		if got := g0.GetValue(c); got != 1 {
			t.Fatalf("expected nil weight to default to preference cost 1 at %v, got %d", c, got)
		}
	}
}

func TestFlowfield_RemovePreferencePathsRestoresBaseCost(t *testing.T) {
	g0 := grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0}, []string{"g0"})
	if err != nil {
		t.Fatal(err)
	}

	uuid1, uuid2 := uuid.New(), uuid.New()
	weight1 := uint16(1)
	routeGraphs := []pubdomain.RouteGraph{
		{
			Floor: 0,
			Waypoints: []pubdomain.GraphWaypoint{
				{Id: &uuid1, X: 0.5 * pubdomain.CELL_SIZE_M, Y: 2.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid2, X: 3.5 * pubdomain.CELL_SIZE_M, Y: 2.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
			},
			Edges: []pubdomain.GraphEdge{
				{FromWaypoint: &uuid1, ToWaypoint: &uuid2, Weight: &weight1},
			},
		},
	}

	affectedCells := []grid.Coords{
		{X: 0, Y: 2},
		{X: 1, Y: 2},
		{X: 2, Y: 2},
	}
	ApplyPreferencePaths(routeGraphs, w)
	for _, c := range affectedCells {
		if got := g0.GetValue(c); got != 1 {
			t.Fatalf("expected preference path cell %v to have discounted cost 1, got %d", c, got)
		}
	}

	RemovePreferencePaths(w, grid.EMPTY_SPACE_COST)
	for _, c := range affectedCells {
		if got := g0.GetValue(c); got != grid.EMPTY_SPACE_COST {
			t.Fatalf("expected preference path cell %v to be restored to %d, got %d", c, grid.EMPTY_SPACE_COST, got)
		}
	}
}

func TestFlowfield_SetLightDir(t *testing.T) {
	g0 := grid.NewFilled(6, 5, grid.EMPTY_SPACE_COST)
	names := []string{"g0"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0}, names)
	if err != nil {
		t.Fatal(err)
	}

	realGoals := []grid.GlobalCoords{{Coords: grid.Coords{X: 2, Y: 5}, Layer: 0}}
	n := &mock.Node{
		Element: mock.Element{
			X: 2 * pubdomain.CELL_SIZE_M,
			Y: 0,
			Z: 6,
		},
		Dir: pubdomain.DIR_UNKNOWN,
	}
	nodes := []*mock.Node{n}
	nodesReal := []domain.INode{n}

	ex, err := mock.NewExternalSystem(names, []float64{0}, nodes)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)

	dir, err := ex.SignalingDirection(t.Context(), n.Id())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("NODE DIR:\t\033[0;34m%d\033[0m\n", dir)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	t.Log(g0)

	d, err := newSysDStarLite(realGoals, nodesReal, w)
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()

	dir, err = ex.SignalingDirection(t.Context(), n.Id())
	if err != nil {
		t.Fatal(err)
	}
	if dir != pubdomain.DIR_DOWN {
		t.Errorf("Expected light (X:2,Y:0) to point down but points:  %d\n", dir)
	}
	t.Log(d)
	t.Logf("NODE DIR:\t\033[0;34m%d\033[0m\n", dir)

	r := &grid.RegionGrid{
		Origin: grid.Coords{X: 0, Y: 0},
		Rows:   6,
		Cols:   5,
		Cells: []int32{
			0, 0, 0, 1000, 1000,
			10, 1000, 1000, 1000, 1000,
			0, 0, 0, 0, 0,
			0, 0, 0, 0, 0,
			0, 0, 0, 0, 0,
			0, 0, 0, 0, 0,
		},
	}

	g0.ReplaceObjectRegion(r)
	t.Log(g0)

	if !d.EmptyChangedEdges() {

		d.applyChanges()

	}
	t.Log(g0)
	dir, err = ex.SignalingDirection(t.Context(), n.Id())
	if err != nil {
		t.Fatal(err)
	}
	if dir != pubdomain.DIR_LEFT {
		t.Errorf("Expected light (X:2,Y:0) to point left but points:  %d\n", dir)
	}
	t.Log(d)
	t.Logf("NODE DIR:\t\033[0;34m%d\033[0m\n", dir)
}

func TestFlowfield_PreferencePathEffect(t *testing.T) {

	g0 := grid.NewFilled(10, 10, grid.EMPTY_SPACE_COST)
	names := []string{"g0"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g0}, names)
	if err != nil {
		t.Fatal(err)
	}

	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)

	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	uuid1, uuid2, uuid3, uuid4 := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	weight1 := uint16(1)

	routeGraphs := []pubdomain.RouteGraph{
		{
			Floor: 0,
			Waypoints: []pubdomain.GraphWaypoint{
				{Id: &uuid1, X: 0.5 * pubdomain.CELL_SIZE_M, Y: 0.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid2, X: 0.5 * pubdomain.CELL_SIZE_M, Y: 8.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid3, X: 8.5 * pubdomain.CELL_SIZE_M, Y: 4.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid4, X: 0.5 * pubdomain.CELL_SIZE_M, Y: 4.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
			},
			Edges: []pubdomain.GraphEdge{
				{FromWaypoint: &uuid1, ToWaypoint: &uuid2, Weight: &weight1},
				{FromWaypoint: &uuid3, ToWaypoint: &uuid4, Weight: &weight1},
			},
		},
	}
	ApplyPreferencePaths(routeGraphs, w)

	d, err := newSysDStarLite([]grid.GlobalCoords{{Coords: grid.Coords{X: 0, Y: 9}, Layer: 0}}, nil, w)
	if err != nil {
		t.Fatal(err)
	}
	d.computeShortestPath()
	t.Log(d)
}
