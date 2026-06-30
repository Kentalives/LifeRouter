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

package emergency

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/domain"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/log"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/core"
	"github.com/Kentalives/LifeRouter/internal/raster"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

// flowPublisher owns the single active emergency flow subscription. A new
// subscriber replaces the previous channel and requests a fresh snapshot.
type flowPublisher struct {
	ch        chan<- map[string][]pubdomain.Direction
	requested bool
	mu        sync.RWMutex
}

var publisher flowPublisher

// SetFlowPublisher installs the channel that receives flow snapshots.
// SetFlowPublisher attaches the channel used to stream emergency flow snapshots.
func SetFlowPublisher(ch chan<- map[string][]pubdomain.Direction) {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()

	if publisher.ch != nil && publisher.ch != ch {
		close(publisher.ch)
	}

	publisher.ch = ch
	publisher.requested = true
}

func (p *flowPublisher) close() {
	publisher.mu.Lock()
	defer publisher.mu.Unlock()

	if p.ch != nil {
		close(p.ch)
		p.ch = nil
	}
}

func (p *flowPublisher) publish(d *sysDStarLite) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.requested = false

	if p.ch == nil {
		return
	}

	flowData := d.snapshotFlow()
	select {
	case p.ch <- flowData:
	case <-time.After(200 * time.Millisecond): //Timeout
	}
}
func (p *flowPublisher) publishIfRequested(d *sysDStarLite) {
	p.mu.RLock()
	wantPublish := p.requested
	p.mu.RUnlock()

	if !wantPublish {
		return
	}

	p.publish(d)
}

func cfg() pubconfig.EmergencyConfig {
	if config.Cfg == nil {
		panic("emergency: config.Cfg is nil")
	}
	cfg := config.Cfg.Pathfinding.Emergency
	if cfg.LightingHystheresis <= 0 {
		panic("emergency: config.Pathfinding.Emergency.LightingHystheresis must be > 0")
	}
	if cfg.PriorityPathCellsWidth <= 0 {
		panic("emergency: config.Pathfinding.Emergency.PriorityPathCellsWidth must be > 0")
	}
	return cfg
}

type nodeCell struct {
	node domain.INode
	c    grid.GlobalCoords
	oldG grid.Cost
}

// sysDStarLite maintains one global emergency flow-field. It uses a fake goal
// connected to all exits so each cell can follow one successor toward safety.
type sysDStarLite struct {
	core.DStarLiteCore
	rhsVal, gVal []grid.Cost

	lightNodes         map[grid.GlobalIdx]*nodeCell
	outdatedLightNodes []*nodeCell
	lightsMu           sync.Mutex
	dirCountingCache   []uint8

	linkedObjRegions []*grid.RegionGrid
	fakeGoal         grid.GlobalCoords
	realGoals        []grid.GlobalCoords
}

func appendUniqueLightNodes(s []*nodeCell, v *nodeCell) []*nodeCell {
	for _, val := range s {
		if v == val {
			return s
		}
	}
	return append(s, v)
}

func absCost(v grid.Cost) grid.Cost {
	if v < 0 {
		return -v
	}
	return v
}

// newSysDStarLite initializes the emergency planner, light nodes, and fake-goal
// portals that make multiple exits look like one D* Lite target.
func newSysDStarLite(realGoals []grid.GlobalCoords, nodes []domain.INode, w *grid.World) (*sysDStarLite, error) {
	var d sysDStarLite

	fakeGoal := grid.SentinelGlobalCoords

	startingSize := 1 + w.Size() //NOTE: Add extra 1 for "fakeGoal"

	d.lightNodes = make(map[grid.GlobalIdx]*nodeCell, len(nodes))
	d.outdatedLightNodes = make([]*nodeCell, 0, len(nodes))

	d.lightsMu.Lock()
	for _, node := range nodes {
		x, y, _, floor := node.Position()
		n := &nodeCell{
			node: node,
			c:    grid.GlobalCoords{Coords: grid.CoordsFromFloat64(x, y), Layer: floor}, //grid.GlobalCoordsFromFloat64(x, y, z),
			oldG: grid.UNREACHABLE_COST,
		}
		if !w.Contains(n.c) {
			d.lightsMu.Unlock()
			return nil, fmt.Errorf("light node coordinate %v is outside the world", n.c)
		}

		d.lightNodes[n.c.ToIdx(w)] = n

		d.outdatedLightNodes = append(d.outdatedLightNodes, n)
	}
	d.lightsMu.Unlock()

	d.dirCountingCache = make([]uint8, 4)

	d.Queue = core.NewPrioQueue[grid.GlobalIdx, core.Key](int(math.Ceil(math.Sqrt(float64(startingSize)))), &core.Key_comparator)
	d.Goal = fakeGoal.ToIdx(w)
	d.fakeGoal = fakeGoal
	d.realGoals = append(d.realGoals, realGoals...)

	for _, goal := range realGoals {
		if !w.Contains(goal) {
			return nil, fmt.Errorf("emergency goal coordinate %v is outside the world", goal)
		}
		goalGrid := w.Floor(goal.Layer)
		if goalGrid == nil || grid.IsBlocked(goalGrid.GetValue(goal.Coords)) {
			return nil, fmt.Errorf("emergency goal coordinate %v is blocked", goal)
		}
	}

	for _, goal := range realGoals {
		w.AddLocalBidirectionalPortal(goal, fakeGoal, 1)
	}

	d.gVal = make([]grid.Cost, startingSize)
	d.rhsVal = make([]grid.Cost, startingSize)
	for i := range d.gVal {
		d.gVal[i] = grid.UNREACHABLE_COST
		d.rhsVal[i] = grid.UNREACHABLE_COST
	}

	d.Changed = make(map[grid.GlobalCoords]grid.Cost)
	d.NeighboursCache = core.NewNeighboursCache()
	d.setRhs(d.Goal, 0)

	d.linkedObjRegions = w.LinkedObjectRegions()
	d.World = w
	d.QuitWorldSub = d.World.SubChanges(func(cells []grid.ChangedGlobalCell) { //c grid.GlobalCoords, prevValue float64) {
		d.ChangedMu.Lock()
		defer d.ChangedMu.Unlock()

		for _, cc := range cells {
			if _, ok := d.Changed[cc.C]; ok {
				continue
			}
			d.Changed[cc.C] = cc.PrevVal
			if node, ok := d.lightNodes[cc.C.ToIdx(d.World)]; ok {
				d.lightsMu.Lock()
				d.outdatedLightNodes = appendUniqueLightNodes(d.outdatedLightNodes, node) //append(d.outdatedLightNodes, node)
				d.lightsMu.Unlock()
			}
		}
	})

	d.Queue.Insert(d.Goal, core.Key{K1: 0, K2: 0})

	return &d, nil
}

func (d *sysDStarLite) cleanupFakeGoalPortals() {
	for _, goal := range d.realGoals {
		d.World.RemoveLocalBidirectionalPortal(goal, d.fakeGoal)
	}
}

func (d *sysDStarLite) getG(c grid.GlobalIdx) grid.Cost {

	return d.gVal[c+1]
}
func (d *sysDStarLite) getRhs(c grid.GlobalIdx) grid.Cost {

	return d.rhsVal[c+1]
}
func (d *sysDStarLite) setG(c grid.GlobalIdx, v grid.Cost) {

	d.gVal[c+1] = v
}
func (d *sysDStarLite) setRhs(c grid.GlobalIdx, v grid.Cost) {

	d.rhsVal[c+1] = v
}

func (d *sysDStarLite) minCostSucc(c grid.GlobalCoords, cIdx grid.GlobalIdx) (grid.GlobalCoords, grid.Cost) {
	var outCoord grid.GlobalCoords
	outCost := grid.UNREACHABLE_COST
	succs, depth := d.SuccCosts(c, cIdx)
	for _, s := range succs {
		if s.Idx < 0 && s.C != d.fakeGoal {
			continue
		}
		if reachCost := grid.AddCost(s.Cost, d.getG(s.Idx)); reachCost < outCost {
			outCost = reachCost
			outCoord = s.C
		}
	}
	d.NeighboursCache.Release(depth)
	return outCoord, outCost
}

func (d *sysDStarLite) calcKey(s grid.GlobalIdx) core.Key {
	g, rhs := d.getG(s), d.getRhs(s)
	return d.calcKeyWithGRhs(g, rhs)
}

func (d *sysDStarLite) calcKeyWithGRhs(g, rhs grid.Cost) core.Key {
	minimum := min(g, rhs)
	return core.Key{K1: minimum, K2: minimum}
}

// updateSignaling changes light-node directions only when the path cost changed
// enough to cross the configured hysteresis threshold.
func (d *sysDStarLite) updateSignaling() {
	d.lightsMu.Lock()
	defer d.lightsMu.Unlock()

	for _, n := range d.outdatedLightNodes {

		cIdx := n.c.ToIdx(d.World)
		newG := d.getG(cIdx)
		if absCost(newG-n.oldG) < cfg().LightingHystheresis {
			continue
		}

		clear(d.dirCountingCache)

		updateDirCache := func(flow pubdomain.Direction, dirCache []uint8) {
			switch flow {
			case pubdomain.DIR_UP:
				dirCache[pubdomain.DIR_UP] += 2
			case pubdomain.DIR_UP_LEFT:
				dirCache[pubdomain.DIR_UP]++
				dirCache[pubdomain.DIR_LEFT]++
			case pubdomain.DIR_UP_RIGHT:
				dirCache[pubdomain.DIR_UP]++
				dirCache[pubdomain.DIR_RIGHT]++
			case pubdomain.DIR_DOWN:
				dirCache[pubdomain.DIR_DOWN] += 2
			case pubdomain.DIR_DOWN_LEFT:
				dirCache[pubdomain.DIR_DOWN]++
				dirCache[pubdomain.DIR_LEFT]++
			case pubdomain.DIR_DOWN_RIGHT:
				dirCache[pubdomain.DIR_DOWN]++
				dirCache[pubdomain.DIR_RIGHT]++
			case pubdomain.DIR_LEFT:
				dirCache[pubdomain.DIR_LEFT] += 2
			case pubdomain.DIR_RIGHT:
				dirCache[pubdomain.DIR_RIGHT] += 2
				//No IN or OUT
			}
		}

		touchingCells, depth := d.Neighbors(n.c)
		for _, u := range touchingCells {
			uIdx := u.ToIdx(d.World)

			uFlow := d.getFlow(u, uIdx)
			updateDirCache(uFlow, d.dirCountingCache)
		}
		d.NeighboursCache.Release(depth)

		myFlow := d.getFlow(n.c, cIdx)
		updateDirCache(myFlow, d.dirCountingCache)

		var maxCount uint8 = 0
		selectedDir := myFlow
		for i, count := range d.dirCountingCache {
			var dir pubdomain.Direction = pubdomain.Direction(i)
			if count > maxCount {
				selectedDir = dir
				maxCount = count
			} else if count == maxCount && dir == myFlow {
				selectedDir = dir
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		err := config.Dep.Ex.SignalingSetDirection(ctx, n.node.Id(), selectedDir)
		cancel()
		if err != nil {
			log.Errorf("Could not set signaling direction for (%s): %s", n.node.Id(), err)
			continue
		}

		n.oldG = newG
	}

	d.outdatedLightNodes = d.outdatedLightNodes[:0]
}

func (d *sysDStarLite) computeShortestPath() {
	topKey, ok := d.Queue.TopKey()
	for ok {
		u, _ := d.Queue.Top()
		uCoord := u.ToGlobalCoords(d.World)

		kOld := topKey
		kNew := d.calcKey(u)

		if core.Key_comparator.Less(kOld, kNew) {
			d.Queue.Update(0, u, kNew)

		} else if rhs, gOld := d.getRhs(u), d.getG(u); gOld > rhs {
			d.setG(u, rhs)
			d.Queue.Remove(0, u)
			preds, depth := d.PredCosts(uCoord, u)
			for _, s := range preds {
				d.setRhs(s.Idx, min(d.getRhs(s.Idx), grid.AddCost(s.Cost, rhs))) //NOTE: This final 'rhs' is the same as 'd.getG(u)' in execution
				d.updateVertex(s.Idx)
			}
			d.NeighboursCache.Release(depth)

		} else {
			d.setG(u, grid.UNREACHABLE_COST)

			preds, depth := d.PredCosts(uCoord, u)
			for _, s := range preds {
				if d.getRhs(s.Idx) == grid.AddCost(s.Cost, gOld) && s.Idx != d.Goal {
					_, cost := d.minCostSucc(s.C, s.Idx)
					d.setRhs(s.Idx, cost)
				}
				d.updateVertex(s.Idx)
			}
			d.NeighboursCache.Release(depth)
			if u != d.Goal {
				_, cost := d.minCostSucc(uCoord, u)
				d.setRhs(u, cost)
			}
			d.updateVertex(u)
		}

		topKey, ok = d.Queue.TopKey()
	}
	d.updateSignaling()
}

func (d *sysDStarLite) updateVertex(c grid.GlobalIdx) {
	g := d.getG(c)
	rhs := d.getRhs(c)

	if idx := d.Queue.Find(c); idx != -1 {
		if g != rhs {
			d.Queue.Update(idx, c, d.calcKeyWithGRhs(g, rhs))
		} else {
			d.Queue.Remove(idx, c)
		}
	} else if g != rhs {
		d.Queue.Insert(c, d.calcKeyWithGRhs(g, rhs))
	}
}

// getFlow converts the best successor of c into a public direction value.
func (d *sysDStarLite) getFlow(c grid.GlobalCoords, cIdx grid.GlobalIdx) pubdomain.Direction {
	next, cost := d.minCostSucc(c, cIdx)
	if grid.IsUnreachable(cost) {
		return pubdomain.DIR_UNKNOWN
	}
	x, y, layer := int(next.X)-int(c.X), int(next.Y)-int(c.Y), int(next.Layer)-int(c.Layer)

	switch {
	case layer > 0:
		return pubdomain.DIR_OUT
	case layer < 0:
		return pubdomain.DIR_IN
	//////////
	case x == -1 && y == -1:
		return pubdomain.DIR_UP_LEFT
	case x == 0 && y == -1:
		return pubdomain.DIR_UP
	case x == 1 && y == -1:
		return pubdomain.DIR_UP_RIGHT
	//////////
	case x == -1 && y == 0:
		return pubdomain.DIR_LEFT
	case x == 1 && y == 0:
		return pubdomain.DIR_RIGHT
	//////////
	case x == -1 && y == 1:
		return pubdomain.DIR_DOWN_LEFT
	case x == 0 && y == 1:
		return pubdomain.DIR_DOWN
	case x == 1 && y == 1:
		return pubdomain.DIR_DOWN_RIGHT
	}

	return pubdomain.DIR_UNKNOWN //ERROR
}

// snapshotFlow returns one direction array per floor name for subscribers.
func (d *sysDStarLite) snapshotFlow() map[string][]pubdomain.Direction {
	visualWorld := make(map[string][]pubdomain.Direction, d.World.Len())
	for l := range d.World.Len() {
		layerName, err := grid.IdxToLayerName(l)
		if err != nil {
			log.Errorf("-Visualization- %s", err)
			continue
		}
		g := d.World.Floor(l)

		visualGrid := make([]pubdomain.Direction, 0, g.Cols*g.Rows)
		for i := range g.Rows {
			for j := range g.Cols {
				c := grid.GlobalCoords{Coords: grid.Coords{X: j, Y: i}, Layer: l}
				cIdx := c.ToIdx(d.World)

				state := d.getFlow(c, cIdx)
				visualGrid = append(visualGrid, state)
			}
		}

		visualWorld[layerName] = visualGrid
	}

	return visualWorld
}

func (d *sysDStarLite) String() string {

	var sb strings.Builder
	for i := 0; i < d.World.Len(); i++ {
		currGrid := d.World.Floor(i)
		if currGrid == nil {
			continue
		}

		sb.WriteRune('\n')
		sb.WriteString(fmt.Sprintf("FLOOR: %d\n", i))
		for row := 0; row < int(currGrid.Rows); row++ {
			for col := 0; col < int(currGrid.Cols); col++ {
				c := grid.GlobalCoords{Coords: grid.Coords{X: uint(col), Y: uint(row)}, Layer: i}
				cIdx := c.ToIdx(d.World)
				isPortal := len(d.World.PortalsFrom(cIdx)) > 0
				if isPortal {
					sb.WriteString("\033[0;43m")
				}

				if d.getRhs(cIdx) < 1 {
					sb.WriteString("\033[0;32m")
					sb.WriteRune('■')
					sb.WriteString("\033[0m")
					sb.WriteRune(' ')
					continue
				}

				dir := d.getFlow(c, cIdx) //↑ → ↓ ← ↗ ↘ ↙ ↖ ?
				switch {
				case currGrid.GetValue(c.Coords) <= 0:
					sb.WriteString("\033[0;31m")
					sb.WriteRune('░') // wall/obstacle
					sb.WriteString("\033[0m")
				case dir == pubdomain.DIR_IN:
					sb.WriteString("\033[0;34m")
					sb.WriteRune('x')
					sb.WriteString("\033[0m")
				case dir == pubdomain.DIR_OUT:
					sb.WriteString("\033[0;34m")
					sb.WriteRune('o')
					sb.WriteString("\033[0m")
				//////
				case dir == pubdomain.DIR_UP_LEFT:
					sb.WriteRune('↖')
				case dir == pubdomain.DIR_UP:
					sb.WriteRune('↑')
				case dir == pubdomain.DIR_UP_RIGHT:
					sb.WriteRune('↗')
				//////
				case dir == pubdomain.DIR_LEFT:
					sb.WriteRune('←')
				case dir == pubdomain.DIR_RIGHT:
					sb.WriteRune('→')
				//////
				case dir == pubdomain.DIR_DOWN_LEFT:
					sb.WriteRune('↙')
				case dir == pubdomain.DIR_DOWN:
					sb.WriteRune('↓')
				case dir == pubdomain.DIR_DOWN_RIGHT:
					sb.WriteRune('↘')
				default:
					sb.WriteString("\033[0;31m")
					sb.WriteRune('?')
					sb.WriteString("\033[0m")
				}
				if isPortal {
					sb.WriteString("\033[0m")
				}
				sb.WriteRune(' ')

			}
			sb.WriteRune('\n')
		}
	}

	// print cost info for debugging
	sb.WriteString(fmt.Sprintf("expanded nodes=%d queue size=%d\n",
		len(d.gVal), d.Queue.Len()))

	return sb.String()
}

// applyEnvironmentObjects refreshes object overlays for all floors before
// replanning so the flow field reflects the current environment.
func (d *sysDStarLite) applyEnvironmentObjects() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3000*time.Millisecond)
	allObjects, err := config.Dep.Qu.AllObjectsOriented(ctx, nil)
	cancel()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	//NOTE: Run in parallel the grid replacing functions (one for each floor)
	for i := range d.World.Len() {
		wg.Add(1)
		go func(floorNum int) {
			defer wg.Done()

			floorName, err := grid.IdxToLayerName(floorNum)
			if err != nil {
				log.Error(err)
				return
			}
			objects := allObjects.Regions[floorName]
			grid := d.World.Floor(floorNum)
			linkedRegion := d.linkedObjRegions[floorNum]
			clear(linkedRegion.Cells)

			d.ApplyRegionObjects(linkedRegion, objects)
			grid.UpdateFinal()
		}(i)
	}

	wg.Wait()

	return nil
}

// applyChanges replays grid updates into the flow-field g/rhs values and marks
// affected light nodes for signaling updates.
func (d *sysDStarLite) applyChanges() {
	d.ChangedMu.Lock()
	for v, oldCellVal := range d.Changed {
		vIdx := v.ToIdx(d.World)
		// save g BEFORE clearing, needed for the neighbor rhs check
		oldG := d.getG(vIdx)
		uGrid := d.World.Floor(v.Layer)
		newValue := uGrid.GetValue(v.Coords)

		//Intraversable
		if newValue <= 0 {
			d.setG(vIdx, grid.UNREACHABLE_COST)
			d.setRhs(vIdx, grid.UNREACHABLE_COST)
			if idx := d.Queue.Find(vIdx); idx != -1 {
				d.Queue.Remove(idx, vIdx)
			}

		} else if newValue > 0 {
			if vIdx != d.Goal {
				_, cost := d.minCostSucc(v, vIdx)
				d.setRhs(vIdx, cost)
			}
			d.updateVertex(vIdx)
		}

		//Check effect on neighbours
		preds, depth := d.PredChangeCosts(v, vIdx, oldCellVal)
		for _, u := range preds {
			rhsU := d.getRhs(u.Idx)

			if u.OldCost > u.NewCost {
				d.setRhs(u.Idx, min(rhsU, grid.AddCost(u.NewCost, d.getG(vIdx))))
			} else if rhsU == grid.AddCost(u.OldCost, oldG) && u.Idx != d.Goal {
				_, cost := d.minCostSucc(u.C, u.Idx)
				d.setRhs(u.Idx, cost)
			}

			//Marking as outdated light nodes that get replanned
			if node, ok := d.lightNodes[u.Idx]; ok {
				d.lightsMu.Lock()
				d.outdatedLightNodes = appendUniqueLightNodes(d.outdatedLightNodes, node)
				d.lightsMu.Unlock()
			}

			d.updateVertex(u.Idx)
		}
		d.NeighboursCache.Release(depth)
		delete(d.Changed, v)
	}
	d.ChangedMu.Unlock()
	d.computeShortestPath()
}

// InternalEmergencyStart runs the emergency flow-field loop until quit is
// closed. The returned done channel closes after subscriptions and publishers are cleaned up.
// InternalEmergencyStart starts the emergency flow-field loop.
//
// The returned done channel closes after the loop stops and cleanup owned by the
// algorithm is complete.
func InternalEmergencyStart(w *grid.World, nodes []domain.INode, goals []grid.GlobalCoords, updateTicksPerSecond float64, quit <-chan struct{}, verbose bool) (done <-chan struct{}, err error) {

	d, err := newSysDStarLite(goals, nodes, w)
	if err != nil {
		return nil, err
	}

	internalDone := make(chan struct{})
	done = internalDone

	go func() {
		defer func() {
			publisher.close()
			d.cleanupFakeGoalPortals()
			d.QuitWorldSub.Close()
			close(internalDone)
		}()

		err := d.applyEnvironmentObjects()
		if err != nil {
			log.Errorf("-EmergencyVision- %s", err)
		}
		d.computeShortestPath()
		publisher.publish(d)

		if verbose {
			log.Print(d, "\n")
		}

		waitTickTime := core.TicksPerSecondToWaitDuration(updateTicksPerSecond)

		for {
			select {
			case <-quit:
				return
			case <-time.After(waitTickTime):

				publisher.publishIfRequested(d)
				err := d.applyEnvironmentObjects()
				if err != nil {
					log.Errorf("-EmergencyVision- %s", err)
				}
				if !d.EmptyChangedEdges() {

					d.applyChanges()
					publisher.publish(d)

					if verbose {
						log.Print(d, "\n")
					}
				}
			}
		}
	}()

	return done, nil
}

// ApplyPreferencePaths lowers traversal cost along preferred route graph edges
// so emergency planning favors those corridors.
// ApplyPreferencePaths applies preferred-route discounts from route graphs.
func ApplyPreferencePaths(graphs []pubdomain.RouteGraph, w *grid.World) {
	meanFloorSize := w.Size() / w.Len()
	r := &grid.RegionGrid{
		Origin: grid.Coords{X: 0, Y: 0},
		Rows:   0,
		Cols:   0,
		Cells:  make([]grid.Cost, meanFloorSize),
	}

	for _, graph := range graphs {
		floorGrid := w.Floor(graph.Floor)
		if floorGrid == nil {
			continue
		}
		if r.Rows != 0 {
			if meanFloorSize < int(floorGrid.Cols*floorGrid.Rows) {
				r.Cells = make([]grid.Cost, floorGrid.Cols*floorGrid.Rows)
			} else {
				clear(r.Cells)
			}
		}
		r.ClipToGrid(floorGrid, floorGrid.Cols, floorGrid.Rows, r.Origin)

		for _, edge := range graph.Edges {
			var fromWaypoint, toWaypoint *pubdomain.GraphWaypoint
			for wIdx, waypoint := range graph.Waypoints {
				if waypoint.Id == edge.FromWaypoint {
					fromWaypoint = &graph.Waypoints[wIdx]
					if toWaypoint != nil {
						break
					}
				} else if waypoint.Id == edge.ToWaypoint {
					toWaypoint = &graph.Waypoints[wIdx]
					if fromWaypoint != nil {
						break
					}
				}
			}

			if fromWaypoint == nil || toWaypoint == nil {
				continue
			}
			weight := uint16(1)
			if edge.Weight != nil {
				weight = *edge.Weight
			}
			raster.RasterLine(
				raster.Vec2{X: fromWaypoint.X, Y: fromWaypoint.Y},
				raster.Vec2{X: toWaypoint.X, Y: toWaypoint.Y},
				cfg().PriorityPathCellsWidth*grid.CellSizeM(),
				preferenceDiscount(weight),
				grid.CellSizeM(),
				r,
			)

		}
		floorGrid.MaskBaseRegion(r)
	}
}

// RemovePreferencePaths restores each floor base cost after an emergency run.
// RemovePreferencePaths restores the traversable base cost after preference paths.
func RemovePreferencePaths(w *grid.World, traversableCost grid.Cost) {
	for i := range w.Len() {
		floorGrid := w.Floor(i)
		floorGrid.ResetBase(traversableCost)
	}
}

// preferenceDiscount maps graph edge weight to a negative cost mask.
func preferenceDiscount(weight uint16) grid.Cost {
	if weight <= 0 {
		return -grid.EMPTY_SPACE_COST + 1
	}
	weight--
	return -grid.Cost(math.MaxUint16-weight) * (grid.EMPTY_SPACE_COST - 1) / math.MaxUint16
}
