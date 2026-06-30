package tuning

import (
	"fmt"
	"strings"

	"github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/grid"
	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
)

// RasterizedGrid is a printable snapshot of an image-derived traversability grid.
type RasterizedGrid struct {
	Rows, Cols uint
	Cells      []grid.Cost
}

// String renders the rasterized grid with ANSI-colored cost markers.
func (g RasterizedGrid) String() string {
	var sb strings.Builder
	sb.WriteString("BASE:\n    ")
	for n := range g.Cols {
		sb.WriteString(fmt.Sprintf("%4d", n))
	}
	sb.WriteRune('\n')
	for i := uint(0); i < g.Rows; i++ {
		sb.WriteString(fmt.Sprintf("%4d", i))

		for j := uint(0); j < g.Cols; j++ {

			finalV := g.Cells[i*g.Cols+j]

			if grid.IsBlocked(finalV) {
				sb.WriteString(" \033[0;31m■  \033[0m")
				continue
			} else if finalV == grid.EMPTY_SPACE_COST {
				sb.WriteString(" □  ")
				continue
			} else if finalV < grid.EMPTY_SPACE_COST {
				sb.WriteString(" \033[0;32m□  \033[0m")
				continue
			}
			sb.WriteString(fmt.Sprintf("%3d \033[0m", finalV))
		}
		sb.WriteRune('\n')
	}

	return sb.String()
}

// TraversableImageRender rasterizes a map image using the same grid path as the
// service. It is intended for tuning and inspection, not for running pathfinding.
func TraversableImageRender(imgPath string, rows, cols uint, pxPerCell float64, wallAuraCells int) (RasterizedGrid, error) {
	cfg := &pubconfig.Config{
		Grid: pubconfig.GridConfig{
			CellSizeM:     0.2,
			WallAuraCells: wallAuraCells,
		},
		Pathfinding: pubconfig.PathfindingConfig{
			FreeMovementHeight: 2,
			Emergency: pubconfig.EmergencyConfig{
				LightingHystheresis:    10,
				PriorityPathCellsWidth: 1,
			},
			Agent: pubconfig.AgentConfig{
				VisionRadiusCells:  10,
				CellsForRealUpdate: 5,
			},
		},
	}
	config.Setup(cfg, nil)
	g, err := grid.FromImg(rows, cols, imgPath, pxPerCell)
	if err != nil {
		var none RasterizedGrid
		return none, err
	}

	resp := RasterizedGrid{Rows: rows, Cols: cols, Cells: make([]grid.Cost, 0, rows*cols)}
	for i := range rows {
		for j := range cols {
			c := grid.Coords{X: j, Y: i}
			resp.Cells = append(resp.Cells, g.GetValue(c))
		}
	}

	return resp, nil
}
