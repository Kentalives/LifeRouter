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
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/log"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	"github.com/pkg/errors"
)

var (
	globalWorld           *World
	globalLayersTranslate []string
)

// SentinelGlobalCoords is the artificial coordinate used by emergency planning
// as a fake goal outside every real floor. Its flattened index is intentionally -1.
var SentinelGlobalCoords = GlobalCoords{Coords: Coords{X: math.MaxInt, Y: math.MaxInt}, Layer: math.MaxInt}

// LayerFromName resolves a configured floor name to the internal layer index.
func LayerFromName(layerName string) (int, error) {
	for i, gL := range globalLayersTranslate {
		if gL == layerName {
			return i, nil
		}
	}
	return -1, fmt.Errorf("No registered layer with name: %s", layerName)
}

// IdxToLayerName resolves an internal layer index back to its configured floor name.
func IdxToLayerName(i int) (string, error) {
	if i < 0 || i >= len(globalLayersTranslate) {
		return "", fmt.Errorf("No registered layer for index: %d", i)
	}
	return globalLayersTranslate[i], nil
}

type floorMetadata struct {
	idxAccum   uint
	rows, cols uint
}

type worldSub struct {
	id              int
	gridSubCallback func(floor *Grid, num int)
	floorSubs       []*Subscription
	subscription    *Subscription
}

// World groups floor grids, portal edges, and change subscriptions. Virtual
// worlds lazily link floors from globalWorld and keep local portals separate.
type World struct {
	floors           []*Grid
	globalPortals    map[GlobalIdx][]Portal
	globalPortalsTo  map[GlobalIdx][]Portal
	localPortals     map[GlobalIdx][]Portal
	localPortalsTo   map[GlobalIdx][]Portal
	metadata         []floorMetadata
	cachePortalsFrom []Portal
	cachePortalsTo   []Portal

	nextSubId               int
	worldSubs               []worldSub
	subsMu                  sync.RWMutex
	floorDirty              []bool
	virtual                 bool
	leased                  bool
	maxReusableLinkedFloors uint
}

var virtualWorldPool sync.Pool

// GlobalCoords identifies a cell plus the floor layer that contains it.
type GlobalCoords struct {
	Coords
	Layer int
}

// GlobalIdx is the flattened index of one GlobalCoords within a World.
type GlobalIdx int

// IsSentinel reports whether c is the artificial non-grid coordinate reserved
// for internal emergency planning.
func (c GlobalCoords) IsSentinel() bool {
	return c == SentinelGlobalCoords
}

// Contains reports whether c identifies a real cell inside w. The emergency
// sentinel is intentionally not a real cell.
func (w *World) Contains(c GlobalCoords) bool {
	if w == nil || c.Layer < 0 || c.Layer >= len(w.metadata) {
		return false
	}
	meta := w.metadata[c.Layer]
	return c.X < meta.cols && c.Y < meta.rows
}

// ToIdx converts c to a flattened index within w.
func (c GlobalCoords) ToIdx(w *World) GlobalIdx {
	if !w.Contains(c) {
		return -1
	}
	meta := w.metadata[c.Layer]

	return GlobalIdx(meta.idxAccum + c.Y*meta.cols + c.X)
}

// ToGlobalCoords converts id from a flattened world index to layer-qualified coordinates.
func (id GlobalIdx) ToGlobalCoords(w *World) GlobalCoords {
	if id < 0 {
		return SentinelGlobalCoords
	}
	var i int
	var meta floorMetadata
	for i = len(w.metadata) - 1; i >= 0; i-- {
		meta = w.metadata[i]
		if id >= GlobalIdx(meta.idxAccum) {
			inGridId := int(id) - int(meta.idxAccum)
			y := uint(inGridId / int(meta.cols))
			x := uint(inGridId % int(meta.cols))

			return GlobalCoords{Coords: Coords{X: x, Y: y}, Layer: i}
		}
	}

	return SentinelGlobalCoords
}

// GlobalCoordsFromFloat64 converts real-world meters to grid coordinates and
// asks geotruth which configured layer contains the z position.
func GlobalCoordsFromFloat64(x, y, z float64) (GlobalCoords, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	return GlobalCoordsFromFloat64Context(ctx, x, y, z)
}

// GlobalCoordsFromFloat64Context converts real-world meters to grid coordinates
// using the caller's context for the geotruth region lookup.
func GlobalCoordsFromFloat64Context(ctx context.Context, x, y, z float64) (GlobalCoords, error) {
	layerName, err := config.Dep.Qu.RegionFromPoint(ctx, x, y, z)
	if err != nil {
		var x GlobalCoords
		return x, errors.Wrap(err, "creating GlobalCoords")
	}
	layer, err := LayerFromName(layerName)
	if err != nil {
		var x GlobalCoords
		return x, errors.Wrap(err, "creating GlobalCoords")
	}
	return GlobalCoords{Coords: CoordsFromFloat64(x, y), Layer: layer}, nil
}

// ChangedGlobalCell wraps a floor-local ChangedCell with its layer and previous cost.
type ChangedGlobalCell struct {
	C       GlobalCoords
	PrevVal Cost
}

// Portal is a directed cross-floor edge with an explicit cost.
type Portal struct {
	To   GlobalCoords
	Cost Cost
}

// CostedNeighbor is a reachable edge from the current D* Lite vertex to C.
type CostedNeighbor struct {
	C    GlobalCoords
	Idx  GlobalIdx
	Cost Cost
}

// CostedNeighborChange reports the previous and current cost of an edge that
// enters a changed destination cell.
type CostedNeighborChange struct {
	C       GlobalCoords
	Idx     GlobalIdx
	OldCost Cost
	NewCost Cost
}

// NewWorld rasterizes configured layers and installs them as the global world.
func NewWorld(layers []pubconfig.ConfigLayer, rows, cols uint, pxPerCell float64) (*World, error) {
	floors := make([]*Grid, 0, len(layers))
	names := make([]string, 0, len(layers))

	for _, layerCfg := range layers {
		gL, err := FromImg(rows, cols, layerCfg.ImgPath, pxPerCell)
		if err != nil {
			return nil, fmt.Errorf(" NewWorld, Layer '%s': %s", layerCfg.Name, err)
		}
		floors = append(floors, gL)
		names = append(names, layerCfg.Name)
	}

	return NewWorldFromGrids(floors, names)
}

// NewWorldFromGrids installs already-built grids as the global world and
// computes the flattened-index metadata used by D* Lite.
func NewWorldFromGrids(layers []*Grid, names []string) (*World, error) {

	if len(layers) != len(names) {
		return nil, fmt.Errorf("No matching of layers and names")
	}

	metadata := make([]floorMetadata, 0, len(layers))
	globalLayersTranslate = names

	accumSize := 0
	for i := range layers {
		g := layers[i]
		metadata = append(metadata, floorMetadata{idxAccum: uint(accumSize), cols: g.Cols, rows: g.Rows})
		accumSize += int(g.Cols * g.Rows)
	}

	globalWorld = &World{
		floors:           layers,
		cachePortalsFrom: make([]Portal, 0),
		cachePortalsTo:   make([]Portal, 0),
		globalPortals:    make(map[GlobalIdx][]Portal),
		globalPortalsTo:  make(map[GlobalIdx][]Portal),
		localPortals:     make(map[GlobalIdx][]Portal),
		localPortalsTo:   make(map[GlobalIdx][]Portal),
		metadata:         metadata,
	}
	return globalWorld, nil
}

// NewVirtualWorld leases a world for one pathfinding run. Floors are linked
// lazily so agents only copy overlays for floors they actually inspect.
func NewVirtualWorld() *World {
	maxLinkedFloors := maxReusableWorldLinkedFloors()
	var w *World
	if maxLinkedFloors > 0 {
		w, _ = virtualWorldPool.Get().(*World)
	}
	if w == nil {
		w = &World{}
	}
	w.resetForLease(maxLinkedFloors)
	return w
}

func (w *World) resetForLease(maxLinkedFloors uint) {
	w.subsMu.Lock()
	defer w.subsMu.Unlock()

	numFloors := len(globalWorld.floors)
	if cap(w.floors) < numFloors {
		w.floors = make([]*Grid, numFloors)
	} else {
		if len(w.floors) > numFloors {
			clear(w.floors[numFloors:])
		}
		w.floors = w.floors[:numFloors]
	}
	if cap(w.floorDirty) < numFloors {
		w.floorDirty = make([]bool, numFloors)
	} else {
		w.floorDirty = w.floorDirty[:numFloors]
	}
	for i := range w.floors {
		w.floorDirty[i] = w.floors[i] != nil
	}
	w.globalPortals = globalWorld.globalPortals
	w.globalPortalsTo = globalWorld.globalPortalsTo
	clearPortalMap(w.localPortals)
	if w.localPortals == nil {
		w.localPortals = make(map[GlobalIdx][]Portal)
	}
	clearPortalMap(w.localPortalsTo)
	if w.localPortalsTo == nil {
		w.localPortalsTo = make(map[GlobalIdx][]Portal)
	}
	w.metadata = globalWorld.metadata
	w.cachePortalsFrom = w.cachePortalsFrom[:0]
	w.cachePortalsTo = w.cachePortalsTo[:0]
	w.worldSubs = w.worldSubs[:0]
	w.virtual = true
	w.leased = true
	w.maxReusableLinkedFloors = maxLinkedFloors
}

func maxReusableWorldLinkedFloors() uint {
	if config.Cfg == nil {
		return 0
	}
	return config.Cfg.Pathfinding.Agent.MaxReusableWorldLinkedFloors
}

// ReleaseVirtualWorld closes subscriptions and returns reusable virtual worlds
// to the pool when their linked-floor count is within the configured limit.
func ReleaseVirtualWorld(w *World) {
	if w == nil {
		return
	}

	w.subsMu.Lock()
	if !w.virtual || !w.leased {
		w.subsMu.Unlock()
		return
	}
	w.leased = false
	subscriptions := make([]*Subscription, 0, len(w.worldSubs))
	for i := range w.worldSubs {
		subscriptions = append(subscriptions, w.worldSubs[i].subscription)
	}
	w.subsMu.Unlock()

	for _, subscription := range subscriptions {
		subscription.Close()
	}

	w.subsMu.Lock()
	defer w.subsMu.Unlock()

	linkedFloors := uint(0)
	for i, floor := range w.floors {
		if floor == nil {
			continue
		}
		linkedFloors++
		floor.clearSubscriptions()
		w.floorDirty[i] = true
	}
	clearPortalMap(w.localPortals)
	clearPortalMap(w.localPortalsTo)
	w.cachePortalsFrom = w.cachePortalsFrom[:0]
	w.cachePortalsTo = w.cachePortalsTo[:0]
	w.worldSubs = w.worldSubs[:0]

	if w.canReuse(linkedFloors) {
		virtualWorldPool.Put(w)
	}
}

func (w *World) canReuse(linkedFloors uint) bool {
	return w.maxReusableLinkedFloors > 0 && linkedFloors <= w.maxReusableLinkedFloors
}

// GlobalWorld returns the shared world used by service-level emergency planning.
func GlobalWorld() *World {
	return globalWorld
}

// Size returns the total number of cells across every floor in w.
func (w *World) Size() int {
	lastFloorMeta := w.metadata[len(w.metadata)-1]
	return int(lastFloorMeta.idxAccum + lastFloorMeta.rows*lastFloorMeta.cols)
}

// Len returns the number of floors in w.
func (w *World) Len() int {

	return len(w.floors)
}

// Floor returns a floor grid. Virtual worlds create or reset linked floor grids
// on demand and attach any existing world subscriptions to the new linked floor.
func (w *World) Floor(layer int) *Grid {
	if layer < 0 || layer >= len(w.floors) {
		return nil
	}
	if w == globalWorld {
		return w.floors[layer]
	}
	if !w.virtual {
		return w.floors[layer]
	}

	w.subsMu.Lock()
	defer w.subsMu.Unlock()
	if w.virtual && !w.leased {
		return nil
	}

	myFloor := w.floors[layer]
	needsSubscriptions := w.floorDirty[layer]
	if myFloor == nil {
		myFloor = newLinkedGrid(globalWorld.Floor(layer))
		w.floors[layer] = myFloor
		needsSubscriptions = true
	} else if w.floorDirty[layer] {
		myFloor.resetLinked(globalWorld.Floor(layer))
	}
	if needsSubscriptions {
		w.floorDirty[layer] = false
		for _, sub := range w.worldSubs {
			sub.gridSubCallback(myFloor, layer)
		}
	}
	return myFloor
}

// AddPortal adds a directed global portal from one floor to another.
func (w *World) AddPortal(from, to GlobalCoords, cost Cost) {
	w.addPortal(w.globalPortals, w.globalPortalsTo, from, to, cost, true)
}

// AddBidirectionalPortal adds matching global portals in both directions.
func (w *World) AddBidirectionalPortal(a, b GlobalCoords, cost Cost) {
	w.addBidirectionalPortal(w.globalPortals, w.globalPortalsTo, a, b, cost, true)
}

// RemovePortal removes a directed global portal if present.
func (w *World) RemovePortal(from, to GlobalCoords) {
	w.removePortal(w.globalPortals, w.globalPortalsTo, from, to)
}

// AddLocalPortal adds a portal visible only inside this world lease.
func (w *World) AddLocalPortal(from, to GlobalCoords, cost Cost) {
	w.addPortal(w.localPortals, w.localPortalsTo, from, to, cost, false)
}

// AddLocalBidirectionalPortal adds matching local portals in both directions.
func (w *World) AddLocalBidirectionalPortal(a, b GlobalCoords, cost Cost) {
	w.addBidirectionalPortal(w.localPortals, w.localPortalsTo, a, b, cost, false)
}

// RemoveLocalPortal removes a directed portal visible only inside this world lease.
func (w *World) RemoveLocalPortal(from, to GlobalCoords) {
	w.removePortal(w.localPortals, w.localPortalsTo, from, to)
}

// RemoveLocalBidirectionalPortal removes both local portal directions between a and b.
func (w *World) RemoveLocalBidirectionalPortal(a, b GlobalCoords) {
	w.RemoveLocalPortal(a, b)
	w.RemoveLocalPortal(b, a)
}

func (w *World) addPortal(portals map[GlobalIdx][]Portal, portalsTo map[GlobalIdx][]Portal, from, to GlobalCoords, cost Cost, global bool) {
	if cost < 1 {
		operation := "Local"
		if global {
			operation = "Global"
		}
		log.Errorf("-Add%sPortalErr- Portal FROM: (%v); TO: (%v); had COST <1 (%d); dropped", operation, from, to, cost)
		return
	} else if from.Layer == to.Layer {
		operation := "Local"
		if global {
			operation = "Global"
		}
		log.Errorf("-Add%sPortalErr- Portal FROM: (%v); TO: (%v); are on the same layer; dropped", operation, from, to)
		return
	}
	allowSentinel := !global
	if !w.validPortalEndpoint(from, allowSentinel) || !w.validPortalEndpoint(to, allowSentinel) {
		operation := "Local"
		if global {
			operation = "Global"
		}
		log.Errorf("-Add%sPortalErr- Portal FROM: (%v); TO: (%v); has invalid endpoint; dropped", operation, from, to)
		return
	}
	w.storePortal(portals, portalsTo, from, to, cost)
}

func (w *World) addBidirectionalPortal(portals map[GlobalIdx][]Portal, portalsTo map[GlobalIdx][]Portal, a, b GlobalCoords, cost Cost, global bool) {
	if cost < 1 {
		operation := "Local"
		if global {
			operation = "Global"
		}
		log.Errorf("-Add%sBidirectionalPortalErr- Portal A: (%v); B: (%v); had COST <1 (%d); dropped", operation, a, b, cost)
		return
	} else if a.Layer == b.Layer {
		operation := "Local"
		if global {
			operation = "Global"
		}
		log.Errorf("-Add%sPortalErr- Portal FROM: (%v); TO: (%v); are on the same layer; dropped", operation, a, b)
		return
	}
	allowSentinel := !global
	if !w.validPortalEndpoint(a, allowSentinel) || !w.validPortalEndpoint(b, allowSentinel) {
		operation := "Local"
		if global {
			operation = "Global"
		}
		log.Errorf("-Add%sBidirectionalPortalErr- Portal A: (%v); B: (%v); has invalid endpoint; dropped", operation, a, b)
		return
	}
	w.storePortal(portals, portalsTo, a, b, cost)
	w.storePortal(portals, portalsTo, b, a, cost)
}

func (w *World) storePortal(portals map[GlobalIdx][]Portal, portalsTo map[GlobalIdx][]Portal, from, to GlobalCoords, cost Cost) {
	fromIdx := from.ToIdx(w)
	portals[fromIdx] = append(portals[fromIdx], Portal{To: to, Cost: cost})
	toIdx := to.ToIdx(w)
	portalsTo[toIdx] = append(portalsTo[toIdx], Portal{To: from, Cost: cost})
}

func (w *World) validPortalEndpoint(c GlobalCoords, allowSentinel bool) bool {
	if allowSentinel && c.IsSentinel() {
		return true
	}
	return w.Contains(c)
}

func (w *World) removePortal(portals map[GlobalIdx][]Portal, portalsTo map[GlobalIdx][]Portal, from, to GlobalCoords) {
	fromIdx := from.ToIdx(w)
	if portals[fromIdx] == nil {
		return
	}

	fromPortals := portals[fromIdx]
	for idx, p := range fromPortals {
		if p.To == to {
			removedCost := p.Cost
			fromPortals[idx] = fromPortals[len(fromPortals)-1]
			fromPortals = fromPortals[:len(fromPortals)-1]
			if len(fromPortals) == 0 {
				delete(portals, fromIdx)
			} else {
				portals[fromIdx] = fromPortals
			}
			removePortalTo(w, portalsTo, to, from, removedCost)
			return
		}
	}
}

func removePortalTo(w *World, portalsTo map[GlobalIdx][]Portal, to, from GlobalCoords, cost Cost) {
	toIdx := to.ToIdx(w)
	if portalsTo[toIdx] == nil {
		return
	}

	incoming := portalsTo[toIdx]
	for idx, p := range incoming {
		if p.To == from && p.Cost == cost {
			incoming[idx] = incoming[len(incoming)-1]
			incoming = incoming[:len(incoming)-1]
			if len(incoming) == 0 {
				delete(portalsTo, toIdx)
			} else {
				portalsTo[toIdx] = incoming
			}
			return
		}
	}
}

// PortalsFrom returns global and local outgoing portals for a flattened cell.
func (w *World) PortalsFrom(from GlobalIdx) []Portal {
	global := w.globalPortals[from]
	local := w.localPortals[from]
	if len(global) == 0 {
		return local
	}
	if len(local) == 0 {
		return global
	}
	w.cachePortalsFrom = w.cachePortalsFrom[:0]
	w.cachePortalsFrom = append(w.cachePortalsFrom, global...)
	w.cachePortalsFrom = append(w.cachePortalsFrom, local...)
	return w.cachePortalsFrom
}

// PortalsTo returns global and local portals whose destination is to.
func (w *World) PortalsTo(to GlobalIdx) []Portal {
	global := w.globalPortalsTo[to]
	local := w.localPortalsTo[to]
	if len(global) == 0 {
		return local
	}
	if len(local) == 0 {
		return global
	}
	w.cachePortalsTo = w.cachePortalsTo[:0]
	w.cachePortalsTo = append(w.cachePortalsTo, global...)
	w.cachePortalsTo = append(w.cachePortalsTo, local...)
	return w.cachePortalsTo
}

// AppendSuccCosts appends reachable outgoing same-floor and portal edges from c.
func (w *World) AppendSuccCosts(out []CostedNeighbor, c GlobalCoords, cIdx GlobalIdx) []CostedNeighbor {
	if c.Layer >= 0 && c.Layer < len(w.metadata) {
		if floor := w.Floor(c.Layer); floor != nil {
			out = floor.appendSuccCosts(out, c, w.metadata[c.Layer].idxAccum)
		}
	}
	for _, portal := range w.PortalsFrom(cIdx) {
		out = append(out, CostedNeighbor{C: portal.To, Idx: portal.To.ToIdx(w), Cost: portal.Cost})
	}
	return out
}

// AppendPredCosts appends reachable incoming same-floor and portal edges to c.
func (w *World) AppendPredCosts(out []CostedNeighbor, c GlobalCoords, cIdx GlobalIdx) []CostedNeighbor {
	if c.Layer >= 0 && c.Layer < len(w.metadata) {
		if floor := w.Floor(c.Layer); floor != nil {
			out = floor.appendPredCosts(out, c, w.metadata[c.Layer].idxAccum)
		}
	}
	for _, portal := range w.PortalsTo(cIdx) {
		out = append(out, CostedNeighbor{C: portal.To, Idx: portal.To.ToIdx(w), Cost: portal.Cost})
	}
	return out
}

// AppendPredChangeCosts appends previous and current incoming edge costs to c.
func (w *World) AppendPredChangeCosts(out []CostedNeighborChange, c GlobalCoords, cIdx GlobalIdx, oldToVal Cost) []CostedNeighborChange {
	if c.Layer >= 0 && c.Layer < len(w.metadata) {
		if floor := w.Floor(c.Layer); floor != nil {
			out = floor.appendPredChangeCosts(out, c, w.metadata[c.Layer].idxAccum, oldToVal)
		}
	}
	for _, portal := range w.PortalsTo(cIdx) {
		out = append(out, CostedNeighborChange{
			C:       portal.To,
			Idx:     portal.To.ToIdx(w),
			OldCost: portal.Cost,
			NewCost: portal.Cost,
		})
	}
	return out
}

func clearPortalMap(portals map[GlobalIdx][]Portal) {
	for key := range portals {
		delete(portals, key)
	}
}

// LinkedObjectRegions exposes each floor's object layer as RegionGrids for
// batch updates during emergency environment refresh.
func (w *World) LinkedObjectRegions() []*RegionGrid {
	resp := make([]*RegionGrid, 0, w.Len())

	for _, g := range w.floors {
		r := &RegionGrid{Rows: g.Rows, Cols: g.Cols, Origin: Coords{X: 0, Y: 0}, Cells: g.objects}
		resp = append(resp, r)
	}

	return resp
}

// SubChanges watches all currently linked floors and any floor linked later by
// a virtual world. Callbacks receive layer-qualified changed cells.
func (w *World) SubChanges(callback func(cells []ChangedGlobalCell)) *Subscription { //func(c GlobalCoords, prevValue float64)) chan<- bool {

	pool := &sync.Pool{
		New: func() any {
			return make([]ChangedGlobalCell, 0, 20) //TODO: Specify better the size hint
		},
	}

	gridSubCallback := func(num int) func(cells []ChangedCell) {
		return func(cells []ChangedCell) {
			obj := pool.Get()
			slice := obj.([]ChangedGlobalCell)
			slice = slice[:0]

			for _, cc := range cells {
				slice = append(slice, ChangedGlobalCell{C: GlobalCoords{Coords: cc.C, Layer: num}, PrevVal: cc.PrevVal})
			}

			callback(slice)

			pool.Put(slice)
		}
	}

	w.subsMu.Lock()
	myId := w.nextSubId
	floorsSub := worldSub{
		id: myId,
		gridSubCallback: func(floor *Grid, num int) {
			floorSub := floor.SubChanges(gridSubCallback(num))
			for i := range w.worldSubs {
				if w.worldSubs[i].id == myId {
					w.worldSubs[i].floorSubs = append(w.worldSubs[i].floorSubs, floorSub)
					return
				}
			}
		},
	}
	floorsSub.subscription = &Subscription{closeFn: func() {
		w.subsMu.Lock()
		var floorSubs []*Subscription
		for i, sub := range w.worldSubs {
			if myId == sub.id {
				floorSubs = append(floorSubs, sub.floorSubs...)
				lastIdx := len(w.worldSubs) - 1
				w.worldSubs[i] = w.worldSubs[lastIdx]
				w.worldSubs = w.worldSubs[:lastIdx]
				break
			}
		}
		w.subsMu.Unlock()

		for _, floorSub := range floorSubs {
			floorSub.Close()
		}
	}}
	w.worldSubs = append(w.worldSubs, floorsSub)
	w.nextSubId++
	for num, floor := range w.floors {
		if floor == nil || (w.virtual && w.floorDirty[num]) {
			continue
		}
		w.worldSubs[len(w.worldSubs)-1].floorSubs = append(w.worldSubs[len(w.worldSubs)-1].floorSubs, floor.SubChanges(gridSubCallback(num)))
	}
	subscription := floorsSub.subscription
	w.subsMu.Unlock()
	return subscription
}

// String renders each floor grid for debugging.
func (w *World) String() string {
	var sb strings.Builder

	sb.WriteString("\n===WORLD===\n")
	for key, grid := range w.floors {
		sb.WriteString(fmt.Sprintf("FLOOR: %v\n%s\n", key, grid))
	}

	return sb.String()
}
