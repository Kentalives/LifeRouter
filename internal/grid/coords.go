package grid

import (
	"math"
)

// Coords identifies a grid cell by column X and row Y.
type Coords struct {
	X, Y uint
}

// ToFloat64 converts c to the real-world meter position at the cell center.
func (c Coords) ToFloat64() (x, y float64) {
	cellSizeM := CellSizeM()
	x, y = (float64(c.X)+0.5)*cellSizeM, (float64(c.Y)+0.5)*cellSizeM
	return x, y
}

// CoordsFromFloat64 converts real-world meter coordinates to the containing grid cell.
func CoordsFromFloat64(x, y float64) Coords {
	cellSizeM := CellSizeM()
	return Coords{X: uint(math.Max(0, math.Ceil(x/cellSizeM-0.5))), Y: uint(math.Max(0, math.Ceil(y/cellSizeM-0.5)))}
}
