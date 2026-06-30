package performance

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	embeddedGeo "github.com/midtxwn/geotruth/embedded"
	"github.com/midtxwn/geotruth/pkg/natsclient"
	"github.com/midtxwn/geotruth/pkg/natspublish"
	"github.com/midtxwn/geotruth/pkg/natsquery"

	"github.com/Kentalives/LifeRouter/embedded"
	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/geotruthpool"
	"github.com/Kentalives/LifeRouter/internal/mock"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

const realCapacityFloorHeight = 4.0

type realCapacityGeoTruth struct {
	query     pubdomain.GeoQuery
	publish   pubdomain.GeoPublish
	registrar natspublish.Publish

	includeAgents bool

	mu      sync.RWMutex
	updates map[string][]time.Time
}

func requireRealBench(b testing.TB) {
	b.Helper()
	if os.Getenv("PATHFINDING_BENCH_REAL") != "1" {
		b.Skip("set PATHFINDING_BENCH_REAL=1 to run real geotruth capacity benchmarks")
	}
}

func realCapacityLayerNamesAndHeights(floors int) ([]string, []float64) {
	names := make([]string, floors)
	heights := make([]float64, floors)
	for i := range floors {
		names[i] = fmt.Sprintf("%d", i)
		heights[i] = float64(i) * realCapacityFloorHeight
	}
	return names, heights
}

func realCapacityZFromSynthetic(z float64) float64 {
	layer := int(math.Round(z / 10))
	return float64(layer) * realCapacityFloorHeight
}

func realCapacityAgents(agents []capacityAgent) []capacityAgent {
	out := make([]capacityAgent, len(agents))
	copy(out, agents)
	for i := range out {
		out[i].spawn.Z = realCapacityZFromSynthetic(out[i].spawn.Z)
		out[i].goal[2] = realCapacityZFromSynthetic(out[i].goal[2])
	}
	return out
}

func newRealCapacityGeoTruth(query pubdomain.GeoQuery, publish pubdomain.GeoPublish, registrar natspublish.Publish, includeAgents bool) *realCapacityGeoTruth {
	return &realCapacityGeoTruth{
		query:         query,
		publish:       publish,
		registrar:     registrar,
		includeAgents: includeAgents,
		updates:       make(map[string][]time.Time),
	}
}

func shutdownRealCapacityGeoTruth(serv *embeddedGeo.Services) {
	if serv == nil {
		return
	}
	serv.Shutdown()
	select {
	case <-serv.GeoTruth.Done():
	case <-time.After(5 * time.Second):
	}
}

func (g *realCapacityGeoTruth) AddObject(e *mock.Element) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := g.registrar.RegisterObject(ctx, e.Id(), e.Dims()); err != nil {
		return err
	}
	x, y, z, rotY := e.Position()
	_, err := g.publish.UpdateObjectPosition(ctx, e.Id(), x, y, z, rotY)
	return err
}

func (g *realCapacityGeoTruth) ResetStats() {
	g.mu.Lock()
	defer g.mu.Unlock()
	clear(g.updates)
}

func (g *realCapacityGeoTruth) UpdateCount(id string) int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.updates[id])
}

func (g *realCapacityGeoTruth) TotalUpdates(ids []string) int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	total := 0
	for _, id := range ids {
		total += len(g.updates[id])
	}
	return total
}

func (g *realCapacityGeoTruth) UpdateTimes(id string) []time.Time {
	g.mu.RLock()
	defer g.mu.RUnlock()

	times := g.updates[id]
	out := make([]time.Time, len(times))
	copy(out, times)
	return out
}

func (g *realCapacityGeoTruth) WaitForUpdates(ctx context.Context, ids []string, target map[string]int) bool {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		g.mu.RLock()
		ready := true
		for _, id := range ids {
			if len(g.updates[id]) < target[id] {
				ready = false
				break
			}
		}
		g.mu.RUnlock()
		if ready {
			return true
		}

		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func (g *realCapacityGeoTruth) NearbyObjectsOf(ctx context.Context, objectID string, radiusMeters float64, regex *string) ([]natsquery.ObjectOriented, error) {
	objects, err := g.query.NearbyObjectsOf(ctx, objectID, radiusMeters, regex)
	if err != nil || g.includeAgents {
		return objects, err
	}

	filtered := objects[:0]
	for _, object := range objects {
		if strings.HasPrefix(object.ID, mock.AgentRegex) {
			continue
		}
		filtered = append(filtered, object)
	}
	return filtered, nil
}

func (g *realCapacityGeoTruth) ObjectData(ctx context.Context, objectID string) (*natsquery.Object, error) {
	return g.query.ObjectData(ctx, objectID)
}

func (g *realCapacityGeoTruth) AllObjectsOriented(ctx context.Context, regex *string) (natsquery.AllObjectsOrientedResp, error) {
	return g.query.AllObjectsOriented(ctx, regex)
}

func (g *realCapacityGeoTruth) RegionFromPoint(ctx context.Context, x, y, z float64) (string, error) {
	return g.query.RegionFromPoint(ctx, x, y, z)
}

func (g *realCapacityGeoTruth) UpdateObjectPosition(ctx context.Context, objectID string, x, y, z, rotY float64) (natspublish.CommitAck, error) {
	ack, err := g.publish.UpdateObjectPosition(ctx, objectID, x, y, z, rotY)
	if err != nil {
		return ack, err
	}

	g.mu.Lock()
	g.updates[objectID] = append(g.updates[objectID], time.Now())
	g.mu.Unlock()
	return ack, nil
}

func realCapacityRun(b testing.TB, includeAgents bool) (*realCapacityGeoTruth, func()) {
	b.Helper()

	configFilePath := filepath.Join("..", "..", "data", "hospital", "config.yaml")
	cfg, err := config.LoadConfig(&configFilePath)
	if err != nil {
		b.Fatal(err)
	}
	names, heights := realCapacityLayerNamesAndHeights(len(cfg.Grid.FloorLayers))
	for i, layer := range cfg.Grid.FloorLayers {
		names[i] = layer.Name
	}

	ctx, cancel := context.WithCancel(context.Background())
	serv, err := embeddedGeo.RunLocalStack(ctx, embeddedGeo.DefaultConfig, embeddedGeo.Dependencies{Resolver: embeddedGeo.NewFlatResolver(len(names))})
	if err != nil {
		cancel()
		b.Fatal(err)
	}
	cfg.App.NatsServerUrl = serv.NATSURL()

	queryPool, err := geotruthpool.NewNATSQueryPool(ctx, cfg.App.NatsServerUrl, cfg.Pathfinding.Geotruth.QueryConnections)
	if err != nil {
		shutdownRealCapacityGeoTruth(serv)
		cancel()
		b.Fatal(err)
	}
	publishPool, err := geotruthpool.NewNATSPublishPool(ctx, cfg.App.NatsServerUrl, cfg.Pathfinding.Geotruth.PublishConnections)
	if err != nil {
		queryPool.Close()
		shutdownRealCapacityGeoTruth(serv)
		cancel()
		b.Fatal(err)
	}

	ex, err := mock.NewExternalSystem(names, heights, nil)
	if err != nil {
		publishPool.Close()
		queryPool.Close()
		shutdownRealCapacityGeoTruth(serv)
		cancel()
		b.Fatal(err)
	}

	client, err := natsclient.New(ctx, cfg.App.NatsServerUrl)
	if err != nil {
		publishPool.Close()
		queryPool.Close()
		shutdownRealCapacityGeoTruth(serv)
		cancel()
		b.Fatal(err)
	}

	geo := newRealCapacityGeoTruth(queryPool, publishPool, natspublish.New(client.Conn()), includeAgents)
	dep := &pubconfig.Dependencies{Ex: ex, Qu: geo, Pu: geo}
	config.Setup(cfg, dep)

	disp, err := embedded.Run(ctx, cfg, dep)
	if err != nil {
		client.Close()
		publishPool.Close()
		queryPool.Close()
		shutdownRealCapacityGeoTruth(serv)
		cancel()
		b.Fatal(err)
	}

	cleanup := func() {
		disp.Shutdown()
		client.Close()
		publishPool.Close()
		queryPool.Close()
		shutdownRealCapacityGeoTruth(serv)
		cancel()
	}
	return geo, cleanup
}

func BenchmarkReal_AgentCapacity(b *testing.B) {
	requireRealBench(b)

	modes := []struct {
		name string
		run  func(context.Context, capacityPathfindingPool, capacityGeoTruth, []capacityAgent) capacityRunMetrics
	}{
		{name: "RawStep", run: runRawCapacityScenario},
		{name: "ExternalTick", run: runExternalTickCapacityScenario},
		{name: "RealTime", run: runRealtimeCapacityScenario},
	}
	interactions := []agentInteractionMode{
		{name: "AgentsIgnored", includeAgents: false},
		{name: "AgentsIncluded", includeAgents: true},
	}

	for _, mode := range modes {
		for _, interaction := range interactions {
			for _, n := range capacityAgentCounts() {
				b.Run(fmt.Sprintf("%s/%s/N=%d", mode.name, interaction.name, n), func(b *testing.B) {
					geo, cleanup := realCapacityRun(b, interaction.includeAgents)
					b.Cleanup(cleanup)

					agents := realCapacityAgents(buildCapacityAgents(n, fmt.Sprintf("Agent_real_capacity_%s_%s", mode.name, interaction.name)))
					registerCapacityAgentsWithWorkers(b, geo, agents, min(len(agents), runtime.GOMAXPROCS(0)))

					pool := newCapacityPathfindingPool(b)

					var last capacityRunMetrics
					b.ReportAllocs()
					for b.Loop() {
						b.StopTimer()
						resetCapacityAgents(b, geo, agents)
						b.StartTimer()

						ctx, cancel := context.WithTimeout(context.Background(), capacityRealtimeWindow+capacityUpdateWaitSlack)
						last = mode.run(ctx, pool, geo, agents)
						cancel()
					}
					reportCapacityMetrics(b, n, pool.Size(), last)
				})
			}
		}
	}
}

func TestRealCapacitySmoke(t *testing.T) {
	requireRealBench(t)

	geo, cleanup := realCapacityRun(t, false)
	defer cleanup()

	agents := realCapacityAgents(buildCapacityAgents(1, "Agent_real_capacity_smoke"))
	registerCapacityAgents(t, geo, agents)

	pool := newCapacityPathfindingPool(t)

	resetCapacityAgents(t, geo, agents)
	ctx, cancel := context.WithTimeout(context.Background(), capacityRealtimeWindow+capacityUpdateWaitSlack)
	defer cancel()

	metrics := runRawCapacityScenario(ctx, pool, geo, agents)
	if metrics.errorCounts.total != 0 {
		t.Fatalf("raw real capacity smoke had %d errors", metrics.errorCounts.total)
	}
	if metrics.timeouts != 0 {
		t.Fatalf("raw real capacity smoke had %d timeouts", metrics.timeouts)
	}
	if metrics.updates == 0 {
		t.Fatal("raw real capacity smoke did not observe movement updates")
	}
}
