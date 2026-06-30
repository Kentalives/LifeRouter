package emergency

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

func TestEmergencyErrors_RequestFailure(t *testing.T) {
	nc := testEmergencyNATSConn(t)
	em := New(nc)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := em.Start(ctx, nil, nil, 0)
	if !errors.Is(err, domain.ErrEmergencyStart) || !errors.Is(err, domain.ErrNATSRequest) {
		t.Fatalf("expected emergency start request classification, got %v", err)
	}
}

func TestEmergencyErrors_ResponseFailure(t *testing.T) {
	nc := testEmergencyNATSConn(t)
	subscribeEmergencyResponder(t, nc, subjects.EmergencyStop, messages.ErrResp(domain.ErrEmergencyStillRunning))
	em := New(nc)

	err := em.Stop(context.Background())
	if !errors.Is(err, domain.ErrEmergencyStop) || !errors.Is(err, domain.ErrNATSResponse) || !errors.Is(err, domain.ErrEmergencyStillRunning) {
		t.Fatalf("expected wrapped emergency stop response error, got %v", err)
	}
}

func TestEmergencyErrors_MalformedResponsePayload(t *testing.T) {
	nc := testEmergencyNATSConn(t)
	subscribeEmergencyResponder(t, nc, subjects.EmergencyStart, []byte("{bad json"))
	em := New(nc)

	err := em.Start(context.Background(), nil, nil, 0)
	if !errors.Is(err, domain.ErrEmergencyStart) || !errors.Is(err, domain.ErrNATSResponse) {
		t.Fatalf("expected malformed emergency start response classification, got %v", err)
	}
}

func TestEmergencyErrors_FlowSubSetupFailure(t *testing.T) {
	nc := testEmergencyNATSConn(t)
	nc.Close()
	em := New(nc)

	_, err := em.FlowSub(context.Background())
	if !errors.Is(err, domain.ErrEmergencyFlowSub) || !errors.Is(err, domain.ErrNATSSubscription) {
		t.Fatalf("expected flow subscription setup classification, got %v", err)
	}
}

func TestEmergencyErrors_FlowSubIgnoresMalformedStreamMessage(t *testing.T) {
	nc := testEmergencyNATSConn(t)
	sub, err := nc.Subscribe(subjects.EmergencyFlowWatch, func(msg *nats.Msg) {
		_ = nc.Publish(msg.Reply, []byte("{bad json"))
		_ = nc.Publish(msg.Reply, []byte(`{"done":true}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	em := New(nc)

	ch, err := em.FlowSub(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected malformed message to be ignored until done closes the channel")
		}
	case <-time.After(time.Second):
		t.Fatal("flow subscription did not close after done message")
	}
}

func TestEmergencyFlowSubStreamsChunkedSnapshot(t *testing.T) {
	nc := testEmergencyNATSConn(t)
	want := largeEmergencyFlow(5, 403*1010)
	payloads, err := snapshotstream.MarshalSnapshot(want)
	if err != nil {
		t.Fatalf("marshal chunked flow: %v", err)
	}
	donePayload, err := snapshotstream.MarshalDone()
	if err != nil {
		t.Fatalf("marshal done response: %v", err)
	}
	sub, err := nc.Subscribe(subjects.EmergencyFlowWatch, func(msg *nats.Msg) {
		for _, payload := range payloads {
			_ = nc.Publish(msg.Reply, payload)
		}
		_ = nc.Publish(msg.Reply, donePayload)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	if err := nc.Flush(); err != nil {
		t.Fatal(err)
	}
	ch, err := New(nc).FlowSub(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		if !reflect.DeepEqual(got, want) {
			t.Fatal("chunked flow did not round-trip through client")
		}
	case <-time.After(chunkedStreamTimeout):
		t.Fatal("timed out waiting for chunked flow")
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
func largeEmergencyFlow(floors, cellsPerFloor int) map[string][]domain.Direction {
	flow := make(map[string][]domain.Direction, floors)
	var seed uint32 = 13
	for floor := 0; floor < floors; floor++ {
		cells := make([]domain.Direction, cellsPerFloor)
		for i := range cells {
			seed = seed*1664525 + 1013904223
			cells[i] = domain.Direction((seed >> 16) % 11)
		}
		flow[strconv.Itoa(floor)] = cells
	}
	return flow
}

func testEmergencyNATSConn(t *testing.T) *nats.Conn {
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

func subscribeEmergencyResponder(t *testing.T, nc *nats.Conn, subject string, data []byte) {
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
