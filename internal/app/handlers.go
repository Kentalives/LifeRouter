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
	"encoding/json"
	"fmt"

	"github.com/midtxwn/geotruth/pkg/messages"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/log"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/agent"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/emergency"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"

	"github.com/nats-io/nats.go"
)

func agentDebug() bool {
	return config.Cfg.Pathfinding.Agent.Debug
}

func emergencyDebug() bool {
	return config.Cfg.Pathfinding.Emergency.Debug
}

// AgentFindPathParams is the NATS request payload for starting agent movement.
// Goal is [x, y, z] in meters and AgentId must exist in geotruth.
type AgentFindPathParams struct {
	Goal                          [3]float64 `json:"goal"`
	AgentId                       string     `json:"agentid"`
	DefaultCellsPerSecondMovement float64    `json:"defaultcellspersecondmovement"`
	MoveNSteps                    uint       `json:"movensteps"`
}

func (disp *Dispatcher) handleAgentFindPath(msg *nats.Msg) {
	var req AgentFindPathParams
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	if !disp.isRunning() {
		_ = msg.Respond(messages.ErrResp(pubdomain.ErrDispatcherShuttingDown))
		return
	}

	gridGoal, err := grid.GlobalCoordsFromFloat64Context(disp.ctx, req.Goal[0], req.Goal[1], req.Goal[2])
	if err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	comm, err := agent.InternalAgentFindPathContext(disp.ctx, grid.NewVirtualWorld(), gridGoal, req.AgentId, req.DefaultCellsPerSecondMovement, req.MoveNSteps, agentDebug())
	if err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	id := req.AgentId
	// A new pathfinding request for the same agent replaces the previous
	// communicator. The completion goroutine keeps the terminal result available
	// briefly for follow-up BlockingWait and ExitError calls.
	prevComm, ok := disp.addCommIfRunning(id, comm)
	if !ok {
		comm.Terminate()
		<-comm.BlockingWait()
		_ = msg.Respond(messages.ErrResp(pubdomain.ErrDispatcherShuttingDown))
		return
	}
	if prevComm != nil {
		prevComm.Terminate()
	}

	done := comm.BlockingWait()
	go func() {
		<-done
		disp.completeComm(id, comm)
	}()

	msg.Respond(messages.OKDataResp(id))
}

// AgentNaivePathCostParams requests a one-shot cost calculation without movement.
type AgentNaivePathCostParams struct {
	Goal    [3]float64 `json:"goal"`
	AgentId string     `json:"agentid"`
}

func (disp *Dispatcher) handleAgentNaivePathCost(msg *nats.Msg) {
	var req AgentNaivePathCostParams
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	gridGoal, err := grid.GlobalCoordsFromFloat64Context(disp.ctx, req.Goal[0], req.Goal[1], req.Goal[2])
	if err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	cost, err := agent.InternalAgentNaivePathCostContext(disp.ctx, grid.NewVirtualWorld(), gridGoal, req.AgentId, agentDebug())
	if err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	msg.Respond(messages.OKDataResp(cost))
}

func (disp *Dispatcher) handleAgentPathSub(msg *nats.Msg) {
	var req IdentifierParams
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	comm, ok := disp.dispatch(req.Id)
	if !ok || !comm.IsMoving() {
		_ = msg.Respond(messages.ErrResp(fmt.Errorf("%w: ID - %s", pubdomain.ErrAgentCommNotFound, req.Id)))
		return
	}

	if msg.Reply != "" {
		// The handler returns immediately; this goroutine owns the reply stream
		// until the communicator closes its publisher or the pathfinding job ends.
		go func() {
			channel := make(chan map[string][]pubdomain.CellState, grid.GlobalWorld().Len())

			comm.SetPathPublisher(channel)
			if err := disp.nc.Publish(msg.Reply, messages.OKResp()); err != nil {
				log.Errorf("-AgentPathSub- %s", err)
				return
			}

			done := comm.BlockingWait()
			for {
				select {
				case path, ok := <-channel:
					if !ok {
						data, err := json.Marshal(AgentPathSubResponse{Done: true})
						if err != nil {
							log.Errorf("-AgentPathSub- %s", err)
							return
						}
						if err := disp.nc.Publish(msg.Reply, data); err != nil {
							log.Errorf("-AgentPathSub- publish done: %s", err)
						}
						return
					} else {
						payloads, err := MarshalAgentPathSubSnapshot(path)
						if err != nil {
							log.Errorf("-AgentPathSub- %s", err)
							continue
						}
						for _, data := range payloads {
							if err := disp.nc.Publish(msg.Reply, data); err != nil {
								log.Errorf("-AgentPathSub- publish snapshot: %s", err)
								break
							}
						}
					}
				case <-done:
					data, err := json.Marshal(AgentPathSubResponse{Done: true})
					if err != nil {
						log.Errorf("-AgentPathSub- %s", err)
						return
					}
					if err := disp.nc.Publish(msg.Reply, data); err != nil {
						log.Errorf("-AgentPathSub- publish done: %s", err)
					}
					return
				}
			}
		}()
	}
}

// EmergencyPathParams starts the emergency flow-field loop. Goals are [x, y, z]
// in meters and PreferenceGraph biases traversal costs before planning.
type EmergencyPathParams struct {
	Goals                [][3]float64           `json:"goals"`
	PreferenceGraph      []pubdomain.RouteGraph `json:"preferencegraph"`
	Updatetickspersecond float64                `json:"updatetickspersecond"`
}

func (disp *Dispatcher) handleEmergencyStart(msg *nats.Msg) {
	var req EmergencyPathParams
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	disp.emergency.mu.Lock()
	defer disp.emergency.mu.Unlock()

	if disp.emergency.state != emergency_Stopped {
		_ = msg.Respond(messages.ErrResp(pubdomain.ErrEmergencyStillRunning))
		return
	}

	world := grid.GlobalWorld()

	//Set preference paths
	emergency.ApplyPreferencePaths(req.PreferenceGraph, world)

	//Transform goals into Coords
	var goals []grid.GlobalCoords
	for _, gFloat := range req.Goals {
		gCoord, err := grid.GlobalCoordsFromFloat64(gFloat[0], gFloat[1], gFloat[2])
		if err != nil {
			emergency.RemovePreferencePaths(world, grid.EMPTY_SPACE_COST)
			_ = msg.Respond(messages.ErrResp(err))
			return
		}
		goals = append(goals, gCoord)
	}

	quit := make(chan struct{})

	done, err := emergency.InternalEmergencyStart(world, disp.nodes, goals, req.Updatetickspersecond, quit, emergencyDebug())
	if err != nil {
		emergency.RemovePreferencePaths(world, grid.EMPTY_SPACE_COST)
		_ = msg.Respond(messages.ErrResp(fmt.Errorf("Emergency init error: %w", err)))
		return
	}

	disp.emergency.quit = quit
	disp.emergency.done = done
	disp.emergency.stopDone = make(chan struct{})
	disp.emergency.state = emergency_Running

	_ = msg.Respond(messages.OKResp())
}

func (disp *Dispatcher) handleEmergencyStop(msg *nats.Msg) {

	disp.emergency.mu.Lock()

	if disp.emergency.state == emergency_Stopped {
		_ = msg.Respond(messages.OKResp())
		disp.emergency.mu.Unlock()
		return
	}
	if disp.emergency.state == emergency_Stopping || disp.emergency.state == emergency_Cleanup {
		stopDone := disp.emergency.stopDone
		disp.emergency.mu.Unlock()
		waitEmergencyStopDone(stopDone)
		_ = msg.Respond(messages.OKResp())
		return
	}

	disp.emergency.state = emergency_Stopping

	close(disp.emergency.quit)
	disp.emergency.quit = nil

	done := disp.emergency.done
	stopDone := disp.emergency.stopDone
	disp.emergency.mu.Unlock()

	<-done

	disp.emergency.mu.Lock()
	defer disp.emergency.mu.Unlock()
	if disp.emergency.state != emergency_Stopping {
		disp.emergency.mu.Unlock()
		waitEmergencyStopDone(stopDone)
		disp.emergency.mu.Lock()
		_ = msg.Respond(messages.OKResp())
		return
	}

	disp.completeEmergencyStopLocked()

	_ = msg.Respond(messages.OKResp())
}

func waitEmergencyStopDone(stopDone <-chan struct{}) {
	if stopDone == nil {
		return
	}
	<-stopDone
}

func (disp *Dispatcher) completeEmergencyStopLocked() {
	disp.emergency.done = nil
	disp.emergency.quit = nil
	emergency.RemovePreferencePaths(grid.GlobalWorld(), grid.EMPTY_SPACE_COST)
	disp.emergency.state = emergency_Stopped
	if disp.emergency.stopDone != nil {
		close(disp.emergency.stopDone)
		disp.emergency.stopDone = nil
	}
}

func (disp *Dispatcher) handleEmergencyFlowSub(msg *nats.Msg) {
	if msg.Reply != "" {
		// Flow subscriptions stream from the process-wide emergency publisher, so
		// the goroutine owns the reply subject until the publisher closes it.
		go func() {
			channel := make(chan map[string][]pubdomain.Direction, grid.GlobalWorld().Len())

			emergency.SetFlowPublisher(channel)

			for {
				flow, ok := <-channel
				if !ok {
					data, err := json.Marshal(EmergencySubResponse{Done: true})
					if err != nil {
						log.Errorf("-EmergencyFlowSub- %s", err)
						return
					}
					disp.nc.Publish(msg.Reply, data)
					return
				} else {
					payloads, err := MarshalEmergencyFlowSnapshot(flow)
					if err != nil {
						log.Errorf("-EmergencyFlowSub- %s", err)
						continue
					}
					for _, data := range payloads {
						if err := disp.nc.Publish(msg.Reply, data); err != nil {
							log.Errorf("-EmergencyFlowSub- publish snapshot: %s", err)
							break
						}
					}
				}
			}
		}()
	}
}

// FloatParams is shared by movement speed and meter-based movement commands.
type FloatParams struct {
	Id    string  `json:"id"`
	Float float64 `json:"float"`
}

func (disp *Dispatcher) handleUpdateMovementSpeed(msg *nats.Msg) {
	var req FloatParams
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	comm, ok := disp.dispatch(req.Id)
	if !ok {
		_ = msg.Respond(messages.ErrResp(fmt.Errorf("%w: ID - %s", pubdomain.ErrAgentCommNotFound, req.Id)))
		return
	}

	if ok := comm.UpdateMovementSpeed(req.Float); !ok {
		_ = msg.Respond(messages.ErrResp(fmt.Errorf("agent comm moveNCells got canceled")))
		return
	}

	_ = msg.Respond(messages.OKResp())
}

// IdentifierParams targets a running or recently completed agent communicator.
type IdentifierParams struct {
	Id string `json:"id"`
}

// handleBlockingWait may wait until the agent exits. Run subscribes it through a
// bounded worker wrapper so many outstanding waits do not create unlimited NATS
// callback goroutines.
func (disp *Dispatcher) handleBlockingWait(msg *nats.Msg) {
	var req IdentifierParams
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	comm, ok := disp.dispatch(req.Id)
	if !ok {
		if _, ok := disp.completedExitError(req.Id); ok {
			if msg.Reply != "" {
				disp.nc.Publish(msg.Reply, nil)
			}
			return
		}
		_ = msg.Respond(messages.ErrResp(fmt.Errorf("%w: ID - %s", pubdomain.ErrAgentCommNotFound, req.Id)))
		return
	}

	if msg.Reply != "" {
		<-comm.BlockingWait()
		disp.nc.Publish(msg.Reply, nil)
	}
}

func (disp *Dispatcher) handleIsMoving(msg *nats.Msg) {
	var req IdentifierParams
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	comm, ok := disp.dispatch(req.Id)
	if !ok {
		if _, ok := disp.completedExitError(req.Id); ok {
			_ = msg.Respond(messages.OKDataResp(false))
			return
		}
		_ = msg.Respond(messages.ErrResp(fmt.Errorf("%w: ID - %s", pubdomain.ErrAgentCommNotFound, req.Id)))
		return
	}

	boolVal := comm.IsMoving()

	_ = msg.Respond(messages.OKDataResp(boolVal)) //[]byte(fmt.Sprintf("%v", boolVal))))
}

func (disp *Dispatcher) handleExitError(msg *nats.Msg) {
	var req IdentifierParams
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	comm, ok := disp.dispatch(req.Id)
	if ok {
		if comm.IsMoving() {
			_ = msg.Respond(messages.ErrResp(pubdomain.ErrAgentStillRunning))
			return
		}
		if err := comm.ExitError(); err != nil {
			_ = msg.Respond(messages.ErrResp(err))
			return
		}
		_ = msg.Respond(messages.OKResp())
		return
	}

	if err, ok := disp.completedExitError(req.Id); ok {
		if err != nil {
			_ = msg.Respond(messages.ErrResp(err))
			return
		}
		_ = msg.Respond(messages.OKResp())
		return
	}

	_ = msg.Respond(messages.ErrResp(fmt.Errorf("%w: ID - %s", pubdomain.ErrAgentCommNotFound, req.Id)))
}

func (disp *Dispatcher) handleTerminate(msg *nats.Msg) {
	var req IdentifierParams
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	comm, ok := disp.dispatch(req.Id)
	if !ok {
		_ = msg.Respond(messages.ErrResp(fmt.Errorf("%w: ID - %s", pubdomain.ErrAgentCommNotFound, req.Id)))
		return
	}

	comm.Terminate()

	_ = msg.Respond(messages.OKResp())
}

func (disp *Dispatcher) handleStop(msg *nats.Msg) {
	var req IdentifierParams
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	comm, ok := disp.dispatch(req.Id)
	if !ok {
		_ = msg.Respond(messages.ErrResp(fmt.Errorf("%w: ID - %s", pubdomain.ErrAgentCommNotFound, req.Id)))
		return
	}

	if ok := comm.Stop(); !ok {
		_ = msg.Respond(messages.ErrResp(fmt.Errorf("agent comm stop got canceled")))
		return
	}

	_ = msg.Respond(messages.OKResp())
}

// MoveNCellsParams asks an agent to advance a bounded number of grid cells.
type MoveNCellsParams struct {
	Id string `json:"id"`
	N  uint   `json:"n"`
}

func (disp *Dispatcher) handleMoveNCells(msg *nats.Msg) {
	var req MoveNCellsParams
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	comm, ok := disp.dispatch(req.Id)
	if !ok {
		_ = msg.Respond(messages.ErrResp(fmt.Errorf("%w: ID - %s", pubdomain.ErrAgentCommNotFound, req.Id)))
		return
	}

	if ok := comm.MoveNCells(req.N); !ok {
		_ = msg.Respond(messages.ErrResp(fmt.Errorf("agent comm moveNCells got canceled")))
		return
	}

	_ = msg.Respond(messages.OKResp())
}

// handleMoveFMeters blocks until the requested distance is consumed or the
// agent exits, so Run wraps it with the configured NATS worker limit.
func (disp *Dispatcher) handleMoveFMeters(msg *nats.Msg) {
	var req FloatParams
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	comm, ok := disp.dispatch(req.Id)
	if !ok {
		if _, ok := disp.completedExitError(req.Id); ok {
			_ = msg.Respond(messages.ErrResp(pubdomain.ErrAgentExitedWithMetersLeft))
			return
		}
		_ = msg.Respond(messages.ErrResp(fmt.Errorf("%w: ID - %s", pubdomain.ErrAgentCommNotFound, req.Id)))
		return
	}

	remains, err := comm.MoveFMeters(req.Float)
	if err != nil {
		_ = msg.Respond(messages.ErrResp(err))
		return
	}

	_ = msg.Respond(messages.OKDataResp(remains))
}
