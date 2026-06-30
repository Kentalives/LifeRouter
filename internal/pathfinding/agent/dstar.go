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
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/log"
	"github.com/Kentalives/LifeRouter/internal/pathfinding/core"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
	"github.com/pkg/errors"
)

func cfg() pubconfig.AgentConfig {
	if config.Cfg == nil {
		panic("agent: config.Cfg is nil")
	}
	cfg := config.Cfg.Pathfinding.Agent
	if cfg.VisionRadiusCells == 0 {
		panic("agent: config.Pathfinding.Agent.VisionRadiusCells must be > 0")
	}
	if cfg.CellsForRealUpdate <= 0 {
		panic("agent: config.Pathfinding.Agent.CellsForRealUpdate must be > 0")
	}
	return cfg
}

// agentDStarLite is the per-agent D* Lite state. It plans on a virtual world so
// local object observations can change the agent's grid without mutating the global world.
type agentDStarLite struct {
	core.DStarLiteCore
	km             grid.Cost
	start, prevPos grid.GlobalCoords
	startIdx       grid.GlobalIdx
	vision         *grid.RegionGrid
	values         pagedValues
}

// recordWorldChanges stores the first previous value seen for each changed cell
// until applyChanges can replay the edge updates into g/rhs.
func (d *agentDStarLite) recordWorldChanges(cells []grid.ChangedGlobalCell) {
	d.ChangedMu.Lock()
	defer d.ChangedMu.Unlock()
	for _, cc := range cells {
		if _, ok := d.Changed[cc.C]; ok {
			continue
		}
		d.Changed[cc.C] = cc.PrevVal
	}
}

// Reset clears planner state while retaining reusable allocations.
func (d *agentDStarLite) Reset() {
	d.DStarLiteCore.Reset()
	d.km = 0
	d.start = grid.GlobalCoords{}
	d.prevPos = grid.GlobalCoords{}
	d.startIdx = -1
	d.values.clear()

	//NOTE: Vision is cleared each time it is used
}

var agentDStarLitePool sync.Pool

func newAgentDStarLite(w *grid.World, goal grid.GlobalCoords, agentId string) (*agentDStarLite, error) {
	return newAgentDStarLiteContext(context.Background(), w, goal, agentId)
}

func newAgentDStarLiteContext(ctx context.Context, w *grid.World, goal grid.GlobalCoords, agentId string) (*agentDStarLite, error) {
	d := &agentDStarLite{}
	if err := d.initialize(ctx, w, goal, agentId); err != nil {
		return nil, err
	}
	return d, nil
}

// acquireAgentDStarLite initializes a pooled planner and takes ownership of the
// virtual world until releaseAgentDStarLite is called.
func acquireAgentDStarLite(w *grid.World, goal grid.GlobalCoords, agentId string) (*agentDStarLite, error) {
	return acquireAgentDStarLiteContext(context.Background(), w, goal, agentId)
}

func acquireAgentDStarLiteContext(ctx context.Context, w *grid.World, goal grid.GlobalCoords, agentId string) (*agentDStarLite, error) {
	d, _ := agentDStarLitePool.Get().(*agentDStarLite)
	if d == nil {
		d = &agentDStarLite{}
	}
	if err := d.initialize(ctx, w, goal, agentId); err != nil {
		d.Reset()
		agentDStarLitePool.Put(d)
		grid.ReleaseVirtualWorld(w)
		return nil, err
	}
	return d, nil
}

// releaseAgentDStarLite closes world subscriptions, returns the virtual world,
// and clears planner state before pooling the struct.
func releaseAgentDStarLite(d *agentDStarLite) {
	if d == nil {
		return
	}
	if d.QuitWorldSub != nil {
		d.QuitWorldSub.Close()
	}
	grid.ReleaseVirtualWorld(d.World)
	d.Reset()
	agentDStarLitePool.Put(d)
}

// initialize resolves the agent's current geotruth position, binds the planner
// to the world, and seeds rhs(goal)=0 as the D* Lite starting invariant.
func (d *agentDStarLite) initialize(ctx context.Context, w *grid.World, goal grid.GlobalCoords, agentId string) error {

	var x, y float64
	objectData, err := config.Dep.Qu.ObjectData(ctx, agentId)
	if err != nil {
		return err
	}
	layer, err := grid.LayerFromName(*objectData.Region)
	if err != nil {
		return err
	}
	x = objectData.X
	y = objectData.Y

	start := grid.GlobalCoords{Coords: grid.CoordsFromFloat64(x, y), Layer: layer}

	if !w.Contains(start) {
		return fmt.Errorf("starting coordinate %v is outside the world", start)
	}
	if !w.Contains(goal) {
		return fmt.Errorf("goal coordinate %v is outside the world", goal)
	}

	startGrid := w.Floor(start.Layer)
	if startGrid == nil {
		return fmt.Errorf("starting grid layer (%d) does not exist", start.Layer)
	}
	goalGrid := w.Floor(goal.Layer)
	if goalGrid == nil {
		return fmt.Errorf("goal grid layer (%d) does not exist", goal.Layer)
	}
	if grid.IsBlocked(startGrid.GetValue(start.Coords)) {
		return fmt.Errorf("starting coordinate %v is blocked", start)
	}
	if grid.IsBlocked(goalGrid.GetValue(goal.Coords)) {
		return fmt.Errorf("goal coordinate %v is blocked", goal)
	}

	startingSize := int(math.RoundToEven(math.Min(float64(goalGrid.Cols*goalGrid.Rows/4), 150)))

	if d.Queue == nil {
		d.Queue = core.NewPrioQueue[grid.GlobalIdx, core.Key](startingSize, &core.Key_comparator)
	}
	d.km = 0
	d.Goal = goal.ToIdx(w)
	d.start = start
	d.startIdx = start.ToIdx(w)
	d.prevPos = d.start

	d.values.configure(w.Size(), cfg().SparseValuePageDirectory)
	if d.Changed == nil {
		d.Changed = make(map[grid.GlobalCoords]grid.Cost)
	}
	if d.NeighboursCache == nil {
		d.NeighboursCache = core.NewNeighboursCache()
	}
	d.setRhs(d.Goal, 0)

	d.World = w
	d.QuitWorldSub = d.World.SubChanges(func(cells []grid.ChangedGlobalCell) {
		d.recordWorldChanges(cells)
	})

	visionRadiusCells := cfg().VisionRadiusCells
	visionSideCells := 2*visionRadiusCells + 1
	visionCells := visionSideCells * visionSideCells
	if d.vision == nil {
		d.vision = &grid.RegionGrid{}
	}
	if cap(d.vision.Cells) < int(visionCells) {
		d.vision.Cells = make([]grid.Cost, visionCells)
	} else {
		d.vision.Cells = d.vision.Cells[:visionCells]
	}
	d.vision.Rows = visionSideCells
	d.vision.Cols = visionSideCells

	d.Queue.Insert(d.Goal, core.Key{K1: heuristic(start, goal), K2: 0})

	return nil
}

func (d *agentDStarLite) getG(c grid.GlobalIdx) grid.Cost {
	return d.values.getG(c)
}
func (d *agentDStarLite) getRhs(c grid.GlobalIdx) grid.Cost {
	return d.values.getRhs(c)
}
func (d *agentDStarLite) getGRhs(c grid.GlobalIdx) (grid.Cost, grid.Cost) {
	return d.values.get(c)
}
func (d *agentDStarLite) locallyConsistent(c grid.GlobalIdx) bool {
	g, rhs := d.getGRhs(c)
	return g == rhs
}
func (d *agentDStarLite) setG(c grid.GlobalIdx, v grid.Cost) {
	d.values.setG(c, v)
}
func (d *agentDStarLite) setRhs(c grid.GlobalIdx, v grid.Cost) {
	d.values.setRhs(c, v)
}

func (d *agentDStarLite) minCostSucc(c grid.GlobalCoords, cIdx grid.GlobalIdx) (grid.GlobalCoords, grid.Cost) {
	var outCoord grid.GlobalCoords
	outCost := grid.UNREACHABLE_COST
	succs, depth := d.SuccCosts(c, cIdx)
	for _, s := range succs {
		if s.Idx < 0 {
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

func heuristic(a, b grid.GlobalCoords) grid.Cost {
	dx := grid.Cost(core.AbsDiffUint(a.X, b.X))
	dy := grid.Cost(core.AbsDiffUint(a.Y, b.Y))
	minD := min(dx, dy)
	maxD := max(dx, dy)
	layerD := grid.Cost(absDiffInt(a.Layer, b.Layer))
	return grid.DiagonalCost(minD) + maxD - minD + layerD
}

func absDiffInt(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// calcKey includes km so priorities remain valid after the start moves.
func (d *agentDStarLite) calcKey(s grid.GlobalCoords, sIdx grid.GlobalIdx) core.Key {
	g, rhs := d.getGRhs(sIdx)
	return d.calcKeyWithGRhs(s, g, rhs)
}

func (d *agentDStarLite) calcKeyWithGRhs(s grid.GlobalCoords, g, rhs grid.Cost) core.Key {
	minimum := min(g, rhs)
	return core.Key{K1: minimum + heuristic(d.start, s) + d.km, K2: minimum}
}

// updateVertex keeps the queue synchronized with the local consistency of c.
func (d *agentDStarLite) updateVertex(c grid.GlobalCoords, cIdx grid.GlobalIdx) {
	g, rhs := d.getGRhs(cIdx)

	if idx := d.Queue.Find(cIdx); idx != -1 {
		if g != rhs {
			d.Queue.Update(idx, cIdx, d.calcKeyWithGRhs(c, g, rhs))
		} else {
			d.Queue.Remove(idx, cIdx)
		}
	} else if g != rhs {
		d.Queue.Insert(cIdx, d.calcKeyWithGRhs(c, g, rhs))
	}
}

// computeShortestPath restores local consistency for the current start vertex.
func (d *agentDStarLite) computeShortestPath() {
	topKey, ok := d.Queue.TopKey()
	for ok && (core.Key_comparator.Less(topKey, d.calcKey(d.start, d.startIdx)) || !d.locallyConsistent(d.startIdx)) {
		u, _ := d.Queue.Top()
		uCoord := u.ToGlobalCoords(d.World)

		kOld := topKey
		kNew := d.calcKey(uCoord, u)

		if core.Key_comparator.Less(kOld, kNew) {
			d.Queue.Update(0, u, kNew)

		} else if gOld, rhs := d.getGRhs(u); gOld > rhs {
			d.setG(u, rhs)
			d.Queue.Remove(0, u)
			preds, depth := d.PredCosts(uCoord, u)
			for _, s := range preds {
				d.setRhs(s.Idx, min(d.getRhs(s.Idx), grid.AddCost(s.Cost, rhs))) //NOTE: This final 'rhs' is the same as 'd.getG(u)' in execution
				d.updateVertex(s.C, s.Idx)
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
				d.updateVertex(s.C, s.Idx)
			}
			d.NeighboursCache.Release(depth)
			if u != d.Goal {
				_, cost := d.minCostSucc(uCoord, u)
				d.setRhs(u, cost)
			}
			d.updateVertex(uCoord, u)
		}

		topKey, ok = d.Queue.TopKey()
	}
}

func (d *agentDStarLite) String() string {
	// collect path by following minCostSucc from start to goal
	onPath := make(map[grid.GlobalCoords]bool)
	current := d.start
	visited := make(map[grid.GlobalCoords]bool)

	goalCoords := d.Goal.ToGlobalCoords(d.World)

	layersVisited := make([]int, 0)

	for current != goalCoords {
		onPath[current] = true
		if visited[current] {
			// cycle detected, path is broken
			onPath[current] = false
			break
		}
		if !slices.Contains(layersVisited, current.Layer) {
			layersVisited = append(layersVisited, current.Layer)
		}
		visited[current] = true
		next, cost := d.minCostSucc(current, current.ToIdx(d.World))
		if grid.IsUnreachable(cost) {
			break // no path
		}
		current = next
	}
	onPath[goalCoords] = true

	slices.SortFunc(layersVisited, func(a, b int) int {
		if a < b {
			return -1
		} else if a > b {
			return 1
		}
		return 0
	})

	var sb strings.Builder
	for _, layer := range layersVisited {
		sb.WriteRune('\n')
		sb.WriteString(fmt.Sprintf("FLOOR: %v\n", layer))
		currGrid := d.World.Floor(layer)
		for row := 0; row < int(currGrid.Rows); row++ {
			for col := 0; col < int(currGrid.Cols); col++ {
				c := grid.GlobalCoords{Coords: grid.Coords{X: uint(col), Y: uint(row)}, Layer: layer}
				cIdx := c.ToIdx(d.World)
				v := currGrid.GetValue(c.Coords)
				isPortal := len(d.World.PortalsFrom(cIdx)) > 0
				if isPortal {
					sb.WriteString("\033[0;43m")
				}
				switch {
				case c == d.start:
					sb.WriteRune('S')
				case c == goalCoords:
					sb.WriteRune('G')
				case onPath[c]:
					sb.WriteRune('■')
				case grid.IsBlocked(v):
					sb.WriteString("\033[0;31m")
					sb.WriteRune('░') // wall/obstacle
					sb.WriteString("\033[0m")
				case d.values.hasG(cIdx):
					if v >= grid.HEAVY_OBJECT_COST {
						sb.WriteString("\033[0;45m")
						sb.WriteRune('+') // expanded but not on path
						sb.WriteString("\033[0m")

					} else if v >= grid.LIGHT_OBJECT_COST {
						sb.WriteString("\033[0;44m")
						sb.WriteRune('+') // expanded but not on path
						sb.WriteString("\033[0m")

					} else if v > grid.EMPTY_SPACE_COST {
						sb.WriteString("\033[0;46m")
						sb.WriteRune('+') // expanded but not on path
						sb.WriteString("\033[0m")

					} else if v < grid.EMPTY_SPACE_COST {
						sb.WriteString("\033[0;42m")
						sb.WriteRune('+') // expanded but not on path
						sb.WriteString("\033[0m")

					} else {
						sb.WriteRune('+') // expanded but not on path
					}

				default:
					if v >= grid.HEAVY_OBJECT_COST {
						sb.WriteString("\033[0;45m")
						sb.WriteRune('·') // expanded but not on path
						sb.WriteString("\033[0m")

					} else if v >= grid.LIGHT_OBJECT_COST {
						sb.WriteString("\033[0;44m")
						sb.WriteRune('·') // expanded but not on path
						sb.WriteString("\033[0m")

					} else if v > grid.EMPTY_SPACE_COST {
						sb.WriteString("\033[0;46m")
						sb.WriteRune('·') // expanded but not on path
						sb.WriteString("\033[0m")

					} else if v < grid.EMPTY_SPACE_COST {
						sb.WriteString("\033[0;42m")
						sb.WriteRune('·') // untouched
						sb.WriteString("\033[0m")

					} else {
						sb.WriteRune('·') // untouched
					}

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
	sb.WriteString(fmt.Sprintf("\nstart=%v goal=%v km=%d\n", d.start, d.Goal, d.km))
	sb.WriteString(fmt.Sprintf("g(start)=%d rhs(start)=%d\n",
		d.getG(d.startIdx), d.getRhs(d.startIdx)))
	sb.WriteString(fmt.Sprintf("expanded nodes=%d queue size=%d\n",
		d.values.gLen, d.Queue.Len()))

	return sb.String()
}

// applyLocalVision replaces the agent's local object overlay with nearby
// geotruth objects clipped to the configured vision radius.
func (d *agentDStarLite) applyLocalVision(ctx context.Context, agentId string) error { //TODO: Update local known portals

	//NOTE: Obtain nearby objects from world using "visionRadius"
	visionRadiusCells := cfg().VisionRadiusCells
	objects, err := config.Dep.Qu.NearbyObjectsOf(ctx, agentId, float64(visionRadiusCells)*grid.CellSizeM()*math.Sqrt2, nil)
	if err != nil {
		return err
	}

	currGrid := d.World.Floor(d.start.Layer)
	if currGrid == nil {
		return err
	}

	d.vision.ClipToGrid(currGrid, visionRadiusCells, visionRadiusCells, d.start.Coords)
	clear(d.vision.Cells)

	d.ApplyRegionObjects(d.vision, objects)
	currGrid.ReplaceObjectRegion(d.vision)

	return nil
}

// applyChanges replays recorded grid changes and updates predecessor rhs values
// that depended on the old costs.
func (d *agentDStarLite) applyChanges() {
	d.ChangedMu.Lock()
	for v, oldCellVal := range d.Changed {

		vIdx := v.ToIdx(d.World)

		// save g BEFORE clearing, needed for the neighbor rhs check
		oldG := d.getG(vIdx)
		uGrid := d.World.Floor(v.Layer)
		newValue := uGrid.GetValue(v.Coords)

		//Intraversable
		if grid.IsBlocked(newValue) {
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
			d.updateVertex(v, vIdx)
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
			d.updateVertex(u.C, u.Idx)
		}
		d.NeighboursCache.Release(depth)
		delete(d.Changed, v)
	}
	d.ChangedMu.Unlock()

	d.computeShortestPath()
}

// updateRealPosition publishes the agent's current grid cell as a real-world
// position, using the external system to recover the floor height.
func updateRealPosition(gridPos grid.GlobalCoords, comm *AgentCommunicator) error {
	x, y := gridPos.ToFloat64()

	layerName, err := grid.IdxToLayerName(gridPos.Layer)
	if err != nil {
		return err
	}

	z, err := config.Dep.Ex.HeightAtFloorPoint(comm.ctx, x, y, layerName)
	if err != nil {
		return err
	}

	_, err = config.Dep.Pu.UpdateObjectPosition(comm.ctx, comm.id, x, y, z, 0)
	if err != nil {
		return err
	}

	return nil
}

// InternalAgentFindPath starts an asynchronous adaptive pathfinding run. The
// returned communicator owns movement commands and completion/error reporting.
// InternalAgentFindPath starts an adaptive pathfinding run for agentId.
//
// The returned communicator controls movement and observes completion.
func InternalAgentFindPath(w *grid.World, goal grid.GlobalCoords, agentId string, defaultCellsPerSecondMovement float64, moveNSteps uint, verbose bool) (*AgentCommunicator, error) {
	return InternalAgentFindPathContext(context.Background(), w, goal, agentId, defaultCellsPerSecondMovement, moveNSteps, verbose)
}

// InternalAgentFindPathContext starts an adaptive pathfinding run using ctx for
// startup dependency queries. The returned communicator owns runtime cancellation.
func InternalAgentFindPathContext(ctx context.Context, w *grid.World, goal grid.GlobalCoords, agentId string, defaultCellsPerSecondMovement float64, moveNSteps uint, verbose bool) (*AgentCommunicator, error) {
	stepsForRealUpdate := cfg().CellsForRealUpdate

	d, err := acquireAgentDStarLiteContext(ctx, w, goal, agentId)
	if err != nil {
		return nil, fmt.Errorf("Could not start pathfinding: %w", err)
	}
	comm := NewAgentCommunicator(agentId, moveNSteps, defaultCellsPerSecondMovement)

	go func() {
		steps := 0
		defer func() {
			if steps%stepsForRealUpdate != 0 {

				if err := updateRealPosition(d.start, comm); err != nil {
					comm.setExitError(errors.Wrap(err, "UpdateObjectPosition"))
					comm.sendError(errors.Wrap(err, "UpdateObjectPosition"))
				}
			}

			releaseAgentDStarLite(d)
			comm.destroy()

			if verbose {
				log.Printf("TOTAL STEPS: %v\n", steps)
			}
		}()

		//NOTE: Obtain local vision
		err = d.applyLocalVision(comm.ctx, agentId)
		if err != nil {
			comm.sendError(fmt.Errorf("Could not get local vision: %w", err))
		}

		d.computeShortestPath()
		comm.publishPath(d)

		for d.startIdx != d.Goal {

			if exit := comm.waitUntilStep(); exit {
				return
			}
			comm.publishPathIfRequested(d)

			if steps%stepsForRealUpdate == 0 {
				//NOTE: Obtain local vision
				err = d.applyLocalVision(comm.ctx, agentId)
				if err != nil {
					comm.sendError(fmt.Errorf("Could not get local vision: %w", err))
				}

				if !d.EmptyChangedEdges() {
					d.km += heuristic(d.prevPos, d.start)
					d.prevPos = d.start

					d.applyChanges()
					comm.publishPath(d)
				}
			}

			if verbose {
				log.Print("AGENT: ", agentId, "\n", d, "\n")

			}

			if grid.IsUnreachable(d.getRhs(d.startIdx)) {
				comm.setExitError(pubdomain.ErrAgentNoPath)
				comm.sendError(pubdomain.ErrAgentNoPath)
				return
			}

			next, costOfNext := d.minCostSucc(d.start, d.startIdx)
			nextIdx := next.ToIdx(d.World)
			if grid.IsUnreachable(costOfNext) || nextIdx < 0 {
				comm.setExitError(pubdomain.ErrAgentNoPath)
				comm.sendError(pubdomain.ErrAgentNoPath)
				return
			}

			if !comm.tryReduceMetersMovement(costOfNext - d.getG(nextIdx)) {
				continue
			}

			steps++
			d.start = next
			d.startIdx = nextIdx

			if steps%stepsForRealUpdate == 0 {

				if err := updateRealPosition(d.start, comm); err != nil {
					comm.setExitError(errors.Wrap(err, "UpdateObjectPosition"))
					comm.sendError(errors.Wrap(err, "UpdateObjectPosition"))
				}
			}
		}

		if verbose {
			log.Print("AGENT: ", agentId, "\n", d, "\n")
		}

	}()

	return comm, nil
}

// InternalAgentNaivePathCost computes the current cost to the goal without
// starting the movement loop or publishing position updates.
// InternalAgentNaivePathCost computes a route cost without starting movement.
func InternalAgentNaivePathCost(w *grid.World, goal grid.GlobalCoords, agentId string, verbose bool) (grid.Cost, error) {
	return InternalAgentNaivePathCostContext(context.Background(), w, goal, agentId, verbose)
}

// InternalAgentNaivePathCostContext computes a route cost using ctx for startup
// dependency queries.
func InternalAgentNaivePathCostContext(ctx context.Context, w *grid.World, goal grid.GlobalCoords, agentId string, verbose bool) (grid.Cost, error) {
	d, err := acquireAgentDStarLiteContext(ctx, w, goal, agentId)
	if err != nil {
		return 0, fmt.Errorf("Could not start pathfinding: %w", err)
	}
	defer releaseAgentDStarLite(d)

	d.computeShortestPath()

	if verbose {
		log.Print("AGENT: ", agentId, "\n", d, "\n")
	}

	cost := d.getG(d.startIdx)
	if grid.IsUnreachable(cost) {
		return 0, pubdomain.ErrAgentNoPath
	}

	return cost, nil
}
