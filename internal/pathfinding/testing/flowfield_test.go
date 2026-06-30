package pathfinding_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/mock"
	pathfinding "github.com/Kentalives/LifeRouter/internal/pathfinding/emergency"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

func TestFlowfield_Stepped(t *testing.T) {

	synctest.Test(t, func(t *testing.T) {
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
		ex, err := mock.NewExternalSystem(names, []float64{0}, nil)
		if err != nil {
			t.Fatal(err)
		}
		geoTruth := mock.NewGeoTruth(4, ex)

		setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

		exit := make(chan struct{})

		t.Log(g)

		go func() {
			time.Sleep(time.Second + time.Millisecond)

			w1 := &mock.Element{X: 4 * pubdomain.CELL_SIZE_M, Y: 1.5 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "w1", Width: 2 * pubdomain.CELL_SIZE_M, Height: 1 * pubdomain.CELL_SIZE_M}

			//w1 := &mock.Element{X: 3.5 * pubdomain.CELL_SIZE_M, Y: 1.5 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "w1"}
			//w2 := &mock.Element{X: 4.5 * pubdomain.CELL_SIZE_M, Y: 1.5 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "w2"}
			geoTruth.AddObject(w1)
			//geoTruth.AddObject(w2)

			time.Sleep(time.Second + time.Millisecond)
			t.Log(g)

			w3 := &mock.Element{X: 1.5 * pubdomain.CELL_SIZE_M, Y: 4 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "w3", Width: 1 * pubdomain.CELL_SIZE_M, Height: 2 * pubdomain.CELL_SIZE_M}

			//w3 := &mock.Element{X: 1.5 * pubdomain.CELL_SIZE_M, Y: 3.5 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "w3"}
			//w4 := &mock.Element{X: 1.5 * pubdomain.CELL_SIZE_M, Y: 4.5 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "w4"}
			geoTruth.AddObject(w3)
			//geoTruth.AddObject(w4)

			time.Sleep(time.Second + time.Millisecond)

			t.Log(g)

			time.Sleep(time.Second + time.Millisecond)

			close(exit)
		}()

		pathfinding.InternalEmergencyStart(w, nil, []grid.GlobalCoords{{Coords: grid.Coords{X: 0, Y: 0}, Layer: 0}}, 1, exit, true)

		time.Sleep(5 * time.Second)
	})

}
