package geotruthpool

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/midtxwn/geotruth/pkg/natspublish"
	"github.com/midtxwn/geotruth/pkg/natsquery"

	"github.com/Kentalives/LifeRouter/pkg/domain"
)

type fakeQuery struct {
	idx   int
	calls []atomic.Int64
}

func (f fakeQuery) NearbyObjectsOf(context.Context, string, float64, *string) ([]natsquery.ObjectOriented, error) {
	f.calls[f.idx].Add(1)
	return nil, nil
}

func (f fakeQuery) ObjectData(context.Context, string) (*natsquery.Object, error) {
	f.calls[f.idx].Add(1)
	return nil, nil
}

func (f fakeQuery) AllObjectsOriented(context.Context, *string) (natsquery.AllObjectsOrientedResp, error) {
	f.calls[f.idx].Add(1)
	return natsquery.AllObjectsOrientedResp{}, nil
}

func (f fakeQuery) RegionFromPoint(context.Context, float64, float64, float64) (string, error) {
	f.calls[f.idx].Add(1)
	return "", nil
}

type fakePublisher struct {
	idx   int
	calls []atomic.Int64
}

func (f fakePublisher) UpdateObjectPosition(context.Context, string, float64, float64, float64, float64) (natspublish.CommitAck, error) {
	f.calls[f.idx].Add(1)
	return natspublish.CommitAck{InstanceID: "test", CommitSeq: 1}, nil
}

func TestNormalizeSize(t *testing.T) {
	if got := NormalizeSize(0); got != 1 {
		t.Fatalf("NormalizeSize(0) = %d, want 1", got)
	}
	if got := NormalizeSize(-5); got != 1 {
		t.Fatalf("NormalizeSize(-5) = %d, want 1", got)
	}
	if got := NormalizeSize(4); got != 4 {
		t.Fatalf("NormalizeSize(4) = %d, want 4", got)
	}
}

func TestQueryPoolRoundRobin(t *testing.T) {
	var calls [3]atomic.Int64
	pool, err := NewQueryPool([]domain.GeoQuery{
		fakeQuery{idx: 0, calls: calls[:]},
		fakeQuery{idx: 1, calls: calls[:]},
		fakeQuery{idx: 2, calls: calls[:]},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	for range 7 {
		if _, err := pool.NearbyObjectsOf(context.Background(), "agent", 1, nil); err != nil {
			t.Fatal(err)
		}
	}

	want := []int64{3, 2, 2}
	for i, wantCalls := range want {
		if got := calls[i].Load(); got != wantCalls {
			t.Fatalf("client %d calls = %d, want %d", i, got, wantCalls)
		}
	}
}

func TestPublishPoolRoundRobin(t *testing.T) {
	var calls [2]atomic.Int64
	pool, err := NewPublishPool([]domain.GeoPublish{
		fakePublisher{idx: 0, calls: calls[:]},
		fakePublisher{idx: 1, calls: calls[:]},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	for range 5 {
		if _, err := pool.UpdateObjectPosition(context.Background(), "agent", 1, 2, 3, 4); err != nil {
			t.Fatal(err)
		}
	}

	want := []int64{3, 2}
	for i, wantCalls := range want {
		if got := calls[i].Load(); got != wantCalls {
			t.Fatalf("client %d calls = %d, want %d", i, got, wantCalls)
		}
	}
}

func TestPoolsCloseOwnedResourcesOnce(t *testing.T) {
	var queryCalls [1]atomic.Int64
	var publishCalls [1]atomic.Int64
	var closes atomic.Int64

	queryPool, err := NewQueryPool([]domain.GeoQuery{fakeQuery{idx: 0, calls: queryCalls[:]}}, []func(){func() { closes.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}
	publishPool, err := NewPublishPool([]domain.GeoPublish{fakePublisher{idx: 0, calls: publishCalls[:]}}, []func(){func() { closes.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}

	queryPool.Close()
	queryPool.Close()
	publishPool.Close()
	publishPool.Close()

	if got := closes.Load(); got != 2 {
		t.Fatalf("closers called %d times, want 2", got)
	}
}
