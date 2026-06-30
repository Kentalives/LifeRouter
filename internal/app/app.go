// Copyright 2026 Kentalives
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package app

import (
	"context"
	"time"

	"github.com/midtxwn/geotruth/pkg/natsclient"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/log"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/agent"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	"github.com/Kentalives/LifeRouter/pkg/subjects"

	"github.com/nats-io/nats.go"
	"github.com/pkg/errors"
)

// worldInit builds the global rasterized world and applies configured portals
// before any request handlers can start pathfinding.
func worldInit(cfg pubconfig.GridConfig) error {
	w, err := grid.NewWorld(cfg.FloorLayers, cfg.Rows, cfg.Cols, cfg.PxPerCell)
	if err != nil {
		return errors.Wrap(err, "creating world")
	}

	for _, p := range cfg.Portals {
		from := grid.GlobalCoords{Coords: grid.CoordsFromFloat64(p.From[0], p.From[1]), Layer: p.FromLayer}
		to := grid.GlobalCoords{Coords: grid.CoordsFromFloat64(p.To[0], p.To[1]), Layer: p.ToLayer}
		if !w.Contains(from) || !w.Contains(to) {
			return errors.Errorf("portal out of bounds: from=%v to=%v", from, to)
		}

		if p.Bidirectional {
			w.AddBidirectionalPortal(from, to, p.TraversalCost)
		} else {
			w.AddPortal(from, to, p.TraversalCost)
		}
	}

	return nil
}

// Run initializes global dependencies, creates the dispatcher, and subscribes
// all NATS subjects handled by the pathfinding service.
func Run(ctx context.Context, cfg *pubconfig.Config, dep *pubconfig.Dependencies) (*Dispatcher, error) {

	log.Print("Starting")

	config.Setup(cfg, dep)
	log.Setup(cfg.App)

	if err := worldInit(cfg.Grid); err != nil {
		return nil, err
	}

	//DISPATCHER INIT
	ctx2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	client, err := natsclient.New(ctx2, cfg.App.NatsServerUrl)
	cancel2()
	if err != nil {
		return nil, errors.Wrap(err, "creating nats conn")
	}

	ctx, cancel := context.WithCancel(ctx)
	disp, err := newDispatcher(ctx, cancel, client.Conn())
	if err != nil {
		return nil, errors.Wrap(err, "creating request dispatcher")
	}

	subs := []struct {
		subj    string
		handler nats.MsgHandler
	}{
		// These handlers can block long enough to occupy the NATS callback path,
		// so they use bounded worker wrappers and apply backpressure at the
		// configured limit.
		{subjects.PathfindingAgentFindPath, concurrentHandler(cfg.Pathfinding.Agent.FindPathHandlerWorkers, disp.handleAgentFindPath)},
		{subjects.PathfindingAgentNaivePathCost, disp.handleAgentNaivePathCost},
		{subjects.PathfindingAgentWatchPath, disp.handleAgentPathSub},

		{subjects.EmergencyStart, disp.handleEmergencyStart},
		{subjects.EmergencyStop, disp.handleEmergencyStop},
		{subjects.EmergencyFlowWatch, disp.handleEmergencyFlowSub},

		{subjects.AgentCommUpdateMovementSpeed, disp.handleUpdateMovementSpeed},
		{subjects.AgentCommBlockingWait, concurrentHandler(cfg.Pathfinding.Agent.BlockingWaitHandlerWorkers, disp.handleBlockingWait)},
		{subjects.AgentCommIsMoving, disp.handleIsMoving},
		{subjects.AgentCommExitError, disp.handleExitError},
		{subjects.AgentCommTerminate, disp.handleTerminate},
		{subjects.AgentCommStop, disp.handleStop},
		{subjects.AgentCommMoveNCells, disp.handleMoveNCells},
		{subjects.AgentCommMoveFMeters, concurrentHandler(cfg.Pathfinding.Agent.MoveFMetersHandlerWorkers, disp.handleMoveFMeters)},
	}
	for _, s := range subs {
		if _, err := disp.nc.Subscribe(s.subj, s.handler); err != nil {
			log.Errorf("-NatsSubscriptionErr- %s: %s", s.subj, err)
			continue
		}
		log.Printf("listening on %s", s.subj)
	}
	return disp, nil
}

// Shutdown stops active jobs, drains NATS, cancels the service context, and
// closes publisher resources. It is safe to call on a nil dispatcher.
func (disp *Dispatcher) Shutdown() {
	if disp == nil {
		return
	}

	log.Printf("service shutting down...")

	disp.stopActiveAgents()

	log.Printf("stopped agents")

	disp.stopCurrentEmergency()

	err := disp.nc.Drain()
	if err != nil {
		log.Errorf("-ShutdownErr- NatsConn: %s", err)
	}
	waitNATSDrainClosed(disp.nc, 10*time.Second)

	disp.cancel()

	closeDependencyResources()

	log.Printf("finished")
}

func waitNATSDrainClosed(nc *nats.Conn, timeout time.Duration) {
	if nc == nil {
		return
	}
	if nc.Status() == nats.CLOSED {
		return
	}
	select {
	case <-nc.StatusChanged(nats.CLOSED):
	case <-time.After(timeout):
		nc.Close()
	}
}

type closeableDependency interface {
	Close()
}

func closeDependencyResources() {
	if config.Dep == nil {
		return
	}
	if qu, ok := config.Dep.Qu.(closeableDependency); ok {
		qu.Close()
	}
	if pu, ok := config.Dep.Pu.(closeableDependency); ok {
		pu.Close()
	}
}

// stopCurrentEmergency joins the emergency loop and removes preference paths
// even when shutdown races with a client Stop request.
func (disp *Dispatcher) stopCurrentEmergency() {
	disp.emergency.mu.Lock()
	switch disp.emergency.state {
	case emergency_Stopped:
		disp.emergency.mu.Unlock()
		return
	case emergency_Cleanup:
		stopDone := disp.emergency.stopDone
		disp.emergency.mu.Unlock()
		waitEmergencyStopDone(stopDone)
		return
	case emergency_Running, emergency_Stopping:
		done := disp.emergency.done
		if disp.emergency.state == emergency_Running {
			close(disp.emergency.quit)
			disp.emergency.quit = nil
		}
		disp.emergency.state = emergency_Cleanup
		disp.emergency.mu.Unlock()

		if done != nil {
			<-done
		}

		disp.emergency.mu.Lock()
		disp.completeEmergencyStopLocked()
		log.Printf("stopped emergency")
		disp.emergency.mu.Unlock()
		return
	}
	disp.emergency.mu.Unlock()
}

// stopActiveAgents prevents new registrations, terminates current agents, and
// waits until every communicator has closed its completion channel.
func (r *Dispatcher) stopActiveAgents() {
	r.mu.Lock()
	if r.state == dispatcher_Stopped {
		r.mu.Unlock()
		return
	}
	r.state = dispatcher_ShuttingDown

	comms := make([]*agent.AgentCommunicator, 0, len(r.tab))
	for _, comm := range r.tab {
		comms = append(comms, comm)
	}
	r.mu.Unlock()

	for _, comm := range comms {
		comm.Terminate()
	}
	for _, comm := range comms {
		<-comm.BlockingWait()
	}

	r.mu.Lock()
	clear(r.tab)
	clear(r.completedAgentResults)
	r.state = dispatcher_Stopped
	r.mu.Unlock()
}
