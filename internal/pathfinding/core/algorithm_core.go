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

package core

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/midtxwn/geotruth/pkg/natsquery"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/grid"
	"github.com/Kentalives/LifeRouter/internal/log"
	"github.com/Kentalives/LifeRouter/internal/raster"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
)

// Key is the D* Lite priority key. K1 orders the global priority and K2 breaks
// ties for vertices with the same estimated total cost.
type Key struct {
	K1, K2 grid.Cost
}

func cfg() pubconfig.PathfindingConfig {
	if config.Cfg == nil {
		panic("core: config.Cfg is nil")
	}
	cfg := config.Cfg.Pathfinding
	if cfg.FreeMovementHeight <= 0 {
		panic("core: config.Pathfinding.FreeMovementHeight must be > 0")
	}
	return cfg
}

// Key_comparator keeps the priority queue ordered by K1 and then K2.
var Key_comparator = comparator[Key]{
	Less: func(x, y Key) bool {
		if x.K1 == y.K1 {
			return x.K2 < y.K2
		}
		return x.K1 < y.K1
	},
	More: func(x, y Key) bool {
		if x.K1 == y.K1 {
			return x.K2 > y.K2
		}
		return x.K1 > y.K1
	},
	Equals: func(x, y Key) bool { return x.K1 == y.K1 && x.K2 == y.K2 },
}

// NeighboursCache reuses short neighbor slices during nested successor and
// predecessor scans. Callers must Release the depth returned by GetFreeCache.
type NeighboursCache struct {
	depth int
	c1    []grid.GlobalCoords
	c2    []grid.GlobalCoords
	e1    []grid.CostedNeighbor
	e2    []grid.CostedNeighbor
	ch1   []grid.CostedNeighborChange
	ch2   []grid.CostedNeighborChange
}

// GetFreeCache returns a reusable neighbor slice and its nesting depth.
func (cache *NeighboursCache) GetFreeCache() ([]grid.GlobalCoords, int) {
	d := cache.depth
	cache.depth++
	if d == 0 {
		cache.c1 = cache.c1[:0]
		return cache.c1, d
	} else {
		cache.c2 = cache.c2[:0]
		return cache.c2, d
	}
}

// GetFreeCostCache returns a reusable costed-neighbor slice and nesting depth.
func (cache *NeighboursCache) GetFreeCostCache() ([]grid.CostedNeighbor, int) {
	d := cache.depth
	cache.depth++
	if d == 0 {
		cache.e1 = cache.e1[:0]
		return cache.e1, d
	}
	cache.e2 = cache.e2[:0]
	return cache.e2, d
}

// GetFreeChangeCostCache returns a reusable changed-edge slice and nesting depth.
func (cache *NeighboursCache) GetFreeChangeCostCache() ([]grid.CostedNeighborChange, int) {
	d := cache.depth
	cache.depth++
	if d == 0 {
		cache.ch1 = cache.ch1[:0]
		return cache.ch1, d
	}
	cache.ch2 = cache.ch2[:0]
	return cache.ch2, d
}

// Release restores the cache nesting depth returned by GetFreeCache.
func (cache *NeighboursCache) Release(d int) {
	cache.depth = d
}

// Reset clears cached neighbor slices and nesting state.
func (cache *NeighboursCache) Reset() {
	if cache == nil {
		return
	}
	cache.depth = 0
	cache.c1 = cache.c1[:0]
	cache.c2 = cache.c2[:0]
	cache.e1 = cache.e1[:0]
	cache.e2 = cache.e2[:0]
	cache.ch1 = cache.ch1[:0]
	cache.ch2 = cache.ch2[:0]
}

// NewNeighboursCache allocates a cache for nested neighbor scans.
func NewNeighboursCache() *NeighboursCache {
	return &NeighboursCache{
		depth: 0,
		c1:    make([]grid.GlobalCoords, 0, 10),
		c2:    make([]grid.GlobalCoords, 0, 10),
		e1:    make([]grid.CostedNeighbor, 0, 10),
		e2:    make([]grid.CostedNeighbor, 0, 10),
		ch1:   make([]grid.CostedNeighborChange, 0, 10),
		ch2:   make([]grid.CostedNeighborChange, 0, 10),
	}
}

// DStarLiteCore holds the shared mutable state used by the agent and emergency
// D* Lite variants. Concrete algorithms own the g/rhs storage and use Changed
// to replay grid updates into their local consistency rules.
type DStarLiteCore struct {
	Queue *PrioQueue[grid.GlobalIdx, Key]
	World *grid.World

	Changed         map[grid.GlobalCoords]grid.Cost
	ChangedMu       sync.RWMutex
	Goal            grid.GlobalIdx
	QuitWorldSub    *grid.Subscription
	NeighboursCache *NeighboursCache
}

// Reset clears mutable planner state while retaining reusable allocations.
func (d *DStarLiteCore) Reset() {
	d.ChangedMu.Lock()
	defer d.ChangedMu.Unlock()

	if d.Queue != nil {
		d.Queue.Clear()
	}
	d.World = nil

	for k := range d.Changed {
		delete(d.Changed, k)
	}

	d.Goal = -1
	d.QuitWorldSub = nil
	d.NeighboursCache.Reset()
}

// EmptyChangedEdges reports whether there are pending world changes to process.
func (d *DStarLiteCore) EmptyChangedEdges() bool {
	d.ChangedMu.RLock()
	defer d.ChangedMu.RUnlock()

	for range d.Changed {
		return false
	}
	return true
}

// Neighbors returns the eight same-floor grid neighbors for c. Portal movement
// is added by Succ/Pred so same-floor and cross-floor costs stay separate.
func (d *DStarLiteCore) Neighbors(c grid.GlobalCoords) ([]grid.GlobalCoords, int) {
	var cache, depth = d.NeighboursCache.GetFreeCache()

	currGrid := d.World.Floor(c.Layer)
	if currGrid == nil {
		return cache, depth
	}
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			x, y := int(c.X)+dx, int(c.Y)+dy
			if x >= 0 && x < int(currGrid.Cols) &&
				y >= 0 && y < int(currGrid.Rows) {
				cache = append(cache, grid.GlobalCoords{Coords: grid.Coords{X: uint(x), Y: uint(y)}, Layer: c.Layer})
			}
		}
	}

	return cache, depth
}

// Succ returns same-floor neighbors plus outgoing portals from c.
func (d *DStarLiteCore) Succ(c grid.GlobalCoords, cIdx grid.GlobalIdx) ([]grid.GlobalCoords, int) {
	ret, depth := d.Neighbors(c)
	for _, portal := range d.World.PortalsFrom(cIdx) {
		ret = append(ret, portal.To)
	}
	return ret, depth
}

// Pred returns same-floor neighbors plus portals that enter c.
func (d *DStarLiteCore) Pred(c grid.GlobalCoords, cIdx grid.GlobalIdx) ([]grid.GlobalCoords, int) {
	ret, depth := d.Neighbors(c)
	for _, portal := range d.World.PortalsTo(cIdx) {
		ret = append(ret, portal.To)
	}
	return ret, depth
}

// SuccCosts returns reachable outgoing edges from c with movement costs.
func (d *DStarLiteCore) SuccCosts(c grid.GlobalCoords, cIdx grid.GlobalIdx) ([]grid.CostedNeighbor, int) {
	ret, depth := d.NeighboursCache.GetFreeCostCache()
	ret = d.World.AppendSuccCosts(ret, c, cIdx)
	return ret, depth
}

// PredCosts returns reachable incoming edges to c with movement costs.
func (d *DStarLiteCore) PredCosts(c grid.GlobalCoords, cIdx grid.GlobalIdx) ([]grid.CostedNeighbor, int) {
	ret, depth := d.NeighboursCache.GetFreeCostCache()
	ret = d.World.AppendPredCosts(ret, c, cIdx)
	return ret, depth
}

// PredChangeCosts returns old and new incoming edge costs for a changed cell.
func (d *DStarLiteCore) PredChangeCosts(c grid.GlobalCoords, cIdx grid.GlobalIdx, oldToVal grid.Cost) ([]grid.CostedNeighborChange, int) {
	ret, depth := d.NeighboursCache.GetFreeChangeCostCache()
	ret = d.World.AppendPredChangeCosts(ret, c, cIdx, oldToVal)
	return ret, depth
}

func traversalCost(from, to grid.GlobalCoords, toVal grid.Cost) grid.Cost {
	dx := AbsDiffUint(from.X, to.X)
	dy := AbsDiffUint(from.Y, to.Y)
	if dx+dy == 2 {
		return grid.DiagonalCost(toVal)
	}
	return toVal
}

// Cost returns the current traversal cost from from to to. Same-floor moves use
// the destination cell cost; cross-floor moves are only valid through portals.
// Cost returns the movement cost from from to to, including portal costs.
func (d *DStarLiteCore) Cost(from, to grid.GlobalCoords, fromIdx grid.GlobalIdx) grid.Cost {

	if from.Layer == to.Layer {
		floor := d.World.Floor(to.Layer)
		if floor == nil {
			return grid.UNREACHABLE_COST
		}

		return floor.TraversalCost(from.Coords, to.Coords)

	} else {
		if fromIdx < 0 && !from.IsSentinel() {
			return grid.UNREACHABLE_COST
		}
		for _, p := range d.World.PortalsFrom(fromIdx) {
			if p.To == to {
				return p.Cost
			}
		}
		return grid.UNREACHABLE_COST
	}
}

// OldCost mirrors Cost using a previous destination-cell value, which lets
// replanning decide whether predecessor rhs values depended on the old edge.
// OldCost returns the previous movement cost using oldToVal for the destination.
func (d *DStarLiteCore) OldCost(oldToVal grid.Cost, from, to grid.GlobalCoords, fromIdx grid.GlobalIdx) grid.Cost {
	if from.Layer == to.Layer {
		if !d.World.Contains(from) || !d.World.Contains(to) {
			return grid.UNREACHABLE_COST
		}

		if grid.IsBlocked(oldToVal) {
			return grid.UNREACHABLE_COST
		}

		return traversalCost(from, to, oldToVal)

	} else {
		if fromIdx < 0 && !from.IsSentinel() {
			return grid.UNREACHABLE_COST
		}
		for _, p := range d.World.PortalsFrom(fromIdx) {
			if p.To == to {
				return p.Cost
			}
		}
		return grid.UNREACHABLE_COST
	}
}

// AbsDiffUint returns the absolute difference between two unsigned integers.
func AbsDiffUint(a, b uint) uint {
	if a > b {
		return a - b
	}
	return b - a
}

// TicksPerSecondToWaitDuration converts a movement/update rate into the wait
// interval used by agent movement and emergency refresh loops.
// TicksPerSecondToWaitDuration converts a tick rate into a wait duration.
func TicksPerSecondToWaitDuration(ticks float64) time.Duration {
	return time.Duration(math.Round(1_000_000/math.Abs(ticks))) * time.Microsecond
}

// ApplyRegionObjects rasterizes oriented objects into a local RegionGrid. Objects with a z
// coordinate above FreeMovementHeight are ignored; solid objects also get a lighter aura.
// ApplyRegionObjects rasterizes object bounds into region as dynamic costs.
func (d *DStarLiteCore) ApplyRegionObjects(region *grid.RegionGrid, objects []natsquery.ObjectOriented) {

	x, y := region.Origin.ToFloat64()
	cellSizeM := grid.CellSizeM()
	regionOrigin := raster.Vec2{X: x - cellSizeM/2, Y: y - cellSizeM/2}

	for _, object := range objects {

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		floorHeight, err := config.Dep.Ex.PointFloorHeight(ctx, object.Position.X, object.Position.Y, object.Position.Z)
		cancel()
		if err != nil {
			log.Errorf("Could not get PointFloorHeight for object (%s): %s", object.ID, err)
			continue
		}
		if object.Position.Z-floorHeight > cfg().FreeMovementHeight {
			continue //Not render the object because it is too high to make a difference
		}

		vCorners := []raster.Vec2{raster.Vec2(object.Bounds.TL).Sub(regionOrigin), raster.Vec2(object.Bounds.TR).Sub(regionOrigin), raster.Vec2(object.Bounds.BR).Sub(regionOrigin), raster.Vec2(object.Bounds.BL).Sub(regionOrigin)}

		//NOTE: Expand bounds to raster aura first
		centerPoint := raster.Vec2{X: 0, Y: 0}
		for _, point := range vCorners {
			centerPoint = centerPoint.Add(point)
		}
		centerPoint = centerPoint.Scale(0.25)

		ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
		objectCost, err := config.Dep.Ex.ObjectTraversalCost(ctx, object.ID, "")
		cancel()
		if err != nil {
			objectCost = grid.UNREACHABLE_COST
		}
		var objectAuraCost grid.Cost
		if grid.IsUnreachable(objectCost) {
			objectAuraCost = grid.HEAVY_OBJECT_COST
		} else {
			objectAuraCost = objectCost / 10
			if objectCost > 0 && objectAuraCost == 0 {
				objectAuraCost = 1
			}
			objectCost -= objectAuraCost
		}

		//Aura of the object
		scaleFactor := 1.40
		raster.RasterRectangle(
			vCorners[0].Sub(centerPoint).Scale(scaleFactor).Add(centerPoint),
			vCorners[1].Sub(centerPoint).Scale(scaleFactor).Add(centerPoint),
			vCorners[2].Sub(centerPoint).Scale(scaleFactor).Add(centerPoint),
			vCorners[3].Sub(centerPoint).Scale(scaleFactor).Add(centerPoint),
			objectAuraCost,
			cellSizeM,
			region,
		)

		//Normal object
		raster.RasterRectangle(
			vCorners[0],
			vCorners[1],
			vCorners[2],
			vCorners[3],
			objectCost,
			cellSizeM,
			region,
		)
	}
}
