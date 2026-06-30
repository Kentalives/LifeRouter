package agent

import (
	"testing"

	"github.com/Kentalives/LifeRouter/internal/grid"
)

func TestPagedValuesModes(t *testing.T) {
	for _, sparse := range []bool{false, true} {
		t.Run(map[bool]string{false: "Dense", true: "Sparse"}[sparse], func(t *testing.T) {
			var values pagedValues
			values.configure(2*valuePageSize, sparse)

			for _, index := range []grid.GlobalIdx{0, valuePageSize - 1, valuePageSize} {
				if got := values.getG(index); got != grid.UNREACHABLE_COST {
					t.Fatalf("initial g(%d) = %d, want unreachable", index, got)
				}
				values.setG(index, grid.Cost(index)+10)
				values.setRhs(index, grid.Cost(index)+20)
			}

			if values.gLen != 3 || values.rhsLen != 3 || len(values.activePages) != 2 {
				t.Fatalf("lengths = g:%d rhs:%d pages:%d, want 3, 3, 2", values.gLen, values.rhsLen, len(values.activePages))
			}
			g, rhs := values.get(valuePageSize)
			if g != valuePageSize+10 || rhs != valuePageSize+20 {
				t.Fatalf("pair = (%d, %d), want (%d, %d)", g, rhs, valuePageSize+10, valuePageSize+20)
			}

			values.setG(0, grid.UNREACHABLE_COST)
			values.setRhs(valuePageSize, grid.UNREACHABLE_COST)
			if values.hasG(0) || values.gLen != 2 || values.rhsLen != 2 {
				t.Fatalf("delete left values populated: hasG(0)=%v gLen=%d rhsLen=%d", values.hasG(0), values.gLen, values.rhsLen)
			}

			values.clear()
			if values.gLen != 0 || values.rhsLen != 0 || len(values.activePages) != 0 {
				t.Fatal("clear left state populated")
			}
			if got := values.getG(valuePageSize - 1); got != grid.UNREACHABLE_COST {
				t.Fatalf("cleared g = %d, want unreachable", got)
			}
		})
	}
}

func TestPagedValuesConfigureReusesStoreWithoutStaleValues(t *testing.T) {
	var values pagedValues
	values.configure(valuePageSize+1, false)
	values.setG(valuePageSize, 12)
	values.setRhs(0, 13)

	values.configure(valuePageSize, true)
	if got := values.getG(valuePageSize); got != grid.UNREACHABLE_COST {
		t.Fatalf("reconfigured g = %d, want unreachable", got)
	}
	if got := values.getRhs(0); got != grid.UNREACHABLE_COST {
		t.Fatalf("reconfigured rhs = %d, want unreachable", got)
	}

	values.setG(0, 14)
	if got := values.getG(0); got != 14 {
		t.Fatalf("reused g = %d, want 14", got)
	}
}
