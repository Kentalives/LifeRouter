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

package grid

import (
	"fmt"
	"strings"
	"sync"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/raster"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

// EMPTY_SPACE_COST is the normal cost of traversing one free cell.
const EMPTY_SPACE_COST = pubdomain.COST_EMPTY_SPACE

// AGENT_COST is the default dynamic cost added by another agent.
const AGENT_COST = pubdomain.COST_AGENT

// LIGHT_OBJECT_COST is the default dynamic cost added by a light obstacle.
const LIGHT_OBJECT_COST = pubdomain.COST_LIGHT_OBJECT

// HEAVY_OBJECT_COST is the default dynamic cost added by a heavy obstacle.
const HEAVY_OBJECT_COST = pubdomain.COST_HEAVY_OBJECT

// UNREACHABLE_COST is the sentinel cost for unreachable cells.
const UNREACHABLE_COST = pubdomain.COST_UNREACHABLE

// Cost is the traversal-cost type used by internal grids.
type Cost = pubdomain.Cost

func cfg() pubconfig.GridConfig {
	if config.Cfg == nil {
		panic("grid: config.Cfg is nil")
	}
	cfg := config.Cfg.Grid
	if cfg.CellSizeM <= 0 {
		panic("grid: config.Grid.CellSizeM must be > 0")
	}
	if cfg.WallAuraCells < 0 {
		panic("grid: config.Grid.WallAuraCells must be >= 0")
	}
	return cfg
}

// CellSizeM returns the configured real-world size of one grid cell.
func CellSizeM() float64 {
	return cfg().CellSizeM
}

// IsUnreachable reports whether c is the unreachable sentinel or above it.
func IsUnreachable(c Cost) bool {
	return c >= UNREACHABLE_COST
}

// IsBlocked reports whether c cannot be traversed.
func IsBlocked(c Cost) bool {
	return c <= 0 || IsUnreachable(c)
}

// AddCost adds traversal costs while preserving the unreachable sentinel.
func AddCost(a, b Cost) Cost {
	if IsUnreachable(a) || IsUnreachable(b) {
		return UNREACHABLE_COST
	}
	if b > 0 && a > UNREACHABLE_COST-b {
		return UNREACHABLE_COST
	}
	return a + b
}

// DiagonalCost returns the cost of crossing a cell diagonally.
func DiagonalCost(cell Cost) Cost {
	if IsUnreachable(cell) {
		return UNREACHABLE_COST
	}
	return (cell*1414 + 999) / 1000
}

// Grid stores three cost layers: base map costs, dynamic object costs, and the
// final sum used by pathfinding. Mutations publish ChangedCell batches to subscribers.
type Grid struct {
	Rows, Cols uint
	base       []Cost
	objects    []Cost
	final      []Cost
	subs       []gridSub
	nextSubId  int
	changes    []ChangedCell
	mu         sync.RWMutex
}
type gridSub struct {
	id       int
	callback func(cells []ChangedCell)
}

// Subscription is an idempotent handle returned by grid and world change watches.
type Subscription struct {
	once    sync.Once
	closeFn func()
}

// Close unregisters the subscription. It is safe to call multiple times.
func (s *Subscription) Close() {
	if s == nil {
		return
	}
	s.once.Do(s.closeFn)
}

// ChangedCell reports a cell coordinate and the previous final value.
type ChangedCell struct {
	C       Coords
	PrevVal Cost
}

// New allocates an empty grid; callers normally fill base and final costs before use.
func newGrid(Rows, Cols uint) *Grid {

	return &Grid{
		Rows:    Rows,
		Cols:    Cols,
		base:    make([]Cost, Rows*Cols),
		objects: make([]Cost, Rows*Cols),
		final:   make([]Cost, Rows*Cols),
		changes: make([]ChangedCell, 10),
	}
}

func newLinkedGrid(g *Grid) *Grid {
	g.mu.RLock()
	defer g.mu.RUnlock()

	final := make([]Cost, g.Rows*g.Cols)
	copy(final, g.base)
	return &Grid{
		Rows:    g.Rows,
		Cols:    g.Cols,
		base:    g.base,
		objects: make([]Cost, g.Rows*g.Cols),
		final:   final,
		changes: make([]ChangedCell, 10),
	}
}

func (g *Grid) resetLinked(source *Grid) {
	source.mu.RLock()
	defer source.mu.RUnlock()

	g.mu.Lock()
	defer g.mu.Unlock()

	size := int(source.Rows * source.Cols)
	g.Rows = source.Rows
	g.Cols = source.Cols
	g.base = source.base
	if cap(g.objects) < size {
		g.objects = make([]Cost, size)
	} else {
		g.objects = g.objects[:size]
		clear(g.objects)
	}
	if cap(g.final) < size {
		g.final = make([]Cost, size)
	} else {
		g.final = g.final[:size]
	}
	copy(g.final, source.base)
	g.changes = g.changes[:0]
	g.subs = nil
}

// FromSlice builds a grid from row-major base costs. It returns nil if the size mismatches.
func FromSlice(Rows, Cols uint, cells []Cost) *Grid {

	if len(cells) != int(Rows)*int(Cols) {
		return nil
	}

	grid := newGrid(Rows, Cols)
	for i, cell := range cells {
		grid.base[i] = Cost(cell)
		grid.final[i] = Cost(cell)
	}

	return grid
}

// FromImg builds the base layer from a map image using the production PNG
// rasterizer with wall aura enabled.
func FromImg(Rows, Cols uint, imgPath string, pxPerCell float64) (*Grid, error) {
	grid := newGrid(Rows, Cols)

	r := &RegionGrid{Rows: Rows, Cols: Cols, Origin: Coords{X: 0, Y: 0}, Cells: grid.base}
	if err := raster.RasterPNG4WithAura(imgPath, pxPerCell, r, EMPTY_SPACE_COST, cfg().WallAuraCells); err != nil {
		return &Grid{}, fmt.Errorf(" creating grid: %s ", err)
	}
	copy(grid.final, grid.base)
	return grid, nil
}

// NewFilled builds a grid whose base and final layers are filled with fillCost.
func NewFilled(Rows, Cols uint, fillCost Cost) *Grid {
	g := newGrid(Rows, Cols)
	for idx := range g.base {
		g.base[idx] = fillCost
		g.final[idx] = fillCost
	}
	return g
}

// ReplaceObjectRegion replaces the dynamic object layer of g from r
// and updates final costs for all changed traversable cells.
func (g *Grid) ReplaceObjectRegion(r *RegionGrid) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.changes = g.changes[:0]

	for row := uint(0); row < r.Rows; row++ {
		gridRowIdx := (row+r.Origin.Y)*g.Cols + r.Origin.X
		regionRowIdx := row * r.Cols
		affectedRow := g.objects[gridRowIdx : gridRowIdx+r.Cols]
		for xOffset, val := range affectedRow {
			idx := gridRowIdx + uint(xOffset)
			if g.base[idx] == 0 {
				continue
			}
			if newVal := r.Cells[regionRowIdx+uint(xOffset)]; val != newVal {
				g.changes = append(g.changes, ChangedCell{C: Coords{X: r.Origin.X + uint(xOffset), Y: r.Origin.Y + row}, PrevVal: g.final[idx]})
				affectedRow[xOffset] = newVal
				g.final[idx] = AddCost(g.base[idx], newVal)
			}
		}
	}

	if len(g.changes) > 0 {
		for _, sub := range g.subs {
			sub.callback(g.changes)
		}
	}
}

// MaskBaseRegion applies a cost mask to the base layer, used for temporary
// preference paths that should affect all future final overlays. It also
// notifies subscribers of changed cells.
func (g *Grid) MaskBaseRegion(r *RegionGrid) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.changes = g.changes[:0]

	for row := uint(0); row < r.Rows; row++ {
		gridRowIdx := (row+r.Origin.Y)*g.Cols + r.Origin.X
		regionRowIdx := row * r.Cols
		affectedRow := g.base[gridRowIdx : gridRowIdx+r.Cols]
		for xOffset, val := range affectedRow {
			idx := gridRowIdx + uint(xOffset)
			if val == 0 {
				continue
			}
			if maskVal := r.Cells[regionRowIdx+uint(xOffset)]; maskVal != 0 {
				newVal := AddCost(val, maskVal)
				if newVal < 1 {
					newVal = 1
				}
				g.changes = append(g.changes, ChangedCell{C: Coords{X: r.Origin.X + uint(xOffset), Y: r.Origin.Y + row}, PrevVal: g.final[idx]})
				affectedRow[xOffset] = newVal
				g.final[idx] = AddCost(newVal, g.objects[idx])
			}
		}
	}

	if len(g.changes) > 0 {
		for _, sub := range g.subs {
			sub.callback(g.changes)
		}
	}
}

// GetValue returns the final traversal cost at c, or a blocked cost outside bounds.
func (g *Grid) GetValue(c Coords) Cost {
	if c.X >= g.Cols || c.Y >= g.Rows {
		return UNREACHABLE_COST
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.final[c.Y*g.Cols+c.X]
}

// TraversalCost returns the current same-floor movement cost from from to to.
// It reads both endpoint cells under a single grid lock.
func (g *Grid) TraversalCost(from, to Coords) Cost {
	if from.X >= g.Cols || from.Y >= g.Rows || to.X >= g.Cols || to.Y >= g.Rows {
		return UNREACHABLE_COST
	}

	fromIdx := from.Y*g.Cols + from.X
	toIdx := to.Y*g.Cols + to.X

	g.mu.RLock()
	fromVal := g.final[fromIdx]
	toVal := g.final[toIdx]
	g.mu.RUnlock()

	if IsBlocked(fromVal) || IsBlocked(toVal) {
		return UNREACHABLE_COST
	}

	var dx uint
	if from.X > to.X {
		dx = from.X - to.X
	} else {
		dx = to.X - from.X
	}
	var dy uint
	if from.Y > to.Y {
		dy = from.Y - to.Y
	} else {
		dy = to.Y - from.Y
	}
	if dx+dy == 2 {
		return DiagonalCost(toVal)
	}
	return toVal
}

func sameFloorTraversalCost(from, to Coords, fromVal, toVal Cost) Cost {
	if IsBlocked(fromVal) || IsBlocked(toVal) {
		return UNREACHABLE_COST
	}
	if absDiffUint(from.X, to.X)+absDiffUint(from.Y, to.Y) == 2 {
		return DiagonalCost(toVal)
	}
	return toVal
}

func oldSameFloorTraversalCost(from, to Coords, oldToVal Cost) Cost {
	if IsBlocked(oldToVal) {
		return UNREACHABLE_COST
	}
	if absDiffUint(from.X, to.X)+absDiffUint(from.Y, to.Y) == 2 {
		return DiagonalCost(oldToVal)
	}
	return oldToVal
}

func absDiffUint(a, b uint) uint {
	if a > b {
		return a - b
	}
	return b - a
}

func (g *Grid) appendSuccCosts(out []CostedNeighbor, c GlobalCoords, idxAccum uint) []CostedNeighbor {
	if c.X >= g.Cols || c.Y >= g.Rows {
		return out
	}

	g.mu.RLock()
	centerIdx := c.Y*g.Cols + c.X
	centerVal := g.final[centerIdx]
	if IsBlocked(centerVal) {
		g.mu.RUnlock()
		return out
	}

	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			x, y := int(c.X)+dx, int(c.Y)+dy
			if x < 0 || x >= int(g.Cols) || y < 0 || y >= int(g.Rows) {
				continue
			}
			to := Coords{X: uint(x), Y: uint(y)}
			toIdx := to.Y*g.Cols + to.X
			cost := sameFloorTraversalCost(c.Coords, to, centerVal, g.final[toIdx])
			if IsUnreachable(cost) {
				continue
			}
			out = append(out, CostedNeighbor{
				C:    GlobalCoords{Coords: to, Layer: c.Layer},
				Idx:  GlobalIdx(idxAccum + toIdx),
				Cost: cost,
			})
		}
	}
	g.mu.RUnlock()
	return out
}

func (g *Grid) appendPredCosts(out []CostedNeighbor, c GlobalCoords, idxAccum uint) []CostedNeighbor {
	if c.X >= g.Cols || c.Y >= g.Rows {
		return out
	}

	g.mu.RLock()
	centerIdx := c.Y*g.Cols + c.X
	centerVal := g.final[centerIdx]
	if IsBlocked(centerVal) {
		g.mu.RUnlock()
		return out
	}

	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			x, y := int(c.X)+dx, int(c.Y)+dy
			if x < 0 || x >= int(g.Cols) || y < 0 || y >= int(g.Rows) {
				continue
			}
			from := Coords{X: uint(x), Y: uint(y)}
			fromIdx := from.Y*g.Cols + from.X
			cost := sameFloorTraversalCost(from, c.Coords, g.final[fromIdx], centerVal)
			if IsUnreachable(cost) {
				continue
			}
			out = append(out, CostedNeighbor{
				C:    GlobalCoords{Coords: from, Layer: c.Layer},
				Idx:  GlobalIdx(idxAccum + fromIdx),
				Cost: cost,
			})
		}
	}
	g.mu.RUnlock()
	return out
}

func (g *Grid) appendPredChangeCosts(out []CostedNeighborChange, c GlobalCoords, idxAccum uint, oldToVal Cost) []CostedNeighborChange {
	if c.X >= g.Cols || c.Y >= g.Rows {
		return out
	}

	g.mu.RLock()
	centerIdx := c.Y*g.Cols + c.X
	centerVal := g.final[centerIdx]
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			x, y := int(c.X)+dx, int(c.Y)+dy
			if x < 0 || x >= int(g.Cols) || y < 0 || y >= int(g.Rows) {
				continue
			}
			from := Coords{X: uint(x), Y: uint(y)}
			fromIdx := from.Y*g.Cols + from.X
			oldCost := oldSameFloorTraversalCost(from, c.Coords, oldToVal)
			newCost := sameFloorTraversalCost(from, c.Coords, g.final[fromIdx], centerVal)
			if IsUnreachable(oldCost) && IsUnreachable(newCost) {
				continue
			}
			out = append(out, CostedNeighborChange{
				C:       GlobalCoords{Coords: from, Layer: c.Layer},
				Idx:     GlobalIdx(idxAccum + fromIdx),
				OldCost: oldCost,
				NewCost: newCost,
			})
		}
	}
	g.mu.RUnlock()
	return out
}

// SetValue replaces the dynamic object cost at c and notifies subscribers.
//
// It is primarily intended for tests; pathfinding vision updates can overwrite
// this value during normal agent execution.
func (g *Grid) SetValue(c Coords, v Cost) {
	g.mu.Lock()
	defer g.mu.Unlock()

	idx := c.Y*g.Cols + c.X
	if g.base[idx] == 0 {
		return
	}

	prevVal := g.final[idx]
	g.objects[idx] = v
	g.final[idx] = AddCost(g.base[idx], g.objects[idx])

	g.changes = g.changes[:0]
	g.changes = append(g.changes, ChangedCell{C: c, PrevVal: prevVal})

	for _, sub := range g.subs {
		sub.callback(g.changes) //(c, prevVal)
	}
}

// UpdateFinal recomputes final costs from base and object layers and notifies
// subscribers of changed cells.
func (g *Grid) UpdateFinal() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.changes = g.changes[:0]

	for row := range g.Rows {
		for col := range g.Cols {
			idx := row*g.Cols + col
			if g.base[idx] == 0 {
				continue
			}
			prevVal := g.final[idx]
			newVal := AddCost(g.base[idx], g.objects[idx])
			if newVal != prevVal {
				g.changes = append(g.changes, ChangedCell{C: Coords{X: col, Y: row}, PrevVal: prevVal})
				g.final[idx] = newVal
			}
		}
	}

	if len(g.changes) > 0 {
		for _, sub := range g.subs {
			sub.callback(g.changes)
		}
	}
}

// ResetBase restores every traversable base cell to traversableCost while
// preserving the current object layer. It also notifies subscribers of the
// changed cells.
func (g *Grid) ResetBase(traversableCost Cost) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.changes = g.changes[:0]

	for row := range g.Rows {
		for col := range g.Cols {
			idx := row*g.Cols + col
			if g.base[idx] == 0 {
				continue
			}
			prevVal := g.final[idx]
			newVal := AddCost(traversableCost, g.objects[idx])
			if newVal != prevVal {
				g.changes = append(g.changes, ChangedCell{C: Coords{X: col, Y: row}, PrevVal: prevVal})
				g.base[idx] = traversableCost
				g.final[idx] = newVal
			}
		}
	}

	if len(g.changes) > 0 {
		for _, sub := range g.subs {
			sub.callback(g.changes)
		}
	}
}

// SubChanges registers a synchronous callback for batches of final-cost changes.
func (g *Grid) SubChanges(callback func(cells []ChangedCell)) *Subscription {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.subs == nil {
		g.subs = make([]gridSub, 0, 1)
	}

	myId := g.nextSubId
	g.subs = append(g.subs, gridSub{id: myId, callback: callback})
	g.nextSubId++

	return &Subscription{closeFn: func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		for i, sub := range g.subs {
			if sub.id == myId {
				g.subs[i] = g.subs[len(g.subs)-1]
				g.subs = g.subs[:len(g.subs)-1]
				return
			}
		}
	}}
}

func (g *Grid) clearSubscriptions() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.subs = nil
}

// String renders the final grid costs for debugging.
func (g *Grid) String() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("BASE:\n    ")
	for n := range g.Cols {
		sb.WriteString(fmt.Sprintf("%4d", n))
	}
	sb.WriteRune('\n')
	for i := uint(0); i < g.Rows; i++ {
		sb.WriteString(fmt.Sprintf("%4d", i))

		for j := uint(0); j < g.Cols; j++ {

			finalV := g.final[i*g.Cols+j]

			if IsBlocked(finalV) {
				sb.WriteString(" \033[0;31m■  \033[0m")
				continue
			} else if finalV == EMPTY_SPACE_COST {
				sb.WriteString(" □  ")
				continue
			} else if finalV < EMPTY_SPACE_COST {
				sb.WriteString(" \033[0;32m□  \033[0m")
				continue
			}
			sb.WriteString(fmt.Sprintf("%3d \033[0m", finalV))
		}
		sb.WriteRune('\n')
	}

	return sb.String()
}
