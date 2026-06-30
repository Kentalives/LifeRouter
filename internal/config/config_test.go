package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadConfig_LoadsYamlAndAppliesDefaults(t *testing.T) {
	clearPathfindingServiceEnv(t)
	writeConfigFile(t, `
app:
  serverurl: "nats://yaml:4222"
  stderr: false
grid:
  rows: 7
  cols: 8
  cellsizem: 0.5
  layers:
    - name: "floor-a"
      img: "./floor-a.png"
  portals:
    - from: [1.25, 2.5]
      fromlayer: 0
      to: [3.75, 4.5]
      tolayer: 1
      traversalcost: 9
      bidirectional: true
pathfinding:
  freemoveheight: 3.5
  agent:
    visionradiuscells: 12
  emergency:
    debug: true
`)

	cfg, err := LoadConfig(nil)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.App.NatsServerUrl != "nats://yaml:4222" {
		t.Fatalf("server URL = %q, want yaml value", cfg.App.NatsServerUrl)
	}
	if cfg.App.LogStderr {
		t.Fatal("stderr logging = true, want explicit yaml false")
	}
	if cfg.Grid.Rows != 7 || cfg.Grid.Cols != 8 || cfg.Grid.CellSizeM != 0.5 {
		t.Fatalf("grid dimensions = rows %d cols %d cell size %.1f, want 7, 8, 0.5", cfg.Grid.Rows, cfg.Grid.Cols, cfg.Grid.CellSizeM)
	}
	if len(cfg.Grid.FloorLayers) != 1 || cfg.Grid.FloorLayers[0].Name != "floor-a" || cfg.Grid.FloorLayers[0].ImgPath != "./floor-a.png" {
		t.Fatalf("floor layers = %#v, want configured floor-a layer", cfg.Grid.FloorLayers)
	}
	if len(cfg.Grid.Portals) != 1 {
		t.Fatalf("portals = %#v, want one configured portal", cfg.Grid.Portals)
	}
	portal := cfg.Grid.Portals[0]
	if portal.From != [2]float64{1.25, 2.5} || portal.To != [2]float64{3.75, 4.5} {
		t.Fatalf("portal coordinates = from %#v to %#v, want [1.25 2.5] and [3.75 4.5]", portal.From, portal.To)
	}
	if portal.FromLayer != 0 || portal.ToLayer != 1 || portal.TraversalCost != 9 || !portal.Bidirectional {
		t.Fatalf("portal metadata = %#v, want configured layers, cost, and bidirectional flag", portal)
	}
	if cfg.Grid.PxPerCell != 10 {
		t.Fatalf("px per cell = %.1f, want default 10.0", cfg.Grid.PxPerCell)
	}
	if cfg.Grid.WallAuraCells != 1 {
		t.Fatalf("wall aura cells = %d, want default 1", cfg.Grid.WallAuraCells)
	}
	if cfg.Pathfinding.FreeMovementHeight != 3.5 {
		t.Fatalf("free movement height = %.1f, want yaml value 3.5", cfg.Pathfinding.FreeMovementHeight)
	}
	if cfg.Pathfinding.Agent.VisionRadiusCells != 12 {
		t.Fatalf("vision radius = %d, want yaml value 12", cfg.Pathfinding.Agent.VisionRadiusCells)
	}
	if cfg.Pathfinding.Agent.FindPathHandlerWorkers == 0 {
		t.Fatal("find path handler workers kept zero value, want runtime default")
	}
	if !cfg.Pathfinding.Emergency.Debug {
		t.Fatal("emergency debug = false, want yaml true")
	}
	if cfg.Pathfinding.Emergency.PriorityPathCellsWidth != 1 {
		t.Fatalf("priority path width = %.1f, want default 1.0", cfg.Pathfinding.Emergency.PriorityPathCellsWidth)
	}
}

func TestLoadConfig_EnvironmentOverridesYaml(t *testing.T) {
	clearPathfindingServiceEnv(t)
	writeConfigFile(t, `
app:
  serverurl: "nats://yaml:4222"
grid:
  rows: 7
pathfinding:
  agent:
    visionradiuscells: 12
`)
	t.Setenv("PTHFSERVICE_APP_SERVERURL", "nats://env:4222")
	t.Setenv("PTHFSERVICE_GRID_ROWS", "13")
	t.Setenv("PTHFSERVICE_PATHFINDING_AGENT_VISIONRADIUSCELLS", "21")

	cfg, err := LoadConfig(nil)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.App.NatsServerUrl != "nats://env:4222" {
		t.Fatalf("server URL = %q, want env override", cfg.App.NatsServerUrl)
	}
	if cfg.Grid.Rows != 13 {
		t.Fatalf("rows = %d, want env override 13", cfg.Grid.Rows)
	}
	if cfg.Pathfinding.Agent.VisionRadiusCells != 21 {
		t.Fatalf("vision radius = %d, want env override 21", cfg.Pathfinding.Agent.VisionRadiusCells)
	}
}

func TestLoadConfig_LoadsExplicitFilePath(t *testing.T) {
	clearPathfindingServiceEnv(t)

	customDir := t.TempDir()
	customPath := filepath.Join(customDir, "benchmark-config.yaml")
	if err := os.WriteFile(customPath, []byte(`
app:
  serverurl: "nats://custom:4222"
grid:
  rows: 19
  cols: 23
  layers:
    - name: "benchmark-floor"
      img: "./benchmark-floor.png"
pathfinding:
  agent:
    visionradiuscells: 17
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())
	t.Setenv("PTHFSERVICE_GRID_ROWS", "29")

	cfg, err := LoadConfig(&customPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.App.NatsServerUrl != "nats://custom:4222" {
		t.Fatalf("server URL = %q, want custom file value", cfg.App.NatsServerUrl)
	}
	if cfg.Grid.Rows != 29 {
		t.Fatalf("rows = %d, want env override over custom file", cfg.Grid.Rows)
	}
	if cfg.Grid.Cols != 23 {
		t.Fatalf("cols = %d, want custom file value", cfg.Grid.Cols)
	}
	if len(cfg.Grid.FloorLayers) != 1 || cfg.Grid.FloorLayers[0].Name != "benchmark-floor" || cfg.Grid.FloorLayers[0].ImgPath != "./benchmark-floor.png" {
		t.Fatalf("floor layers = %#v, want configured benchmark floor", cfg.Grid.FloorLayers)
	}
	if cfg.Grid.PxPerCell != 10 {
		t.Fatalf("px per cell = %.1f, want default 10.0", cfg.Grid.PxPerCell)
	}
	if cfg.Pathfinding.Agent.VisionRadiusCells != 17 {
		t.Fatalf("vision radius = %d, want custom file value", cfg.Pathfinding.Agent.VisionRadiusCells)
	}
	if cfg.Pathfinding.Agent.FindPathHandlerWorkers == 0 {
		t.Fatal("find path handler workers kept zero value, want runtime default")
	}
}

func TestLoadConfig_MissingConfigFileReturnsError(t *testing.T) {
	clearPathfindingServiceEnv(t)
	t.Chdir(t.TempDir())

	cfg, err := LoadConfig(nil)
	if err == nil {
		t.Fatalf("LoadConfig() = %#v, nil error; want missing config error", cfg)
	}

	var notFound viper.ConfigFileNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error = %T %[1]v, want %T", err, notFound)
	}
}

func clearPathfindingServiceEnv(t *testing.T) {
	t.Helper()

	type envVar struct {
		key   string
		value string
	}
	var previous []envVar
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "PTHFSERVICE_") {
			continue
		}
		previous = append(previous, envVar{key: key, value: value})
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}

	t.Cleanup(func() {
		for _, entry := range previous {
			if err := os.Setenv(entry.key, entry.value); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func writeConfigFile(t *testing.T, content string) {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)

	configDir := filepath.Join(dir, "config")
	if err := os.Mkdir(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
