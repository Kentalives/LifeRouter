package performance

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/midtxwn/geotruth/pkg/natspublish"
	"github.com/midtxwn/geotruth/pkg/natsquery"

	"github.com/Kentalives/LifeRouter/embedded"
	"github.com/Kentalives/LifeRouter/internal/app"
	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/mock"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
	pubpathfinding "github.com/Kentalives/LifeRouter/pkg/pathfinding"
	"github.com/nats-io/nats.go"
)

const (
	capacityMovementCellsPerSecond = 7.0
	capacityExternalTickDuration   = time.Second
	capacityExternalTicks          = 30
	capacityRawBatches             = 5
	capacityRealtimeWindow         = 10 * time.Second
	capacityDefaultTickTolerance   = 1.10
	capacityUpdateWaitSlack        = 15 * time.Second
	capacityMaxErrorSamples        = 3
)

type agentInteractionMode struct {
	name          string
	includeAgents bool
}

type capacityAgent struct {
	id    string
	spawn spawnPoint
	goal  [3]float64
}

type capacityRunMetrics struct {
	latencies     []time.Duration
	updates       int
	errorCounts   capacityErrorCounts
	timeouts      int
	steps         int
	elapsed       time.Duration
	heapMB        float64
	goroutines    int
	supported     bool
	p50Latency    time.Duration
	p95Latency    time.Duration
	p99Latency    time.Duration
	maxLatency    time.Duration
	expectedTick  time.Duration
	observedTicks int
	missedTicks   int
	activeAgents  int
	completed     int
	moveRequests  int
}

type capacityErrorCounts struct {
	total                  int
	start                  int
	moveRequest            int
	natsRequest            int
	natsResponse           int
	agentNotFound          int
	agentNoPath            int
	agentTerminated        int
	dispatcherShuttingDown int
	geotruthNotFound       int
	contextDeadline        int
	contextCanceled        int
	other                  int
	startSamples           []string
}

func (c *capacityErrorCounts) addStart(err error) {
	if err == nil {
		return
	}
	c.start++
	if len(c.startSamples) < capacityMaxErrorSamples {
		c.startSamples = append(c.startSamples, err.Error())
	}
	c.add(err)
}

func (c *capacityErrorCounts) addMoveRequest(err error) {
	if err == nil {
		return
	}
	c.moveRequest++
	c.add(err)
}

func (c *capacityErrorCounts) add(err error) {
	c.total++
	classified := false

	if errors.Is(err, pubdomain.ErrNATSRequest) {
		c.natsRequest++
		classified = true
	}
	if errors.Is(err, pubdomain.ErrNATSResponse) {
		c.natsResponse++
		classified = true
	}
	if errors.Is(err, pubdomain.ErrAgentCommNotFound) {
		c.agentNotFound++
		classified = true
	}
	if errors.Is(err, pubdomain.ErrAgentNoPath) {
		c.agentNoPath++
		classified = true
	}
	if errors.Is(err, pubdomain.ErrAgentTerminated) {
		c.agentTerminated++
		classified = true
	}
	if errors.Is(err, pubdomain.ErrDispatcherShuttingDown) {
		c.dispatcherShuttingDown++
		classified = true
	}
	if errors.Is(err, natsquery.ErrNotFound) {
		c.geotruthNotFound++
		classified = true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		c.contextDeadline++
		classified = true
	}
	if errors.Is(err, context.Canceled) {
		c.contextCanceled++
		classified = true
	}
	if !classified {
		c.other++
	}
}

func (c *capacityErrorCounts) addCounts(other capacityErrorCounts) {
	c.total += other.total
	c.start += other.start
	c.moveRequest += other.moveRequest
	c.natsRequest += other.natsRequest
	c.natsResponse += other.natsResponse
	c.agentNotFound += other.agentNotFound
	c.agentNoPath += other.agentNoPath
	c.agentTerminated += other.agentTerminated
	c.dispatcherShuttingDown += other.dispatcherShuttingDown
	c.geotruthNotFound += other.geotruthNotFound
	c.contextDeadline += other.contextDeadline
	c.contextCanceled += other.contextCanceled
	c.other += other.other
	for _, sample := range other.startSamples {
		if len(c.startSamples) >= capacityMaxErrorSamples {
			break
		}
		c.startSamples = append(c.startSamples, sample)
	}
}

type capacityPathfindingPool struct {
	pathfinders []pubpathfinding.Pathfinding
}

type capacityGeoTruth interface {
	AddObject(e *mock.Element) error
	ResetStats()
	UpdateCount(id string) int
	TotalUpdates(ids []string) int
	UpdateTimes(id string) []time.Time
	WaitForUpdates(ctx context.Context, ids []string, target map[string]int) bool
	UpdateObjectPosition(ctx context.Context, objectID string, x, y, z, rotY float64) (natspublish.CommitAck, error)
}

type instrumentedGeoTruth struct {
	base          *mock.GeoTruth
	includeAgents bool

	mu      sync.RWMutex
	updates map[string][]time.Time
}

func newInstrumentedGeoTruth(base *mock.GeoTruth, includeAgents bool) *instrumentedGeoTruth {
	return &instrumentedGeoTruth{
		base:          base,
		includeAgents: includeAgents,
		updates:       make(map[string][]time.Time),
	}
}

func (g *instrumentedGeoTruth) AddObject(e *mock.Element) error {
	return g.base.AddObject(e)
}

func (g *instrumentedGeoTruth) ResetStats() {
	g.mu.Lock()
	defer g.mu.Unlock()
	clear(g.updates)
}

func (g *instrumentedGeoTruth) UpdateCount(id string) int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.updates[id])
}

func (g *instrumentedGeoTruth) TotalUpdates(ids []string) int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	total := 0
	for _, id := range ids {
		total += len(g.updates[id])
	}
	return total
}

func (g *instrumentedGeoTruth) UpdateTimes(id string) []time.Time {
	g.mu.RLock()
	defer g.mu.RUnlock()

	times := g.updates[id]
	out := make([]time.Time, len(times))
	copy(out, times)
	return out
}

func (g *instrumentedGeoTruth) WaitForUpdates(ctx context.Context, ids []string, target map[string]int) bool {
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

func (g *instrumentedGeoTruth) NearbyObjectsOf(ctx context.Context, objectID string, radiusMeters float64, regex *string) ([]natsquery.ObjectOriented, error) {
	objects, err := g.base.NearbyObjectsOf(ctx, objectID, radiusMeters, regex)
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

func (g *instrumentedGeoTruth) ObjectData(ctx context.Context, objectID string) (*natsquery.Object, error) {
	return g.base.ObjectData(ctx, objectID)
}

func (g *instrumentedGeoTruth) AllObjectsOriented(ctx context.Context, regex *string) (natsquery.AllObjectsOrientedResp, error) {
	return g.base.AllObjectsOriented(ctx, regex)
}

func (g *instrumentedGeoTruth) RegionFromPoint(ctx context.Context, x, y, z float64) (string, error) {
	return g.base.RegionFromPoint(ctx, x, y, z)
}

func (g *instrumentedGeoTruth) UpdateObjectPosition(ctx context.Context, objectID string, x, y, z, rotY float64) (natspublish.CommitAck, error) {
	var ack natspublish.CommitAck
	if _, err := g.base.UpdateObjectPosition(ctx, objectID, x, y, z, rotY); err != nil {
		return ack, err
	}

	g.mu.Lock()
	g.updates[objectID] = append(g.updates[objectID], time.Now())
	g.mu.Unlock()
	return ack, nil
}

func syntheticCapacityRun(b testing.TB, nodes []*mock.Node, nObjects int, includeAgents bool) (*app.Dispatcher, *instrumentedGeoTruth) {
	b.Helper()

	nc := testNATSConn(b)

	dep, geo := syntheticCapacityDependencies(b, nodes, nObjects, includeAgents)
	setupTestConfig(b, dep)

	cfg := config.Cfg
	cfg.App.NatsServerUrl = nc.ConnectedUrl()
	disp, err := embedded.Run(context.Background(), cfg, config.Dep)
	if err != nil {
		b.Fatal(err)
	}

	return disp, geo
}

func syntheticCapacityDependencies(b testing.TB, nodes []*mock.Node, nObjects int, includeAgents bool) (*pubconfig.Dependencies, *instrumentedGeoTruth) {
	b.Helper()

	ex, err := mock.NewExternalSystem([]string{"0", "1", "2", "3", "4"}, []float64{0, 10, 20, 30, 40}, nodes)
	if err != nil {
		b.Fatal(err)
	}

	baseGeoTruth := mock.NewGeoTruth(nObjects+len(nodes), ex)
	geoTruth := newInstrumentedGeoTruth(baseGeoTruth, includeAgents)
	for _, n := range nodes {
		if err := geoTruth.AddObject(&n.Element); err != nil {
			b.Fatal(err)
		}
	}

	return &pubconfig.Dependencies{Ex: ex, Qu: geoTruth, Pu: geoTruth}, geoTruth
}

func capacityAgentCounts() []int {
	if os.Getenv("PATHFINDING_BENCH_HEAVY") == "1" {
		return []int{1, 5, 10, 20, 50, 100, 150, 175, 200} //, 200, 400, 700, 1000}
	}
	return []int{1, 5, 10, 20}
}

func buildCapacityAgents(n int, prefix string) []capacityAgent {
	agents := make([]capacityAgent, 0, n)
	for i := range n {
		agents = append(agents, capacityAgent{
			id:    fmt.Sprintf("%s_%d", prefix, i),
			spawn: spawnPoints[i%len(spawnPoints)],
			goal:  goals[i%len(goals)],
		})
	}
	return agents
}

func capacityAgentIDs(agents []capacityAgent) []string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.id)
	}
	return ids
}

func registerCapacityAgents(b testing.TB, geo capacityGeoTruth, agents []capacityAgent) {
	b.Helper()

	for _, agent := range agents {
		if err := geo.AddObject(capacityAgentElement(agent)); err != nil {
			b.Fatal(err)
		}
	}
}

func registerCapacityAgentsWithWorkers(b testing.TB, geo capacityGeoTruth, agents []capacityAgent, workers int) {
	b.Helper()

	if len(agents) == 0 {
		return
	}
	if workers <= 1 {
		registerCapacityAgents(b, geo, agents)
		return
	}

	workers = min(workers, len(agents))
	jobs := make(chan capacityAgent)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error

	for range workers {
		wg.Go(func() {
			for agent := range jobs {
				if err := geo.AddObject(capacityAgentElement(agent)); err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
				}
			}
		})
	}

	for _, agent := range agents {
		jobs <- agent
	}
	close(jobs)
	wg.Wait()

	if firstErr != nil {
		b.Fatal(firstErr)
	}
}

func capacityAgentElement(agent capacityAgent) *mock.Element {
	x, y := spawnMeters(agent.spawn)
	return &mock.Element{
		X:      x,
		Y:      y,
		Z:      agent.spawn.Z,
		RotY:   0,
		IdName: agent.id,
		Width:  grid.CellSizeM(),
		Height: grid.CellSizeM(),
	}
}

func resetCapacityAgents(b testing.TB, geo capacityGeoTruth, agents []capacityAgent) {
	b.Helper()

	for _, agent := range agents {
		x, y := spawnMeters(agent.spawn)
		if _, err := geo.UpdateObjectPosition(context.Background(), agent.id, x, y, agent.spawn.Z, 0); err != nil {
			b.Fatal(err)
		}
	}
	geo.ResetStats()
}

func spawnMeters(spawn spawnPoint) (float64, float64) {
	return float64(spawn.Coord[0]) * grid.CellSizeM(), float64(spawn.Coord[1]) * grid.CellSizeM()
}

func goalMeters(goal [3]float64) [3]float64 {
	return [3]float64{goal[0] * grid.CellSizeM(), goal[1] * grid.CellSizeM(), goal[2]}
}

func newCapacityPathfindingPool(b testing.TB) capacityPathfindingPool {
	b.Helper()

	size := max(1, runtime.GOMAXPROCS(0))
	pathfinders := make([]pubpathfinding.Pathfinding, 0, size)
	for range size {
		nc, err := nats.Connect(config.Cfg.App.NatsServerUrl)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(nc.Close)
		pathfinders = append(pathfinders, pubpathfinding.New(nc))
	}

	return capacityPathfindingPool{pathfinders: pathfinders}
}

func (p capacityPathfindingPool) Pick(agentIndex int) pubpathfinding.Pathfinding {
	return p.pathfinders[agentIndex%len(p.pathfinders)]
}

func (p capacityPathfindingPool) Size() int {
	return len(p.pathfinders)
}

func startCapacityAgents(ctx context.Context, pool capacityPathfindingPool, agents []capacityAgent, speed float64) ([]*pubpathfinding.AgentCommunicator, capacityErrorCounts) {
	comms := make([]*pubpathfinding.AgentCommunicator, len(agents))
	errCounts := capacityErrorCounts{}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, agent := range agents {
		wg.Go(func() {
			p := pool.Pick(i)
			comm, err := p.AgentFindPath(ctx, goalMeters(agent.goal), agent.id, speed, 0)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errCounts.addStart(err)
				return
			}
			comms[i] = comm
		})
	}
	wg.Wait()

	return comms, errCounts
}

func terminateCapacityAgents(ctx context.Context, comms []*pubpathfinding.AgentCommunicator) {
	var wg sync.WaitGroup
	for _, comm := range comms {
		if comm == nil {
			continue
		}
		wg.Go(func() {
			_ = comm.Terminate(ctx)
		})
	}
	wg.Wait()
}

func runRawCapacityScenario(ctx context.Context, pool capacityPathfindingPool, geo capacityGeoTruth, agents []capacityAgent) capacityRunMetrics {
	ids := capacityAgentIDs(agents)
	comms, errCounts := startCapacityAgents(ctx, pool, agents, 0)
	defer terminateCapacityAgents(context.Background(), comms)

	stepsPerBatch := max(1, config.Cfg.Pathfinding.Agent.CellsForRealUpdate)
	expectedTick := time.Duration(float64(time.Second) * float64(stepsPerBatch) / capacityMovementCellsPerSecond)
	latencies := make([]time.Duration, 0, capacityRawBatches)
	timeouts := 0

	ok, batchErrs := runRawMovementBatch(ctx, geo, ids, comms, stepsPerBatch, expectedTick+capacityUpdateWaitSlack)
	errCounts.addCounts(batchErrs)
	if !ok {
		return finalizeCapacityMetrics(capacityRunMetrics{
			errorCounts:  errCounts,
			timeouts:     1,
			expectedTick: expectedTick,
			activeAgents: len(agents),
		}, capacityDefaultTickTolerance)
	}
	geo.ResetStats()
	start := time.Now()

	for range capacityRawBatches {
		batchStart := time.Now()
		ok, batchErrs := runRawMovementBatch(ctx, geo, ids, comms, stepsPerBatch, expectedTick+capacityUpdateWaitSlack)
		errCounts.addCounts(batchErrs)
		if !ok {
			timeouts++
			break
		}
		latencies = append(latencies, time.Since(batchStart))
	}

	return finalizeCapacityMetrics(capacityRunMetrics{
		latencies:     latencies,
		updates:       geo.TotalUpdates(ids),
		errorCounts:   errCounts,
		timeouts:      timeouts,
		steps:         len(latencies) * len(agents) * stepsPerBatch,
		elapsed:       time.Since(start),
		expectedTick:  expectedTick,
		observedTicks: len(latencies),
		activeAgents:  len(agents),
		moveRequests:  len(latencies) * len(agents),
	}, capacityDefaultTickTolerance)
}

func runRawMovementBatch(ctx context.Context, geo capacityGeoTruth, ids []string, comms []*pubpathfinding.AgentCommunicator, stepsPerBatch int, timeout time.Duration) (bool, capacityErrorCounts) {
	target := make(map[string]int, len(ids))
	for _, id := range ids {
		target[id] = geo.UpdateCount(id) + 1
	}

	errCounts := capacityErrorCounts{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, comm := range comms {
		if comm == nil {
			continue
		}
		wg.Go(func() {
			if err := comm.MoveNCells(ctx, uint(stepsPerBatch)); err != nil {
				mu.Lock()
				errCounts.addMoveRequest(err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	ok := geo.WaitForUpdates(waitCtx, ids, target)
	cancel()
	return ok, errCounts
}

func runExternalTickCapacityScenario(ctx context.Context, pool capacityPathfindingPool, geo capacityGeoTruth, agents []capacityAgent) capacityRunMetrics {
	ids := capacityAgentIDs(agents)
	comms, errCounts := startCapacityAgents(ctx, pool, agents, 0)
	defer terminateCapacityAgents(context.Background(), comms)

	active := make([]bool, len(comms))
	leftovers := make([]float64, len(comms))
	activeCount := 0
	for i, comm := range comms {
		if comm == nil {
			continue
		}
		active[i] = true
		activeCount++
	}

	metersPerTick := capacityMovementCellsPerSecond * grid.CellSizeM() * capacityExternalTickDuration.Seconds()
	latencies := make([]time.Duration, 0, capacityExternalTicks)
	missedTicks := 0
	timeouts := 0
	moveRequests := 0
	completedTotal := 0

	warmupErrs, warmupTimeouts, warmupCompleted, _ := runExternalMoveMetersTick(ctx, comms, active, leftovers, metersPerTick, capacityExternalTickDuration+capacityUpdateWaitSlack)
	errCounts.addCounts(warmupErrs)
	timeouts += warmupTimeouts
	activeCount -= warmupCompleted
	completedTotal += warmupCompleted

	start := time.Now()
	for range capacityExternalTicks {
		if activeCount == 0 {
			break
		}

		tickStart := time.Now()
		tickErrs, tickTimeouts, tickCompleted, tickRequests := runExternalMoveMetersTick(ctx, comms, active, leftovers, metersPerTick, capacityExternalTickDuration)
		errCounts.addCounts(tickErrs)
		timeouts += tickTimeouts
		activeCount -= tickCompleted
		completedTotal += tickCompleted
		moveRequests += tickRequests

		latency := time.Since(tickStart)
		if latency > capacityExternalTickDuration {
			missedTicks++
		}
		latencies = append(latencies, latency)

		if tickTimeouts > 0 {
			break
		}
	}

	updates := geo.TotalUpdates(ids)
	stepsPerUpdate := max(1, config.Cfg.Pathfinding.Agent.CellsForRealUpdate)
	return finalizeCapacityMetrics(capacityRunMetrics{
		latencies:     latencies,
		updates:       updates,
		errorCounts:   errCounts,
		timeouts:      timeouts,
		steps:         updates * stepsPerUpdate,
		elapsed:       time.Since(start),
		expectedTick:  capacityExternalTickDuration,
		observedTicks: len(latencies),
		missedTicks:   missedTicks,
		activeAgents:  activeCount,
		completed:     completedTotal,
		moveRequests:  moveRequests,
	}, 1.0)
}

func runExternalMoveMetersTick(ctx context.Context, comms []*pubpathfinding.AgentCommunicator, active []bool, leftovers []float64, metersPerTick float64, timeout time.Duration) (errCounts capacityErrorCounts, timeouts int, completed int, requests int) {
	type result struct {
		i         int
		remaining float64
		err       error
	}

	tickCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	results := make(chan result, len(comms))
	var wg sync.WaitGroup
	for i, comm := range comms {
		if comm == nil || !active[i] {
			continue
		}
		requests++
		move := metersPerTick + leftovers[i]
		wg.Go(func() {
			remaining, err := comm.MoveFMeters(tickCtx, move)
			results <- result{i: i, remaining: remaining, err: err}
		})
	}

	wg.Wait()
	close(results)

	for res := range results {
		if res.err == nil {
			leftovers[res.i] = res.remaining
			continue
		}
		if errors.Is(res.err, context.DeadlineExceeded) || errors.Is(res.err, context.Canceled) {
			timeouts++
			continue
		}
		if errors.Is(res.err, pubdomain.ErrAgentExitedWithMetersLeft) {
			active[res.i] = false
			leftovers[res.i] = 0
			completed++
			continue
		}
		errCounts.addMoveRequest(res.err)
	}

	return errCounts, timeouts, completed, requests
}

func runRealtimeCapacityScenario(ctx context.Context, pool capacityPathfindingPool, geo capacityGeoTruth, agents []capacityAgent) capacityRunMetrics {
	ids := capacityAgentIDs(agents)
	start := time.Now()
	comms, errCounts := startCapacityAgents(ctx, pool, agents, capacityMovementCellsPerSecond)
	defer terminateCapacityAgents(context.Background(), comms)

	stepsPerUpdate := max(1, config.Cfg.Pathfinding.Agent.CellsForRealUpdate)
	expectedTick := time.Duration(float64(time.Second) * float64(stepsPerUpdate) / capacityMovementCellsPerSecond)

	select {
	case <-ctx.Done():
	case <-time.After(capacityRealtimeWindow):
	}

	latencies := make([]time.Duration, 0)
	for _, id := range ids {
		times := geo.UpdateTimes(id)
		for i := 1; i < len(times); i++ {
			latencies = append(latencies, times[i].Sub(times[i-1]))
		}
	}

	updates := geo.TotalUpdates(ids)
	return finalizeCapacityMetrics(capacityRunMetrics{
		latencies:     latencies,
		updates:       updates,
		errorCounts:   errCounts,
		steps:         updates * stepsPerUpdate,
		elapsed:       time.Since(start),
		expectedTick:  expectedTick,
		observedTicks: len(latencies),
		activeAgents:  len(agents),
	}, capacityDefaultTickTolerance)
}

func finalizeCapacityMetrics(m capacityRunMetrics, tolerance float64) capacityRunMetrics {
	m.p50Latency = percentileDuration(m.latencies, 0.50)
	m.p95Latency = percentileDuration(m.latencies, 0.95)
	m.p99Latency = percentileDuration(m.latencies, 0.99)
	m.maxLatency = maxDuration(m.latencies)
	m.supported = m.errorCounts.total == 0 && m.timeouts == 0 && m.missedTicks == 0 && len(m.latencies) > 0 && float64(m.p95Latency) <= float64(m.expectedTick)*tolerance

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	m.heapMB = float64(mem.Alloc) / 1024 / 1024
	m.goroutines = runtime.NumGoroutine()
	return m
}

func percentileDuration(values []time.Duration, p float64) time.Duration {
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

func maxDuration(values []time.Duration) time.Duration {
	var maxVal time.Duration
	for _, value := range values {
		if value > maxVal {
			maxVal = value
		}
	}
	return maxVal
}

func reportCapacityMetrics(b *testing.B, n int, poolSize int, m capacityRunMetrics) {
	b.ReportMetric(float64(n), "agents")
	b.ReportMetric(float64(m.activeAgents), "active_agents")
	b.ReportMetric(float64(m.completed), "completed_agents")
	b.ReportMetric(float64(poolSize), "nats_client_pool")
	b.ReportMetric(float64(runtime.GOMAXPROCS(0)), "gomaxprocs")
	b.ReportMetric(float64(m.p50Latency.Microseconds())/1000, "p50_tick_ms")
	b.ReportMetric(float64(m.p95Latency.Microseconds())/1000, "p95_tick_ms")
	b.ReportMetric(float64(m.p99Latency.Microseconds())/1000, "p99_tick_ms")
	b.ReportMetric(float64(m.maxLatency.Microseconds())/1000, "max_tick_ms")
	b.ReportMetric(float64(m.expectedTick.Microseconds())/1000, "target_tick_ms")
	b.ReportMetric(float64(m.observedTicks), "observed_ticks")
	b.ReportMetric(float64(m.missedTicks), "missed_ticks")
	b.ReportMetric(float64(m.updates)/max(m.elapsed.Seconds(), 0.001), "updates_per_sec")
	b.ReportMetric(float64(m.steps)/max(m.elapsed.Seconds(), 0.001), "steps_per_sec")
	b.ReportMetric(float64(m.moveRequests)/max(m.elapsed.Seconds(), 0.001), "move_requests_per_sec")
	b.ReportMetric(float64(m.errorCounts.total), "errors")
	b.ReportMetric(float64(m.errorCounts.start), "start_errors")
	b.ReportMetric(float64(m.errorCounts.moveRequest), "move_request_errors")
	b.ReportMetric(float64(m.errorCounts.natsRequest), "nats_request_errors")
	b.ReportMetric(float64(m.errorCounts.natsResponse), "nats_response_errors")
	b.ReportMetric(float64(m.errorCounts.agentNotFound), "agent_not_found_errors")
	b.ReportMetric(float64(m.errorCounts.agentNoPath), "agent_no_path_errors")
	b.ReportMetric(float64(m.errorCounts.agentTerminated), "agent_terminated_errors")
	b.ReportMetric(float64(m.errorCounts.dispatcherShuttingDown), "dispatcher_shutdown_errors")
	b.ReportMetric(float64(m.errorCounts.geotruthNotFound), "geotruth_not_found_errors")
	b.ReportMetric(float64(m.errorCounts.contextDeadline), "context_deadline_errors")
	b.ReportMetric(float64(m.errorCounts.contextCanceled), "context_canceled_errors")
	b.ReportMetric(float64(m.errorCounts.other), "other_errors")
	b.ReportMetric(float64(m.timeouts), "timeouts")
	b.ReportMetric(m.heapMB, "heap_mb")
	b.ReportMetric(float64(m.goroutines), "goroutines")
	if m.supported {
		b.ReportMetric(1, "supported")
	} else {
		b.ReportMetric(0, "supported")
	}
	if len(m.errorCounts.startSamples) > 0 {
		b.Logf("start error samples: %s", strings.Join(m.errorCounts.startSamples, " | "))
	}
}

func BenchmarkSynthetic_AgentCapacity(b *testing.B) {
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
					disp, geo := syntheticCapacityRun(b, nil, n, interaction.includeAgents)
					b.Cleanup(disp.Shutdown)

					agents := buildCapacityAgents(n, fmt.Sprintf("Agent_capacity_%s_%s", mode.name, interaction.name))
					registerCapacityAgents(b, geo, agents)

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

func TestSyntheticCapacitySmoke(t *testing.T) {
	disp, geo := syntheticCapacityRun(t, nil, 1, false)
	defer disp.Shutdown()

	agents := buildCapacityAgents(1, "Agent_capacity_smoke")
	registerCapacityAgents(t, geo, agents)

	pool := newCapacityPathfindingPool(t)

	resetCapacityAgents(t, geo, agents)
	ctx, cancel := context.WithTimeout(context.Background(), capacityRealtimeWindow+capacityUpdateWaitSlack)
	defer cancel()

	metrics := runRawCapacityScenario(ctx, pool, geo, agents)
	if metrics.errorCounts.total != 0 {
		t.Fatalf("raw capacity smoke had %d errors", metrics.errorCounts.total)
	}
	if metrics.timeouts != 0 {
		t.Fatalf("raw capacity smoke had %d timeouts", metrics.timeouts)
	}
	if metrics.updates == 0 {
		t.Fatal("raw capacity smoke did not observe movement updates")
	}

	resetCapacityAgents(t, geo, agents)
	ctx, cancel = context.WithTimeout(context.Background(), capacityRealtimeWindow+capacityUpdateWaitSlack)
	defer cancel()

	metrics = runExternalTickCapacityScenario(ctx, pool, geo, agents)
	if metrics.errorCounts.total != 0 {
		t.Fatalf("external tick capacity smoke had %d errors", metrics.errorCounts.total)
	}
	if metrics.timeouts != 0 {
		t.Fatalf("external tick capacity smoke had %d timeouts", metrics.timeouts)
	}
	if metrics.observedTicks == 0 {
		t.Fatal("external tick capacity smoke did not observe ticks")
	}
}
