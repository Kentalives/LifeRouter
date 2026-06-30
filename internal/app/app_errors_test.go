package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/midtxwn/geotruth/pkg/messages"

	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/agent"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
	"github.com/Kentalives/LifeRouter/pkg/subjects"
	"github.com/nats-io/nats.go"
)

func TestHandlerErrors_MalformedJSON(t *testing.T) {
	nc := testNATSConn(t)
	disp := &Dispatcher{
		nc:                    nc,
		tab:                   make(map[string]*agent.AgentCommunicator),
		completedAgentResults: make(map[string]agentExitResult),
	}
	mustSubscribe(t, nc, subjects.AgentCommIsMoving, disp.handleIsMoving)
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}

	resp := requestHandlerError(t, nc, subjects.AgentCommIsMoving, []byte("{bad json"))
	if resp == nil {
		t.Fatal("expected malformed JSON to return an error")
	}
}

func TestHandlerErrors_MissingCommunicator(t *testing.T) {
	nc := testNATSConn(t)
	disp := &Dispatcher{
		nc:                    nc,
		tab:                   make(map[string]*agent.AgentCommunicator),
		completedAgentResults: make(map[string]agentExitResult),
	}
	handlersBySubject := map[string]nats.MsgHandler{
		subjects.AgentCommIsMoving:         disp.handleIsMoving,
		subjects.AgentCommTerminate:        disp.handleTerminate,
		subjects.AgentCommStop:             disp.handleStop,
		subjects.AgentCommMoveNCells:       disp.handleMoveNCells,
		subjects.PathfindingAgentWatchPath: disp.handleAgentPathSub,
	}
	for subject, handler := range handlersBySubject {
		mustSubscribe(t, nc, subject, handler)
	}
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}

	idData, err := json.Marshal(IdentifierParams{Id: "missing-agent"})
	if err != nil {
		t.Fatal(err)
	}
	moveData, err := json.Marshal(MoveNCellsParams{Id: "missing-agent", N: 1})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		subject string
		data    []byte
	}{
		{"IsMoving", subjects.AgentCommIsMoving, idData},
		{"Terminate", subjects.AgentCommTerminate, idData},
		{"Stop", subjects.AgentCommStop, idData},
		{"MoveNCells", subjects.AgentCommMoveNCells, moveData},
		{"PathSub", subjects.PathfindingAgentWatchPath, idData},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := requestHandlerError(t, nc, tc.subject, tc.data)
			if !errors.Is(err, pubdomain.ErrAgentCommNotFound) {
				t.Fatalf("expected ErrAgentCommNotFound, got %v", err)
			}
		})
	}
}

func TestHandler_AgentPathSubSendsOKHandshake(t *testing.T) {
	nc := testNATSConn(t)
	if _, err := grid.NewWorldFromGrids([]*grid.Grid{grid.NewFilled(2, 2, grid.EMPTY_SPACE_COST)}, []string{"0"}); err != nil {
		t.Fatal(err)
	}

	id := "agent-1"
	comm := agent.NewAgentCommunicator(id, 0, 0)
	disp := &Dispatcher{
		nc:                    nc,
		tab:                   map[string]*agent.AgentCommunicator{id: comm},
		completedAgentResults: make(map[string]agentExitResult),
	}
	mustSubscribe(t, nc, subjects.PathfindingAgentWatchPath, disp.handleAgentPathSub)
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(IdentifierParams{Id: id})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := nc.RequestWithContext(ctx, subjects.PathfindingAgentWatchPath, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := messages.Err(resp.Data); err != nil {
		t.Fatalf("expected OK subscription handshake, got %v", err)
	}

	comm.SetPathPublisher(make(chan map[string][]pubdomain.CellState))
}

func TestHandlerErrors_AgentFindPathRejectsShuttingDownDispatcher(t *testing.T) {
	nc := testNATSConn(t)
	disp := &Dispatcher{
		nc:                    nc,
		tab:                   make(map[string]*agent.AgentCommunicator),
		completedAgentResults: make(map[string]agentExitResult),
		state:                 dispatcher_ShuttingDown,
	}
	mustSubscribe(t, nc, subjects.PathfindingAgentFindPath, disp.handleAgentFindPath)
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(AgentFindPathParams{AgentId: "agent-1"})
	if err != nil {
		t.Fatal(err)
	}

	err = requestHandlerError(t, nc, subjects.PathfindingAgentFindPath, data)
	if !errors.Is(err, pubdomain.ErrDispatcherShuttingDown) {
		t.Fatalf("expected dispatcher shutdown error, got %v", err)
	}
}

func TestHandlerErrors_EmergencyStartRejectsRunningEmergency(t *testing.T) {
	nc := testNATSConn(t)
	disp := &Dispatcher{
		nc: nc,
		emergency: emergencyManager{
			state: emergency_Running,
		},
	}
	mustSubscribe(t, nc, subjects.EmergencyStart, disp.handleEmergencyStart)
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(EmergencyPathParams{})
	if err != nil {
		t.Fatal(err)
	}

	err = requestHandlerError(t, nc, subjects.EmergencyStart, data)
	if !errors.Is(err, pubdomain.ErrEmergencyStillRunning) {
		t.Fatalf("expected emergency running error, got %v", err)
	}
}

func TestHandlerErrors_EmergencyStopIsIdempotentWhenStopped(t *testing.T) {
	nc := testNATSConn(t)
	disp := &Dispatcher{nc: nc}
	mustSubscribe(t, nc, subjects.EmergencyStop, disp.handleEmergencyStop)
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}

	if err := requestHandlerError(t, nc, subjects.EmergencyStop, nil); err != nil {
		t.Fatalf("expected stopped emergency stop to succeed, got %v", err)
	}
}

func requestHandlerError(t *testing.T, nc *nats.Conn, subject string, data []byte) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := nc.RequestWithContext(ctx, subject, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := messages.Err(resp.Data); err != nil {
		return err
	}
	if _, err := messages.Data[struct{}](resp.Data); err != nil {
		return err
	}
	return nil
}
