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
	"sync"
	"time"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/domain"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/agent"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/emergency"

	"github.com/nats-io/nats.go"
)

// Dispatcher owns the active NATS connection, running agent communicators, and
// emergency lifecycle state for one service instance.
type Dispatcher struct {
	ctx    context.Context
	cancel context.CancelFunc

	nc  *nats.Conn
	tab map[string]*agent.AgentCommunicator
	mu  sync.RWMutex

	state dispatcherState

	emergency emergencyManager

	nodes []domain.INode //emergency.Node

	completedAgentResults map[string]agentExitResult
	resultTTL             time.Duration
}

// dispatcherState prevents new agent registration while shutdown is draining jobs.
type dispatcherState int

const (
	dispatcher_Running dispatcherState = iota
	dispatcher_ShuttingDown
	dispatcher_Stopped
)

// emergencyState separates client stop, shutdown cleanup, and fully stopped cases.
type emergencyState int

const (
	emergency_Stopped = iota
	emergency_Running
	emergency_Stopping
	emergency_Cleanup
)

// emergencyManager holds the channels used to stop and join the current emergency run.
// done belongs to the emergency algorithm goroutine; stopDone is closed after
// dispatcher-owned cleanup finishes and the lifecycle reaches emergency_Stopped.
type emergencyManager struct {
	quit     chan<- struct{}
	done     <-chan struct{}
	stopDone chan struct{}
	state    emergencyState
	mu       sync.Mutex
}

// agentExitResult keeps a completed agent's terminal error briefly so clients
// can query ExitError or BlockingWait after the communicator leaves the active table.
type agentExitResult struct {
	err       error
	expiresAt time.Time
}

// newDispatcher captures the initial emergency signaling nodes and prepares the
// active-agent tables. Light nodes are resolved once at startup.
func newDispatcher(ctx context.Context, cancel context.CancelFunc, nc *nats.Conn) (*Dispatcher, error) {

	disp := &Dispatcher{
		tab:                   make(map[string]*agent.AgentCommunicator, 100),
		completedAgentResults: make(map[string]agentExitResult, 100),
		resultTTL:             10 * time.Second,
		nc:                    nc,
		ctx:                   ctx,
		cancel:                cancel,
	}

	//NOTE: Populate "nodes"
	ctx2, cancel := context.WithTimeout(disp.ctx, 5000*time.Millisecond) //TODO: Connect to watch to add or remove light nodes
	regx, err := config.Dep.Ex.LightNodeRegex(ctx2)
	if err != nil {
		cancel()
		return nil, err
	}
	allNodes, err := config.Dep.Qu.AllObjectsOriented(ctx2, &regx)
	cancel()
	if err != nil {
		return nil, err
	}

	for name, nodes := range allNodes.Regions {
		layer, err := grid.LayerFromName(name)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			disp.nodes = append(disp.nodes, emergency.NewNode(n.ID, n.Position.X, n.Position.Y, n.Position.Z, layer))
		}
	}

	return disp, nil
}

func (r *Dispatcher) removeComm(id string, expected *agent.AgentCommunicator) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if expected != nil && r.tab[id] != expected {
		return
	}
	delete(r.tab, id)
}

// completeComm removes the active communicator and retains its terminal result
// for resultTTL, unless the same agent was already replaced by a newer run.
func (r *Dispatcher) completeComm(id string, comm *agent.AgentCommunicator) {
	expiresAt := time.Now().Add(r.resultTTL)

	r.mu.Lock()
	if r.tab[id] != comm {
		r.mu.Unlock()
		return
	}
	delete(r.tab, id)
	if r.completedAgentResults == nil {
		r.completedAgentResults = make(map[string]agentExitResult, 100)
	}
	r.completedAgentResults[id] = agentExitResult{err: comm.ExitError(), expiresAt: expiresAt}
	r.mu.Unlock()

	time.AfterFunc(r.resultTTL, func() {
		r.mu.Lock()
		defer r.mu.Unlock()

		result, ok := r.completedAgentResults[id]
		if ok && result.expiresAt.Equal(expiresAt) {
			delete(r.completedAgentResults, id)
		}
	})
}

func (r *Dispatcher) addComm(id string, comm *agent.AgentCommunicator) *agent.AgentCommunicator {
	r.mu.Lock()
	defer r.mu.Unlock()

	prev := r.tab[id]
	r.tab[id] = comm
	delete(r.completedAgentResults, id)
	return prev
}

// addCommIfRunning registers a communicator only while the dispatcher accepts
// new work; it returns the previous same-agent run so the caller can terminate it.
func (r *Dispatcher) addCommIfRunning(id string, comm *agent.AgentCommunicator) (*agent.AgentCommunicator, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state != dispatcher_Running {
		return nil, false
	}

	prev := r.tab[id]
	r.tab[id] = comm
	delete(r.completedAgentResults, id)
	return prev, true
}

func (r *Dispatcher) isRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.state == dispatcher_Running
}

func (r *Dispatcher) dispatch(id string) (*agent.AgentCommunicator, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.tab[id]
	return a, ok
}

func (r *Dispatcher) completedExitError(id string) (error, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result, ok := r.completedAgentResults[id]
	if !ok {
		return nil, false
	}
	return result.err, true
}
