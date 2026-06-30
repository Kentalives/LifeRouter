package emergency

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	embeddedGeo "github.com/midtxwn/geotruth/embedded"
	"github.com/midtxwn/geotruth/pkg/natsclient"
	"github.com/midtxwn/geotruth/pkg/natspublish"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/domain"
	"github.com/Kentalives/LifeRouter/internal/geotruthpool"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/mock"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

const realEmergencyFloorHeight = 4.0

type realEmergencyGeoTruth struct {
	publish   pubdomain.GeoPublish
	registrar natspublish.Publish
}

func requireRealEmergencyBench(b testing.TB) {
	b.Helper()
	if os.Getenv("PATHFINDING_BENCH_REAL") != "1" {
		b.Skip("set PATHFINDING_BENCH_REAL=1 to run real geotruth emergency benchmarks")
	}
}

func realEmergencyLayerNamesAndHeights(floors int) ([]string, []float64) {
	names := make([]string, floors)
	heights := make([]float64, floors)
	for i := range floors {
		names[i] = fmt.Sprintf("%d", i)
		heights[i] = float64(i) * realEmergencyFloorHeight
	}
	return names, heights
}

func (g *realEmergencyGeoTruth) AddObject(e *mock.Element) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := g.registrar.RegisterObject(ctx, e.Id(), e.Dims()); err != nil {
		return err
	}
	x, y, z, rotY := e.Position()
	_, err := g.publish.UpdateObjectPosition(ctx, e.Id(), x, y, z, rotY)
	return err
}

func (g *realEmergencyGeoTruth) UpdateObjectPosition(ctx context.Context, objectID string, x, y, z, rotY float64) (natspublish.CommitAck, error) {
	return g.publish.UpdateObjectPosition(ctx, objectID, x, y, z, rotY)
}

func newRealHospitalFlowBenchFixture(b testing.TB, exits, nodes int) *flowBenchFixture {
	b.Helper()

	configFilePath := filepath.Join("..", "..", "..", "data", "hospital", "config.yaml")
	cfg, err := config.LoadConfig(&configFilePath)
	if err != nil {
		b.Fatal(err)
	}
	for i := range cfg.Grid.FloorLayers {
		cfg.Grid.FloorLayers[i].ImgPath = filepath.Join("..", "..", "..", "data", "hospital", filepath.Base(cfg.Grid.FloorLayers[i].ImgPath))
	}

	names, heights := realEmergencyLayerNamesAndHeights(len(cfg.Grid.FloorLayers))
	syntheticHeights := make([]float64, len(cfg.Grid.FloorLayers))
	for i, layer := range cfg.Grid.FloorLayers {
		names[i] = layer.Name
		syntheticHeights[i] = float64(i) * 10
	}
	mockNodes := make([]*mock.Node, 0, nodes)
	realNodes := make([]domain.INode, 0, nodes)
	for i := range nodes {
		layer := i % len(heights)
		cellSizeM := cfg.Grid.CellSizeM
		n := &mock.Node{
			Element: mock.Element{
				IdName: fmt.Sprintf("Emergency_real_capacity_node_%d", i),
				X:      float64(20+(i*97)%max(1, int(cfg.Grid.Cols)-40)) * cellSizeM,
				Y:      float64(20+(i*61)%max(1, int(cfg.Grid.Rows)-40)) * cellSizeM,
				Z:      float64(layer) * 10,
				Width:  cellSizeM,
				Height: cellSizeM,
			},
			Dir: pubdomain.DIR_UNKNOWN,
		}
		mockNodes = append(mockNodes, n)
		realNodes = append(realNodes, n)
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
		serv.Shutdown()
		cancel()
		b.Fatal(err)
	}
	publishPool, err := geotruthpool.NewNATSPublishPool(ctx, cfg.App.NatsServerUrl, cfg.Pathfinding.Geotruth.PublishConnections)
	if err != nil {
		queryPool.Close()
		serv.Shutdown()
		cancel()
		b.Fatal(err)
	}

	ex, err := mock.NewExternalSystem(names, heights, mockNodes)
	if err != nil {
		publishPool.Close()
		queryPool.Close()
		serv.Shutdown()
		cancel()
		b.Fatal(err)
	}

	client, err := natsclient.New(ctx, cfg.App.NatsServerUrl)
	if err != nil {
		publishPool.Close()
		queryPool.Close()
		serv.Shutdown()
		cancel()
		b.Fatal(err)
	}

	geoTruth := &realEmergencyGeoTruth{
		publish:   publishPool,
		registrar: natspublish.New(client.Conn()),
	}
	for i, n := range mockNodes {
		nodeElement := n.Element
		nodeElement.Z = heights[i%len(heights)]
		if err := geoTruth.AddObject(&nodeElement); err != nil {
			client.Close()
			publishPool.Close()
			queryPool.Close()
			serv.Shutdown()
			cancel()
			b.Fatal(err)
		}
	}

	dep := &pubconfig.Dependencies{Ex: ex, Qu: queryPool, Pu: geoTruth}
	config.Setup(cfg, dep)

	w, err := grid.NewWorld(cfg.Grid.FloorLayers, cfg.Grid.Rows, cfg.Grid.Cols, cfg.Grid.PxPerCell)
	if err != nil {
		client.Close()
		publishPool.Close()
		queryPool.Close()
		serv.Shutdown()
		cancel()
		b.Fatal(err)
	}
	for _, p := range cfg.Grid.Portals {
		from := grid.GlobalCoords{Coords: grid.CoordsFromFloat64(p.From[0], p.From[1]), Layer: p.FromLayer}
		to := grid.GlobalCoords{Coords: grid.CoordsFromFloat64(p.To[0], p.To[1]), Layer: p.ToLayer}
		if p.Bidirectional {
			w.AddBidirectionalPortal(from, to, p.TraversalCost)
		} else {
			w.AddPortal(from, to, p.TraversalCost)
		}
	}

	return &flowBenchFixture{
		world:    w,
		geoTruth: geoTruth,
		ex:       ex,
		names:    names,
		heights:  heights,
		goals:    hospitalCapacityExits(b, w, syntheticHeights, exits),
		nodes:    realNodes,
		cleanup: func() {
			client.Close()
			publishPool.Close()
			queryPool.Close()
			serv.Shutdown()
			cancel()
		},
	}
}

func BenchmarkReal_Flowfield_MovingObjectsCapacity(b *testing.B) {
	requireRealEmergencyBench(b)

	const exits = 10
	const nodes = 20

	for _, objectCount := range emergencyCapacityObjectCounts() {
		b.Run(fmt.Sprintf("MovingObjects=%d", objectCount), func(b *testing.B) {
			var last emergencyCapacityMetrics
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				f := newRealHospitalFlowBenchFixture(b, exits, nodes)
				b.StartTimer()

				last = runEmergencyMovingObjectsCapacity(b, f, objectCount, emergencyCapacityTicks)

				b.StopTimer()
				if f.cleanup != nil {
					f.cleanup()
				}
				b.StartTimer()
			}
			reportEmergencyCapacityMetrics(b, last)
		})
	}
}

func TestRealFlowfield_MovingObjectsCapacitySmoke(t *testing.T) {
	requireRealEmergencyBench(t)

	f := newRealHospitalFlowBenchFixture(t, 2, 2)
	defer f.cleanup()

	metrics := runEmergencyMovingObjectsCapacity(t, f, 3, 2)
	if metrics.errors != 0 {
		t.Fatalf("real emergency moving-object capacity smoke had %d errors", metrics.errors)
	}
	if metrics.observedTicks != 2 {
		t.Fatalf("observed ticks = %d, want 2", metrics.observedTicks)
	}
	if metrics.changedCells == 0 {
		t.Fatal("expected real moving objects to change emergency grid cells")
	}
}
