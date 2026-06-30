package pathfinding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/midtxwn/geotruth/pkg/messages"

	"github.com/Kentalives/LifeRouter/pkg/domain"
	"github.com/Kentalives/LifeRouter/pkg/subjects"
)

func TestAgentCommErrors_RequestFailure(t *testing.T) {
	nc := testPathfindingNATSConn(t)
	comm := &AgentCommunicator{nc: nc, id: "agent-1"}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := comm.Terminate(ctx)
	if !errors.Is(err, domain.ErrAgentCommTerminate) || !errors.Is(err, domain.ErrNATSRequest) {
		t.Fatalf("expected terminate request classification, got %v", err)
	}
}

func TestAgentCommErrors_ResponseFailure(t *testing.T) {
	nc := testPathfindingNATSConn(t)
	subscribePathfindingResponder(t, nc, subjects.AgentCommStop, messages.ErrResp(domain.ErrAgentCommNotFound))
	comm := &AgentCommunicator{nc: nc, id: "agent-1"}

	err := comm.Stop(context.Background())
	if !errors.Is(err, domain.ErrAgentCommStop) || !errors.Is(err, domain.ErrNATSResponse) || !errors.Is(err, domain.ErrAgentCommNotFound) {
		t.Fatalf("expected wrapped stop response error, got %v", err)
	}
}

func TestAgentCommErrors_MalformedResponsePayload(t *testing.T) {
	nc := testPathfindingNATSConn(t)
	subscribePathfindingResponder(t, nc, subjects.AgentCommIsMoving, []byte("{bad json"))
	comm := &AgentCommunicator{nc: nc, id: "agent-1"}

	_, err := comm.IsMoving(context.Background())
	if !errors.Is(err, domain.ErrAgentCommIsMoving) || !errors.Is(err, domain.ErrNATSResponse) {
		t.Fatalf("expected malformed isMoving response classification, got %v", err)
	}
}

func TestAgentCommErrors_DataResponseFailure(t *testing.T) {
	nc := testPathfindingNATSConn(t)
	subscribePathfindingResponder(t, nc, subjects.AgentCommMoveFMeters, messages.ErrResp(domain.ErrAgentExitedWithMetersLeft))
	comm := &AgentCommunicator{nc: nc, id: "agent-1"}

	remaining, err := comm.MoveFMeters(context.Background(), 2)
	if remaining != 2 {
		t.Fatalf("remaining meters = %.1f, want original request value", remaining)
	}
	if !errors.Is(err, domain.ErrAgentCommMoveFMeters) || !errors.Is(err, domain.ErrNATSResponse) || !errors.Is(err, domain.ErrAgentExitedWithMetersLeft) {
		t.Fatalf("expected wrapped moveFMeters response error, got %v", err)
	}
}

func TestAgentCommErrors_BlockingWaitSetupFailure(t *testing.T) {
	nc := testPathfindingNATSConn(t)
	nc.Close()
	comm := &AgentCommunicator{nc: nc, id: "agent-1"}

	_, err := comm.BlockingWait()
	if !errors.Is(err, domain.ErrAgentCommBlockingWait) || !errors.Is(err, domain.ErrNATSSubscription) {
		t.Fatalf("expected blocking wait setup classification, got %v", err)
	}
}
