package agent

import (
	"errors"
	"math"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/mock"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

const EPSILON = 0.0001

func TestAgentComm_DestroyEarly(t *testing.T) {
	setupTestConfig(nil)

	t.Run("UpdateMovementSpeed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				comm.destroy()
			}()

			time.Sleep(1 * time.Millisecond)

			if ok := comm.UpdateMovementSpeed(3); ok {
				t.Fatal("Communicator should be closed")
			}
		})
	})

	t.Run("BlockingWait", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				comm.destroy()
			}()

			time.Sleep(1 * time.Millisecond)

			done := comm.BlockingWait()

			select {
			case <-done:
			case <-time.After(1 * time.Millisecond): //Timeout
				t.Fatal("Closed channel should have made me exit, but it was not on time")
			}
		})
	})

	t.Run("IsMoving", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				comm.destroy()
			}()

			time.Sleep(1 * time.Millisecond)

			b := comm.IsMoving()
			if b {
				t.Fatal("Expected NOT moving")
			}
		})
	})

	t.Run("Terminate", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				comm.destroy()
			}()

			time.Sleep(1 * time.Millisecond)

			comm.Terminate()
		})
	})

	t.Run("Stop", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				comm.destroy()
			}()

			time.Sleep(1 * time.Millisecond)

			if ok := comm.Stop(); ok {
				t.Fatal("Communicator should be closed")
			}
		})
	})

	t.Run("MoveNCells", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			comm.destroy()

			if ok := comm.MoveNCells(3); ok {
				t.Fatal("Communicator should be closed")
			}
		})
	})

	t.Run("MoveFMeters", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				comm.destroy()
			}()

			time.Sleep(1 * time.Millisecond)

			if _, err := comm.MoveFMeters(3.); err == nil {
				t.Fatal("Communicator should be closed")
			}
		})
	})
}

func TestAgentComm_ExitError(t *testing.T) {
	setupTestConfig(nil)

	t.Run("DestroyWithoutError", func(t *testing.T) {
		comm := NewAgentCommunicator("test", 0, 0)
		comm.destroy()

		if err := comm.ExitError(); err != nil {
			t.Fatalf("expected normal completion to have no exit error, got %v", err)
		}
	})

	t.Run("TerminateRecordsCancellation", func(t *testing.T) {
		comm := NewAgentCommunicator("test", 0, 0)
		comm.Terminate()

		if err := comm.ExitError(); !errors.Is(err, pubdomain.ErrAgentTerminated) {
			t.Fatalf("expected terminated exit error, got %v", err)
		}
	})

	t.Run("FirstErrorWins", func(t *testing.T) {
		comm := NewAgentCommunicator("test", 0, 0)
		comm.setExitError(pubdomain.ErrAgentNoPath)
		comm.setExitError(pubdomain.ErrAgentTerminated)

		if err := comm.ExitError(); !errors.Is(err, pubdomain.ErrAgentNoPath) {
			t.Fatalf("expected first exit error to win, got %v", err)
		}
	})
}

func TestAgentComm_DestroyAfterRequest(t *testing.T) {
	setupTestConfig(nil)

	t.Run("UpdateMovementSpeed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				time.Sleep(1 * time.Millisecond)

				comm.destroy()
			}()

			if ok := comm.UpdateMovementSpeed(3); ok {
				t.Fatal("Communicator should be closed")
			}
		})
	})

	t.Run("BlockingWait", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				time.Sleep(1 * time.Millisecond)

				comm.destroy()
			}()

			done := comm.BlockingWait()

			select {
			case <-done:
			case <-time.After(2 * time.Millisecond): //Timeout
				t.Fatal("Closed channel should have made me exit, but it was not on time")
			}
		})
	})

	t.Run("IsMoving", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				time.Sleep(1 * time.Millisecond)

				comm.destroy()
			}()

			b := comm.IsMoving()
			if !b {
				t.Fatal("Expected YES moving")
			}

			time.Sleep(2 * time.Millisecond)
		})
	})

	t.Run("Terminate", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				time.Sleep(1 * time.Millisecond)

				comm.destroy()
			}()

			comm.Terminate()

			time.Sleep(2 * time.Millisecond)
		})
	})

	t.Run("Stop", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				time.Sleep(1 * time.Millisecond)

				comm.destroy()
			}()

			if ok := comm.Stop(); ok {
				t.Fatal("Communicator should be closed")
			}
		})
	})

	t.Run("MoveNCells", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			if ok := comm.MoveNCells(3); !ok {
				t.Fatal("Communicator should accept one queued step command")
			}
			comm.destroy()

		})
	})

	t.Run("MoveFMeters1", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				time.Sleep(1 * time.Millisecond)

				comm.destroy()
			}()

			if _, err := comm.MoveFMeters(3.); err == nil {
				t.Fatal("Communicator should be closed")
			}
		})
	})

	t.Run("MoveFMeters2", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				time.Sleep(1 * time.Millisecond)
				comm.waitUntilStep()

				comm.destroy()
			}()

			_, err := comm.MoveFMeters(3.)
			if err == nil {
				t.Fatal("Expected to end movement while waiting for response (needed less meters than provided to reach goal)")
			}

		})
	})
}

func TestAgentComm_WaitingBasic(t *testing.T) {
	setupTestConfig(nil)

	t.Run("UpdateMovementSpeed1", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				comm.waitUntilStep()
				if comm.getMoveCellsPerSecond() != 2 {
					t.Errorf("Expected movespeed update final")
				}

			}()

			time.Sleep(1 * time.Millisecond)

			comm.UpdateMovementSpeed(2)

			time.Sleep(1 * time.Millisecond) //Block so receiver updates the value

			if comm.getMoveCellsPerSecond() != 2 {
				t.Errorf("Expected movespeed update after processing")
			}

			time.Sleep(500 * time.Millisecond) //Wait for second internal "waitUntilStep" timeout to run out

			time.Sleep(1 * time.Millisecond)
		})
	})

	t.Run("UpdateMovementSpeed2", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 4)

			go func() {
				comm.waitUntilStep()
				if comm.getMoveCellsPerSecond() != 2 {
					t.Errorf("Expected movespeed update final")
				}

			}()

			time.Sleep(1 * time.Millisecond)

			comm.UpdateMovementSpeed(2)

			time.Sleep(1 * time.Millisecond) //Block so receiver updates the value

			if comm.getMoveCellsPerSecond() != 2 {
				t.Errorf("Expected movespeed update after processing")
			}

			time.Sleep(1 * time.Millisecond)
		})
	})

	t.Run("BlockingWait", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)

			go func() {
				comm.waitUntilStep()

				comm.destroy()
			}()

			time.Sleep(1 * time.Millisecond)

			done := comm.BlockingWait()

			select {
			case <-done:
				t.Fatal("Waiting goroutine should not have resumed")
			case <-time.After(1 * time.Millisecond): //Timeout
				comm.Terminate()
			}
		})
	})

	t.Run("IsMoving", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)
			done := make(chan struct{})

			go func() {
				comm.waitUntilStep()
				close(done)
			}()

			time.Sleep(1 * time.Millisecond)

			b := comm.IsMoving()
			if !b {
				t.Fatal("Expected YES moving")
			}

			select {
			case <-done:
				t.Fatal("Waiting goroutine should not have resumed")
			case <-time.After(1 * time.Millisecond):
				comm.Terminate()
			}
		})
	})

	t.Run("Terminate", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)
			done := make(chan struct{})

			go func() {
				exit := comm.waitUntilStep()
				if !exit {
					t.Errorf("Expected to be told to exit, but did not\n")
				}

				close(done)
			}()

			time.Sleep(1 * time.Millisecond)

			comm.Terminate()

			select {
			case <-done:
			case <-time.After(1 * time.Millisecond):
				t.Fatal("Waiting goroutine should HAVE resumed and signaled")
			}
		})
	})

	t.Run("Stop_Stopped", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)
			done := make(chan struct{})

			go func() {
				comm.waitUntilStep()

				close(done)
			}()

			time.Sleep(1 * time.Millisecond)

			comm.Stop()

			select {
			case <-done:
				t.Fatal("Waiting goroutine should have not resumed")
			case <-time.After(1 * time.Millisecond):
				comm.Terminate()
			}
		})
	})

	t.Run("Stop_Moving", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 2)
			done := make(chan struct{})

			go func() {
				comm.waitUntilStep()

				done <- struct{}{}

				comm.waitUntilStep()

				close(done)
			}()

			time.Sleep(1 * time.Millisecond)

			comm.Stop()

			select {
			case <-done:

			case <-time.After(1 * time.Millisecond):
				t.Fatal("Waiting goroutine should have resumed once")
			}

			select {
			case <-done:
				t.Fatal("Waiting goroutine should have not resumed")
			case <-time.After(1 * time.Millisecond):
				comm.Terminate()
			}
		})
	})

	t.Run("MoveNCells1", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)
			done := make(chan struct{})

			go func() {
				exit := comm.waitUntilStep()
				if exit || comm.getStepsLeft() != 2 {
					t.Errorf("Expected not exit and to have 2 steps left")
				}
				exit = comm.waitUntilStep()
				if exit || comm.getStepsLeft() != 1 {
					t.Errorf("Exptected not exit and to have 1 steps left")
				}
				comm.waitUntilStep()
				if comm.getStepsLeft() != 0 {
					t.Errorf("Expected to have 0 steps left")
				}
				comm.waitUntilStep()

				close(done)
			}()

			time.Sleep(1 * time.Millisecond)

			comm.MoveNCells(3)

			select {
			case <-done:
				t.Fatal("Waiting goroutine should have not resumed")
			case <-time.After(1 * time.Millisecond):
				comm.Terminate()
			}
		})
	})

	t.Run("MoveNCells2", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)
			done := make(chan struct{})

			go func() {
				comm.waitUntilStep()
				if comm.getStepsLeft() != 2 {
					t.Errorf("Expected to have 2 steps left")
				}
				comm.waitUntilStep()
				if comm.getStepsLeft() != 1 {
					t.Errorf("Exptected to have 1 steps left")
				}
				comm.waitUntilStep()
				if comm.getStepsLeft() != 0 {
					t.Errorf("Expected to have 0 steps left")
				}

				close(done)
			}()

			time.Sleep(1 * time.Millisecond)

			comm.MoveNCells(3)

			select {
			case <-done:

			case <-time.After(1 * time.Millisecond):
				t.Fatal("Goroutine should have exited")
			}
		})
	})

	t.Run("MoveFMeters1", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)
			done := make(chan struct{})

			go func() {
				exit := comm.waitUntilStep() //Called with 3 meters

				expectedMetersLeft := 0.3 * pubdomain.CELL_SIZE_M
				if exit || math.Abs(comm.getMetersLeft()-expectedMetersLeft) > EPSILON {
					t.Errorf("Expected not exit and %.2f meters left, got %.2f\n", expectedMetersLeft, comm.getMetersLeft())
				}

				shouldMove := comm.tryReduceMetersMovement(pubdomain.COST_EMPTY_SPACE) //Returns flow to caller

				if shouldMove || math.Abs(comm.getMetersLeft()-0) > EPSILON {
					t.Errorf("Expected not to move and 0 meters left, got %v, %.2f\n", shouldMove, comm.getMetersLeft())
				}

				exit = comm.waitUntilStep() //Called with 6 meters

				expectedMetersLeft = 1.1 * pubdomain.CELL_SIZE_M
				if exit || math.Abs(comm.getMetersLeft()-expectedMetersLeft) > EPSILON {
					t.Errorf("Expected not exit and %.2f meters left, got %v, %.2f\n", expectedMetersLeft, exit, comm.getMetersLeft())
				}

				shouldMove = comm.tryReduceMetersMovement(pubdomain.COST_EMPTY_SPACE)

				expectedMetersLeft = 0.1 * pubdomain.CELL_SIZE_M
				if !shouldMove || math.Abs(comm.getMetersLeft()-expectedMetersLeft) > EPSILON {
					t.Errorf("Expected to move and %.2f meters left, got %v, %.2f\n", expectedMetersLeft, shouldMove, comm.getMetersLeft())
				}

				exit = comm.waitUntilStep() //Keeps going

				if exit || math.Abs(comm.getMetersLeft()-expectedMetersLeft) > EPSILON {
					t.Errorf("Expected not exit and %2.f meters left, got %v, %.2f\n", expectedMetersLeft, exit, comm.getMetersLeft())
				}

				comm.tryReduceMetersMovement(pubdomain.COST_EMPTY_SPACE) //Returns flow to caller

				if math.Abs(comm.getMetersLeft()-0) > EPSILON {
					t.Errorf("Expected 0 meters left, got %.2f\n", comm.getMetersLeft())
				}

				shouldMove = comm.tryReduceMetersMovement(pubdomain.COST_EMPTY_SPACE) //Check if can move without meters left (other types of movement)
				if !shouldMove {
					t.Errorf("Expected to be able to move without meters left, got %v\n", shouldMove)
				}

				close(done)
			}()

			time.Sleep(1 * time.Millisecond)

			move := 0.3 * pubdomain.CELL_SIZE_M
			left, err := comm.MoveFMeters(move)
			expected := move
			if math.Abs(left-expected) > EPSILON || err != nil {
				t.Fatalf("Expected to have %.2f meters left and not fail, got (left: %.2f, ok: %v)\n", expected, left, err)
			}

			move = 1.1 * pubdomain.CELL_SIZE_M
			left, err = comm.MoveFMeters(move)
			expected = 0.1 * pubdomain.CELL_SIZE_M
			if math.Abs(left-expected) > EPSILON || err != nil {
				t.Fatalf("Expected to have %.2f meters left and not fail, got (left: %.2f, ok: %v)\n", expected, left, err)
			}

			select {
			case <-done:

			case <-time.After(1 * time.Millisecond):
				t.Fatal("Goroutine should have exited")
			}

		})
	})
	t.Run("MoveFMeters2", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)
			done := make(chan struct{})

			go func() {
				comm.waitUntilStep() //Called with 14 meters

				expectedMetersLeft := 1.4 * pubdomain.CELL_SIZE_M
				if math.Abs(comm.getMetersLeft()-expectedMetersLeft) > EPSILON {
					t.Errorf("Expected %.2f meters left, got %.2f\n", expectedMetersLeft, comm.getMetersLeft())
				}

				comm.tryReduceMetersMovement(pubdomain.COST_EMPTY_SPACE)

				expectedMetersLeft = 0.4 * pubdomain.CELL_SIZE_M
				if math.Abs(comm.getMetersLeft()-expectedMetersLeft) > EPSILON {
					t.Errorf("Expected %.2f meters left, got %.2f\n", expectedMetersLeft, comm.getMetersLeft())
				}

				//Reaches goal

				comm.destroy() //Returns flow to caller

				close(done)
			}()

			time.Sleep(1 * time.Millisecond)

			move := 1.4 * pubdomain.CELL_SIZE_M
			left, err := comm.MoveFMeters(move)
			if math.Abs(left-0) > EPSILON || err != pubdomain.ErrAgentExitedWithMetersLeft {
				t.Fatalf("Expected to have gotten to destination (not ok), got (left: %.2f, ok: %v)\n", left, err)
			}

			select {
			case <-done:

			case <-time.After(1 * time.Millisecond):
				t.Fatal("Goroutine should have exited")
			}

		})
	})

	t.Run("MoveFMetersExactStep", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)
			done := make(chan struct{})

			go func() {
				comm.waitUntilStep()
				if moved := comm.tryReduceMetersMovement(pubdomain.COST_EMPTY_SPACE); !moved {
					t.Errorf("expected exact step movement to be accepted")
				}

				//Same but for a prioritized path step
				comm.waitUntilStep()
				if moved := comm.tryReduceMetersMovement(pubdomain.COST_EMPTY_SPACE / 2); !moved {
					t.Errorf("expected prioritized step movement to be accepted")
				}
				close(done)
			}()

			time.Sleep(1 * time.Millisecond)

			left, err := comm.MoveFMeters(pubdomain.CELL_SIZE_M)
			if err != nil {
				t.Fatalf("expected exact movement to complete, got %v", err)
			}
			if math.Abs(left) > EPSILON {
				t.Fatalf("expected no remaining meters, got %.4f", left)
			}

			//Same but for a prioritized path step
			left, err = comm.MoveFMeters(pubdomain.CELL_SIZE_M)
			if err != nil {
				t.Fatalf("expected exact movement to complete, got %v", err)
			}
			if math.Abs(left) > EPSILON {
				t.Fatalf("expected no remaining meters, got %.4f", left)
			}

			select {
			case <-done:
			case <-time.After(1 * time.Millisecond):
				t.Fatal("movement goroutine did not finish")
			}
		})
	})

	t.Run("MoveNCellsZeroDoesNotStartMovement", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 0)
			done := make(chan struct{})

			go func() {
				comm.waitUntilStep()
				close(done)
			}()

			time.Sleep(1 * time.Millisecond)

			if ok := comm.MoveNCells(0); !ok {
				t.Fatal("zero-step movement should be a successful no-op")
			}

			select {
			case <-done:
				t.Fatal("zero-step movement should not resume a stopped communicator")
			case <-time.After(1 * time.Millisecond):
				comm.Terminate()
			}

			select {
			case <-done:
			case <-time.After(1 * time.Millisecond):
				t.Fatal("communicator did not terminate")
			}
		})
	})

	t.Run("WaitUntilStepTimeout", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 2)
			done := make(chan struct{})

			go func() {
				comm.waitUntilStep()

				close(done)
			}()

			time.Sleep(1 * time.Millisecond)

			select {
			case <-time.After(498 * time.Millisecond):
				select {
				case <-done:

				case <-time.After(2 * time.Millisecond):
					comm.Terminate()
					t.Fatal("Goroutine should have exited")
				}

			case <-done:
				t.Fatal("Goroutine exited too early")
			}
		})
	})

	t.Run("LateTickResetsSchedule", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			comm := NewAgentCommunicator("test", 0, 2)

			comm.readVarMu.Lock()
			comm.nextTickMovementTime = time.Now().Add(-time.Second)
			comm.readVarMu.Unlock()

			if exit := comm.waitUntilStep(); exit {
				t.Fatal("late tick should allow one movement step, not exit")
			}

			if nextTick := comm.getNextTickMovementTime(); !nextTick.After(time.Now()) {
				t.Fatalf("expected next tick to be rescheduled in the future, got %v", nextTick)
			}

			done := make(chan struct{})
			go func() {
				comm.waitUntilStep()
				close(done)
			}()

			select {
			case <-time.After(498 * time.Millisecond):
				select {
				case <-done:
					t.Fatal("second wait should not spin after a late tick")
				default:
				}
			case <-done:
				t.Fatal("second wait exited too early")
			}

			select {
			case <-done:
			case <-time.After(10 * time.Millisecond):
				comm.Terminate()
				t.Fatal("second wait should complete after the refreshed interval")
			}
		})
	})
}

func TestAgentComm_PathPublishing(t *testing.T) {
	setupTestConfig(nil)

	synctest.Test(t, func(t *testing.T) {
		g := grid.NewFilled(7, 5, grid.EMPTY_SPACE_COST)
		g2 := grid.NewFilled(7, 5, grid.EMPTY_SPACE_COST)
		names := []string{"g", "g2"}
		w, err := grid.NewWorldFromGrids([]*grid.Grid{g, g2}, names)
		if err != nil {
			t.Fatal(err)
		}
		w.AddBidirectionalPortal(grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 6}, Layer: 0}, grid.GlobalCoords{Coords: grid.Coords{X: 0, Y: 6}, Layer: 1}, grid.EMPTY_SPACE_COST)

		goal := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 2}, Layer: 0}
		e := &mock.Element{X: 0, Y: 0, Z: 10, RotY: 0}
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
		d.QuitWorldSub.Close()
		comm := NewAgentCommunicator(e.Id(), 0, 2)
		t.Cleanup(comm.Terminate)

		dataReceiver1 := make(chan map[string][]pubdomain.CellState)
		comm.SetPathPublisher(dataReceiver1)

		var dataReceiver2 chan map[string][]pubdomain.CellState
		go func() {
			time.Sleep(1 * time.Millisecond)
			dataReceiver2 = make(chan map[string][]pubdomain.CellState)
			comm.SetPathPublisher(dataReceiver2)
		}()

		select {
		case _, ok := <-dataReceiver1:
			if ok {
				t.Fatal("Expected to get liberated because of the closed channel")
			}
		case <-time.After(5 * time.Millisecond):
			t.Fatal("Expected to receive closed channel")
		}

		go func() {
			t.Log(d)
			comm.publishPath(d)
		}()

		select {
		case data, ok := <-dataReceiver2:
			if !ok {
				t.Fatal("Expected to receive data and not a closed channel")
			}
			t.Logf("%#v", data)
			for floor, states := range data {
				for i, s := range states {
					if s != pubdomain.STATE_Unvisited {
						if s == pubdomain.STATE_OnPath && ((floor == names[0] && i == 2*5+4) || (floor == names[1] && i == 0)) {
							continue //They are the start and end
						}
						t.Fatalf("Expected all states to be unvisited, since we did not run the algorithm yet (%s, %d, %v)", floor, i, s)
					}
				}
			}
		case <-time.After(5 * time.Millisecond):
			t.Fatal("Expected to receive data")
		}

		d.computeShortestPath()
		t.Log(d)
		go func() {
			comm.publishPath(d)
		}()

		select {
		case data, ok := <-dataReceiver2:
			if !ok {
				t.Fatal("Expected to receive data and not a closed channel")
			}
			t.Logf("%#v", data)
			for floor, states := range data {
				for i, s := range states {
					if s != pubdomain.STATE_Unvisited {
						if s == pubdomain.STATE_OnPath && ((floor == names[0] && i == 2*5+4) || (floor == names[1] && i == 0)) {
							continue //They are the start and end
						}
						return
					}
				}
			}
			t.Fatal("Expected some cell state to not be Unvisited")
		case <-time.After(5 * time.Millisecond):
			t.Fatal("Expected to receive data")
		}

		go comm.Terminate()

		select {
		case _, ok := <-dataReceiver2:
			if ok {
				t.Fatal("Expected no data to be received and get a closed channel")
			}
		case <-time.After(5 * time.Millisecond):
			t.Fatal("Expected Terminate to free me in the select")
		}
	})
}
