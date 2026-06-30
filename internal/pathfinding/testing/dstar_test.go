package pathfinding_test

import (
	"math"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/log"
	"github.com/Kentalives/LifeRouter/internal/mock"
	pathfinding "github.com/Kentalives/LifeRouter/internal/pathfinding/agent"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

func fuzzyEq(a, b float64, t *testing.T) bool {
	t.Helper()

	return math.Abs(a-b) <= 0.0001
}

func TestDStarLite_Stepped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		x := grid.EMPTY_SPACE_COST
		g := grid.FromSlice(5, 5, []int32{
			x, x, x, x, x,
			x, x, 0, x, x,
			x, x, 0, x, x,
			x, x, 0, x, x,
			x, x, x, x, x,
		})
		names := []string{"g"}
		w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
		if err != nil {
			t.Fatal(err)
		}

		e := &mock.Element{X: 0, Y: 1 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: ""}
		ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
		if err != nil {
			t.Fatal(err)
		}
		geoTruth := mock.NewGeoTruth(3, ex)
		geoTruth.AddObject(e)

		go func() {
			<-time.Tick(1500 * time.Millisecond)
			w1 := &mock.Element{X: 3.5 * pubdomain.CELL_SIZE_M, Y: 1.5 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "w1", Width: 1 * pubdomain.CELL_SIZE_M, Height: 1 * pubdomain.CELL_SIZE_M}
			geoTruth.AddObject(w1)
			g.SetValue(grid.Coords{X: 3, Y: 1}, 0)

			<-time.Tick(1000 * time.Millisecond)
			t.Log(g)
			w2 := &mock.Element{X: 4.5 * pubdomain.CELL_SIZE_M, Y: 1.5 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "w2", Width: 1 * pubdomain.CELL_SIZE_M, Height: 1 * pubdomain.CELL_SIZE_M}
			geoTruth.AddObject(w2)
			//g.SetValue(grid.Coords{X: 4, Y: 1}, 0)
			<-time.Tick(1000 * time.Millisecond)
			t.Log(g)
		}()

		setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

		comm, err := pathfinding.InternalAgentFindPath(w, grid.GlobalCoords{Coords: grid.Coords{X: 3, Y: 2}, Layer: 0}, e.Id(), 1, 0, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}

		quit := make(chan struct{})
		flag := make(chan struct{})
		go func() {
			flagCoordsX1, flagCoordsY1, flagCoordsX2, flagCoordsY2 := 3.5*pubdomain.CELL_SIZE_M, 1.5*pubdomain.CELL_SIZE_M, 4.5*pubdomain.CELL_SIZE_M, 1.5*pubdomain.CELL_SIZE_M
			//t.Logf("FLAG1: (X: %.2f, Y: %.2f)\tFLAG2: (X: %.2f, Y: %.2f)", flagCoordsX1, flagCoordsY1, flagCoordsX2, flagCoordsY2)
			time.Sleep(200 * time.Millisecond)
			for {
				select {
				case <-quit:
					return
				case <-time.After(1 * time.Second):
					x, y, _, _ := e.Position()
					//t.Logf("POS: (X: %.2f, Y: %.2f)", x, y)
					if (fuzzyEq(x, flagCoordsX1, t) && fuzzyEq(y, flagCoordsY1, t)) || (fuzzyEq(x, flagCoordsX2, t) && fuzzyEq(y, flagCoordsY2, t)) {
						close(flag)
						return
					}
				}
			}
		}()

		t.Cleanup(func() {
			comm.Terminate()
			close(quit)
		})

		done := comm.BlockingWait()
		select {
		case <-time.After(6 * time.Second):
		case <-flag:
			t.Fatal("Agent passed through the wrong cell")
		case <-done:
			t.Fatal("Agent was too fast! Probably passed through a wall")
		}

		select {
		case <-done:
		case <-flag:
			t.Fatal("Agent passed through the wrong cell")
		case <-time.After(14 * time.Second):
			t.Fatal("Agent took too long to reach the goal")
		}

		time.Sleep(1 * time.Second)
		select {
		case <-flag:
			t.Fatal("Agent passed through the wrong cell")
		default:
		}
	})
}

func TestDStarLite_HeuristicCorrectness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		x := grid.EMPTY_SPACE_COST
		g := grid.FromSlice(5, 3, []int32{
			x, 0, x,
			x, 0, x,
			x, x, x,
			x, x, x,
			x, x, x,
		})
		names := []string{"g"}
		w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
		if err != nil {
			t.Fatal(err)
		}

		goal := grid.GlobalCoords{Coords: grid.Coords{X: 2, Y: 0}, Layer: 0}

		e := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0, IdName: ""}
		ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
		if err != nil {
			t.Fatal(err)
		}
		geoTruth := mock.NewGeoTruth(3, ex)
		geoTruth.AddObject(e)

		go func() {
			time.Sleep(900 * time.Millisecond)
			w1 := &mock.Element{X: 1.5 * pubdomain.CELL_SIZE_M, Y: 2.5 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "w1", Width: 1 * pubdomain.CELL_SIZE_M, Height: 1 * pubdomain.CELL_SIZE_M}
			geoTruth.AddObject(w1)
			//g.SetValue(grid.Coords{X: 1, Y: 2}, 0)

			time.Sleep(1000 * time.Millisecond)
			w2 := &mock.Element{X: 1.5 * pubdomain.CELL_SIZE_M, Y: 3.5 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "w2", Width: 1 * pubdomain.CELL_SIZE_M, Height: 1 * pubdomain.CELL_SIZE_M}
			geoTruth.AddObject(w2)
			//g.SetValue(grid.Coords{X: 1, Y: 3}, 0)
		}()

		setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

		comm, err := pathfinding.InternalAgentFindPath(w, goal, e.Id(), 1, 0, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}

		quit := make(chan struct{})
		flag := make(chan struct{})
		go func() {
			flagCoordsX, flagCoordsY := 0.5*pubdomain.CELL_SIZE_M, 4.5*pubdomain.CELL_SIZE_M
			time.Sleep(200 * time.Millisecond)
			for {
				select {
				case <-quit:
					return
				case <-time.After(1 * time.Second):
					x, y, _, _ := e.Position()
					if fuzzyEq(x, flagCoordsX, t) && fuzzyEq(y, flagCoordsY, t) {
						close(flag)
						return
					}
				}
			}
		}()

		t.Cleanup(func() {
			comm.Terminate()
			close(quit)
		})

		select {
		case <-comm.BlockingWait():
		case <-flag:
			t.Fatal("Agent went through a cell that should not have been used (X:0,Y:4)")
		case <-time.After(20 * time.Second):
			t.Fatal("Agent took too long to reach the goal")
		}

		time.Sleep(1 * time.Second)
		select {
		case <-flag:
			t.Fatal("Agent passed through the wrong cell")
		default:
		}

	})
}

func TestDStarLite_WorldVirtualization(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log.Setup(pubconfig.AppConfig{LogStderr: testing.Verbose()})

		g := grid.FromSlice(1, 30, []int32{
			1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		})
		names := []string{"g"}
		w, err := grid.NewWorldFromGrids([]*grid.Grid{g}, names)
		if err != nil {
			t.Fatal(err)
		}

		goal1 := grid.GlobalCoords{Coords: grid.Coords{X: 29, Y: 0}, Layer: 0}
		goal2 := grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 0}, Layer: 0}

		a1 := &mock.Element{X: 0, Y: 0, Z: 0, RotY: 0, IdName: "Agent_1"}
		a2 := &mock.Element{X: 29.5 * pubdomain.CELL_SIZE_M, Y: 0, Z: 0, RotY: 0, IdName: "Agent_2"}
		ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
		if err != nil {
			t.Fatal(err)
		}
		geoTruth := mock.NewGeoTruth(3, ex)
		geoTruth.AddObject(a1)
		geoTruth.AddObject(a2)

		w2 := grid.NewVirtualWorld()
		go func() {
			time.Sleep(900 * time.Millisecond)
			wall1 := &mock.Element{X: 11.5 * pubdomain.CELL_SIZE_M, Y: 0.5 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "w1", Width: 1 * pubdomain.CELL_SIZE_M, Height: 1 * pubdomain.CELL_SIZE_M}
			geoTruth.AddObject(wall1)
			//g.SetValue(grid.Coords{X: 1, Y: 3}, 0)
		}()

		setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

		comm1, err := pathfinding.InternalAgentFindPath(w, goal1, a1.Id(), 1, 0, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}

		comm2, err := pathfinding.InternalAgentFindPath(w2, goal2, a2.Id(), 1, 0, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}

		done1 := comm1.BlockingWait()
		done2 := comm2.BlockingWait()
		t.Cleanup(func() {
			comm1.Terminate()
			comm2.Terminate()
		})

		select {
		case <-done1:
		case <-done2:
			t.Fatal("Agent1 should have stopped before Agent2")
		case <-time.After(4 * time.Second):
			t.Fatal("Agent1 should have stopped before 4 Seconds elapsed (4 steps)")
		}

		select {
		case <-done2:
		case <-time.After(8 * time.Second):
			t.Fatal("Agent1 should have stopped before 8 Seconds elapsed (8 steps)")
		}
	})
}
