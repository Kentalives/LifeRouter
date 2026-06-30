package pathfinding

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/midtxwn/geotruth/pkg/messages"

	"github.com/Kentalives/LifeRouter/pkg/domain"
	"github.com/Kentalives/LifeRouter/pkg/snapshotstream"
	"github.com/Kentalives/LifeRouter/pkg/subjects"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

const chunkedStreamTimeout = 10 * time.Second

func TestPathfindingErrors_RequestFailure(t *testing.T) {
	nc := testPathfindingNATSConn(t)
	pf := New(nc)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := pf.AgentFindPath(ctx, [3]float64{}, "agent-1", 0, 0)
	if !errors.Is(err, domain.ErrAgentFindPath) || !errors.Is(err, domain.ErrNATSRequest) {
		t.Fatalf("expected agent find path request classification, got %v", err)
	}
}

func TestPathfindingErrors_ResponseFailure(t *testing.T) {
	nc := testPathfindingNATSConn(t)
	subscribePathfindingResponder(t, nc, subjects.PathfindingAgentFindPath, messages.ErrResp(domain.ErrAgentCommNotFound))
	pf := New(nc)

	_, err := pf.AgentFindPath(context.Background(), [3]float64{}, "agent-1", 0, 0)
	if !errors.Is(err, domain.ErrAgentFindPath) || !errors.Is(err, domain.ErrNATSResponse) || !errors.Is(err, domain.ErrAgentCommNotFound) {
		t.Fatalf("expected wrapped response error, got %v", err)
	}
}

func TestPathfindingErrors_MalformedResponsePayload(t *testing.T) {
	nc := testPathfindingNATSConn(t)
	subscribePathfindingResponder(t, nc, subjects.PathfindingAgentNaivePathCost, []byte("{bad json"))
	pf := New(nc)

	_, err := pf.AgentNaivePathCost(context.Background(), [3]float64{}, "agent-1")
	if !errors.Is(err, domain.ErrAgentNaivePathCost) || !errors.Is(err, domain.ErrNATSResponse) {
		t.Fatalf("expected malformed response classification, got %v", err)
	}
}

func TestPathfindingErrors_SubscriptionSetupFailure(t *testing.T) {
	nc := testPathfindingNATSConn(t)
	nc.Close()
	pf := New(nc)

	_, err := pf.AgentPathSub(context.Background(), "agent-1")
	if !errors.Is(err, domain.ErrAgentPathSub) || !errors.Is(err, domain.ErrNATSSubscription) {
		t.Fatalf("expected path subscription setup classification, got %v", err)
	}
}

func TestPathfindingErrors_PathSubRemoteError(t *testing.T) {
	nc := testPathfindingNATSConn(t)
	subscribePathfindingResponder(t, nc, subjects.PathfindingAgentWatchPath, messages.ErrResp(domain.ErrAgentCommNotFound))
	pf := New(nc)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ch, err := pf.AgentPathSub(ctx, "agent-1")
	if ch != nil {
		t.Fatal("expected no path channel on subscription setup failure")
	}
	if !errors.Is(err, domain.ErrAgentPathSub) || !errors.Is(err, domain.ErrNATSResponse) || !errors.Is(err, domain.ErrAgentCommNotFound) {
		t.Fatalf("expected wrapped path subscription response error, got %v", err)
	}
}

func TestPathfinding_AgentPathSubHandshakeSuccess(t *testing.T) {
	nc := testPathfindingNATSConn(t)
	sub, err := nc.Subscribe(subjects.PathfindingAgentWatchPath, func(msg *nats.Msg) {
		_ = msg.Respond(messages.OKResp())
		data, err := snapshotstream.MarshalDone()
		if err != nil {
			t.Errorf("marshal done response: %v", err)
			return
		}
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	pf := New(nc)
	ch, err := pf.AgentPathSub(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("expected successful path subscription, got %v", err)
	}
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected path channel to close on done response")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for path channel to close")
	}
}

func TestPathfinding_AgentPathSubStreamsChunkedSnapshot(t *testing.T) {
	nc := testPathfindingNATSConn(t)
	want := largePathfindingHeatmap(5, 403*1010)
	payloads, err := snapshotstream.MarshalSnapshot(want)
	if err != nil {
		t.Fatalf("marshal chunked snapshot: %v", err)
	}
	donePayload, err := snapshotstream.MarshalDone()
	if err != nil {
		t.Fatalf("marshal done response: %v", err)
	}
	sub, err := nc.Subscribe(subjects.PathfindingAgentWatchPath, func(msg *nats.Msg) {
		_ = msg.Respond(messages.OKResp())
		for _, payload := range payloads {
			_ = msg.Respond(payload)
		}
		_ = msg.Respond(donePayload)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	ch, err := New(nc).AgentPathSub(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("AgentPathSub: %v", err)
	}
	select {
	case got := <-ch:
		if !reflect.DeepEqual(got, want) {
			t.Fatal("chunked heatmap did not round-trip through client")
		}
	case <-time.After(chunkedStreamTimeout):
		t.Fatal("timed out waiting for chunked heatmap")
	}
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to close after done")
		}
	case <-time.After(chunkedStreamTimeout):
		t.Fatal("timed out waiting for channel close")
	}
}

func testPathfindingNATSConn(t *testing.T) *nats.Conn {
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

func largePathfindingHeatmap(floors, cellsPerFloor int) map[string][]domain.CellState {
	heatmap := make(map[string][]domain.CellState, floors)
	var seed uint32 = 7
	for floor := 0; floor < floors; floor++ {
		cells := make([]domain.CellState, cellsPerFloor)
		for i := range cells {
			seed = seed*1664525 + 1013904223
			cells[i] = domain.CellState((seed >> 16) % 6)
		}
		heatmap[strconv.Itoa(floor)] = cells
	}
	return heatmap
}

func subscribePathfindingResponder(t *testing.T, nc *nats.Conn, subject string, data []byte) {
	t.Helper()

	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		_ = msg.Respond(data)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
}
