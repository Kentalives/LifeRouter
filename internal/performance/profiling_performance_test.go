package performance

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Kentalives/LifeRouter/embedded"
	"github.com/Kentalives/LifeRouter/internal/app"
	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/mock"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubAgents "github.com/Kentalives/LifeRouter/pkg/pathfinding"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

type spawnPoint struct {
	Coord [2]uint
	Z     float64
}

var spawnPoints = []spawnPoint{
	{Coord: [2]uint{493, 29}, Z: 0},
	{Coord: [2]uint{493, 29}, Z: 10},
	{Coord: [2]uint{493, 29}, Z: 20},
	{Coord: [2]uint{493, 29}, Z: 30},
	{Coord: [2]uint{493, 29}, Z: 40},

	{Coord: [2]uint{874, 40}, Z: 0},
	{Coord: [2]uint{26, 243}, Z: 10},
	{Coord: [2]uint{685, 373}, Z: 20},
	{Coord: [2]uint{128, 55}, Z: 30},
	{Coord: [2]uint{128, 383}, Z: 40},
}

var goals = [][3]float64{
	{810, 320, 0},
	{195, 238, 0},
	{227, 295, 10},
	{872, 95, 10},
	{441, 299, 20},
	{994, 331, 20},
	{640, 18, 30},
	{28, 315, 30},
	{651, 360, 40},
	{118, 220, 40},
}

func testNATSConn(b testing.TB) *nats.Conn {
	b.Helper()

	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1})
	if err != nil {
		b.Fatal(err)
	}
	go s.Start()
	if !s.ReadyForConnections(time.Second) {
		b.Fatal("nats server did not start")
	}
	b.Cleanup(s.Shutdown)

	nc, err := nats.Connect(s.ClientURL())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(nc.Close)
	return nc
}

func setupTestConfig(b testing.TB, dep *pubconfig.Dependencies) {
	b.Helper()

	configFilePath := filepath.Join("..", "..", "data", "hospital", "config.yaml")
	cfg, err := config.LoadConfig(&configFilePath)
	if err != nil {
		b.Fatal(err)
	}

	config.Setup(cfg, dep)
}

func syntheticRun(b *testing.B, nodes []*mock.Node, nObjects int) (*app.Dispatcher, *mock.GeoTruth) {
	b.Helper()

	nc := testNATSConn(b)

	dep, geo := syntheticDependencies(b, nodes, nObjects)
	setupTestConfig(b, dep)

	cfg := config.Cfg
	cfg.App.NatsServerUrl = nc.ConnectedUrl()
	//cfg.Pathfinding.Agent.Debug = true
	disp, err := embedded.Run(b.Context(), cfg, config.Dep)
	if err != nil {
		b.Fatal(err)
	}

	return disp, geo
}

func syntheticDependencies(b testing.TB, nodes []*mock.Node, nObjects int) (*pubconfig.Dependencies, *mock.GeoTruth) {
	b.Helper()

	ex, err := mock.NewExternalSystem([]string{"0", "1", "2", "3", "4"}, []float64{0, 10, 20, 30, 40}, nodes)
	if err != nil {
		b.Fatal(err)
	}

	geoTruth := mock.NewGeoTruth(nObjects+len(nodes), ex)
	for _, n := range nodes {
		err := geoTruth.AddObject(&n.Element)
		if err != nil {
			b.Fatal(err)
		}
	}

	return &pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth}, geoTruth
}

func syntheticAgentsLoad(b *testing.B, geoTruth *mock.GeoTruth, ids []string) {
	b.Helper()

	nSpawns := len(spawnPoints)

	for i, id := range ids {
		spwn := spawnPoints[i%nSpawns]

		x, y := float64(spwn.Coord[0])*grid.CellSizeM(), float64(spwn.Coord[1])*grid.CellSizeM()
		e := &mock.Element{X: x, Y: y, Z: spwn.Z, RotY: 0, IdName: id}
		err := geoTruth.AddObject(e)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func loadWorld(b *testing.B, cfg pubconfig.GridConfig) {
	w, err := grid.NewWorld(cfg.FloorLayers, cfg.Rows, cfg.Cols, cfg.PxPerCell)
	if err != nil {
		b.Fatal(err)
	}

	for _, p := range cfg.Portals {
		from := grid.GlobalCoords{Coords: grid.CoordsFromFloat64(p.From[0], p.From[1]), Layer: p.FromLayer}
		to := grid.GlobalCoords{Coords: grid.CoordsFromFloat64(p.To[0], p.To[1]), Layer: p.ToLayer}
		if !w.Contains(from) || !w.Contains(to) {
			b.Fatalf("portal out of bounds: from=%v to=%v", from, to)
		}

		if p.Bidirectional {
			w.AddBidirectionalPortal(from, to, p.TraversalCost)
		} else {
			w.AddPortal(from, to, p.TraversalCost)
		}
	}
}

func loadMapConfig(b testing.TB, configPath string) *pubconfig.Config {
	b.Helper()

	cfg, err := config.LoadConfig(&configPath)
	if err != nil {
		b.Fatal(err)
	}

	for i := range cfg.Grid.FloorLayers {
		imgPath, err := resolveBenchmarkLayerPath(configPath, cfg.Grid.FloorLayers[i].ImgPath)
		if err != nil {
			b.Fatal(err)
		}
		cfg.Grid.FloorLayers[i].ImgPath = imgPath
	}
	return cfg
}

func resolveBenchmarkLayerPath(configPath string, imgPath string) (string, error) {
	if filepath.IsAbs(imgPath) {
		if _, err := os.Stat(imgPath); err == nil {
			return imgPath, nil
		}
		return "", fmt.Errorf("layer image %q does not exist", imgPath)
	}

	roots := []string{
		".",
		filepath.Join("..", ".."),
		filepath.Dir(configPath),
	}
	for _, root := range roots {
		candidate := filepath.Clean(filepath.Join(root, imgPath))
		if _, err := os.Stat(candidate); err == nil {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return "", err
			}
			return abs, nil
		}
	}

	return "", fmt.Errorf("layer image %q not found for config %q", imgPath, configPath)
}

func reportMapLoadMetrics(b *testing.B, cfg pubconfig.GridConfig) {
	b.ReportMetric(float64(len(cfg.FloorLayers)), "floors")
	b.ReportMetric(float64(cfg.Rows), "rows")
	b.ReportMetric(float64(cfg.Cols), "cols")
	b.ReportMetric(float64(len(cfg.Portals)), "portals")
	b.ReportMetric(float64(len(cfg.FloorLayers))*float64(cfg.Rows)*float64(cfg.Cols), "cells")
}

func BenchmarkSynthetic_MapLoad(b *testing.B) {
	scenarios := []struct {
		name       string
		configPath string
	}{
		{name: "DebugMap", configPath: filepath.Join("..", "..", "config", "config.yaml")},
		{name: "Hospital", configPath: filepath.Join("..", "..", "data", "hospital", "config.yaml")},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			cfg := loadMapConfig(b, scenario.configPath)
			config.Setup(cfg, &pubconfig.Dependencies{})
			b.ReportAllocs()
			for b.Loop() {
				loadWorld(b, cfg.Grid)
			}
			reportMapLoadMetrics(b, cfg.Grid)
		})
	}
}

func BenchmarkSynthetic_AgentsNoObjects(b *testing.B) {

	for i, n := range []int{1, 20} { //50, 100, 250, 700, 1000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {

			disp, geo := syntheticRun(b, nil, n)
			b.Cleanup(func() {
				disp.Shutdown()
			})

			ids := make([]string, 0, n)
			for j := range n {
				id := fmt.Sprintf("Agent_%d_%d", i, j)
				//log.Warnf("REGISTERED ID: %s", id)
				ids = append(ids, id)
			}
			syntheticAgentsLoad(b, geo, ids)
			nc, err := nats.Connect(config.Cfg.App.NatsServerUrl)
			if err != nil {
				b.Fatal(err)
			}
			p := pubAgents.New(nc)

			nGoals := len(goals)

			for b.Loop() {

				rand.Shuffle(nGoals, func(i, j int) {
					goals[i], goals[j] = goals[j], goals[i]
				})

				var wg sync.WaitGroup
				for j, id := range ids {
					wg.Go(func() {
						goal := goals[j%nGoals]
						goal[0] *= grid.CellSizeM()
						goal[1] *= grid.CellSizeM()
						comm, err := p.AgentFindPath(b.Context(), goal, id, 700, 0) //NOTE: 7 cells/s with cellsizem = 0.2 are about 1.4 m/s (normal human walking speed)
						if err != nil {
							b.Errorf("Agent: %s: %s", id, err)
							return
						}

						ch, err := comm.BlockingWait()
						if err != nil {
							b.Error(err)
							return
						}
						<-ch

						//if remaining, err := comm.MoveFMeters(b.Context(), 1000000); err != nil {
						//	if errors.Is(err, pubdomain.ErrAgentExitedWithMetersLeft) {
						//		if exitErr := comm.ExitError(b.Context()); exitErr != nil {
						//			b.Errorf("Agent: %s: exited with meters left after pathfinding error: remaining=%f: %s", id, remaining, exitErr)
						//		}
						//		return
						//	}
						//	b.Errorf("Agent: %s: move meters: remaining=%f: %s", id, remaining, err)
						//}
					})
				}

				wg.Wait()
			}
		})
	}

}
