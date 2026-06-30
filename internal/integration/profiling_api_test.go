package integration

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	embeddedGeo "github.com/midtxwn/geotruth/embedded"
	ingesterDomain "github.com/midtxwn/geotruth/pkg/domain"
	"github.com/midtxwn/geotruth/pkg/natsclient"
	"github.com/midtxwn/geotruth/pkg/natspublish"

	"github.com/Kentalives/LifeRouter/embedded"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
	"github.com/Kentalives/LifeRouter/pkg/emergency"
	"github.com/Kentalives/LifeRouter/pkg/pathfinding"
	"github.com/google/uuid"
)

// API benchmarks measure the public wrapper path through NATS and embedded
// service stacks. They are opt-in because they start real service dependencies.

const apiBenchTimeout = time.Second * 1000000000 //1000 //10

type apiBenchClientMode string

const (
	apiBenchSharedClient   apiBenchClientMode = "shared"
	apiBenchPerAgentClient apiBenchClientMode = "per-agent"
)

type apiBenchOptions struct {
	clientMode            apiBenchClientMode
	workerScale           int
	geoQueryConnections   int
	geoPublishConnections int
}

type apiBenchStack struct {
	client  *natsclient.Client
	natsURL string
	cleanup func()
}

type apiBenchMetrics struct {
	findPathNs        atomic.Int64
	findPathCalls     atomic.Int64
	moveFMetersNs     atomic.Int64
	moveFMetersCalls  atomic.Int64
	terminateNs       atomic.Int64
	terminateCalls    atomic.Int64
	blockingWaitNs    atomic.Int64
	blockingWaitCalls atomic.Int64
	expectedMoveErrs  atomic.Int64
	unexpectedErrs    atomic.Int64
}

func requireAPIBench(b *testing.B) {
	b.Helper()
	if os.Getenv("PATHFINDING_BENCH_API") != "1" {
		b.Skip("set PATHFINDING_BENCH_API=1 to run API/NATS benchmarks")
	}
}

func setupAPIBench(b *testing.B) (*natsclient.Client, func()) {
	stack := setupAPIBenchStack(b, apiBenchOptions{})
	return stack.client, stack.cleanup
}

func setupAPIBenchStack(b *testing.B, opts apiBenchOptions) apiBenchStack {
	b.Helper()
	opts = apiBenchOptsOrDefault(opts)

	ctx, cancel := context.WithCancel(context.Background())
	serv, err := embeddedGeo.RunLocalStack(ctx, embeddedGeo.DefaultConfig, embeddedGeo.DefaultDependencies)
	if err != nil {
		cancel()
		b.Fatal(err)
	}

	cfg := embedded.DefaultConfig()
	cfg.App.NatsServerUrl = serv.NATSURL()
	cfg.Pathfinding.Geotruth.QueryConnections = opts.geoQueryConnections
	cfg.Pathfinding.Geotruth.PublishConnections = opts.geoPublishConnections
	if opts.workerScale > 1 {
		cfg.Pathfinding.Agent.FindPathHandlerWorkers *= opts.workerScale
		cfg.Pathfinding.Agent.MoveFMetersHandlerWorkers *= opts.workerScale
		cfg.Pathfinding.Agent.BlockingWaitHandlerWorkers *= opts.workerScale
	}
	dep, err := embedded.DefaultDependencies(ctx, cfg)
	if err != nil {
		serv.Shutdown()
		cancel()
		b.Fatal(err)
	}
	disp, err := embedded.Run(ctx, cfg, dep)
	if err != nil {
		serv.Shutdown()
		cancel()
		b.Fatal(err)
	}

	client, err := natsclient.New(ctx, cfg.App.NatsServerUrl)
	if err != nil {
		disp.Shutdown()
		serv.Shutdown()
		cancel()
		b.Fatal(err)
	}

	cleanup := func() {
		client.Close()
		disp.Shutdown()
		serv.Shutdown()
		cancel()
	}
	return apiBenchStack{client: client, natsURL: cfg.App.NatsServerUrl, cleanup: cleanup}
}

func apiBenchContext(b *testing.B) (context.Context, context.CancelFunc) {
	b.Helper()
	return context.WithTimeout(context.Background(), apiBenchTimeout)
}

func apiBenchOptsOrDefault(opts apiBenchOptions) apiBenchOptions {
	if opts.clientMode == "" {
		opts.clientMode = apiBenchSharedClient
	}
	if opts.workerScale <= 0 {
		opts.workerScale = 1
	}
	if opts.geoQueryConnections <= 0 {
		opts.geoQueryConnections = runtime.GOMAXPROCS(0)
	}
	if opts.geoPublishConnections <= 0 {
		opts.geoPublishConnections = runtime.GOMAXPROCS(0)
	}
	return opts
}

func apiBenchPathfinders(b *testing.B, stack apiBenchStack, n int, opts apiBenchOptions) ([]pathfinding.Pathfinding, func()) {
	b.Helper()

	opts = apiBenchOptsOrDefault(opts)
	pathfinders := make([]pathfinding.Pathfinding, n)
	if opts.clientMode == apiBenchSharedClient {
		p := pathfinding.New(stack.client.Conn())
		for i := range pathfinders {
			pathfinders[i] = p
		}
		return pathfinders, func() {}
	}

	clients := make([]*natsclient.Client, 0, n)
	for i := range pathfinders {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		client, err := natsclient.New(ctx, stack.natsURL)
		cancel()
		if err != nil {
			for _, c := range clients {
				c.Close()
			}
			b.Fatal(err)
		}
		clients = append(clients, client)
		pathfinders[i] = pathfinding.New(client.Conn())
	}

	return pathfinders, func() {
		for _, c := range clients {
			c.Close()
		}
	}
}

func (m *apiBenchMetrics) report(b *testing.B, n int, iterations int64, opts apiBenchOptions) {
	b.Helper()

	opts = apiBenchOptsOrDefault(opts)
	b.ReportMetric(float64(n), "agents")
	b.ReportMetric(float64(runtime.GOMAXPROCS(0)), "gomaxprocs")
	b.ReportMetric(float64(runtime.NumGoroutine()), "goroutines")
	b.ReportMetric(float64(opts.workerScale), "worker_scale")
	b.ReportMetric(float64(opts.geoQueryConnections), "geo_query_conns")
	b.ReportMetric(float64(opts.geoPublishConnections), "geo_publish_conns")

	reportAvg := func(totalNs, calls int64, name string) {
		if calls == 0 {
			return
		}
		b.ReportMetric(float64(totalNs)/float64(calls)/float64(time.Millisecond), name)
	}

	reportAvg(m.findPathNs.Load(), m.findPathCalls.Load(), "findpath_ms/request")
	reportAvg(m.moveFMetersNs.Load(), m.moveFMetersCalls.Load(), "movefmeters_ms/request")
	reportAvg(m.terminateNs.Load(), m.terminateCalls.Load(), "terminate_ms/request")
	reportAvg(m.blockingWaitNs.Load(), m.blockingWaitCalls.Load(), "blockingwait_ms/request")

	if iterations > 0 {
		b.ReportMetric(float64(m.expectedMoveErrs.Load())/float64(iterations), "expected_move_errors/op")
		b.ReportMetric(float64(m.unexpectedErrs.Load())/float64(iterations), "unexpected_errors/op")
	}
}

func recordAPIBenchDuration(total *atomic.Int64, calls *atomic.Int64, start time.Time) {
	total.Add(time.Since(start).Nanoseconds())
	calls.Add(1)
}

func registerBenchmarkAgent(b *testing.B, nc *natsclient.Client, id string) {
	b.Helper()

	pu := natspublish.New(nc.Conn())

	if _, err := pu.RegisterObject(context.Background(), id, ingesterDomain.ObjectDimensions{Width: pubdomain.CELL_SIZE_M, Height: pubdomain.CELL_SIZE_M}); err != nil {
		b.Fatal(err)
	}
	if _, err := pu.UpdateObjectPosition(context.Background(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0); err != nil {
		b.Fatal(err)
	}
}

func registerBenchmarkAgentMultiple(b *testing.B, nc *natsclient.Client, ids []string) {
	b.Helper()

	pu := natspublish.New(nc.Conn())

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() {
			if _, err := pu.RegisterObject(context.Background(), id, ingesterDomain.ObjectDimensions{Width: pubdomain.CELL_SIZE_M, Height: pubdomain.CELL_SIZE_M}); err != nil {
				b.Error(err)
				log.Printf("ERR: %s", err)
				return
			}
			if _, err := pu.UpdateObjectPosition(context.Background(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0); err != nil {
				b.Error(err)
				return
			}
		})
		//if err := pu.RegisterObject(context.Background(), id, ingesterDomain.ObjectDimensions{Width: pubdomain.CELL_SIZE_M, Height: pubdomain.CELL_SIZE_M}); err != nil {
		//	b.Error(err)
		//	log.Printf("ERR: %s", err)
		//	return
		//}
		//if err := pu.UpdateObjectPosition(context.Background(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0); err != nil {
		//	b.Error(err)
		//	return
		//}
	}
	wg.Wait()
}

func BenchmarkAPI_AgentNaivePathCost(b *testing.B) {
	requireAPIBench(b)

	client, cleanup := setupAPIBench(b)
	defer cleanup()

	const agentID = "Agent_api_cost_bench"
	registerBenchmarkAgent(b, client, agentID)
	p := pathfinding.New(client.Conn())
	goal := [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}

	b.ReportAllocs()
	for b.Loop() {
		ctx, cancel := apiBenchContext(b)
		_, err := p.AgentNaivePathCost(ctx, goal, agentID)
		cancel()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAPI_AgentFindPathStartTerminate(b *testing.B) {
	requireAPIBench(b)

	client, cleanup := setupAPIBench(b)
	defer cleanup()

	const agentID = "Agent_api_start_bench"
	registerBenchmarkAgent(b, client, agentID)
	p := pathfinding.New(client.Conn())
	goal := [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}

	b.ReportAllocs()
	for b.Loop() {
		ctx, cancel := apiBenchContext(b)
		comm, err := p.AgentFindPath(ctx, goal, agentID, 0, 0)
		cancel()
		if err != nil {
			b.Fatal(err)
		}

		done, err := comm.BlockingWait()
		if err != nil {
			b.Fatal(err)
		}

		ctx, cancel = apiBenchContext(b)
		err = comm.Terminate(ctx)
		cancel()
		if err != nil {
			b.Fatal(err)
		}

		select {
		case <-done:
		case <-time.After(apiBenchTimeout):
			b.Fatal("agent did not finish after terminate")
		}
	}
}

func BenchmarkAPI_EmergencyStartStopFreshStack(b *testing.B) {
	requireAPIBench(b)

	weight := uint16(1)
	id1, id2 := uuid.New(), uuid.New()
	graphs := []pubdomain.RouteGraph{
		{
			Floor: 0,
			Waypoints: []pubdomain.GraphWaypoint{
				{Id: &id1, X: 15.5 * pubdomain.CELL_SIZE_M, Y: 22.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &id2, X: 72.5 * pubdomain.CELL_SIZE_M, Y: 22.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
			},
			Edges: []pubdomain.GraphEdge{{FromWaypoint: &id1, ToWaypoint: &id2, Weight: &weight}},
		},
	}
	goals := [][3]float64{{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}}

	b.ReportAllocs()
	for b.Loop() {
		// Use a fresh stack per iteration so each run starts from a clean global World.
		b.StopTimer()
		client, cleanup := setupAPIBench(b)
		e := emergency.New(client.Conn())
		b.StartTimer()

		ctx, cancel := apiBenchContext(b)
		err := e.Start(ctx, goals, graphs, 10)
		cancel()
		if err != nil {
			b.Fatal(err)
		}

		ctx, cancel = apiBenchContext(b)
		err = e.Stop(ctx)
		cancel()
		if err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		cleanup()
		b.StartTimer()
	}
}

func BenchmarkAPI_AgentFindPathByAgents(b *testing.B) {
	requireAPIBench(b)

	for i, n := range []int{1, 5, 10, 60, 100} { //, 100, 450, 1000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			benchmarkAPIAgentFindPathByAgents(b, i, n, apiBenchOptions{})
		})
	}

}

func BenchmarkAPI_AgentFindPathAndResetByAgents(b *testing.B) {
	requireAPIBench(b)

	for i, n := range []int{1, 5, 10, 60, 100} { //, 100, 450, 1000} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			benchmarkAPIAgentFindPathAndResetByAgents(b, i, n, apiBenchOptions{})
		})
	}

}

func BenchmarkAPI_AgentFindPathByAgentsVariants(b *testing.B) {
	requireAPIBench(b)

	gomaxprocs := runtime.GOMAXPROCS(0)
	variants := []apiBenchOptions{
		{clientMode: apiBenchSharedClient, workerScale: 1, geoQueryConnections: 1, geoPublishConnections: 1},
		{clientMode: apiBenchSharedClient, workerScale: 1, geoQueryConnections: gomaxprocs, geoPublishConnections: gomaxprocs},
		{clientMode: apiBenchSharedClient, workerScale: 4, geoQueryConnections: gomaxprocs, geoPublishConnections: gomaxprocs},
		{clientMode: apiBenchPerAgentClient, workerScale: 1, geoQueryConnections: gomaxprocs, geoPublishConnections: gomaxprocs},
	}
	for i, opts := range variants {
		opts := opts
		b.Run(fmt.Sprintf("conn=%s/workers=x%d/geo=q%d-p%d/N=100", opts.clientMode, opts.workerScale, opts.geoQueryConnections, opts.geoPublishConnections), func(b *testing.B) {
			benchmarkAPIAgentFindPathByAgents(b, i, 100, opts)
		})
	}
}

func BenchmarkAPI_AgentFindPathAndResetByAgentsVariants(b *testing.B) {
	requireAPIBench(b)

	gomaxprocs := runtime.GOMAXPROCS(0)
	variants := []apiBenchOptions{
		{clientMode: apiBenchSharedClient, workerScale: 1, geoQueryConnections: 1, geoPublishConnections: 1},
		{clientMode: apiBenchSharedClient, workerScale: 1, geoQueryConnections: gomaxprocs, geoPublishConnections: gomaxprocs},
		{clientMode: apiBenchSharedClient, workerScale: 4, geoQueryConnections: gomaxprocs, geoPublishConnections: gomaxprocs},
		{clientMode: apiBenchPerAgentClient, workerScale: 1, geoQueryConnections: gomaxprocs, geoPublishConnections: gomaxprocs},
	}
	for i, opts := range variants {
		opts := opts
		b.Run(fmt.Sprintf("conn=%s/workers=x%d/geo=q%d-p%d/N=100", opts.clientMode, opts.workerScale, opts.geoQueryConnections, opts.geoPublishConnections), func(b *testing.B) {
			benchmarkAPIAgentFindPathAndResetByAgents(b, i, 100, opts)
		})
	}
}

func benchmarkAPIAgentFindPathByAgents(b *testing.B, runID, n int, opts apiBenchOptions) {
	opts = apiBenchOptsOrDefault(opts)
	stack := setupAPIBenchStack(b, opts)
	defer stack.cleanup()

	ids := make([]string, 0, n)
	for j := range n {
		ids = append(ids, fmt.Sprintf("Agent_%d_%d", runID, j))
	}

	log.Println("STARTING REGISTRATION")
	registerBenchmarkAgentMultiple(b, stack.client, ids)

	pathfinders, cleanupClients := apiBenchPathfinders(b, stack, n, opts)
	defer cleanupClients()

	pos2 := [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}
	pos1 := [3]float64{15.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}
	log.Printf("STARTING MOVEMENT conn=%s workers=x%d geo=q%d-p%d agents=%d", opts.clientMode, opts.workerScale, opts.geoQueryConnections, opts.geoPublishConnections, n)

	var metrics apiBenchMetrics
	var iterations int64
	shouldWalkTo2 := true
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		iterations++

		goal := pos1
		if shouldWalkTo2 {
			goal = pos2
		}
		shouldWalkTo2 = !shouldWalkTo2

		var wg sync.WaitGroup
		ctx, cancel := apiBenchContext(b)
		for idx, id := range ids {
			idx, id := idx, id
			wg.Go(func() {
				start := time.Now()
				comm, err := pathfinders[idx].AgentFindPath(ctx, goal, id, 0, 0)
				recordAPIBenchDuration(&metrics.findPathNs, &metrics.findPathCalls, start)
				if err != nil {
					metrics.unexpectedErrs.Add(1)
					b.Errorf("Agent %s find path: %s", id, err)
					return
				}

				start = time.Now()
				_, err = comm.MoveFMeters(ctx, 100000)
				recordAPIBenchDuration(&metrics.moveFMetersNs, &metrics.moveFMetersCalls, start)
				if err != nil {
					if errors.Is(err, pubdomain.ErrAgentExitedWithMetersLeft) {
						metrics.expectedMoveErrs.Add(1)
						return
					}
					metrics.unexpectedErrs.Add(1)
					b.Errorf("Agent %s move meters: %s", id, err)
				}
			})
		}
		wg.Wait()
		cancel()
	}
	b.StopTimer()
	metrics.report(b, n, iterations, opts)
}

func benchmarkAPIAgentFindPathAndResetByAgents(b *testing.B, runID, n int, opts apiBenchOptions) {
	opts = apiBenchOptsOrDefault(opts)
	stack := setupAPIBenchStack(b, opts)
	defer stack.cleanup()

	ids := make([]string, 0, n)
	for j := range n {
		ids = append(ids, fmt.Sprintf("Agent_%d_%d", runID, j))
	}

	log.Println("STARTING REGISTRATION")
	registerBenchmarkAgentMultiple(b, stack.client, ids)

	pathfinders, cleanupClients := apiBenchPathfinders(b, stack, n, opts)
	defer cleanupClients()

	goal := [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}
	log.Printf("STARTING MOVEMENT conn=%s workers=x%d geo=q%d-p%d agents=%d", opts.clientMode, opts.workerScale, opts.geoQueryConnections, opts.geoPublishConnections, n)

	var metrics apiBenchMetrics
	var iterations int64
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		iterations++

		var wg sync.WaitGroup
		ctx, cancel := apiBenchContext(b)
		for idx, id := range ids {
			idx, id := idx, id
			wg.Go(func() {
				start := time.Now()
				comm, err := pathfinders[idx].AgentFindPath(ctx, goal, id, 0, 0)
				recordAPIBenchDuration(&metrics.findPathNs, &metrics.findPathCalls, start)
				if err != nil {
					metrics.unexpectedErrs.Add(1)
					b.Errorf("Agent %s find path: %s", id, err)
					return
				}

				start = time.Now()
				err = comm.Terminate(ctx)
				recordAPIBenchDuration(&metrics.terminateNs, &metrics.terminateCalls, start)
				if err != nil {
					metrics.unexpectedErrs.Add(1)
					b.Errorf("Agent %s termination: %s", id, err)
					return
				}

				start = time.Now()
				done, err := comm.BlockingWait()
				if err != nil {
					recordAPIBenchDuration(&metrics.blockingWaitNs, &metrics.blockingWaitCalls, start)
					metrics.unexpectedErrs.Add(1)
					b.Errorf("Agent %s blocking wait: %s", id, err)
					return
				}

				select {
				case <-done:
					recordAPIBenchDuration(&metrics.blockingWaitNs, &metrics.blockingWaitCalls, start)
				case <-ctx.Done():
					recordAPIBenchDuration(&metrics.blockingWaitNs, &metrics.blockingWaitCalls, start)
					metrics.unexpectedErrs.Add(1)
					b.Errorf("Agent %s did not finish after terminate: %s", id, ctx.Err())
				}
			})
		}
		wg.Wait()
		cancel()
	}
	b.StopTimer()
	metrics.report(b, n, iterations, opts)
}
