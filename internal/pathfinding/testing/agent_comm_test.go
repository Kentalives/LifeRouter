package pathfinding_test

import (
	"math"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/mock"
	pathfinding "github.com/Kentalives/LifeRouter/internal/pathfinding/agent"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

const EPSILON = 0.0001

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
				CellsForRealUpdate: 1,
			},
		},
	}
}

func setupTestConfig(dep *pubconfig.Dependencies) {
	config.Setup(defaultTestConfig(), dep)
}

type agentRun interface {
	Terminate()
	BlockingWait() <-chan struct{}
}

func cleanupAgentRun(t *testing.T, comm agentRun) {
	t.Helper()
	t.Cleanup(func() {
		comm.Terminate()
		select {
		case <-comm.BlockingWait():
		case <-time.After(2 * time.Second):
			t.Error("timed out waiting for agent run to terminate")
		}
	})
}

func TestAgentComm_MoveN(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := grid.FromSlice(1, 15, []int32{
			1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		})
		names := []string{"g"}
		w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
		if err != nil {
			t.Fatal(err)
		}

		mockAgent := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0, IdName: ""}
		ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
		if err != nil {
			t.Fatal(err)
		}
		geoTruth := mock.NewGeoTruth(1, ex)
		geoTruth.AddObject(mockAgent)

		setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

		comm, err := pathfinding.InternalAgentFindPath(w, grid.GlobalCoords{Coords: grid.Coords{X: 14, Y: 0}, Layer: 0}, mockAgent.Id(), 0, 0, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}

		t.Log("===WAITING===")

		time.Sleep(1 * time.Second)

		//Did not move
		var expectedX, expectedY float64 = 0, 0
		x, y, _, _ := mockAgent.Position()
		if math.Abs(x-expectedX) > EPSILON || math.Abs(y-expectedY) > EPSILON {
			t.Errorf("Agent not in starting position expected (X: %.1f, Y: %.1f), got (X: %.1f, Y: %.1f)\n", expectedX, expectedY, x, y)
		}

		t.Logf("===MOVING 4===")

		comm.MoveNCells(4)
		time.Sleep(1 * time.Second)

		//Moved 4 cells
		expectedX, expectedY = 4.5*pubdomain.CELL_SIZE_M, 0.5*pubdomain.CELL_SIZE_M
		x, y, _, _ = mockAgent.Position()
		if math.Abs(x-expectedX) > EPSILON || math.Abs(y-expectedY) > EPSILON {
			t.Errorf("Agent wrongly positioned expected (X: %.1f, Y: %.1f), got (X: %.1f, Y: %.1f)\n", expectedX, expectedY, x, y)
		}

		t.Log("===AUTO-MOVING 5/second===")
		time.Sleep(1 * time.Second)
		comm.UpdateMovementSpeed(5)

		select {
		case <-comm.BlockingWait():
		case <-time.After((2000 + 20) * time.Millisecond): //20 of buffer (in the goroutine, it should take 2 seconds)
			t.Errorf("Agent took too long to reach the destination")
			comm.Terminate()
		}
	})
}

func TestAgentComm_MoveFOrthogonal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := grid.FromSlice(1, 15, []int32{
			1 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST, 2 * grid.EMPTY_SPACE_COST, 2 * grid.EMPTY_SPACE_COST, 2 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST,
		})
		names := []string{"g"}
		w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
		if err != nil {
			t.Fatal(err)
		}

		mockAgent := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0, IdName: ""}
		ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
		if err != nil {
			t.Fatal(err)
		}
		geoTruth := mock.NewGeoTruth(1, ex)
		geoTruth.AddObject(mockAgent)

		setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

		comm, err := pathfinding.InternalAgentFindPath(w, grid.GlobalCoords{Coords: grid.Coords{X: 14, Y: 0}, Layer: 0}, mockAgent.Id(), 0, 0, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}

		t.Log("===WAITING===")

		time.Sleep(1 * time.Millisecond)

		//Did not move
		var expectedX, expectedY float64 = 0, 0
		x, y, _, _ := mockAgent.Position()
		if math.Abs(x-expectedX) > EPSILON || math.Abs(y-expectedY) > EPSILON {
			t.Errorf("Agent not in starting position expected (X: %.1f, Y: %.1f), got (X: %.1f, Y: %.1f)\n", expectedX, expectedY, x, y)
		}

		move := 0.4 * pubdomain.CELL_SIZE_M
		t.Logf("===MOVING %.2fm (not even 1 cell)===", move)

		remains, _ := comm.MoveFMeters(move) //Synchronous

		//Moved 0 cells and 4 meters remaining
		expectedX, expectedY, expectedRemainM := 0, 0, move
		x, y, _, _ = mockAgent.Position()
		if math.Abs(x-expectedX) > EPSILON || math.Abs(y-expectedY) > EPSILON || math.Abs(remains-expectedRemainM) > EPSILON {
			t.Errorf("Agent wrongly positioned or meters remainder expected (X: %.1f, Y: %.1f, Remainder: %.1f), got (X: %.1f, Y: %.1f, Remainder: %.1f)\n", expectedX, expectedY, expectedRemainM, x, y, remains)
		}

		move = 3.6 * pubdomain.CELL_SIZE_M
		t.Logf("===MOVING %.2fm (3 cells)===", move)

		remains, _ = comm.MoveFMeters(move) //Synchronous

		//Moved 3 cells and 6 meters remaining
		expectedX, expectedY, expectedRemainM = 3.5*pubdomain.CELL_SIZE_M, 0.5*pubdomain.CELL_SIZE_M, 0.6*pubdomain.CELL_SIZE_M
		x, y, _, _ = mockAgent.Position()
		if math.Abs(x-expectedX) > EPSILON || math.Abs(y-expectedY) > EPSILON || math.Abs(remains-expectedRemainM) > EPSILON {
			t.Errorf("Agent wrongly positioned or meters remainder expected (X: %.1f, Y: %.1f, Remainder: %.1f), got (X: %.1f, Y: %.1f, Remainder: %.1f)\n", expectedX, expectedY, expectedRemainM, x, y, remains)
		}

		move = 3.6 * pubdomain.CELL_SIZE_M
		t.Logf("===MOVING %.2fm (1 cells in double as hard terrain)===", move)

		remains, _ = comm.MoveFMeters(move) //Synchronous

		//Moved 1 cells and 16 meters remaining
		expectedX, expectedY, expectedRemainM = 4.5*pubdomain.CELL_SIZE_M, 0.5*pubdomain.CELL_SIZE_M, 1.6*pubdomain.CELL_SIZE_M
		x, y, _, _ = mockAgent.Position()
		if math.Abs(x-expectedX) > EPSILON || math.Abs(y-expectedY) > EPSILON || math.Abs(remains-expectedRemainM) > EPSILON {
			t.Errorf("Agent wrongly positioned or meters remainder expected (X: %.1f, Y: %.1f, Remainder: %.1f), got (X: %.1f, Y: %.1f, Remainder: %.1f)\n", expectedX, expectedY, expectedRemainM, x, y, remains)
		}

		comm.Terminate()
	})
}

func TestAgentComm_MoveFDiagonal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := grid.FromSlice(2, 2, []int32{
			1 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST,
			1 * grid.EMPTY_SPACE_COST, 1 * grid.EMPTY_SPACE_COST,
		})
		names := []string{"g"}
		w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
		if err != nil {
			t.Fatal(err)
		}

		mockAgent := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0, IdName: ""}
		ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
		if err != nil {
			t.Fatal(err)
		}
		geoTruth := mock.NewGeoTruth(1, ex)
		geoTruth.AddObject(mockAgent)

		setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

		comm, err := pathfinding.InternalAgentFindPath(w, grid.GlobalCoords{Coords: grid.Coords{X: 1, Y: 1}, Layer: 0}, mockAgent.Id(), 0, 0, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}

		t.Log("===WAITING===")

		time.Sleep(1 * time.Millisecond)

		//Did not move
		var expectedX, expectedY float64 = 0, 0
		x, y, _, _ := mockAgent.Position()
		if math.Abs(x-expectedX) > EPSILON || math.Abs(y-expectedY) > EPSILON {
			t.Errorf("Agent not in starting position expected (X: %.1f, Y: %.1f), got (X: %.1f, Y: %.1f)\n", expectedX, expectedY, x, y)
		}

		move := 1 * pubdomain.CELL_SIZE_M
		t.Logf("===MOVING %.2fm (enough for 1 orthogonal cell, but not diagonal)===", move)

		remains, _ := comm.MoveFMeters(move) //Synchronous

		//Moved 0 cells and 10 meters remaining
		expectedX, expectedY, expectedRemainM := 0, 0, 1*pubdomain.CELL_SIZE_M
		x, y, _, _ = mockAgent.Position()
		if math.Abs(x-expectedX) > EPSILON || math.Abs(y-expectedY) > EPSILON || math.Abs(remains-expectedRemainM) > EPSILON {
			t.Errorf("Agent wrongly positioned or meters remainder expected (X: %.1f, Y: %.1f, Remainder: %.1f), got (X: %.1f, Y: %.1f, Remainder: %.1f)\n", expectedX, expectedY, expectedRemainM, x, y, remains)
		}

		move = 2 * pubdomain.CELL_SIZE_M
		t.Logf("===MOVING %.2fm (just enough for 1 diagonal cell)===", move)

		remains, _ = comm.MoveFMeters(move) //Synchronous

		//Moved 1 cells and 0 meters remaining
		expectedX, expectedY, expectedRemainM = 1.5*pubdomain.CELL_SIZE_M, 1.5*pubdomain.CELL_SIZE_M, 0.
		x, y, _, _ = mockAgent.Position()
		if math.Abs(x-expectedX) > EPSILON || math.Abs(y-expectedY) > EPSILON || math.Abs(remains-expectedRemainM) > EPSILON {
			t.Errorf("Agent wrongly positioned or meters remainder expected (X: %.1f, Y: %.1f, Remainder: %.1f), got (X: %.1f, Y: %.1f, Remainder: %.1f)\n", expectedX, expectedY, expectedRemainM, x, y, remains)
		}

		comm.Terminate()
	})
}

func TestAgentComm_Stop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := grid.FromSlice(1, 15, []int32{
			1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		})
		names := []string{"g"}
		w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
		if err != nil {
			t.Fatal(err)
		}

		mockAgent := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0, IdName: ""}
		ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
		if err != nil {
			t.Fatal(err)
		}
		geoTruth := mock.NewGeoTruth(1, ex)
		geoTruth.AddObject(mockAgent)

		setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

		t.Log("===AUTO-MOVING 5/second")
		comm, err := pathfinding.InternalAgentFindPath(w, grid.GlobalCoords{Coords: grid.Coords{X: 14, Y: 0}, Layer: 0}, mockAgent.Id(), 5, 0, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(1 * time.Second)

		t.Log("===STOPPING===")
		comm.Stop()
		time.Sleep(1 * time.Second)

		//Expected to not have moved after Stop()
		var expectedX1, expectedX2, expectedY float64 = 5.5 * pubdomain.CELL_SIZE_M, 6.5 * pubdomain.CELL_SIZE_M, 0.5 * pubdomain.CELL_SIZE_M
		x, y, _, _ := mockAgent.Position()
		if (math.Abs(x-expectedX1) > EPSILON && math.Abs(x-expectedX2) > EPSILON) || math.Abs(y-expectedY) > EPSILON {
			t.Errorf("Agent wrongly positioned expected (X: %.1f or %.1f, Y: %.1f), got (X: %.1f, Y: %.1f)\n", expectedX1, expectedX2, expectedY, x, y)
		}

		t.Log("===AUTO-MOVING 5/second")
		comm.UpdateMovementSpeed(5)

		<-comm.BlockingWait()
	})
}

func TestAgentComm_MoveNOverDefaultSpeed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := grid.FromSlice(1, 15, []int32{
			1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		})
		names := []string{"g"}
		w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
		if err != nil {
			t.Fatal(err)
		}

		mockAgent := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0, IdName: ""}
		ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
		if err != nil {
			t.Fatal(err)
		}
		geoTruth := mock.NewGeoTruth(1, ex)
		geoTruth.AddObject(mockAgent)

		setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

		t.Log("===AUTO-MOVING 2/second")
		comm, err := pathfinding.InternalAgentFindPath(w, grid.GlobalCoords{Coords: grid.Coords{X: 14, Y: 0}, Layer: 0}, mockAgent.Id(), 2, 0, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(3020 * time.Millisecond)

		//Steady movement
		var expectedX, expectedY float64 = 6.5 * pubdomain.CELL_SIZE_M, 0.5 * pubdomain.CELL_SIZE_M
		x, y, _, _ := mockAgent.Position()
		if math.Abs(x-expectedX) > EPSILON || math.Abs(y-expectedY) > EPSILON {
			t.Errorf("Agent wrongly positioned expected (X: %.1f, Y: %.1f), got (X: %.1f, Y: %.1f)\n", expectedX, expectedY, x, y)
		}

		t.Log("===MOVING 5===")

		comm.MoveNCells(5)
		time.Sleep(10 * time.Millisecond) //Wait for the pathfinder to move the agent

		expectedX, expectedY = 11.5*pubdomain.CELL_SIZE_M, 0.5*pubdomain.CELL_SIZE_M
		x, y, _, _ = mockAgent.Position()
		if math.Abs(x-expectedX) > EPSILON || math.Abs(y-expectedY) > EPSILON {
			t.Errorf("Agent wrongly positioned expected (X: %.1f, Y: %.1f), got (X: %.1f, Y: %.1f)\n", expectedX, expectedY, x, y)
		}

		t.Log("===AUTO-MOVING 2/second")
		start := time.Now()
		select {
		case <-comm.BlockingWait():
			if time.Since(start) < (1500-20)*time.Millisecond {
				t.Errorf("Agent moved too fast to reach the destination")
			}
		case <-time.After((1500 + 20) * time.Millisecond): //20 of buffer (in the goroutine, it should take 2 seconds)
			t.Errorf("Agent took too long to reach the destination")
			comm.Terminate()
		}
	})
}

func TestAgentComm_PublishPath(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := grid.FromSlice(1, 15, []int32{
			1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		})
		names := []string{"g"}
		w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
		if err != nil {
			t.Fatal(err)
		}

		mockAgent := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0, IdName: ""}
		ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
		if err != nil {
			t.Fatal(err)
		}
		geoTruth := mock.NewGeoTruth(1, ex)
		geoTruth.AddObject(mockAgent)

		setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

		t.Log("===AUTO-MOVING 2/second")
		comm, err := pathfinding.InternalAgentFindPath(w, grid.GlobalCoords{Coords: grid.Coords{X: 14, Y: 0}, Layer: 0}, mockAgent.Id(), 2, 0, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}
		cleanupAgentRun(t, comm)

		dataReceiver := make(chan map[string][]pubdomain.CellState)
		comm.SetPathPublisher(dataReceiver)
		count := 0
	Outer:
		for {
			select {
			case data, ok := <-dataReceiver:
				if count == 0 {
					if !ok {
						t.Fatal("Expected to receive at least 1 path data")
					} else {
						t.Logf("%#v", data)
						count++
						continue
					}
				}
				if count > 0 {
					if ok {
						t.Errorf("Expected to receive at max 1 path data, also got %#v\n", data)
						count++
						continue
					} else {
						break Outer
					}
				}
			case <-time.After(20 * time.Second):
				if count == 0 {
					t.Fatal("Expected to receive at least 1 path data before timeout")
				}
				break Outer
			}
		}

		time.Sleep(2 * time.Second)
	})
}

func TestAgentComm_SubscribeThenMovePublishesPathSnapshot(t *testing.T) {
	g := grid.FromSlice(1, 15, []int32{
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	})
	names := []string{"0"}
	w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
	if err != nil {
		t.Fatal(err)
	}
	mockAgent := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0, IdName: ""}
	ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(1, ex)
	geoTruth.AddObject(mockAgent)
	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})
	comm, err := pathfinding.InternalAgentFindPath(w, grid.GlobalCoords{Coords: grid.Coords{X: 14, Y: 0}, Layer: 0}, mockAgent.Id(), 0, 0, testing.Verbose())
	if err != nil {
		t.Fatal(err)
	}
	cleanupAgentRun(t, comm)
	dataReceiver := make(chan map[string][]pubdomain.CellState)
	comm.SetPathPublisher(dataReceiver)
	moveDone := make(chan error, 1)
	go func() {
		_, err := comm.MoveFMeters(pubdomain.CELL_SIZE_M)
		moveDone <- err
	}()
	select {
	case data, ok := <-dataReceiver:
		if !ok {
			t.Fatal("expected open path publisher")
		}
		if !hasDrawablePathState(data) {
			t.Fatalf("expected drawable path state, got %#v", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for path snapshot after movement command")
	}
	select {
	case err := <-moveDone:
		if err != nil {
			t.Fatalf("MoveFMeters: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for movement command to finish")
	}
}
func hasDrawablePathState(data map[string][]pubdomain.CellState) bool {
	for _, cells := range data {
		for _, cell := range cells {
			if cell != pubdomain.STATE_Unvisited {
				return true
			}
		}
	}
	return false
}
