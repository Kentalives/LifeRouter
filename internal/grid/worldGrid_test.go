package grid

import (
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Kentalives/LifeRouter/internal/config"
)

func setupVirtualWorldPoolTest(maxLinkedFloors uint) {
	cfg := defaultTestConfig()
	cfg.Pathfinding.Agent.MaxReusableWorldLinkedFloors = maxLinkedFloors
	config.Setup(cfg, nil)
	virtualWorldPool = sync.Pool{}
}

func TestWorldGrid_Virtualization(t *testing.T) {
	g := FromSlice(3, 3, []int32{
		1, 0, 1,
		1, 0, 1,
		1, 0, 1,
	})
	NewWorldFromGrids([]*Grid{g}, []string{"g1"})

	w2 := NewVirtualWorld()
	g2 := w2.Floor(0)

	g.base[0] = 5
	if g2.base[0] != 5 {
		t.Errorf("Base layer of grid not synced between real and virtual worlds")
	} else {
		t.Log("Correct sync of base layer across real and virtual worlds")
	}

	g.SetValue(Coords{X: 2, Y: 0}, 5)
	if g2.objects[2] == 5 {
		t.Errorf("Objects layer of grid IS synced between real and virtual worlds when it should NOT be")
	} else {
		t.Log("Correct non-synced of objects layer across real and virtual worlds")
	}

}

func TestWorldGrid_GlobalPortalsAreVisibleFromVirtualWorlds(t *testing.T) {
	g0 := NewFilled(2, 2, EMPTY_SPACE_COST)
	g1 := NewFilled(2, 2, EMPTY_SPACE_COST)
	w, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"})
	if err != nil {
		t.Fatal(err)
	}

	from := GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}
	to := GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 1}
	w.AddPortal(from, to, 3)

	virtual := NewVirtualWorld()
	if !hasPortalTo(virtual.PortalsFrom(from.ToIdx(virtual)), to, 3) {
		t.Fatal("global portal was not visible from virtual world")
	}
}

func TestWorldGrid_GlobalCoordsValidationAndSentinel(t *testing.T) {
	g0 := NewFilled(2, 3, EMPTY_SPACE_COST)
	g1 := NewFilled(2, 3, EMPTY_SPACE_COST)
	w, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"})
	if err != nil {
		t.Fatal(err)
	}

	valid := GlobalCoords{Coords: Coords{X: 2, Y: 1}, Layer: 0}
	if !w.Contains(valid) {
		t.Fatal("valid boundary coordinate was not contained")
	}
	if got := valid.ToIdx(w); got != 5 {
		t.Fatalf("valid boundary index = %d, want 5", got)
	}

	for _, invalid := range []GlobalCoords{
		{Coords: Coords{X: 3, Y: 0}, Layer: 0},
		{Coords: Coords{X: 0, Y: 2}, Layer: 0},
		{Coords: Coords{X: 0, Y: 0}, Layer: 2},
	} {
		if w.Contains(invalid) {
			t.Fatalf("invalid coordinate %v was contained", invalid)
		}
		if got := invalid.ToIdx(w); got != -1 {
			t.Fatalf("invalid coordinate %v index = %d, want -1", invalid, got)
		}
	}

	if !SentinelGlobalCoords.IsSentinel() {
		t.Fatal("sentinel did not identify itself")
	}
	if w.Contains(SentinelGlobalCoords) {
		t.Fatal("sentinel was reported as a real world cell")
	}
	if got := SentinelGlobalCoords.ToIdx(w); got != -1 {
		t.Fatalf("sentinel index = %d, want -1", got)
	}
	if got := GlobalIdx(-1).ToGlobalCoords(w); got != SentinelGlobalCoords {
		t.Fatalf("index -1 converted to %v, want sentinel", got)
	}
}

func TestWorldGrid_AppendSuccCostsIncludesReachableGridAndPortals(t *testing.T) {
	g0 := FromSlice(3, 3, []Cost{
		1, 2, 0,
		4, 5, 6,
		7, 8, 9,
	})
	g1 := NewFilled(3, 3, EMPTY_SPACE_COST)
	w, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"})
	if err != nil {
		t.Fatal(err)
	}

	center := GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 0}
	portalTo := GlobalCoords{Coords: Coords{X: 2, Y: 2}, Layer: 1}
	w.AddPortal(center, portalTo, 11)

	edges := w.AppendSuccCosts(nil, center, center.ToIdx(w))
	if len(edges) != 8 {
		t.Fatalf("AppendSuccCosts returned %d edges, want 8", len(edges))
	}
	if cost, ok := costedNeighborCost(edges, GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}); !ok || cost != DiagonalCost(1) {
		t.Fatalf("diagonal successor cost = %d, %v; want %d, true", cost, ok, DiagonalCost(1))
	}
	if _, ok := costedNeighborCost(edges, GlobalCoords{Coords: Coords{X: 2, Y: 0}, Layer: 0}); ok {
		t.Fatal("blocked successor was returned")
	}
	if cost, ok := costedNeighborCost(edges, portalTo); !ok || cost != 11 {
		t.Fatalf("portal successor cost = %d, %v; want 11, true", cost, ok)
	}
}

func TestWorldGrid_AppendPredCostsUsesDestinationCost(t *testing.T) {
	g := FromSlice(3, 3, []Cost{
		1, 2, 3,
		4, 5, 6,
		7, 8, 9,
	})
	w, err := NewWorldFromGrids([]*Grid{g}, []string{"0"})
	if err != nil {
		t.Fatal(err)
	}

	center := GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 0}
	edges := w.AppendPredCosts(nil, center, center.ToIdx(w))
	if len(edges) != 8 {
		t.Fatalf("AppendPredCosts returned %d edges, want 8", len(edges))
	}
	if cost, ok := costedNeighborCost(edges, GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}); !ok || cost != DiagonalCost(5) {
		t.Fatalf("diagonal predecessor cost = %d, %v; want %d, true", cost, ok, DiagonalCost(5))
	}
	if cost, ok := costedNeighborCost(edges, GlobalCoords{Coords: Coords{X: 1, Y: 0}, Layer: 0}); !ok || cost != 5 {
		t.Fatalf("orthogonal predecessor cost = %d, %v; want 5, true", cost, ok)
	}
}

func TestWorldGrid_AppendPredChangeCostsPreservesOldCostForBlockedSource(t *testing.T) {
	g := FromSlice(2, 2, []Cost{
		0, 1,
		1, 5,
	})
	w, err := NewWorldFromGrids([]*Grid{g}, []string{"0"})
	if err != nil {
		t.Fatal(err)
	}

	changed := GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 0}
	edges := w.AppendPredChangeCosts(nil, changed, changed.ToIdx(w), 3)
	source := GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}
	edge, ok := changedNeighbor(edges, source)
	if !ok {
		t.Fatal("changed edge from blocked source was not returned")
	}
	if edge.OldCost != DiagonalCost(3) {
		t.Fatalf("old changed edge cost = %d, want %d", edge.OldCost, DiagonalCost(3))
	}
	if edge.NewCost != UNREACHABLE_COST {
		t.Fatalf("new changed edge cost = %d, want %d", edge.NewCost, UNREACHABLE_COST)
	}
}

func TestWorldGrid_LocalPortalsAreOwnedByTheirWorld(t *testing.T) {
	g0 := NewFilled(2, 2, EMPTY_SPACE_COST)
	g1 := NewFilled(2, 2, EMPTY_SPACE_COST)
	w, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"})
	if err != nil {
		t.Fatal(err)
	}

	from := GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}
	to := GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 1}
	w.AddLocalPortal(from, to, 3)

	if !hasPortalTo(w.PortalsFrom(from.ToIdx(w)), to, 3) {
		t.Fatal("local portal was not visible from owning world")
	}

	virtual := NewVirtualWorld()
	if hasPortalTo(virtual.PortalsFrom(from.ToIdx(virtual)), to, 3) {
		t.Fatal("local portal from global world leaked into virtual world")
	}
}

func TestWorldGrid_InvalidPortalEndpointsDoNotAliasSentinel(t *testing.T) {
	g0 := NewFilled(2, 2, EMPTY_SPACE_COST)
	g1 := NewFilled(2, 2, EMPTY_SPACE_COST)
	w, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"})
	if err != nil {
		t.Fatal(err)
	}

	real := GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}
	invalid := GlobalCoords{Coords: Coords{X: 5, Y: 0}, Layer: 1}
	w.AddLocalPortal(invalid, real, 3)
	if got := w.PortalsFrom(-1); len(got) != 0 {
		t.Fatalf("invalid local portal was stored under sentinel index: %v", got)
	}

	w.AddLocalBidirectionalPortal(real, SentinelGlobalCoords, 1)
	if !hasPortalTo(w.PortalsFrom(real.ToIdx(w)), SentinelGlobalCoords, 1) {
		t.Fatal("real-to-sentinel local portal was not stored")
	}
	if !hasPortalTo(w.PortalsFrom(SentinelGlobalCoords.ToIdx(w)), real, 1) {
		t.Fatal("sentinel-to-real local portal was not stored")
	}
}

func TestWorldGrid_PortalsFromAndToMergeGlobalAndLocalPortals(t *testing.T) {
	g0 := NewFilled(2, 2, EMPTY_SPACE_COST)
	g1 := NewFilled(2, 2, EMPTY_SPACE_COST)
	w, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"})
	if err != nil {
		t.Fatal(err)
	}

	fromGlobal := GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}
	fromLocal := GlobalCoords{Coords: Coords{X: 1, Y: 0}, Layer: 0}
	toGlobal := GlobalCoords{Coords: Coords{X: 0, Y: 1}, Layer: 1}
	toShared := GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 1}
	w.AddPortal(fromGlobal, toGlobal, 3)
	w.AddLocalPortal(fromGlobal, toShared, 4)
	w.AddLocalPortal(fromLocal, toGlobal, 5)

	fromPortals := w.PortalsFrom(fromGlobal.ToIdx(w))
	if !hasPortalTo(fromPortals, toGlobal, 3) || !hasPortalTo(fromPortals, toShared, 4) {
		t.Fatalf("merged PortalsFrom did not include global and local portals: %#v", fromPortals)
	}

	toPortals := w.PortalsTo(toGlobal.ToIdx(w))
	if !hasPortalTo(toPortals, fromGlobal, 3) || !hasPortalTo(toPortals, fromLocal, 5) {
		t.Fatalf("merged PortalsTo did not include global and local portals: %#v", toPortals)
	}
}

func TestWorldGrid_RemoveLocalPortalRemovesIncomingPortal(t *testing.T) {
	g0 := NewFilled(2, 2, EMPTY_SPACE_COST)
	g1 := NewFilled(2, 2, EMPTY_SPACE_COST)
	w, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"})
	if err != nil {
		t.Fatal(err)
	}

	from := GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}
	to := GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 1}
	w.AddLocalPortal(from, to, 3)

	if !hasPortalTo(w.PortalsTo(to.ToIdx(w)), from, 3) {
		t.Fatal("local incoming portal was not visible before removal")
	}

	w.RemoveLocalPortal(from, to)

	if hasPortalTo(w.PortalsFrom(from.ToIdx(w)), to, 3) {
		t.Fatal("removed local portal was still visible from source")
	}
	if hasPortalTo(w.PortalsTo(to.ToIdx(w)), from, 3) {
		t.Fatal("removed local portal was still visible from destination")
	}
}

func TestWorldGrid_RemovePortalRemovesGlobalPortal(t *testing.T) {
	g0 := NewFilled(2, 2, EMPTY_SPACE_COST)
	g1 := NewFilled(2, 2, EMPTY_SPACE_COST)
	w, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"})
	if err != nil {
		t.Fatal(err)
	}

	from := GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}
	to := GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 1}
	w.AddPortal(from, to, 3)

	w.RemovePortal(from, to)

	if hasPortalTo(w.PortalsFrom(from.ToIdx(w)), to, 3) {
		t.Fatal("removed global portal was still visible from source")
	}
	if hasPortalTo(w.PortalsTo(to.ToIdx(w)), from, 3) {
		t.Fatal("removed global portal was still visible from destination")
	}
}

func TestWorldGrid_RemovePortalIsDirectionalForBidirectionalPortals(t *testing.T) {
	g0 := NewFilled(2, 2, EMPTY_SPACE_COST)
	g1 := NewFilled(2, 2, EMPTY_SPACE_COST)
	w, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"})
	if err != nil {
		t.Fatal(err)
	}

	a := GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}
	b := GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 1}
	w.AddBidirectionalPortal(a, b, 3)

	w.RemovePortal(a, b)

	if hasPortalTo(w.PortalsFrom(a.ToIdx(w)), b, 3) {
		t.Fatal("removed forward direction was still visible")
	}
	if !hasPortalTo(w.PortalsFrom(b.ToIdx(w)), a, 3) {
		t.Fatal("removing forward direction also removed reverse direction")
	}

	w.RemovePortal(b, a)

	if hasPortalTo(w.PortalsFrom(b.ToIdx(w)), a, 3) {
		t.Fatal("removed reverse direction was still visible")
	}
}

func TestWorldGrid_RemoveMissingPortalDoesNotRemoveOtherPortals(t *testing.T) {
	g0 := NewFilled(2, 2, EMPTY_SPACE_COST)
	g1 := NewFilled(2, 2, EMPTY_SPACE_COST)
	w, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"})
	if err != nil {
		t.Fatal(err)
	}

	from := GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}
	to := GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 1}
	otherTo := GlobalCoords{Coords: Coords{X: 0, Y: 1}, Layer: 1}
	missingTo := GlobalCoords{Coords: Coords{X: 1, Y: 0}, Layer: 1}
	w.AddPortal(from, to, 3)
	w.AddPortal(from, otherTo, 4)

	w.RemovePortal(from, missingTo)
	w.RemovePortal(GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 0}, to)

	portals := w.PortalsFrom(from.ToIdx(w))
	if !hasPortalTo(portals, to, 3) || !hasPortalTo(portals, otherTo, 4) {
		t.Fatalf("removing missing portals changed existing portals: %#v", portals)
	}
}

func TestWorldGrid_RemoveOneIncomingPortalKeepsOthers(t *testing.T) {
	g0 := NewFilled(2, 2, EMPTY_SPACE_COST)
	g1 := NewFilled(2, 2, EMPTY_SPACE_COST)
	w, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"})
	if err != nil {
		t.Fatal(err)
	}

	fromA := GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}
	fromB := GlobalCoords{Coords: Coords{X: 1, Y: 0}, Layer: 0}
	to := GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 1}
	w.AddPortal(fromA, to, 3)
	w.AddPortal(fromB, to, 4)

	w.RemovePortal(fromA, to)

	incoming := w.PortalsTo(to.ToIdx(w))
	if hasPortalTo(incoming, fromA, 3) {
		t.Fatal("removed incoming portal was still visible")
	}
	if !hasPortalTo(incoming, fromB, 4) {
		t.Fatalf("unrelated incoming portal was removed: %#v", incoming)
	}
}

func TestWorldGrid_VirtualWorldReuseClearsLocalPortals(t *testing.T) {
	setupVirtualWorldPoolTest(1)
	g0 := NewFilled(2, 2, EMPTY_SPACE_COST)
	g1 := NewFilled(2, 2, EMPTY_SPACE_COST)
	if _, err := NewWorldFromGrids([]*Grid{g0, g1}, []string{"0", "1"}); err != nil {
		t.Fatal(err)
	}

	from := GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}
	to := GlobalCoords{Coords: Coords{X: 1, Y: 1}, Layer: 1}
	w := NewVirtualWorld()
	w.AddLocalPortal(from, to, 3)
	if !hasPortalTo(w.PortalsFrom(from.ToIdx(w)), to, 3) {
		t.Fatal("local portal was not visible before release")
	}
	if !hasPortalTo(w.PortalsTo(to.ToIdx(w)), from, 3) {
		t.Fatal("local incoming portal was not visible before release")
	}

	ReleaseVirtualWorld(w)
	w.resetForLease(1)
	if hasPortalTo(w.PortalsFrom(from.ToIdx(w)), to, 3) {
		t.Fatal("local portal survived virtual world reuse")
	}
	if hasPortalTo(w.PortalsTo(to.ToIdx(w)), from, 3) {
		t.Fatal("local incoming portal survived virtual world reuse")
	}
	ReleaseVirtualWorld(w)
}

func TestWorldGrid_LocalSentinelPortalIsVisibleFromDestination(t *testing.T) {
	g0 := NewFilled(2, 2, EMPTY_SPACE_COST)
	if _, err := NewWorldFromGrids([]*Grid{g0}, []string{"0"}); err != nil {
		t.Fatal(err)
	}

	w := NewVirtualWorld()
	real := GlobalCoords{Coords: Coords{X: 0, Y: 0}, Layer: 0}
	w.AddLocalPortal(real, SentinelGlobalCoords, 1)

	if !hasPortalTo(w.PortalsTo(SentinelGlobalCoords.ToIdx(w)), real, 1) {
		t.Fatal("real-to-sentinel local portal was not visible from destination")
	}

	w.RemoveLocalPortal(real, SentinelGlobalCoords)
	if hasPortalTo(w.PortalsTo(SentinelGlobalCoords.ToIdx(w)), real, 1) {
		t.Fatal("removed real-to-sentinel local portal was still visible from destination")
	}
	ReleaseVirtualWorld(w)
}

func hasPortalTo(portals []Portal, to GlobalCoords, cost Cost) bool {
	for _, portal := range portals {
		if portal.To == to && portal.Cost == cost {
			return true
		}
	}
	return false
}

func costedNeighborCost(edges []CostedNeighbor, c GlobalCoords) (Cost, bool) {
	for _, edge := range edges {
		if edge.C == c {
			return edge.Cost, true
		}
	}
	return 0, false
}

func changedNeighbor(edges []CostedNeighborChange, c GlobalCoords) (CostedNeighborChange, bool) {
	for _, edge := range edges {
		if edge.C == c {
			return edge, true
		}
	}
	return CostedNeighborChange{}, false
}

func TestWorldGrid_ChangesCallbackForDiscoveredFloors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := FromSlice(5, 5, []int32{
			1, 1, 1, 1, 1,
			1, 1, 1, 1, 1,
			1, 1, 1, 1, 1,
			1, 1, 1, 1, 1,
			1, 1, 1, 1, 1,
		})
		g2 := FromSlice(5, 5, []int32{
			1, 1, 1, 1, 1,
			1, 1, 1, 1, 1,
			1, 1, 1, 1, 1,
			1, 1, 1, 1, 1,
			1, 1, 1, 1, 1,
		})
		NewWorldFromGrids([]*Grid{g, g2}, []string{"g1", "g2"})

		w2 := NewVirtualWorld()
		f1 := w2.Floor(0)
		testNotifyChan := make(chan []ChangedGlobalCell)
		quit := w2.SubChanges(func(cells []ChangedGlobalCell) {
			t.Log("RECEIVED CHANGES:")
			for _, cc := range cells {
				t.Logf("Changed: %#v\n", cc)
			}
			snapshot := append([]ChangedGlobalCell(nil), cells...)

			select {
			case <-time.After(1 * time.Second):
				t.Error("Change callback received but no one waiting for it")
			case testNotifyChan <- snapshot:

			}
		})

		r := &RegionGrid{
			Origin: Coords{X: 0, Y: 0},
			Rows:   2,
			Cols:   2,
			Cells: []int32{
				2, 2,
				2, 2,
			},
		}
		go func() {
			//Floor 0 changes
			select {
			case v := <-testNotifyChan:
				if len(v) != 4 {
					t.Errorf("Expected 4 changed cells, got %d", len(v))
				}
				expectedCells := []GlobalCoords{
					{Coords: Coords{X: 0, Y: 0}, Layer: 0},
					{Coords: Coords{X: 0, Y: 1}, Layer: 0},
					{Coords: Coords{X: 1, Y: 0}, Layer: 0},
					{Coords: Coords{X: 1, Y: 1}, Layer: 0},
				}
				for _, chGlobalCell := range v {
					found := false
					for _, eC := range expectedCells {
						if chGlobalCell.C == eC {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Changes received from FLOOR 0 were not the expected!\nExpected:\n\t%#v\nGot:\n\t%#v\n", expectedCells, v)
					}
				}
			case <-time.After(1 * time.Second):
				t.Error("Not received changes from FLOOR 0")
			}

			//Floor 1 changes
			select {
			case v := <-testNotifyChan:
				if len(v) != 2 {
					t.Errorf("Expected 2 changed cells, got %d", len(v))
				}
				expectedCells := []GlobalCoords{
					{Coords: Coords{X: 4, Y: 3}, Layer: 1},
					{Coords: Coords{X: 3, Y: 4}, Layer: 1},
				}
				for _, chGlobalCell := range v {
					found := false
					for _, eC := range expectedCells {
						if chGlobalCell.C == eC {
							found = true
						}
					}
					if !found {
						t.Errorf("Changes received from FLOOR 1 were not the expected!\nExpected:\n\t%#v\nGot:\n\t%#v\n", expectedCells, v)
					}
				}
			case <-time.After(1 * time.Second):
				t.Error("Not received changes from the FLOOR 1")
			}
		}()

		//time.Sleep(1 * time.Nanosecond)

		f1.ReplaceObjectRegion(r)

		time.Sleep(1 * time.Millisecond)

		f2 := w2.Floor(1)
		r.Origin = Coords{X: 3, Y: 3}
		r.Cells[0] = 0
		r.Cells[3] = 0
		f2.ReplaceObjectRegion(r)

		time.Sleep(2 * time.Second)
		quit.Close()
	})
}

func TestVirtualWorldReuseRestoresLinkedFloorLazily(t *testing.T) {
	setupVirtualWorldPoolTest(2)
	g := NewFilled(3, 3, EMPTY_SPACE_COST)
	if _, err := NewWorldFromGrids([]*Grid{g}, []string{"g"}); err != nil {
		t.Fatal(err)
	}

	w := NewVirtualWorld()
	floor := w.Floor(0)
	cell := Coords{X: 1, Y: 1}
	idx := cell.Y*floor.Cols + cell.X
	floor.SetValue(cell, 5)
	objectCapacity := cap(floor.objects)
	finalCapacity := cap(floor.final)

	ReleaseVirtualWorld(w)
	virtualWorldPool = sync.Pool{}
	w.resetForLease(2)
	if w.floors[0] != floor || !w.floorDirty[0] || floor.objects[idx] != 5 {
		t.Fatal("pooled floor was reset before lazy access")
	}

	g.mu.Lock()
	g.base[idx] = 7
	g.final[idx] = 7
	g.mu.Unlock()

	reusedFloor := w.Floor(0)
	if reusedFloor != floor {
		t.Fatal("pooled world did not retain its linked floor")
	}
	if reusedFloor.objects[idx] != 0 || reusedFloor.final[idx] != 7 {
		t.Fatalf("restored cell = object %d final %d, want object 0 final 7", reusedFloor.objects[idx], reusedFloor.final[idx])
	}
	if cap(reusedFloor.objects) != objectCapacity || cap(reusedFloor.final) != finalCapacity {
		t.Fatal("linked-floor restoration discarded reusable buffers")
	}
	ReleaseVirtualWorld(w)
}

func TestVirtualWorldPoolRespectsLinkedFloorLimit(t *testing.T) {
	setupVirtualWorldPoolTest(2)
	grids := []*Grid{NewFilled(2, 2, 1), NewFilled(2, 2, 1), NewFilled(2, 2, 1)}
	if _, err := NewWorldFromGrids(grids, []string{"0", "1", "2"}); err != nil {
		t.Fatal(err)
	}

	w := NewVirtualWorld()
	w.Floor(0)
	w.Floor(1)
	ReleaseVirtualWorld(w)
	if !w.canReuse(2) {
		t.Fatal("world with two linked floors was not eligible for pooling")
	}

	virtualWorldPool = sync.Pool{}
	w = NewVirtualWorld()
	w.Floor(0)
	w.Floor(1)
	w.Floor(2)
	ReleaseVirtualWorld(w)
	if w.canReuse(3) {
		t.Fatal("world exceeding linked-floor limit was eligible for pooling")
	}
}

func TestVirtualWorldPoolCanBeDisabled(t *testing.T) {
	setupVirtualWorldPoolTest(0)
	if _, err := NewWorldFromGrids([]*Grid{NewFilled(2, 2, 1)}, []string{"0"}); err != nil {
		t.Fatal(err)
	}

	w := NewVirtualWorld()
	w.Floor(0)
	ReleaseVirtualWorld(w)
	if w.canReuse(1) {
		t.Fatal("disabled virtual-world pool marked a world reusable")
	}
}

func TestGridSubscriptionCloseWaitsAndIsIdempotent(t *testing.T) {
	g := NewFilled(2, 2, 1)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	sub := g.SubChanges(func([]ChangedCell) {
		calls.Add(1)
		close(entered)
		<-release
	})

	setDone := make(chan struct{})
	go func() {
		g.SetValue(Coords{}, 2)
		close(setDone)
	}()
	<-entered

	closeDone := make(chan struct{})
	go func() {
		sub.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("subscription close returned while callback was still running")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	<-setDone
	<-closeDone
	sub.Close()
	g.SetValue(Coords{X: 1}, 2)
	if calls.Load() != 1 {
		t.Fatalf("callback calls after close = %d, want 1", calls.Load())
	}
}

func TestReleasedVirtualWorldReceivesNoStaleCallbacks(t *testing.T) {
	setupVirtualWorldPoolTest(1)
	if _, err := NewWorldFromGrids([]*Grid{NewFilled(2, 2, 1)}, []string{"0"}); err != nil {
		t.Fatal(err)
	}

	w := NewVirtualWorld()
	floor := w.Floor(0)
	var calls atomic.Int32
	sub := w.SubChanges(func([]ChangedGlobalCell) {
		calls.Add(1)
	})
	floor.SetValue(Coords{}, 2)
	if calls.Load() != 1 {
		t.Fatalf("callback calls before release = %d, want 1", calls.Load())
	}

	ReleaseVirtualWorld(w)
	floor.SetValue(Coords{X: 1}, 2)
	sub.Close()
	if calls.Load() != 1 {
		t.Fatalf("callback calls after release = %d, want 1", calls.Load())
	}
}

func TestVirtualWorldReuseHandlesChangedGlobalFloorDimensions(t *testing.T) {
	setupVirtualWorldPoolTest(1)
	if _, err := NewWorldFromGrids([]*Grid{NewFilled(2, 2, 1)}, []string{"0"}); err != nil {
		t.Fatal(err)
	}

	w := NewVirtualWorld()
	floor := w.Floor(0)
	ReleaseVirtualWorld(w)
	virtualWorldPool = sync.Pool{}

	if _, err := NewWorldFromGrids([]*Grid{NewFilled(3, 4, 2)}, []string{"0"}); err != nil {
		t.Fatal(err)
	}
	w.resetForLease(1)
	reusedFloor := w.Floor(0)
	if reusedFloor != floor {
		t.Fatal("dimension change discarded the retained linked grid")
	}
	if reusedFloor.Rows != 3 || reusedFloor.Cols != 4 || len(reusedFloor.objects) != 12 || len(reusedFloor.final) != 12 {
		t.Fatalf("restored dimensions = %dx%d objects=%d final=%d, want 3x4 objects=12 final=12", reusedFloor.Rows, reusedFloor.Cols, len(reusedFloor.objects), len(reusedFloor.final))
	}
	for _, value := range reusedFloor.final {
		if value != 2 {
			t.Fatalf("restored final value = %d, want 2", value)
		}
	}
	ReleaseVirtualWorld(w)
}
