package grid

import (
	"fmt"
	"strings"
)

// RegionGrid is a clipped rectangular view over grid costs. Origin is expressed
// in the parent grid; Cells are stored row-major relative to that origin.
type RegionGrid struct {
	Origin     Coords
	Rows, Cols uint
	Cells      []Cost
}

// Dimensions returns the region size in rows and columns.
func (r *RegionGrid) Dimensions() (rows, cols uint) {
	return r.Rows, r.Cols
}

// CellData returns the row-major cost data backing the region.
func (r *RegionGrid) CellData() []Cost {
	return r.Cells
}

// ClipToGrid sets the region bounds around pos while clamping to the parent
// grid, so callers can request an apothem near borders without out-of-range cells.
func (r *RegionGrid) ClipToGrid(g *Grid, xApothem, yApothem uint, pos Coords) {
	minX := uint(max(0, int(pos.X)-int(xApothem)))
	minY := uint(max(0, int(pos.Y)-int(yApothem)))

	maxX := min(g.Cols-1, pos.X+xApothem)
	maxY := min(g.Rows-1, pos.Y+yApothem)

	r.Origin.X = minX
	r.Origin.Y = minY

	r.Cols = uint(max(0, int(maxX)-int(minX)+1))
	r.Rows = uint(max(0, int(maxY)-int(minY)+1))
}

// String renders the region costs for debugging.
func (r *RegionGrid) String() string {
	var sb strings.Builder
	sb.WriteString("REGION:\n   ")
	for n := range r.Cols {
		sb.WriteString(fmt.Sprintf("%3d", n))
	}
	sb.WriteRune('\n')
	for i := uint(0); i < r.Rows; i++ {
		sb.WriteString(fmt.Sprintf("%3d", i))
		for j := uint(0); j < r.Cols; j++ {

			finalV := r.Cells[i*r.Cols+j]
			if finalV == 0 {
				sb.WriteString(" □ ")
				continue
			} else if finalV == 1 {
				sb.WriteString(" ■ ")
				continue
			} else if finalV > 1 {
				sb.WriteString("\033[0;34m ■ \033[0m")
				continue
			}
		}
		sb.WriteRune('\n')
	}

	return sb.String()
}
