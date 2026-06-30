package emergency

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/midtxwn/geotruth/pkg/natspublish"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/domain"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/mock"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
	"github.com/google/uuid"
)

// Emergency benchmarks isolate flow-field planning, multi-floor portals,
// environment object refresh, replanning, and preference-path application.

const (
	emergencyCapacityTicks      = 30
	emergencyCapacityTargetTick = time.Second
	emergencyObjectCells        = 3
)

type flowBenchGeoTruth interface {
	AddObject(e *mock.Element) error
	UpdateObjectPosition(ctx context.Context, objectID string, x, y, z, rotY float64) (natspublish.CommitAck, error)
}

type flowBenchFixture struct {
	world    *grid.World
	floors   []*grid.Grid
	geoTruth flowBenchGeoTruth
	ex       *mock.ExternalSystem
	names    []string
	heights  []float64
	goals    []grid.GlobalCoords
	nodes    []domain.INode
	cleanup  func()
}

func newFlowBenchFixture(b testing.TB, rows, cols uint, floors int, exits int, nodes int) *flowBenchFixture {
	b.Helper()

	grids := make([]*grid.Grid, floors)
	names := make([]string, floors)
	heights := make([]float64, floors)
	for i := range floors {
		grids[i] = grid.NewFilled(rows, cols, grid.EMPTY_SPACE_COST)
		names[i] = fmt.Sprintf("%d", i)
		heights[i] = float64(i) * 4.3
	}

	w, err := grid.NewWorldFromGrids(grids, names)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < floors-1; i++ {
		c := grid.Coords{X: cols / 2, Y: rows / 2}
		w.AddBidirectionalPortal(grid.GlobalCoords{Coords: c, Layer: i}, grid.GlobalCoords{Coords: c, Layer: i + 1}, 1)
	}

	realGoals := deterministicFlowExits(rows, cols, exits, floors-1)
	mockNodes := make([]*mock.Node, 0, nodes)
	realNodes := make([]domain.INode, 0, nodes)
	for i := range nodes {
		n := &mock.Node{
			Element: mock.Element{
				IdName: fmt.Sprintf("Node_bench_%d", i),
				X:      float64(2+(i*11)%max(1, int(cols)-4)) * pubdomain.CELL_SIZE_M,
				Y:      float64(2+(i*17)%max(1, int(rows)-4)) * pubdomain.CELL_SIZE_M,
				Z:      heights[i%floors],
				Width:  pubdomain.CELL_SIZE_M,
				Height: pubdomain.CELL_SIZE_M,
			},
			Dir: pubdomain.DIR_UNKNOWN,
		}
		mockNodes = append(mockNodes, n)
		realNodes = append(realNodes, n)
	}

	ex, err := mock.NewExternalSystem(names, heights, mockNodes)
	if err != nil {
		b.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(nodes+8, ex)
	setupTestConfig(&pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	return &flowBenchFixture{
		world:    w,
		floors:   grids,
		geoTruth: geoTruth,
		ex:       ex,
		names:    names,
		heights:  heights,
		goals:    realGoals,
		nodes:    realNodes,
	}
}

func deterministicFlowExits(rows, cols uint, n int, layer int) []grid.GlobalCoords {
	exits := make([]grid.GlobalCoords, n)
	for i := range n {
		exits[i] = grid.GlobalCoords{
			Coords: grid.Coords{
				X: uint(2 + (i*23)%max(1, int(cols)-4)),
				Y: uint(2 + (i*31)%max(1, int(rows)-4)),
			},
			Layer: layer,
		}
	}
	return exits
}

func deterministicFlowChanges(rows, cols uint, n int) []grid.Coords {
	changes := make([]grid.Coords, n)
	for i := range n {
		changes[i] = grid.Coords{
			X: uint(3 + (i*19)%max(1, int(cols)-6)),
			Y: uint(3 + (i*37)%max(1, int(rows)-6)),
		}
	}
	return changes
}

func newPlannedFlowfield(b testing.TB, f *flowBenchFixture) *sysDStarLite {
	b.Helper()

	d, err := newSysDStarLite(f.goals, f.nodes, f.world)
	if err != nil {
		b.Fatal(err)
	}
	d.computeShortestPath()
	return d
}

type emergencyCapacityObject struct {
	id    string
	layer int
	xCell uint
	yCell uint
}

type emergencyCapacityMetrics struct {
	latencies          []time.Duration
	errors             int
	elapsed            time.Duration
	expectedTick       time.Duration
	observedTicks      int
	missedTicks        int
	changedCells       int
	environmentTime    time.Duration
	replanTime         time.Duration
	supported          bool
	p50Latency         time.Duration
	p95Latency         time.Duration
	p99Latency         time.Duration
	maxLatency         time.Duration
	heapMB             float64
	goroutines         int
	movingObjects      int
	floors             int
	exits              int
	nodes              int
	changedCellsPerSec float64
}

type hospitalCapacityPoint struct {
	coord [2]uint
	z     float64
}

// Mirrors internal/performance/profiling_performance_test.go spawnPoints so
// emergency exits and agent capacity routes use the same hospital anchors.
var hospitalCapacityExitPoints = []hospitalCapacityPoint{
	{coord: [2]uint{493, 29}, z: 0},
	{coord: [2]uint{493, 29}, z: 10},
	{coord: [2]uint{493, 29}, z: 20},
	{coord: [2]uint{493, 29}, z: 30},
	{coord: [2]uint{493, 29}, z: 40},

	{coord: [2]uint{874, 40}, z: 0},
	{coord: [2]uint{26, 243}, z: 10},
	{coord: [2]uint{685, 373}, z: 20},
	{coord: [2]uint{128, 55}, z: 30},
	{coord: [2]uint{128, 383}, z: 40},
}

func emergencyCapacityObjectCounts() []int {
	if os.Getenv("PATHFINDING_BENCH_HEAVY") == "1" {
		return []int{1, 5, 10, 25, 50, 100, 200, 400, 700, 1000, 1250} //, 200, 400}
	}
	return []int{1, 5, 10, 25}
}

func newHospitalFlowBenchFixture(b testing.TB, exits, nodes int) *flowBenchFixture {
	b.Helper()

	configFilePath := filepath.Join("..", "..", "..", "data", "hospital", "config.yaml")
	cfg, err := config.LoadConfig(&configFilePath)
	if err != nil {
		b.Fatal(err)
	}
	for i := range cfg.Grid.FloorLayers {
		cfg.Grid.FloorLayers[i].ImgPath = filepath.Join("..", "..", "..", "data", "hospital", filepath.Base(cfg.Grid.FloorLayers[i].ImgPath))
	}

	heights := make([]float64, len(cfg.Grid.FloorLayers))
	names := make([]string, len(cfg.Grid.FloorLayers))
	for i, layer := range cfg.Grid.FloorLayers {
		names[i] = layer.Name
		heights[i] = float64(i) * 10
	}

	mockNodes := make([]*mock.Node, 0, nodes)
	realNodes := make([]domain.INode, 0, nodes)
	for i := range nodes {
		cellSizeM := cfg.Grid.CellSizeM
		n := &mock.Node{
			Element: mock.Element{
				IdName: fmt.Sprintf("Emergency_capacity_node_%d", i),
				X:      float64(20+(i*97)%max(1, int(cfg.Grid.Cols)-40)) * cellSizeM,
				Y:      float64(20+(i*61)%max(1, int(cfg.Grid.Rows)-40)) * cellSizeM,
				Z:      heights[i%len(heights)],
				Width:  cellSizeM,
				Height: cellSizeM,
			},
			Dir: pubdomain.DIR_UNKNOWN,
		}
		mockNodes = append(mockNodes, n)
		realNodes = append(realNodes, n)
	}

	ex, err := mock.NewExternalSystem(names, heights, mockNodes)
	if err != nil {
		b.Fatal(err)
	}
	geoTruth := mock.NewGeoTruth(nodes+8, ex)
	for _, n := range mockNodes {
		if err := geoTruth.AddObject(&n.Element); err != nil {
			b.Fatal(err)
		}
	}
	config.Setup(cfg, &pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth})

	w, err := grid.NewWorld(cfg.Grid.FloorLayers, cfg.Grid.Rows, cfg.Grid.Cols, cfg.Grid.PxPerCell)
	if err != nil {
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
		goals:    hospitalCapacityExits(b, w, heights, exits),
		nodes:    realNodes,
	}
}

func hospitalCapacityExits(b testing.TB, w *grid.World, heights []float64, n int) []grid.GlobalCoords {
	b.Helper()

	if n > len(hospitalCapacityExitPoints) {
		b.Fatalf("requested %d hospital capacity exits, only %d configured", n, len(hospitalCapacityExitPoints))
	}

	exits := make([]grid.GlobalCoords, 0, n)
	for _, point := range hospitalCapacityExitPoints[:n] {
		layer := floorLayerFromHeight(b, heights, point.z)
		exit := grid.GlobalCoords{Coords: grid.Coords{X: point.coord[0], Y: point.coord[1]}, Layer: layer}
		if !w.Contains(exit) {
			b.Fatalf("hospital capacity exit %v is outside the world", exit)
		}
		floor := w.Floor(layer)
		if floor == nil || grid.IsBlocked(floor.GetValue(exit.Coords)) {
			b.Fatalf("hospital capacity exit %v is blocked", exit)
		}
		exits = append(exits, exit)
	}
	return exits
}

func floorLayerFromHeight(b testing.TB, heights []float64, z float64) int {
	b.Helper()

	layer := 0
	bestHeight := math.Inf(-1)
	for i, height := range heights {
		if (height < z || math.Abs(height-z) < 0.0001) && height > bestHeight {
			layer = i
			bestHeight = height
		}
	}
	return layer
}

func (f *flowBenchFixture) layerHeight(layer int) float64 {
	if layer >= 0 && layer < len(f.heights) {
		return f.heights[layer]
	}
	return float64(layer) * 10
}

func registerEmergencyCapacityObjects(b testing.TB, f *flowBenchFixture, n int) []emergencyCapacityObject {
	b.Helper()

	rows := f.world.Floor(0).Rows
	cols := f.world.Floor(0).Cols
	objects := make([]emergencyCapacityObject, 0, n)
	for i := range n {
		layer := i % f.world.Len()
		object := emergencyCapacityObject{
			id:    fmt.Sprintf("Emergency_capacity_object_%d", i),
			layer: layer,
			xCell: uint(5 + (i*29)%max(1, int(cols)-10)),
			yCell: uint(5 + (i*47)%max(1, int(rows)-10)),
		}
		elem := &mock.Element{
			IdName: object.id,
			X:      float64(object.xCell) * grid.CellSizeM(),
			Y:      float64(object.yCell) * grid.CellSizeM(),
			Z:      f.layerHeight(layer),
			Width:  float64(emergencyObjectCells) * grid.CellSizeM(),
			Height: float64(emergencyObjectCells) * grid.CellSizeM(),
		}
		if err := f.geoTruth.AddObject(elem); err != nil {
			b.Fatal(err)
		}
		objects = append(objects, object)
	}
	return objects
}

func moveEmergencyCapacityObjects(b testing.TB, f *flowBenchFixture, objects []emergencyCapacityObject, tick int) {
	b.Helper()

	rows := f.world.Floor(0).Rows
	cols := f.world.Floor(0).Cols
	usableCols := max(1, int(cols)-10)
	usableRows := max(1, int(rows)-10)
	for i := range objects {
		obj := &objects[i]
		x := uint(5 + (int(obj.xCell)+tick+i%7)%usableCols)
		y := uint(5 + (int(obj.yCell)+(tick*2)+i%11)%usableRows)
		if _, err := f.geoTruth.UpdateObjectPosition(context.Background(), obj.id, float64(x)*grid.CellSizeM(), float64(y)*grid.CellSizeM(), f.layerHeight(obj.layer), 0); err != nil {
			b.Fatal(err)
		}
	}
}

func runEmergencyCapacityTick(b testing.TB, d *sysDStarLite) (changed int, environmentTime time.Duration, replanTime time.Duration, err error) {
	b.Helper()

	environmentStart := time.Now()
	if err := d.applyEnvironmentObjects(); err != nil {
		return 0, time.Since(environmentStart), 0, err
	}
	environmentTime = time.Since(environmentStart)

	d.ChangedMu.Lock()
	changed = len(d.Changed)
	d.ChangedMu.Unlock()

	if changed > 0 {
		replanStart := time.Now()
		d.applyChanges()
		replanTime = time.Since(replanStart)
	}
	return changed, environmentTime, replanTime, nil
}

func runEmergencyMovingObjectsCapacity(b testing.TB, f *flowBenchFixture, objectCount int, ticks int) emergencyCapacityMetrics {
	b.Helper()

	objects := registerEmergencyCapacityObjects(b, f, objectCount)
	d := newPlannedFlowfield(b, f)
	defer d.QuitWorldSub.Close()
	defer d.cleanupFakeGoalPortals()

	moveEmergencyCapacityObjects(b, f, objects, -1)
	if _, _, _, err := runEmergencyCapacityTick(b, d); err != nil {
		return finalizeEmergencyCapacityMetrics(emergencyCapacityMetrics{
			errors:        1,
			expectedTick:  emergencyCapacityTargetTick,
			movingObjects: objectCount,
			floors:        f.world.Len(),
			exits:         len(f.goals),
			nodes:         len(f.nodes),
		})
	}

	latencies := make([]time.Duration, 0, ticks)
	errors := 0
	changedCells := 0
	missedTicks := 0
	environmentTime := time.Duration(0)
	replanTime := time.Duration(0)
	start := time.Now()
	for tick := range ticks {
		moveEmergencyCapacityObjects(b, f, objects, tick)

		tickStart := time.Now()
		changed, tickEnvironmentTime, tickReplanTime, err := runEmergencyCapacityTick(b, d)
		latency := time.Since(tickStart)
		if err != nil {
			errors++
		}
		if latency > emergencyCapacityTargetTick {
			missedTicks++
		}
		changedCells += changed
		environmentTime += tickEnvironmentTime
		replanTime += tickReplanTime
		latencies = append(latencies, latency)
	}

	return finalizeEmergencyCapacityMetrics(emergencyCapacityMetrics{
		latencies:       latencies,
		errors:          errors,
		elapsed:         time.Since(start),
		expectedTick:    emergencyCapacityTargetTick,
		observedTicks:   len(latencies),
		missedTicks:     missedTicks,
		changedCells:    changedCells,
		environmentTime: environmentTime,
		replanTime:      replanTime,
		movingObjects:   objectCount,
		floors:          f.world.Len(),
		exits:           len(f.goals),
		nodes:           len(f.nodes),
	})
}

func finalizeEmergencyCapacityMetrics(m emergencyCapacityMetrics) emergencyCapacityMetrics {
	m.p50Latency = flowPercentileDuration(m.latencies, 0.50)
	m.p95Latency = flowPercentileDuration(m.latencies, 0.95)
	m.p99Latency = flowPercentileDuration(m.latencies, 0.99)
	m.maxLatency = flowMaxDuration(m.latencies)
	m.supported = m.errors == 0 && len(m.latencies) > 0 && m.p95Latency <= m.expectedTick

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	m.heapMB = float64(mem.Alloc) / 1024 / 1024
	m.goroutines = runtime.NumGoroutine()
	m.changedCellsPerSec = float64(m.changedCells) / math.Max(m.elapsed.Seconds(), 0.001)
	return m
}

func TestFinalizeEmergencyCapacityMetricsSupportUsesP95(t *testing.T) {
	underBudget := make([]time.Duration, 0, 100)
	for range 99 {
		underBudget = append(underBudget, 900*time.Millisecond)
	}
	underBudget = append(underBudget, 1100*time.Millisecond)

	metrics := finalizeEmergencyCapacityMetrics(emergencyCapacityMetrics{
		latencies:    underBudget,
		expectedTick: emergencyCapacityTargetTick,
		missedTicks:  1,
	})
	if !metrics.supported {
		t.Fatalf("expected support when p95=%s is inside the tick budget despite missed ticks", metrics.p95Latency)
	}

	overBudget := make([]time.Duration, 0, 100)
	for range 90 {
		overBudget = append(overBudget, 900*time.Millisecond)
	}
	for range 10 {
		overBudget = append(overBudget, 1100*time.Millisecond)
	}
	metrics = finalizeEmergencyCapacityMetrics(emergencyCapacityMetrics{
		latencies:    overBudget,
		expectedTick: emergencyCapacityTargetTick,
	})
	if metrics.supported {
		t.Fatalf("expected unsupported when p95=%s exceeds the tick budget", metrics.p95Latency)
	}

	metrics = finalizeEmergencyCapacityMetrics(emergencyCapacityMetrics{
		latencies:    underBudget,
		errors:       1,
		expectedTick: emergencyCapacityTargetTick,
	})
	if metrics.supported {
		t.Fatal("expected unsupported when the run has errors")
	}

	metrics = finalizeEmergencyCapacityMetrics(emergencyCapacityMetrics{
		expectedTick: emergencyCapacityTargetTick,
	})
	if metrics.supported {
		t.Fatal("expected unsupported when the run has no latency samples")
	}
}

func flowPercentileDuration(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]time.Duration, len(values))
	copy(sorted, values)
	slices.Sort(sorted)

	idx := int(math.Ceil(float64(len(sorted))*p)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func flowMaxDuration(values []time.Duration) time.Duration {
	var maxVal time.Duration
	for _, value := range values {
		if value > maxVal {
			maxVal = value
		}
	}
	return maxVal
}

func reportEmergencyCapacityMetrics(b *testing.B, m emergencyCapacityMetrics) {
	b.ReportMetric(float64(m.movingObjects), "moving_objects")
	b.ReportMetric(float64(m.floors), "floors")
	b.ReportMetric(float64(m.exits), "exits")
	b.ReportMetric(float64(m.nodes), "nodes")
	b.ReportMetric(float64(m.p50Latency.Microseconds())/1000, "p50_tick_ms")
	b.ReportMetric(float64(m.p95Latency.Microseconds())/1000, "p95_tick_ms")
	b.ReportMetric(float64(m.p99Latency.Microseconds())/1000, "p99_tick_ms")
	b.ReportMetric(float64(m.maxLatency.Microseconds())/1000, "max_tick_ms")
	b.ReportMetric(float64(m.expectedTick.Microseconds())/1000, "target_tick_ms")
	b.ReportMetric(float64(m.observedTicks), "observed_ticks")
	b.ReportMetric(float64(m.missedTicks), "missed_ticks")
	b.ReportMetric(float64(m.environmentTime.Microseconds())/1000/float64(max(1, m.observedTicks)), "environment_ms/tick")
	b.ReportMetric(float64(m.replanTime.Microseconds())/1000/float64(max(1, m.observedTicks)), "replan_ms/tick")
	b.ReportMetric(float64(m.changedCells)/float64(max(1, m.observedTicks)), "changed_cells_per_tick")
	b.ReportMetric(m.changedCellsPerSec, "changed_cells_per_sec")
	b.ReportMetric(float64(m.observedTicks)/math.Max(m.elapsed.Seconds(), 0.001), "ticks_per_sec")
	b.ReportMetric(float64(m.errors), "errors")
	b.ReportMetric(m.heapMB, "heap_mb")
	b.ReportMetric(float64(m.goroutines), "goroutines")
	if m.supported {
		b.ReportMetric(1, "supported")
	} else {
		b.ReportMetric(0, "supported")
	}
}

func BenchmarkFlowfield_MovingObjectsCapacity(b *testing.B) {
	const exits = 10
	const nodes = 20

	for _, objectCount := range emergencyCapacityObjectCounts() {
		b.Run(fmt.Sprintf("MovingObjects=%d", objectCount), func(b *testing.B) {
			var last emergencyCapacityMetrics
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				f := newHospitalFlowBenchFixture(b, exits, nodes)
				b.StartTimer()

				last = runEmergencyMovingObjectsCapacity(b, f, objectCount, emergencyCapacityTicks)
			}
			reportEmergencyCapacityMetrics(b, last)
		})
	}
}

func TestFlowfield_MovingObjectsCapacitySmoke(t *testing.T) {
	f := newFlowBenchFixture(t, 20, 24, 2, 2, 2)
	metrics := runEmergencyMovingObjectsCapacity(t, f, 3, 2)

	if metrics.errors != 0 {
		t.Fatalf("emergency moving-object capacity smoke had %d errors", metrics.errors)
	}
	if metrics.observedTicks != 2 {
		t.Fatalf("observed ticks = %d, want 2", metrics.observedTicks)
	}
	if metrics.changedCells == 0 {
		t.Fatal("expected moving objects to change emergency grid cells")
	}
}

func BenchmarkFlowfield_PlanByExits(b *testing.B) {
	for _, numExits := range []int{1, 5, 10, 50} {
		b.Run(fmt.Sprintf("Exits=%d", numExits), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				f := newFlowBenchFixture(b, 80, 120, 1, numExits, 0)
				b.StartTimer()

				d, err := newSysDStarLite(f.goals, nil, f.world)
				if err != nil {
					b.Fatal(err)
				}
				d.computeShortestPath()

				d.QuitWorldSub.Close()
			}
			b.ReportMetric(float64(numExits), "exits")
		})
	}
}

func BenchmarkFlowfield_PlanMultiFloor(b *testing.B) {
	floors := 3

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		f := newFlowBenchFixture(b, 60, 90, floors, 4, 12)
		b.StartTimer()

		d, err := newSysDStarLite(f.goals, f.nodes, f.world)
		if err != nil {
			b.Fatal(err)
		}
		d.computeShortestPath()

		d.QuitWorldSub.Close()
	}
	b.ReportMetric(float64(floors), "floors")
}

func BenchmarkFlowfield_PlanLarge(b *testing.B) {
	if os.Getenv("PATHFINDING_BENCH_HEAVY") != "1" {
		b.Skip("set PATHFINDING_BENCH_HEAVY=1 to run large flowfield benchmark")
	}

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		f := newFlowBenchFixture(b, 400, 875, 1, 10, 0)
		b.StartTimer()

		d, err := newSysDStarLite(f.goals, nil, f.world)
		if err != nil {
			b.Fatal(err)
		}
		d.computeShortestPath()

		d.QuitWorldSub.Close()
	}
}

func BenchmarkFlowfield_ReplanByChanges(b *testing.B) {
	for _, numChanges := range []int{1, 5, 10, 50} {
		b.Run(fmt.Sprintf("Changes=%d", numChanges), func(b *testing.B) {
			changes := deterministicFlowChanges(80, 120, numChanges)

			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				f := newFlowBenchFixture(b, 80, 120, 1, 10, 0)
				d := newPlannedFlowfield(b, f)
				for j, c := range changes {
					f.floors[0].SetValue(c, grid.Cost(5+j%11))
				}
				b.StartTimer()

				d.applyChanges()

				d.QuitWorldSub.Close()
			}
			b.ReportMetric(float64(numChanges), "changes")
		})
	}
}

func BenchmarkFlowfield_CalculateKey(b *testing.B) {
	f := newFlowBenchFixture(b, 80, 120, 1, 10, 0)
	d := newPlannedFlowfield(b, f)
	defer d.QuitWorldSub.Close()

	cell := grid.GlobalCoords{Coords: grid.Coords{X: 40, Y: 30}, Layer: 0}
	cellIdx := cell.ToIdx(d.World)

	b.ReportAllocs()
	for b.Loop() {
		d.calcKey(cellIdx)
	}
}

func BenchmarkFlowfield_EnvironmentObjectsPipeline(b *testing.B) {
	objectCount := 24

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		f := newFlowBenchFixture(b, 80, 120, 2, 4, 12)
		for j := range objectCount {
			object := &mock.Element{
				IdName: fmt.Sprintf("bench-object-%d", j),
				X:      float64(3+(j*13)%100) * pubdomain.CELL_SIZE_M,
				Y:      float64(3+(j*17)%60) * pubdomain.CELL_SIZE_M,
				Z:      float64(j%2) * 4.3,
				Width:  float64(1+j%4) * pubdomain.CELL_SIZE_M,
				Height: float64(1+(j+1)%4) * pubdomain.CELL_SIZE_M,
			}
			if err := f.geoTruth.AddObject(object); err != nil {
				b.Fatal(err)
			}
		}
		d := newPlannedFlowfield(b, f)
		b.StartTimer()

		if err := d.applyEnvironmentObjects(); err != nil {
			b.Fatal(err)
		}
		if !d.EmptyChangedEdges() {
			d.applyChanges()
		}

		d.QuitWorldSub.Close()
	}
	b.ReportMetric(float64(objectCount), "objects")
}

func BenchmarkFlowfield_ApplyPreferencePaths(b *testing.B) {
	cases := []struct {
		name       string
		floors     int
		waypoints  int
		edges      int
		rows, cols uint
	}{
		{name: "SingleFloor100Edges", floors: 1, waypoints: 100, edges: 100, rows: 80, cols: 120},
		{name: "ThreeFloors300Edges", floors: 3, waypoints: 120, edges: 300, rows: 80, cols: 120},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			graphs := deterministicRouteGraphs(tc.floors, tc.waypoints, tc.edges, tc.rows, tc.cols)

			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				f := newFlowBenchFixture(b, tc.rows, tc.cols, tc.floors, 1, 0)
				b.StartTimer()

				ApplyPreferencePaths(graphs, f.world)
			}
			b.ReportMetric(float64(tc.edges), "route_edges")
			b.ReportMetric(float64(tc.floors), "floors")
		})
	}
}

func deterministicRouteGraphs(floors, waypoints, edges int, rows, cols uint) []pubdomain.RouteGraph {
	graphs := make([]pubdomain.RouteGraph, floors)
	weight := uint16(1)
	for floor := range floors {
		ids := make([]uuid.UUID, waypoints)
		wps := make([]pubdomain.GraphWaypoint, waypoints)
		for i := range waypoints {
			ids[i] = uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%d-%d", floor, i)))
			wps[i] = pubdomain.GraphWaypoint{
				Id:    &ids[i],
				X:     float64(1+(i*13)%max(1, int(cols)-2)) * pubdomain.CELL_SIZE_M,
				Y:     float64(1+(i*17)%max(1, int(rows)-2)) * pubdomain.CELL_SIZE_M,
				Z:     float64(floor) * 4.3,
				Floor: floor,
			}
		}
		graphEdges := make([]pubdomain.GraphEdge, edges/floors)
		for i := range graphEdges {
			from := i % waypoints
			to := (i*7 + 1) % waypoints
			graphEdges[i] = pubdomain.GraphEdge{FromWaypoint: &ids[from], ToWaypoint: &ids[to], Weight: &weight}
		}
		graphs[floor] = pubdomain.RouteGraph{Floor: floor, Waypoints: wps, Edges: graphEdges}
	}
	return graphs
}
