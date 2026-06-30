package pathfinding_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/mock"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/agent"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/emergency"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

func TestCombination_AgentPathOnEmergency(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := grid.NewFilled(5, 5, pubdomain.COST_EMPTY_SPACE)
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

		exit := make(chan struct{})

		var emergencyDone <-chan struct{} = nil
		timer := time.AfterFunc(8*time.Second, func() {
			close(exit)
			<-emergencyDone
		})
		t.Cleanup(func() {
			stopped := timer.Stop()
			if stopped {
				close(exit)
				<-emergencyDone
			}
		})

		emergencyDone, err = emergency.InternalEmergencyStart(w, nil, []grid.GlobalCoords{{Coords: grid.Coords{X: 0, Y: 0}, Layer: 0}}, 1, exit, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}

		e := &mock.Element{X: 4 * pubdomain.CELL_SIZE_M, Y: 4 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "Agent_1"}
		geoTruth.AddObject(e)
		comm, err := agent.InternalAgentFindPath(grid.NewVirtualWorld(), grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 0}, Layer: 0}, "Agent_1", 0.25, 0, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(comm.Terminate)
		agentDone := comm.BlockingWait()

		select {
		case <-agentDone:
			t.Fatal("Expected emergency to end before agent reached the goal")
		case <-emergencyDone:
		case <-time.After(10 * time.Second):
			t.Fatal("Expected done to be notified")
		}
	})
}

func TestCombination_EmergencyOnRunningAgent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := grid.NewFilled(5, 5, pubdomain.COST_EMPTY_SPACE)
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

		e := &mock.Element{X: 4 * pubdomain.CELL_SIZE_M, Y: 4 * pubdomain.CELL_SIZE_M, Z: 0, RotY: 0, IdName: "Agent_1"}
		geoTruth.AddObject(e)
		comm, err := agent.InternalAgentFindPath(grid.NewVirtualWorld(), grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 0}, Layer: 0}, "Agent_1", 0.25, 0, testing.Verbose())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(comm.Terminate)
		agentDone := comm.BlockingWait()

		exit := make(chan struct{})

		configured := make(chan bool, 1)
		var emergencyDone <-chan struct{} = nil
		timer := time.AfterFunc(12*time.Second, func() {
			set, ok := <-configured

			close(exit)
			if set && ok {
				<-emergencyDone
			}
		})
		t.Cleanup(func() {
			stopped := timer.Stop()
			if stopped {
				close(exit)
				<-emergencyDone
			}
		})

		emergencyDone, err = emergency.InternalEmergencyStart(w, nil, []grid.GlobalCoords{{Coords: grid.Coords{X: 0, Y: 0}, Layer: 0}}, 1, exit, testing.Verbose())
		if err != nil {
			close(configured)
			t.Fatal(err)
		}
		configured <- true

		select {
		case <-agentDone:
			t.Fatal("Expected emergency to end before agent reached the goal")
		case <-emergencyDone:

		case <-time.After(14 * time.Second):
			t.Fatal("Expected emergency to end before timeout")
		}

		select {
		case <-agentDone:
		case <-time.After(20 * time.Second):
			t.Fatal("Expected agent to end before timeout")
		}
	})
}
