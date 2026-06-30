package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	embeddedGeo "github.com/midtxwn/geotruth/embedded"
	"github.com/midtxwn/geotruth/pkg/domain"
	"github.com/midtxwn/geotruth/pkg/natsclient"
	"github.com/midtxwn/geotruth/pkg/natspublish"
	"github.com/midtxwn/geotruth/pkg/natsquery"

	"github.com/Kentalives/LifeRouter/embedded"
	"github.com/Kentalives/LifeRouter/internal/mock"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
	"github.com/Kentalives/LifeRouter/pkg/emergency"
	"github.com/Kentalives/LifeRouter/pkg/pathfinding"
	"github.com/google/uuid"
)

// These API tests exercise the public NATS wrappers against embedded geotruth
// and embedded pathfinding stacks. They cover single-floor and multi-floor
// emergency flow, agent routing, path subscriptions, movement commands, and
// multiple subscribers.

func multiFloorEmbeddedGeoDeps() embeddedGeo.Dependencies {
	return embeddedGeo.Dependencies{
		Resolver: embeddedGeo.NewFlatResolver(3),
	}
}

func multiFloorPathfindingConfig(natsURL string) *pubconfig.Config {
	cfg := embedded.DefaultConfig()
	cfg.App.NatsServerUrl = natsURL
	cfg.Grid.FloorLayers = []pubconfig.ConfigLayer{
		{Name: "0", ImgPath: "../../data/map.png"},
		{Name: "1", ImgPath: "../../data/floor1.png"},
		{Name: "2", ImgPath: "../../data/floor2.png"},
	}
	cfg.Grid.Portals = []pubconfig.ConfigPortal{
		{
			From: [2]float64{15.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M}, FromLayer: 0,
			To: [2]float64{15.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M}, ToLayer: 1,
			TraversalCost: 1, Bidirectional: true,
		},
		{
			From: [2]float64{15.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M}, FromLayer: 1,
			To: [2]float64{15.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M}, ToLayer: 2,
			TraversalCost: 1, Bidirectional: true,
		},
		{
			From: [2]float64{82.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M}, FromLayer: 2,
			To: [2]float64{82.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M}, ToLayer: 1,
			TraversalCost: 1, Bidirectional: true,
		},
	}
	return cfg
}

func multiFloorExternal(nodes []*mock.Node) (*mock.ExternalSystem, error) {
	return mock.NewExternalSystem([]string{"0", "1", "2"}, []float64{0, 4.3, 8.6}, nodes)
}

func waitForObjectRegion(ctx context.Context, t *testing.T, qu natsquery.Query, id, region string) {
	t.Helper()

	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for object %s in region %s", id, region)
		case <-tick.C:
			obj, err := qu.ObjectData(ctx, id)
			if err == nil && obj.Region != nil && *obj.Region == region {
				return
			}
		}
	}
}

func TestAPI_Emergency(t *testing.T) {
	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, embeddedGeo.DefaultDependencies)
	if err != nil {
		t.Fatal(err)
	}

	cfg := embedded.DefaultConfig()
	cfg.App.NatsServerUrl = serv.NATSURL()
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}

	//nc, _, err := embedded.RunDefault(t.Context(), serv.NATSConn().ConnectedUrl())
	//if err != nil {
	//	t.Fatal(err)
	//}

	//pu, err := natspublish.New(nc, natspublish.DefaultConfig())
	//if err != nil {
	//	t.Fatal(err)
	//}
	//id := "agent"
	//_, err = pu.RegisterObject(t.Context(), id, domain.ObjectDimensions{Width: 10, Height: 10})
	//if err != nil {
	//	t.Fatal(err)
	//}
	//_, err = pu.UpdateObjectPosition(t.Context(), id, 155, 205, 0, 0)
	//if err != nil {
	//	t.Fatal(err)
	//}

	uuid1, uuid2, uuid3 := uuid.New(), uuid.New(), uuid.New()
	weight1 := uint16(1)
	routeGraphs := []pubdomain.RouteGraph{
		{
			Floor: 0,
			Waypoints: []pubdomain.GraphWaypoint{
				{Id: &uuid1, X: 15.5 * pubdomain.CELL_SIZE_M, Y: 22.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid2, X: 72.5 * pubdomain.CELL_SIZE_M, Y: 22.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid3, X: 72.5 * pubdomain.CELL_SIZE_M, Y: 40.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
			},
			Edges: []pubdomain.GraphEdge{
				{FromWaypoint: &uuid1, ToWaypoint: &uuid2, Weight: &weight1},
				{FromWaypoint: &uuid2, ToWaypoint: &uuid3, Weight: &weight1},
			},
		},
	}

	e := emergency.New(client.Conn())
	goals := [][3]float64{{15.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}, {72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}}
	err = e.Start(t.Context(), goals, routeGraphs, 1)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Second)

	err = e.Stop(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

func TestAPI_Emergency_MultipleFloors(t *testing.T) {
	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, multiFloorEmbeddedGeoDeps())
	if err != nil {
		t.Fatal(err)
	}
	defer serv.Shutdown()

	cfg := multiFloorPathfindingConfig(serv.NATSURL())
	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}
	pu := natspublish.New(client.Conn())
	qu := natsquery.New(client.Conn())

	nodeID := "Node_multifloor"
	if _, err := pu.RegisterObject(t.Context(), nodeID, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M}); err != nil {
		t.Fatal(err)
	}
	if _, err := pu.UpdateObjectPosition(t.Context(), nodeID, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 4.3, 0); err != nil {
		t.Fatal(err)
	}
	waitForObjectRegion(t.Context(), t, qu, nodeID, "1")

	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	mockNode := &mock.Node{
		Element: mock.Element{
			IdName: nodeID,
			X:      15.5 * pubdomain.CELL_SIZE_M,
			Y:      20.5 * pubdomain.CELL_SIZE_M,
			Z:      4.3,
			Width:  1.0 * pubdomain.CELL_SIZE_M,
			Height: 1.0 * pubdomain.CELL_SIZE_M,
		},
		Dir: pubdomain.DIR_UNKNOWN,
	}
	ex, err := multiFloorExternal([]*mock.Node{mockNode})
	if err != nil {
		t.Fatal(err)
	}
	dep.Ex = ex

	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	weight1 := uint16(1)
	uuid1, uuid2, uuid3 := uuid.New(), uuid.New(), uuid.New()
	routeGraphs := []pubdomain.RouteGraph{
		{
			Floor: 0,
			Waypoints: []pubdomain.GraphWaypoint{
				{Id: &uuid1, X: 15.5 * pubdomain.CELL_SIZE_M, Y: 20.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid2, X: 15.5 * pubdomain.CELL_SIZE_M, Y: 20.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
			},
			Edges: []pubdomain.GraphEdge{{FromWaypoint: &uuid1, ToWaypoint: &uuid2, Weight: &weight1}},
		},
		{
			Floor: 2,
			Waypoints: []pubdomain.GraphWaypoint{
				{Id: &uuid2, X: 15.5 * pubdomain.CELL_SIZE_M, Y: 20.5 * pubdomain.CELL_SIZE_M, Z: 8.6, Floor: 2},
				{Id: &uuid3, X: 20.5 * pubdomain.CELL_SIZE_M, Y: 20.5 * pubdomain.CELL_SIZE_M, Z: 8.6, Floor: 2},
			},
			Edges: []pubdomain.GraphEdge{{FromWaypoint: &uuid2, ToWaypoint: &uuid3, Weight: &weight1}},
		},
	}

	e := emergency.New(client.Conn())
	goals := [][3]float64{{20.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 8.6}}
	if err := e.Start(t.Context(), goals, routeGraphs, 20); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = e.Stop(t.Context())
	})

	time.Sleep(2 * time.Second)
	if dir, err := ex.SignalingDirection(t.Context(), nodeID); err != nil {
		t.Fatal(err)
	} else if dir == pubdomain.DIR_UNKNOWN {
		t.Fatal("expected node direction to be updated during multi-floor emergency")
	}

	if err := e.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAPI_EmergencyFlowWatch(t *testing.T) {
	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, embeddedGeo.DefaultDependencies)
	if err != nil {
		t.Fatal(err)
	}

	cfg := embedded.DefaultConfig()
	cfg.App.NatsServerUrl = serv.NATSURL()
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}

	uuid1, uuid2, uuid3 := uuid.New(), uuid.New(), uuid.New()
	weight1 := uint16(1)
	routeGraphs := []pubdomain.RouteGraph{
		{
			Floor: 0,
			Waypoints: []pubdomain.GraphWaypoint{
				{Id: &uuid1, X: 15.5 * pubdomain.CELL_SIZE_M, Y: 22.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid2, X: 72.5 * pubdomain.CELL_SIZE_M, Y: 22.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid3, X: 72.5 * pubdomain.CELL_SIZE_M, Y: 40.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
			},
			Edges: []pubdomain.GraphEdge{
				{FromWaypoint: &uuid1, ToWaypoint: &uuid2, Weight: &weight1},
				{FromWaypoint: &uuid2, ToWaypoint: &uuid3, Weight: &weight1},
			},
		},
	}

	e := emergency.New(client.Conn())
	goals := [][3]float64{{15.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}, {72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}}
	err = e.Start(t.Context(), goals, routeGraphs, 1)
	if err != nil {
		t.Fatal(err)
	}

	ch, err := e.FlowSub(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	select {
	case data, ok := <-ch:
		if !ok {
			t.Fatal("Expected to receive the emergency flow before closing of the channel")
		}
		t.Logf("%#v", data)
	case <-time.After(1 * time.Second):
		t.Fatal("Expected to receive the emergency flow")
	}

	time.Sleep(2 * time.Second)

	err = e.Stop(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

func TestAPI_EmergencyFlowWatch_MultipleFloors(t *testing.T) {
	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, multiFloorEmbeddedGeoDeps())
	if err != nil {
		t.Fatal(err)
	}
	defer serv.Shutdown()

	cfg := multiFloorPathfindingConfig(serv.NATSURL())
	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}
	pu := natspublish.New(client.Conn())
	qu := natsquery.New(client.Conn())

	nodeID := "Node_multifloor"
	if _, err := pu.RegisterObject(t.Context(), nodeID, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M}); err != nil {
		t.Fatal(err)
	}
	if _, err := pu.UpdateObjectPosition(t.Context(), nodeID, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 4.3, 0); err != nil {
		t.Fatal(err)
	}
	waitForObjectRegion(t.Context(), t, qu, nodeID, "1")

	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	mockNode := &mock.Node{
		Element: mock.Element{
			IdName: nodeID,
			X:      15.5 * pubdomain.CELL_SIZE_M,
			Y:      20.5 * pubdomain.CELL_SIZE_M,
			Z:      4.3,
			Width:  1.0 * pubdomain.CELL_SIZE_M,
			Height: 1.0 * pubdomain.CELL_SIZE_M,
		},
		Dir: pubdomain.DIR_UNKNOWN,
	}
	ex, err := multiFloorExternal([]*mock.Node{mockNode})
	if err != nil {
		t.Fatal(err)
	}
	dep.Ex = ex

	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	weight1 := uint16(1)
	uuid1, uuid2, uuid3 := uuid.New(), uuid.New(), uuid.New()
	routeGraphs := []pubdomain.RouteGraph{
		{
			Floor: 0,
			Waypoints: []pubdomain.GraphWaypoint{
				{Id: &uuid1, X: 15.5 * pubdomain.CELL_SIZE_M, Y: 20.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
				{Id: &uuid2, X: 15.5 * pubdomain.CELL_SIZE_M, Y: 20.5 * pubdomain.CELL_SIZE_M, Z: 0, Floor: 0},
			},
			Edges: []pubdomain.GraphEdge{{FromWaypoint: &uuid1, ToWaypoint: &uuid2, Weight: &weight1}},
		},
		{
			Floor: 2,
			Waypoints: []pubdomain.GraphWaypoint{
				{Id: &uuid2, X: 15.5 * pubdomain.CELL_SIZE_M, Y: 20.5 * pubdomain.CELL_SIZE_M, Z: 8.6, Floor: 2},
				{Id: &uuid3, X: 20.5 * pubdomain.CELL_SIZE_M, Y: 20.5 * pubdomain.CELL_SIZE_M, Z: 8.6, Floor: 2},
			},
			Edges: []pubdomain.GraphEdge{{FromWaypoint: &uuid2, ToWaypoint: &uuid3, Weight: &weight1}},
		},
	}

	e := emergency.New(client.Conn())
	goals := [][3]float64{{20.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 8.6}}
	if err := e.Start(t.Context(), goals, routeGraphs, 20); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = e.Stop(t.Context())
	})

	ch, err := e.FlowSub(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	select {
	case data, ok := <-ch:
		if !ok {
			t.Fatal("Expected to receive the emergency flow before closing of the channel")
		}
		t.Logf("%#v", data)
	case <-time.After(1 * time.Second):
		t.Fatal("Expected to receive the emergency flow")
	}

	time.Sleep(2 * time.Second)
	if dir, err := ex.SignalingDirection(t.Context(), nodeID); err != nil {
		t.Fatal(err)
	} else if dir == pubdomain.DIR_UNKNOWN {
		t.Fatal("expected node direction to be updated during multi-floor emergency")
	}

	if err := e.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

}

func TestAPI_AgentFindPath(t *testing.T) {
	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, embeddedGeo.DefaultDependencies)
	if err != nil {
		t.Fatal(err)
	}

	cfg := embedded.DefaultConfig()
	cfg.App.NatsServerUrl = serv.NATSURL()
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}

	pu := natspublish.New(client.Conn())
	id := "Agent_agent"
	_, err = pu.RegisterObject(t.Context(), id, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pu.UpdateObjectPosition(t.Context(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	p := pathfinding.New(client.Conn())
	comm, err := p.AgentFindPath(t.Context(), [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}, id, 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		comm.Terminate(t.Context())
	})

	done, err := comm.BlockingWait()
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Agent took too long to reach the destination")
	}

	if err := comm.ExitError(t.Context()); err != nil {
		t.Fatalf("expected successful pathfinding to have no exit error, got %v", err)
	}

	moving, err := comm.IsMoving(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if moving {
		t.Fatal("completed agent should report not moving")
	}

	doneAgain, err := comm.BlockingWait()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-doneAgain:
	case <-time.After(time.Second):
		t.Fatal("BlockingWait after completion should return immediately")
	}
}

func TestAPI_AgentFindPath_MultipleFloors(t *testing.T) {
	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, multiFloorEmbeddedGeoDeps())
	if err != nil {
		t.Fatal(err)
	}
	defer serv.Shutdown()

	cfg := multiFloorPathfindingConfig(serv.NATSURL())
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := multiFloorExternal(nil)
	if err != nil {
		t.Fatal(err)
	}
	dep.Ex = ex

	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}
	pu := natspublish.New(client.Conn())
	qu := natsquery.New(client.Conn())

	id := "Agent_multifloor"
	if _, err := pu.RegisterObject(t.Context(), id, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M}); err != nil {
		t.Fatal(err)
	}
	if _, err := pu.UpdateObjectPosition(t.Context(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0); err != nil {
		t.Fatal(err)
	}

	p := pathfinding.New(client.Conn())
	comm, err := p.AgentFindPath(t.Context(), [3]float64{105.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 4.3}, id, 500, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		comm.Terminate(t.Context())
	})

	done, err := comm.BlockingWait()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Agent took too long to reach the multi-floor destination")
	}

	waitForObjectRegion(t.Context(), t, qu, id, "1")
	obj, err := qu.ObjectData(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Region == nil || *obj.Region != "1" {
		t.Fatalf("expected final region 2, got %#v", obj.Region)
	}
	if obj.Z < 4 {
		t.Fatalf("expected final z to be on floor 1, got %.2f", obj.Z)
	}
}

func TestAPI_AgentPathWatch(t *testing.T) {
	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, embeddedGeo.DefaultDependencies)
	if err != nil {
		t.Fatal(err)
	}

	cfg := embedded.DefaultConfig()
	cfg.App.NatsServerUrl = serv.NATSURL()
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}

	pu := natspublish.New(client.Conn())
	id := "Agent_agent"
	_, err = pu.RegisterObject(t.Context(), id, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pu.UpdateObjectPosition(t.Context(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	p := pathfinding.New(client.Conn())
	comm, err := p.AgentFindPath(t.Context(), [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}, id, 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		comm.Terminate(t.Context())
	})

	channel, err := p.AgentPathSub(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case data, ok := <-channel:
		if !ok {
			t.Fatal("Expected to receive the path of the agent before closing of the channel")
		}
		t.Logf("%#v", data)
	case <-time.After(1 * time.Second):
		t.Fatal("Expected to receive the agents path")
	}

	done, err := comm.BlockingWait()
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Agent took too long to reach the destination")
	}
}

func TestAPI_AgentPathWatch_MultipleFloors(t *testing.T) {
	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, multiFloorEmbeddedGeoDeps())
	if err != nil {
		t.Fatal(err)
	}
	defer serv.Shutdown()

	cfg := multiFloorPathfindingConfig(serv.NATSURL())
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := multiFloorExternal(nil)
	if err != nil {
		t.Fatal(err)
	}
	dep.Ex = ex

	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}
	pu := natspublish.New(client.Conn())

	id := "Agent_multifloor"
	if _, err := pu.RegisterObject(t.Context(), id, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M}); err != nil {
		t.Fatal(err)
	}
	if _, err := pu.UpdateObjectPosition(t.Context(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0); err != nil {
		t.Fatal(err)
	}

	p := pathfinding.New(client.Conn())
	comm, err := p.AgentFindPath(t.Context(), [3]float64{105.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 4.3}, id, 500, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		comm.Terminate(t.Context())
	})

	channel, err := p.AgentPathSub(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case data, ok := <-channel:
		if !ok {
			t.Fatal("Expected to receive the path of the agent before closing of the channel")
		}
		t.Logf("%#v", data)
	case <-time.After(1 * time.Second):
		t.Fatal("Expected to receive the agents path")
	}

	done, err := comm.BlockingWait()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Agent took too long to reach the multi-floor destination")
	}

}

func TestAPI_AgentNaivePathCost(t *testing.T) {
	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, embeddedGeo.DefaultDependencies)
	if err != nil {
		t.Fatal(err)
	}

	cfg := embedded.DefaultConfig()
	cfg.App.NatsServerUrl = serv.NATSURL()
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}

	pu := natspublish.New(client.Conn())
	id := "agent"
	_, err = pu.RegisterObject(t.Context(), id, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pu.UpdateObjectPosition(t.Context(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	p := pathfinding.New(client.Conn())

	cost, err := p.AgentNaivePathCost(t.Context(), [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("COST: %d\n", cost)

}

func TestAPI_AgentNaivePathCost_MultipleFloors(t *testing.T) {
	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, multiFloorEmbeddedGeoDeps())
	if err != nil {
		t.Fatal(err)
	}
	defer serv.Shutdown()

	cfg := multiFloorPathfindingConfig(serv.NATSURL())
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := multiFloorExternal(nil)
	if err != nil {
		t.Fatal(err)
	}
	dep.Ex = ex

	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}
	pu := natspublish.New(client.Conn())

	id := "Agent_multifloor_cost"
	if _, err := pu.RegisterObject(t.Context(), id, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M}); err != nil {
		t.Fatal(err)
	}
	if _, err := pu.UpdateObjectPosition(t.Context(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0); err != nil {
		t.Fatal(err)
	}

	p := pathfinding.New(client.Conn())
	cost, err := p.AgentNaivePathCost(t.Context(), [3]float64{20.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 8.6}, id)
	if err != nil {
		t.Fatal(err)
	}
	if cost <= 0 || cost >= pubdomain.COST_UNREACHABLE {
		t.Fatalf("expected reachable multi-floor path cost, got %d", cost)
	}
}

func TestAPI_AgentCommIsMoving(t *testing.T) {

	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, embeddedGeo.DefaultDependencies)
	if err != nil {
		t.Fatal(err)
	}

	cfg := embedded.DefaultConfig()
	cfg.App.NatsServerUrl = serv.NATSURL()
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}
	//nc, _, err := embedded.RunDefault(t.Context(), serv.NATSConn().ConnectedUrl())
	//if err != nil {
	//	t.Fatal(err)
	//}

	pu := natspublish.New(client.Conn())

	id := "agent"
	_, err = pu.RegisterObject(t.Context(), id, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pu.UpdateObjectPosition(t.Context(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	p := pathfinding.New(client.Conn())
	comm, err := p.AgentFindPath(t.Context(), [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}, id, 8, 0)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Second)
	moving, err := comm.IsMoving(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !moving {
		t.Fatal(fmt.Errorf("Expected to be moving 1s after launch"))
	}

	err = comm.Terminate(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		moving, err := comm.IsMoving(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !moving {
			break
		}

		select {
		case <-ctx.Done():
			t.Fatal("agent did not report stopped after Terminate")
		case <-time.After(10 * time.Millisecond):
		}
	}

	if err := comm.ExitError(t.Context()); !errors.Is(err, pubdomain.ErrAgentTerminated) {
		t.Fatalf("expected terminated exit error, got %v", err)
	}
}

func TestAPI_AgentCommMovement(t *testing.T) {

	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, embeddedGeo.DefaultDependencies)
	if err != nil {
		t.Fatal(err)
	}

	cfg := embedded.DefaultConfig()
	cfg.App.NatsServerUrl = serv.NATSURL()
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}
	//nc, _, err := embedded.RunDefault(t.Context(), serv.NATSConn().ConnectedUrl())
	//if err != nil {
	//	t.Fatal(err)
	//}

	pu := natspublish.New(client.Conn())
	id := "agent"
	_, err = pu.RegisterObject(t.Context(), id, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pu.UpdateObjectPosition(t.Context(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	p := pathfinding.New(client.Conn())
	//Starts stationary
	comm, err := p.AgentFindPath(t.Context(), [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}, id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Second)

	err = comm.UpdateMovementSpeed(t.Context(), 3)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second)

	err = comm.Stop(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second)

	done, err := comm.BlockingWait()
	if err != nil {
		t.Fatal(err)
	}

	err = comm.MoveNCells(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(3 * time.Second)

	select {
	case <-done:
		return
	default:
	}

	err = comm.Terminate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

func TestAPI_AgentCommMoveF(t *testing.T) {

	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, embeddedGeo.DefaultDependencies)
	if err != nil {
		t.Fatal(err)
	}

	cfg := embedded.DefaultConfig()
	cfg.App.NatsServerUrl = serv.NATSURL()
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}
	//nc, _, err := embedded.RunDefault(t.Context(), serv.NATSConn().ConnectedUrl())
	//if err != nil {
	//	t.Fatal(err)
	//}

	pu := natspublish.New(client.Conn())
	id := "agent"
	_, err = pu.RegisterObject(t.Context(), id, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pu.UpdateObjectPosition(t.Context(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	p := pathfinding.New(client.Conn())
	//Starts stationary
	comm, err := p.AgentFindPath(t.Context(), [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}, id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Millisecond)

	remaining, err := comm.MoveFMeters(t.Context(), 3.0*pubdomain.CELL_SIZE_M)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("MOVEMENT METERS REMAINDER: %.2f\n", remaining)

	remaining, err = comm.MoveFMeters(t.Context(), 3.0*pubdomain.CELL_SIZE_M+remaining)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("MOVEMENT METERS REMAINDER: %.2f\n", remaining)

	done, err := comm.BlockingWait()
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
		return
	default:
	}

	err = comm.Terminate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
}

func TestAPI_AgentCommMoveFOvershoot(t *testing.T) {

	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, embeddedGeo.DefaultDependencies)
	if err != nil {
		t.Fatal(err)
	}

	cfg := embedded.DefaultConfig()
	cfg.App.NatsServerUrl = serv.NATSURL()
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}
	//nc, _, err := embedded.RunDefault(t.Context(), serv.NATSConn().ConnectedUrl())
	//if err != nil {
	//	t.Fatal(err)
	//}

	pu := natspublish.New(client.Conn())
	id := "agent"
	_, err = pu.RegisterObject(t.Context(), id, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pu.UpdateObjectPosition(t.Context(), id, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	p := pathfinding.New(client.Conn())
	//Starts stationary
	comm, err := p.AgentFindPath(t.Context(), [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}, id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	_, err = comm.MoveFMeters(ctx, 100000*pubdomain.CELL_SIZE_M)
	cancel()
	if !errors.Is(err, pubdomain.ErrAgentExitedWithMetersLeft) {
		t.Fatalf("Expected to get an 'overshoot' error, got: \t%s\n", err)
	}

	if err := comm.ExitError(t.Context()); err != nil {
		t.Fatalf("expected overshoot after reaching goal to have no exit error, got %v", err)
	}

	_, err = comm.MoveFMeters(t.Context(), pubdomain.CELL_SIZE_M)
	if !errors.Is(err, pubdomain.ErrAgentExitedWithMetersLeft) {
		t.Fatalf("expected MoveFMeters after completion to report exited, got %v", err)
	}

}

func TestAPI_AgentPathSubscriptionAllowsMultiple(t *testing.T) {
	serv, err := embeddedGeo.RunLocalStack(t.Context(), embeddedGeo.DefaultConfig, embeddedGeo.DefaultDependencies)
	if err != nil {
		t.Fatal(err)
	}

	cfg := embedded.DefaultConfig()
	cfg.App.NatsServerUrl = serv.NATSURL()
	dep, err := embedded.DefaultDependencies(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	disp, err := embedded.Run(t.Context(), cfg, dep)
	if err != nil {
		t.Fatal(err)
	}
	defer disp.Shutdown()

	client, err := natsclient.New(t.Context(), cfg.App.NatsServerUrl)
	if err != nil {
		t.Fatal(err)
	}
	pu := natspublish.New(client.Conn())
	id1 := "Agent_agent1"
	id2 := "Agent_agent2"
	_, err = pu.RegisterObject(t.Context(), id1, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pu.UpdateObjectPosition(t.Context(), id1, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pu.RegisterObject(t.Context(), id2, domain.ObjectDimensions{Width: 1.0 * pubdomain.CELL_SIZE_M, Height: 1.0 * pubdomain.CELL_SIZE_M})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pu.UpdateObjectPosition(t.Context(), id2, 15.5*pubdomain.CELL_SIZE_M, 20.5*pubdomain.CELL_SIZE_M, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	p := pathfinding.New(client.Conn())

	comm1, err := p.AgentFindPath(t.Context(), [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}, id1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		comm1.Terminate(t.Context())
	})
	comm2, err := p.AgentFindPath(t.Context(), [3]float64{72.5 * pubdomain.CELL_SIZE_M, 20.5 * pubdomain.CELL_SIZE_M, 0}, id2, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		comm2.Terminate(t.Context())
	})

	chan1, err := p.AgentPathSub(t.Context(), id1)
	if err != nil {
		t.Fatal(err)
	}
	chan2, err := p.AgentPathSub(t.Context(), id2)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-chan1:
	case <-time.After(1 * time.Second):
		t.Fatalf("Expected to receive Agent: %s path", id1)
	}

	select {
	case <-chan2:
	case <-time.After(1 * time.Second):
		t.Fatalf("Expected to recieve Agent: %s path", id2)
	}
}
