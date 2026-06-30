package geotruthpool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/midtxwn/geotruth/pkg/natsclient"
	"github.com/midtxwn/geotruth/pkg/natspublish"
	"github.com/midtxwn/geotruth/pkg/natsquery"

	"github.com/Kentalives/LifeRouter/pkg/domain"
)

// NormalizeSize keeps pool configuration defensive without making zero values
// fatal for callers that build config structs manually.
func NormalizeSize(size int) int {
	if size < 1 {
		return 1
	}
	return size
}

// QueryPool distributes geotruth read calls across multiple query clients.
type QueryPool struct {
	queries []domain.GeoQuery
	closers []func()
	next    atomic.Uint64
	once    sync.Once
}

// NewQueryPool wraps existing query clients. The optional closers are owned by
// the pool and are called once when Close is invoked.
func NewQueryPool(queries []domain.GeoQuery, closers []func()) (*QueryPool, error) {
	if len(queries) == 0 {
		return nil, fmt.Errorf("geotruth query pool requires at least one client")
	}
	for i, q := range queries {
		if q == nil {
			return nil, fmt.Errorf("geotruth query pool client %d is nil", i)
		}
	}
	return &QueryPool{
		queries: queries,
		closers: closers,
	}, nil
}

// NewNATSQueryPool creates a geotruth query pool backed by independent NATS
// connections.
func NewNATSQueryPool(ctx context.Context, natsURL string, size int) (*QueryPool, error) {
	size = NormalizeSize(size)

	queries := make([]domain.GeoQuery, 0, size)
	closers := make([]func(), 0, size)
	for i := 0; i < size; i++ {
		client, err := natsclient.New(ctx, natsURL)
		if err != nil {
			closeAll(closers)
			return nil, fmt.Errorf("creating geotruth query connection %d: %w", i, err)
		}
		queries = append(queries, natsquery.New(client.Conn()))
		closers = append(closers, client.Close)
	}

	return NewQueryPool(queries, closers)
}

func (p *QueryPool) pick() domain.GeoQuery {
	idx := (p.next.Add(1) - 1) % uint64(len(p.queries))
	return p.queries[idx]
}

func (p *QueryPool) NearbyObjectsOf(ctx context.Context, objectID string, radiusMeters float64, regex *string) ([]natsquery.ObjectOriented, error) {
	return p.pick().NearbyObjectsOf(ctx, objectID, radiusMeters, regex)
}

func (p *QueryPool) ObjectData(ctx context.Context, objectID string) (*natsquery.Object, error) {
	return p.pick().ObjectData(ctx, objectID)
}

func (p *QueryPool) AllObjectsOriented(ctx context.Context, regex *string) (natsquery.AllObjectsOrientedResp, error) {
	return p.pick().AllObjectsOriented(ctx, regex)
}

func (p *QueryPool) RegionFromPoint(ctx context.Context, x, y, z float64) (string, error) {
	return p.pick().RegionFromPoint(ctx, x, y, z)
}

// Close releases every owned query connection once.
func (p *QueryPool) Close() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		closeAll(p.closers)
	})
}

// PublishPool distributes geotruth write calls across multiple publish clients.
type PublishPool struct {
	publishers []domain.GeoPublish
	closers    []func()
	next       atomic.Uint64
	once       sync.Once
}

// NewPublishPool wraps existing publish clients. The optional closers are owned
// by the pool and are called once when Close is invoked.
func NewPublishPool(publishers []domain.GeoPublish, closers []func()) (*PublishPool, error) {
	if len(publishers) == 0 {
		return nil, fmt.Errorf("geotruth publish pool requires at least one client")
	}
	for i, p := range publishers {
		if p == nil {
			return nil, fmt.Errorf("geotruth publish pool client %d is nil", i)
		}
	}
	return &PublishPool{
		publishers: publishers,
		closers:    closers,
	}, nil
}

// NewNATSPublishPool creates a geotruth publish pool backed by independent
// NATS connections.
func NewNATSPublishPool(ctx context.Context, natsURL string, size int) (*PublishPool, error) {
	size = NormalizeSize(size)

	publishers := make([]domain.GeoPublish, 0, size)
	closers := make([]func(), 0, size)
	for i := 0; i < size; i++ {
		client, err := natsclient.New(ctx, natsURL)
		if err != nil {
			closeAll(closers)
			return nil, fmt.Errorf("creating geotruth publish connection %d: %w", i, err)
		}
		publishers = append(publishers, natspublish.New(client.Conn()))
		closers = append(closers, client.Close)
	}

	return NewPublishPool(publishers, closers)
}

func (p *PublishPool) pick() domain.GeoPublish {
	idx := (p.next.Add(1) - 1) % uint64(len(p.publishers))
	return p.publishers[idx]
}

func (p *PublishPool) UpdateObjectPosition(ctx context.Context, objectID string, x, y, z, rotY float64) (natspublish.CommitAck, error) {
	return p.pick().UpdateObjectPosition(ctx, objectID, x, y, z, rotY)
}

// Close releases every owned publish connection once.
func (p *PublishPool) Close() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		closeAll(p.closers)
	})
}

func closeAll(closers []func()) {
	for _, closeFn := range closers {
		if closeFn != nil {
			closeFn()
		}
	}
}
