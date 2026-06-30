package mock

import (
	"fmt"
	"strings"
)

// Grid is an in-memory GridData implementation for raster tests.
type Grid struct {
	Rows, Cols uint
	Cells      []int32
}

// Dimensions returns the grid dimensions.
func (m *Grid) Dimensions() (rows, cols uint) {
	return m.Rows, m.Cols
}

// CellData returns the row-major cell data.
func (m *Grid) CellData() []int32 {
	return m.Cells
}

// String renders the grid for debugging.
func (r *Grid) String() string {
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
