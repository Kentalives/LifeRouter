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

package agent

import (
	"context"
	"sync"
	"time"

	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/log"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/core"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

// AgentCommunicator is the in-process control channel for one pathfinding run.
// The pathfinding goroutine owns destroy; callers interact through the public methods.
type AgentCommunicator struct {
	id                     string
	updateCellsPerSecondCh chan float64
	endedCh                chan struct{}
	errorCh                chan error
	nStepsCh               chan uint
	stepsLeft              uint
	fMetersReqCh           chan float64
	fMetersRespCh          chan float64
	metersLeft             float64
	moveCellsPerSecond     float64
	nextTickMovementTime   time.Time
	movementMu             sync.Mutex
	readVarMu              sync.RWMutex
	exitErr                error
	exitErrMu              sync.RWMutex

	pathPublisher    chan<- map[string][]pubdomain.CellState
	publishMu        sync.RWMutex
	publishRequested bool

	ctx    context.Context
	cancel context.CancelFunc
}

// NewAgentCommunicator initializes movement channels and optional initial
// step-limited movement for a new pathfinding run.
func NewAgentCommunicator(id string, moveNSteps uint, defaultCellsPerSecondMovementSpeed float64) *AgentCommunicator {
	ctx, cancel := context.WithCancel(context.Background())

	comm := &AgentCommunicator{
		id:                     id,
		endedCh:                make(chan struct{}),
		updateCellsPerSecondCh: make(chan float64),
		errorCh:                make(chan error),
		nStepsCh:               make(chan uint, 1),
		fMetersReqCh:           make(chan float64),
		fMetersRespCh:          make(chan float64),
		moveCellsPerSecond:     defaultCellsPerSecondMovementSpeed,

		ctx:    ctx,
		cancel: cancel,
	}
	if moveNSteps > 0 {
		comm._startStepsMovement(moveNSteps)
	}
	if defaultCellsPerSecondMovementSpeed > 0 {
		comm.setNextTickMovementTime()
	}
	return comm
}

// UpdateMovementSpeed changes continuous movement speed in cells per second.
func (c *AgentCommunicator) UpdateMovementSpeed(newTilesPerSecond float64) bool {
	select {
	case c.updateCellsPerSecondCh <- newTilesPerSecond:
		return true
	case <-c.ctx.Done():
		return false
	}
}

// BlockingWait exposes the completion signal for the pathfinding goroutine.
func (c *AgentCommunicator) BlockingWait() <-chan struct{} {
	return c.endedCh
}

// IsMoving reports whether the pathfinding run is still active.
//
// It does not report whether automatic movement is currently paused.
func (c *AgentCommunicator) IsMoving() bool {
	select {
	case <-c.endedCh:
		return false
	default:
		return true
	}
}

// ErrorCh returns asynchronous pathfinding errors.
func (c *AgentCommunicator) ErrorCh() <-chan error {
	return c.errorCh
}

// ExitError returns the terminal error stored for the run.
func (c *AgentCommunicator) ExitError() error {
	c.exitErrMu.RLock()
	defer c.exitErrMu.RUnlock()

	return c.exitErr
}

// Terminate cancels the pathfinding run and records ErrAgentTerminated.
func (c *AgentCommunicator) Terminate() {
	if c.IsMoving() {
		c.setExitError(pubdomain.ErrAgentTerminated)
	}
	c.cancel()
}

// Stop pauses automatic movement by setting movement speed to zero.
func (c *AgentCommunicator) Stop() bool {

	return c.UpdateMovementSpeed(0)
}

// MoveNCells requests movement by up to n grid cells.
func (c *AgentCommunicator) MoveNCells(n uint) bool {
	if n == 0 {
		return true
	}
	select {
	case <-c.ctx.Done():
		return false
	default:
	}
	select {
	case c.nStepsCh <- n:
		return true
	case <-c.ctx.Done():
		return false
	}
}

// MoveFMeters requests movement by up to f real-world meters and waits for the result.
func (c *AgentCommunicator) MoveFMeters(f float64) (remaining float64, err error) {
	select {
	case c.fMetersReqCh <- f:
	case <-c.ctx.Done():
		return f, pubdomain.ErrAgentExitedWithMetersLeft
	}

	remains, ok := <-c.fMetersRespCh
	if !ok {
		return 0, pubdomain.ErrAgentExitedWithMetersLeft
	}
	return remains, nil
}

// SetPathPublisher installs one path snapshot receiver. Replacing it closes the
// previous channel so only one live publisher target owns future snapshots.
// SetPathPublisher attaches a channel for path visualization snapshots.
func (c *AgentCommunicator) SetPathPublisher(channel chan<- map[string][]pubdomain.CellState) {
	c.publishMu.Lock()
	defer c.publishMu.Unlock()
	if c.pathPublisher != nil && c.pathPublisher != channel {
		close(c.pathPublisher)
	}

	c.pathPublisher = channel
	c.publishRequested = true
}

///////////////////////Private

// destroy is called once by the pathfinding goroutine after it has stopped
// reading command channels. Command channels stay open so blocked senders exit via context.
func (c *AgentCommunicator) destroy() {
	c.cancel()
	close(c.endedCh)
	close(c.fMetersRespCh)
}

func (c *AgentCommunicator) setExitError(err error) {
	if err == nil {
		return
	}

	c.exitErrMu.Lock()
	defer c.exitErrMu.Unlock()

	if c.exitErr == nil {
		c.exitErr = err
	}
}

func (c *AgentCommunicator) sendError(err error) {
	log.Errorf("-Agent: %s- %s", c.id, err)
	select {
	case c.errorCh <- err:
	case <-c.ctx.Done():
	case <-time.After(200 * time.Millisecond): //Timeout
	}
}

// Visualization helpers
func (d *agentDStarLite) deriveCellState(u grid.GlobalCoords, g *grid.Grid) pubdomain.CellState {
	// 1. Obstacle — grid cost is infinite/blocked
	if grid.IsBlocked(g.GetValue(u.Coords)) {
		return pubdomain.STATE_Obstacle
	}

	uIdx := u.ToIdx(d.World)

	gVal, rhsVal := d.getGRhs(uIdx)

	// 2. Unvisited — neither g nor rhs have been touched
	if grid.IsUnreachable(gVal) && grid.IsUnreachable(rhsVal) {
		return pubdomain.STATE_Unvisited
	}

	// 3. Queued — rhs has been updated but g hasn't caught up yet,
	//    AND the vertex is currently in the priority queue
	if gVal != rhsVal && d.Queue.Find(uIdx) != -1 {
		return pubdomain.STATE_Queued
	}

	// 4. Inconsistent — g != rhs but NOT in queue
	//    (disturbed by a cost change, waiting to be re-queued)
	if gVal != rhsVal {
		return pubdomain.STATE_Inconsistent
	}

	// 5. Consistent but not on path — was expanded and settled -> Overwritten if on path
	return pubdomain.STATE_Expanded
}
func (d *agentDStarLite) extractPath() map[grid.GlobalCoords]bool {
	onPath := make(map[grid.GlobalCoords]bool)
	current := d.start

	goalCoords := d.Goal.ToGlobalCoords(d.World)

	for current != goalCoords {
		onPath[current] = true

		next, cost := d.minCostSucc(current, current.ToIdx(d.World))
		if grid.IsUnreachable(cost) {
			break // no path
		}
		current = next
	}
	onPath[goalCoords] = true

	return onPath
}
func (d *agentDStarLite) snapshotStates() map[string][]pubdomain.CellState {
	onPath := d.extractPath()
	visualWorld := make(map[string][]pubdomain.CellState, d.World.Len())
	for l := range d.World.Len() {
		layerName, err := grid.IdxToLayerName(l)
		if err != nil {
			log.Errorf("-Visualization- %s", err)
			continue
		}

		g := d.World.Floor(l)

		visualGrid := make([]pubdomain.CellState, 0, g.Cols*g.Rows)
		for i := range g.Rows {
			for j := range g.Cols {
				c := grid.GlobalCoords{Coords: grid.Coords{X: j, Y: i}, Layer: l}

				state := d.deriveCellState(c, g)
				if onPath[c] {
					state = pubdomain.STATE_OnPath // override Expanded if on path
				}
				visualGrid = append(visualGrid, state)
			}
		}

		visualWorld[layerName] = visualGrid
	}

	return visualWorld
}

// /////
func (c *AgentCommunicator) publishPathIfRequested(d *agentDStarLite) {
	c.publishMu.RLock()
	wantPublish := c.publishRequested
	c.publishMu.RUnlock()

	if !wantPublish {
		return
	}

	c.publishPath(d)
}

func (c *AgentCommunicator) publishPath(d *agentDStarLite) {
	c.publishMu.Lock()
	defer c.publishMu.Unlock()

	c.publishRequested = false

	if c.pathPublisher == nil {
		return
	}

	pathData := d.snapshotStates()

	select {
	case c.pathPublisher <- pathData:
	case <-c.ctx.Done():
		close(c.pathPublisher)
		c.pathPublisher = nil
	case <-time.After(200 * time.Millisecond): //Timeout
	}
}

// waitUntilStep arbitrates continuous speed, explicit cell movement, explicit
// meter movement, and cancellation before the pathfinder advances one cell.
func (c *AgentCommunicator) waitUntilStep() (finishExecution bool) {

	//Told to move F and did not finish
	if c.getMetersLeft() > 0 {
		return false
	}

	//Told to move N and did not finish
	if c.getStepsLeft() > 0 {
		c._reduceStepsMovement()
		return false
	}

	//No default speed (stop until notified to move)
	if c.getMoveCellsPerSecond() == 0 {
	forLoop:
		for {
			select {
			case moveF := <-c.fMetersReqCh:
				c._startMetersMovement(moveF)
				return false
			case moveN := <-c.nStepsCh:
				c._startStepsMovement(moveN)
				return false

			case <-c.ctx.Done():
				return true

			case cellsPerSecond := <-c.updateCellsPerSecondCh:
				if cellsPerSecond == 0 { //Avoid exiting when told Stop
					continue forLoop
				}
				c.setMoveCellsPerSecond(cellsPerSecond)

				c.setNextTickMovementTime()
				break forLoop
			}
		}
	}

	//Wait until next time to move according to current movement speed or notified to move N
	now := time.Now()
	nextTick := c.getNextTickMovementTime()
	if !now.Before(nextTick) {
		c.setNextTickMovementTime()
		return false
	}

	timeToWait := nextTick.Sub(now)
	select {
	case <-time.After(timeToWait):
		select {
		case cellsPerSecond := <-c.updateCellsPerSecondCh:
			c.setMoveCellsPerSecond(cellsPerSecond)
		default:
		}
		c.setNextTickMovementTime()

		return false
	case moveF := <-c.fMetersReqCh:
		c._startMetersMovement(moveF)
		return false
	case moveN := <-c.nStepsCh:
		c._startStepsMovement(moveN)
		return false

	case <-c.ctx.Done():
		return true

	}
}

func (c *AgentCommunicator) _startStepsMovement(n uint) {
	c.movementMu.Lock()

	c.setStepsLeft(n)
	c._reduceStepsMovement()
}
func (c *AgentCommunicator) _reduceStepsMovement() {
	stepsLeft := c.getStepsLeft()
	c.setStepsLeft(stepsLeft - 1)

	if stepsLeft == 1 {
		c.movementMu.Unlock()
		c.setNextTickMovementTime()
	}
}

func (c *AgentCommunicator) _startMetersMovement(f float64) {
	c.movementMu.Lock()

	c.setMetersLeft(f)
}

// tryReduceMetersMovement converts the next step cost to meters and reports
// whether the pathfinder may advance through that step.
func (c *AgentCommunicator) tryReduceMetersMovement(stepCost grid.Cost) bool {
	metersLeft := c.getMetersLeft()
	if metersLeft == 0 {
		return true
	}

	//NOTE: Clamp stepCost to prevent priority paths from making an agent go further with the same meters
	if stepCost < grid.EMPTY_SPACE_COST {
		stepCost = grid.EMPTY_SPACE_COST
	}
	stepMeters := float64(stepCost) / float64(grid.EMPTY_SPACE_COST) * grid.CellSizeM() //NOTE: Making EMPTY_SPACE_COST the unit value for movement cost

	if stepMeters <= metersLeft {
		remaining := metersLeft - stepMeters
		c.setMetersLeft(remaining)

		if remaining <= grid.CellSizeM()*0.001 {

			c.setMetersLeft(0)
			c.fMetersRespCh <- 0
			c.movementMu.Unlock()
			c.setNextTickMovementTime()
			return true
		}

		return true

	} else {
		c.fMetersRespCh <- metersLeft

		c.setMetersLeft(0)
		c.movementMu.Unlock()
		c.setNextTickMovementTime()
		return false
	}
}

////////

func (c *AgentCommunicator) setStepsLeft(val uint) {
	c.readVarMu.Lock()
	defer c.readVarMu.Unlock()

	c.stepsLeft = val
}

func (c *AgentCommunicator) setMetersLeft(val float64) {
	c.readVarMu.Lock()
	defer c.readVarMu.Unlock()

	c.metersLeft = val
}

func (c *AgentCommunicator) setMoveCellsPerSecond(val float64) {
	c.readVarMu.Lock()
	defer c.readVarMu.Unlock()

	c.moveCellsPerSecond = val
}

func (c *AgentCommunicator) setNextTickMovementTime() {
	movesPerSecond := c.getMoveCellsPerSecond()
	if movesPerSecond == 0 {
		return
	}

	now := time.Now()
	interval := core.TicksPerSecondToWaitDuration(movesPerSecond)

	c.readVarMu.Lock()
	defer c.readVarMu.Unlock()

	c.nextTickMovementTime = now.Add(interval)
}

func (c *AgentCommunicator) getStepsLeft() uint {
	c.readVarMu.RLock()
	defer c.readVarMu.RUnlock()

	return c.stepsLeft
}

func (c *AgentCommunicator) getMetersLeft() float64 {
	c.readVarMu.RLock()
	defer c.readVarMu.RUnlock()

	return c.metersLeft
}

func (c *AgentCommunicator) getMoveCellsPerSecond() float64 {
	c.readVarMu.RLock()
	defer c.readVarMu.RUnlock()

	return c.moveCellsPerSecond
}

func (c *AgentCommunicator) getNextTickMovementTime() time.Time {
	c.readVarMu.RLock()
	defer c.readVarMu.RUnlock()

	return c.nextTickMovementTime
}
