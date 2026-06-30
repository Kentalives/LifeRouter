package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/midtxwn/geotruth/pkg/messages"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/mock"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/agent"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
	"github.com/Kentalives/LifeRouter/pkg/subjects"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// Aux
func testNATSConn(t *testing.T) *nats.Conn {
	t.Helper()

	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	if !s.ReadyForConnections(time.Second) {
		t.Fatal("nats server did not start")
	}
	t.Cleanup(s.Shutdown)

	nc, err := nats.Connect(s.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func mustSubscribe(t *testing.T, nc *nats.Conn, subject string, handler nats.MsgHandler) {
	t.Helper()

	sub, err := nc.Subscribe(subject, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
}

func waitEmergencyState(t *testing.T, disp *Dispatcher, want emergencyState, timeout time.Duration) {
	t.Helper()

	deadline := time.After(timeout)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		disp.emergency.mu.Lock()
		got := disp.emergency.state
		disp.emergency.mu.Unlock()

		if got == want {
			return
		}

		select {
		case <-deadline:
			t.Fatalf("timed out waiting for emergency state %v, last state %v", want, got)
		case <-ticker.C:
		}
	}
}

func setupAppPathfindingTestConfig(t *testing.T, objectCount int) *mock.GeoTruth {
	t.Helper()

	oldCfg, oldDep := config.Cfg, config.Dep
	t.Cleanup(func() { config.Setup(oldCfg, oldDep) })

	ex, err := mock.NewExternalSystem([]string{"0"}, []float64{0}, nil)
	if err != nil {
		t.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(objectCount, ex)
	config.Setup(&pubconfig.Config{
		Grid: pubconfig.GridConfig{
			CellSizeM:     0.2,
			WallAuraCells: 0,
		},
		Pathfinding: pubconfig.PathfindingConfig{
			FreeMovementHeight: 2,
			Emergency: pubconfig.EmergencyConfig{
				LightingHystheresis:    10,
				PriorityPathCellsWidth: 1,
			},
			Agent: pubconfig.AgentConfig{
				VisionRadiusCells:  1,
				CellsForRealUpdate: 5,
			},
		},
	}, &pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	return geoTruth
}

/////

func TestDispatcherRemoveCommOnlyRemovesExpectedInstance(t *testing.T) {
	disp := &Dispatcher{
		tab: make(map[string]*agent.AgentCommunicator),
	}

	oldComm := agent.NewAgentCommunicator("agent-1", 0, 0)
	newComm := agent.NewAgentCommunicator("agent-1", 0, 0)
	t.Cleanup(oldComm.Terminate)
	t.Cleanup(newComm.Terminate)

	if prev := disp.addComm("agent-1", oldComm); prev != nil {
		t.Fatal("first registration should not replace an existing communicator")
	}
	if prev := disp.addComm("agent-1", newComm); prev != oldComm {
		t.Fatal("second registration should return the previous communicator")
	}

	disp.removeComm("agent-1", oldComm)
	if got, ok := disp.dispatch("agent-1"); !ok || got != newComm {
		t.Fatal("old communicator completion removed the newer communicator")
	}

	disp.removeComm("agent-1", newComm)
	if _, ok := disp.dispatch("agent-1"); ok {
		t.Fatal("current communicator was not removed")
	}
}

func TestDispatcherCompletedExitResult(t *testing.T) {
	disp := &Dispatcher{
		tab:                   make(map[string]*agent.AgentCommunicator),
		completedAgentResults: make(map[string]agentExitResult),
		resultTTL:             20 * time.Millisecond,
	}

	comm := agent.NewAgentCommunicator("agent-1", 0, 0)
	comm.Terminate()
	disp.addComm("agent-1", comm)
	disp.completeComm("agent-1", comm)

	if _, ok := disp.dispatch("agent-1"); ok {
		t.Fatal("completed communicator should be removed from live table")
	}
	if err, ok := disp.completedExitError("agent-1"); !ok || !errors.Is(err, pubdomain.ErrAgentTerminated) {
		t.Fatalf("expected retained termination error, got ok=%v err=%v", ok, err)
	}

	time.Sleep(50 * time.Millisecond)
	if err, ok := disp.completedExitError("agent-1"); ok {
		t.Fatalf("expected retained result to expire, got %v", err)
	}
}

func TestDispatcherCompleteCommIgnoresReplacedInstance(t *testing.T) {
	disp := &Dispatcher{
		tab:                   make(map[string]*agent.AgentCommunicator),
		completedAgentResults: make(map[string]agentExitResult),
		resultTTL:             time.Second,
	}

	oldComm := agent.NewAgentCommunicator("agent-1", 0, 0)
	newComm := agent.NewAgentCommunicator("agent-1", 0, 0)
	oldComm.Terminate()

	disp.addComm("agent-1", oldComm)
	disp.addComm("agent-1", newComm)
	disp.completeComm("agent-1", oldComm)

	if got, ok := disp.dispatch("agent-1"); !ok || got != newComm {
		t.Fatal("old communicator completion should not remove the newer communicator")
	}
	if err, ok := disp.completedExitError("agent-1"); ok {
		t.Fatalf("old communicator completion should not store a result, got %v", err)
	}
}

func TestDispatcherCompletedExitResultOverwriteIgnoresOldTimer(t *testing.T) {
	disp := &Dispatcher{
		tab:                   make(map[string]*agent.AgentCommunicator),
		completedAgentResults: make(map[string]agentExitResult),
		resultTTL:             40 * time.Millisecond,
	}

	first := agent.NewAgentCommunicator("agent-1", 0, 0)
	first.Terminate()
	disp.addComm("agent-1", first)
	disp.completeComm("agent-1", first)

	time.Sleep(20 * time.Millisecond)

	second := agent.NewAgentCommunicator("agent-1", 0, 0)
	disp.addComm("agent-1", second)
	disp.completeComm("agent-1", second)

	time.Sleep(30 * time.Millisecond)
	if err, ok := disp.completedExitError("agent-1"); !ok || err != nil {
		t.Fatalf("expected newer nil result to survive older timer, got ok=%v err=%v", ok, err)
	}

	time.Sleep(30 * time.Millisecond)
	if err, ok := disp.completedExitError("agent-1"); ok {
		t.Fatalf("expected newer retained result to expire, got %v", err)
	}
}

func TestDispatcherHandlersUseRetainedExitResult(t *testing.T) {
	nc := testNATSConn(t)
	disp := &Dispatcher{
		nc:                    nc,
		tab:                   make(map[string]*agent.AgentCommunicator),
		completedAgentResults: make(map[string]agentExitResult),
		resultTTL:             time.Second,
	}
	disp.completedAgentResults["agent-1"] = agentExitResult{
		err:       pubdomain.ErrAgentNoPath,
		expiresAt: time.Now().Add(time.Second),
	}

	mustSubscribe(t, nc, subjects.AgentCommIsMoving, disp.handleIsMoving)
	mustSubscribe(t, nc, subjects.AgentCommMoveFMeters, disp.handleMoveFMeters)
	mustSubscribe(t, nc, subjects.AgentCommBlockingWait, disp.handleBlockingWait)
	mustSubscribe(t, nc, subjects.AgentCommExitError, disp.handleExitError)
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	idData, err := json.Marshal(IdentifierParams{Id: "agent-1"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := nc.RequestWithContext(ctx, subjects.AgentCommIsMoving, idData)
	if err != nil {
		t.Fatal(err)
	}
	moving, err := messages.Data[bool](resp.Data)
	if err != nil {
		t.Fatal(err)
	}
	if moving {
		t.Fatal("retained completed agent should report not moving")
	}

	moveData, err := json.Marshal(FloatParams{Id: "agent-1", Float: 1})
	if err != nil {
		t.Fatal(err)
	}
	resp, err = nc.RequestWithContext(ctx, subjects.AgentCommMoveFMeters, moveData)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := messages.Data[float64](resp.Data); !errors.Is(err, pubdomain.ErrAgentExitedWithMetersLeft) {
		t.Fatalf("expected retained completed agent to reject MoveFMeters with exited error, got %v", err)
	}

	replySubj := nc.NewRespInbox()
	done := make(chan struct{})
	sub, err := nc.Subscribe(replySubj, func(*nats.Msg) {
		close(done)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	if err := nc.PublishRequest(subjects.AgentCommBlockingWait, replySubj, idData); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retained completed agent should immediately satisfy BlockingWait")
	}

	resp, err = nc.RequestWithContext(ctx, subjects.AgentCommExitError, idData)
	if err != nil {
		t.Fatal(err)
	}
	if err := messages.Err(resp.Data); !errors.Is(err, pubdomain.ErrAgentNoPath) {
		t.Fatalf("expected retained no-path exit error, got %v", err)
	}
}

func TestDispatcherShutdownRacesWithEmergencyStop(t *testing.T) {
	const timeout = 5 * time.Second

	setupAppPathfindingTestConfig(t, 0)

	if _, err := grid.NewWorldFromGrids([]*grid.Grid{grid.NewFilled(2, 2, grid.EMPTY_SPACE_COST)}, []string{"0"}); err != nil {
		t.Fatal(err)
	}

	serverNC := testNATSConn(t)
	clientNC, err := nats.Connect(serverNC.ConnectedUrl())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(clientNC.Close)

	ctx, cancel := context.WithCancel(context.Background())
	quit := make(chan struct{})
	done := make(chan struct{})
	disp := &Dispatcher{
		ctx:    ctx,
		cancel: cancel,
		nc:     serverNC,
		emergency: emergencyManager{
			quit:     quit,
			done:     done,
			stopDone: make(chan struct{}),
			state:    emergency_Running,
		},
	}
	mustSubscribe(t, serverNC, subjects.EmergencyStop, disp.handleEmergencyStop)
	if err := serverNC.Flush(); err != nil {
		t.Fatal(err)
	}

	replySubj := clientNC.NewRespInbox()
	stopDone := make(chan error, 1)
	sub, err := clientNC.Subscribe(replySubj, func(msg *nats.Msg) {
		stopDone <- messages.Err(msg.Data)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	if err := clientNC.PublishRequest(subjects.EmergencyStop, replySubj, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-quit:
	case <-time.After(timeout):
		t.Fatal("emergency stop handler did not close quit")
	}
	waitEmergencyState(t, disp, emergency_Stopping, timeout)

	shutdownDone := make(chan struct{})
	go func() {
		disp.Shutdown()
		close(shutdownDone)
	}()
	waitEmergencyState(t, disp, emergency_Cleanup, timeout)

	close(done)

	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("stop response returned error: %v", err)
		}
	case <-time.After(timeout):
		t.Fatal("emergency stop handler did not respond")
	}

	select {
	case <-shutdownDone:
	case <-time.After(timeout):
		t.Fatal("shutdown did not finish")
	}

	disp.emergency.mu.Lock()
	defer disp.emergency.mu.Unlock()
	if disp.emergency.state != emergency_Stopped {
		t.Fatalf("expected stopped emergency state, got %v", disp.emergency.state)
	}
	if disp.emergency.done != nil {
		t.Fatal("expected shutdown to clear emergency done channel")
	}
	if disp.emergency.stopDone != nil {
		t.Fatal("expected shutdown to clear emergency stopDone channel")
	}
}

func TestDispatcherEmergencyStopIsIdempotentWhileStopping(t *testing.T) {
	const timeout = 5 * time.Second

	setupAppPathfindingTestConfig(t, 0)

	if _, err := grid.NewWorldFromGrids([]*grid.Grid{grid.NewFilled(2, 2, grid.EMPTY_SPACE_COST)}, []string{"0"}); err != nil {
		t.Fatal(err)
	}

	serverNC := testNATSConn(t)
	clientNC, err := nats.Connect(serverNC.ConnectedUrl())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(clientNC.Close)

	quit := make(chan struct{})
	done := make(chan struct{})
	disp := &Dispatcher{
		nc: serverNC,
		emergency: emergencyManager{
			quit:     quit,
			done:     done,
			stopDone: make(chan struct{}),
			state:    emergency_Running,
		},
	}
	mustSubscribe(t, serverNC, subjects.EmergencyStop, disp.handleEmergencyStop)
	if err := serverNC.Flush(); err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		resp, err := clientNC.RequestWithContext(ctx, subjects.EmergencyStop, nil)
		if err != nil {
			firstDone <- err
			return
		}
		firstDone <- messages.Err(resp.Data)
	}()

	select {
	case <-quit:
	case <-time.After(timeout):
		t.Fatal("first emergency stop did not close quit")
	}
	waitEmergencyState(t, disp, emergency_Stopping, timeout)

	secondDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		resp, err := clientNC.RequestWithContext(ctx, subjects.EmergencyStop, nil)
		if err != nil {
			secondDone <- err
			return
		}
		secondDone <- messages.Err(resp.Data)
	}()

	select {
	case err := <-secondDone:
		t.Fatalf("second emergency stop returned before active stop completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(done)

	for name, ch := range map[string]<-chan error{"first": firstDone, "second": secondDone} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("%s emergency stop returned error: %v", name, err)
			}
		case <-time.After(timeout):
			t.Fatalf("%s emergency stop did not return", name)
		}
	}

	disp.emergency.mu.Lock()
	defer disp.emergency.mu.Unlock()
	if disp.emergency.state != emergency_Stopped {
		t.Fatalf("expected stopped emergency state, got %v", disp.emergency.state)
	}
	if disp.emergency.done != nil {
		t.Fatal("expected emergency done channel to be cleared")
	}
	if disp.emergency.stopDone != nil {
		t.Fatal("expected emergency stopDone channel to be cleared")
	}
}

func TestDispatcherShutdownTerminatesActiveAgents(t *testing.T) {
	const timeout = 5 * time.Second

	geoTruth := setupAppPathfindingTestConfig(t, 2)
	if _, err := grid.NewWorldFromGrids([]*grid.Grid{grid.NewFilled(5, 5, grid.EMPTY_SPACE_COST)}, []string{"0"}); err != nil {
		t.Fatal(err)
	}

	agents := []*mock.Element{
		{IdName: "Agent_shutdown_1", X: 0, Y: 0, Z: 0, Width: pubdomain.CELL_SIZE_M, Height: pubdomain.CELL_SIZE_M},
		{IdName: "Agent_shutdown_2", X: 0, Y: 0, Z: 0, Width: pubdomain.CELL_SIZE_M, Height: pubdomain.CELL_SIZE_M},
	}
	for _, a := range agents {
		if err := geoTruth.AddObject(a); err != nil {
			t.Fatal(err)
		}
	}

	goal := grid.GlobalCoords{Coords: grid.Coords{X: 4, Y: 4}, Layer: 0}
	comm1, err := agent.InternalAgentFindPath(grid.NewVirtualWorld(), goal, agents[0].Id(), 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	comm2, err := agent.InternalAgentFindPath(grid.NewVirtualWorld(), goal, agents[1].Id(), 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}

	nc := testNATSConn(t)
	ctx, cancel := context.WithCancel(context.Background())
	disp := &Dispatcher{
		ctx:                   ctx,
		cancel:                cancel,
		nc:                    nc,
		tab:                   make(map[string]*agent.AgentCommunicator),
		completedAgentResults: make(map[string]agentExitResult),
	}
	if prev, ok := disp.addCommIfRunning(agents[0].Id(), comm1); !ok || prev != nil {
		t.Fatalf("first agent registration got prev=%v ok=%v", prev, ok)
	}
	if prev, ok := disp.addCommIfRunning(agents[1].Id(), comm2); !ok || prev != nil {
		t.Fatalf("second agent registration got prev=%v ok=%v", prev, ok)
	}

	shutdownDone := make(chan struct{})
	go func() {
		disp.Shutdown()
		close(shutdownDone)
	}()

	for _, comm := range []*agent.AgentCommunicator{comm1, comm2} {
		select {
		case <-comm.BlockingWait():
		case <-time.After(timeout):
			t.Fatal("shutdown did not terminate active agent")
		}
	}

	select {
	case <-shutdownDone:
	case <-time.After(timeout):
		t.Fatal("shutdown did not finish after terminating agents")
	}

	disp.mu.RLock()
	defer disp.mu.RUnlock()
	if len(disp.tab) != 0 {
		t.Fatalf("expected shutdown to clear agent table, got %d entries", len(disp.tab))
	}
	if len(disp.completedAgentResults) != 0 {
		t.Fatalf("expected shutdown to clear completed agent results, got %d entries", len(disp.completedAgentResults))
	}
	if disp.state != dispatcher_Stopped {
		t.Fatalf("expected stopped dispatcher state, got %v", disp.state)
	}
}

func TestDispatcherRejectsAgentRegistrationDuringShutdown(t *testing.T) {
	disp := &Dispatcher{
		tab:                   make(map[string]*agent.AgentCommunicator),
		completedAgentResults: make(map[string]agentExitResult),
		state:                 dispatcher_ShuttingDown,
	}
	comm := agent.NewAgentCommunicator("Agent_shutdown_reject", 0, 0)
	t.Cleanup(comm.Terminate)

	if prev, ok := disp.addCommIfRunning("Agent_shutdown_reject", comm); ok || prev != nil {
		t.Fatalf("expected registration rejection during shutdown, got prev=%v ok=%v", prev, ok)
	}
	if len(disp.tab) != 0 {
		t.Fatalf("rejected communicator entered table: %d entries", len(disp.tab))
	}
}
