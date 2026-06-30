package raster

import (
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"math"
	"os"

	"github.com/Kentalives/LifeRouter/internal/domain"
	"github.com/Kentalives/LifeRouter/internal/log"
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

// Vec2 is the 2D vector type used by raster geometry.
type Vec2 = domain.Vec2

// Cost is the traversal-cost type written by rasterizers.
type Cost = pubdomain.Cost

func rasterRange(start, end float64, limit uint) (int, int) {
	startIdx := int(math.Ceil(start - 0.5))
	endIdx := int(math.Ceil(end - 0.5))
	limitIdx := int(limit)

	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx > limitIdx {
		endIdx = limitIdx
	}

	return startIdx, endIdx
}

func rasterFlatTopTriangle(pv0, pv1, pv2 Vec2, cost Cost, r domain.GridData) {
	//calculate slope in screen space
	rRows, rCols := r.Dimensions()
	rCells := r.CellData()

	//tot := 0

	m0 := ((pv2.X - pv0.X) / (pv2.Y - pv0.Y)) // could consider float32, but math.Ceil forces float64
	m1 := ((pv2.X - pv1.X) / (pv2.Y - pv1.Y))

	yStart, yEnd := rasterRange(pv0.Y, pv2.Y, rRows)

	for y := yStart; y < yEnd; y++ {
		//calculate start and end points (x-coords)
		// add 0.5 to y value because we're calculating based on pixel CENTERS (d3d10) (https://learn.microsoft.com/es-es/windows/win32/direct3d10/d3d10-graphics-programming-guide-resources-coordinates)
		yOffset := float64(y) + 0.5 - pv0.Y //NOTE: Can be reused for both 'px' because the Y value for pv0 and pv1 are the same by definition of "flat top triangle"
		px0 := m0*yOffset + pv0.X
		px1 := m1*yOffset + pv1.X

		xStart, xEnd := rasterRange(px0, px1, rCols)

		for x := xStart; x < xEnd; x++ {
			//tot++
			rCells[uint(y)*rCols+uint(x)] += cost
		}
	}
	//return tot
}

func rasterFlatBottomTriangle(pv0, pv1, pv2 Vec2, cost Cost, r domain.GridData) {
	//calculate slope in screen space
	rRows, rCols := r.Dimensions()
	rCells := r.CellData()

	//tot := 0

	m0 := (pv1.X - pv0.X) / (pv1.Y - pv0.Y)
	m1 := (pv2.X - pv0.X) / (pv2.Y - pv0.Y)

	yStart, yEnd := rasterRange(pv0.Y, pv2.Y, rRows)

	for y := yStart; y < yEnd; y++ {
		//calculate start and end points (x-coords)
		// add 0.5 to y value because we're calculating based on pixel CENTERS (d3d10) (https://learn.microsoft.com/es-es/windows/win32/direct3d10/d3d10-graphics-programming-guide-resources-coordinates)
		yOffset := float64(y) + 0.5 - pv0.Y
		px0 := m0*yOffset + pv0.X
		px1 := m1*yOffset + pv0.X

		xStart, xEnd := rasterRange(px0, px1, rCols)

		for x := xStart; x < xEnd; x++ {
			//tot++
			rCells[uint(y)*rCols+uint(x)] += cost
		}
	}
	//return tot
}

// RasterTriangle adds cost to every grid cell covered by the triangle using
// cell-center sampling and clipping to the target region bounds.
func RasterTriangle(t *Triangle, cost Cost, r domain.GridData) {
	//pointers so we can swap
	pv0, pv1, pv2 := &t.A, &t.B, &t.C

	//tot := 0

	//sorting vertices by y
	//pv0 is top vertex, pv2 is bottom vertex
	if pv1.Y < pv0.Y {
		*pv0, *pv1 = *pv1, *pv0
	}
	if pv2.Y < pv1.Y {
		*pv2, *pv1 = *pv1, *pv2
	}
	if pv1.Y < pv0.Y {
		*pv0, *pv1 = *pv1, *pv0
	}

	if pv0.Y == pv1.Y { //natural flat top // TODO revise: barely will be possible with floating point? maybe not worth checking? or needed cuz otherwise will yield collinear triangle
		//sorting top vertices by x
		if pv1.X < pv0.X {
			*pv0, *pv1 = *pv1, *pv0
		}
		rasterFlatTopTriangle(*pv0, *pv1, *pv2, cost, r)
	} else if pv1.Y == pv2.Y { //natural flat bottom // TODO revise same considerations as before
		//sorting bottom vertices by x
		if pv2.X < pv1.X {
			*pv1, *pv2 = *pv2, *pv1
		}
		rasterFlatBottomTriangle(*pv0, *pv1, *pv2, cost, r)
	} else { // general triangle
		//find splitting vertex
		alphaSplit := (pv1.Y - pv0.Y) / (pv2.Y - pv0.Y)
		vi := pv0.Add(pv2.Sub(*pv0).Scale(alphaSplit))

		if pv1.X < vi.X { // major right
			rasterFlatBottomTriangle(*pv0, *pv1, vi, cost, r)
			rasterFlatTopTriangle(*pv1, vi, *pv2, cost, r)
		} else // major left
		{
			rasterFlatBottomTriangle(*pv0, vi, *pv1, cost, r)
			rasterFlatTopTriangle(vi, *pv1, *pv2, cost, r)
		}

	}
	//return tot
}

// RasterRectangle maps world-space rectangle corners to grid cells and draws
// them as two triangles.
func RasterRectangle(pv0, pv1, pv2, pv3 Vec2, cost Cost, canvasScale float64, r domain.GridData) {
	t0 := Vec2{X: pv0.X / canvasScale, Y: pv0.Y / canvasScale}
	t1 := Vec2{X: pv1.X / canvasScale, Y: pv1.Y / canvasScale}
	t2 := Vec2{X: pv2.X / canvasScale, Y: pv2.Y / canvasScale}
	t3 := Vec2{X: pv3.X / canvasScale, Y: pv3.Y / canvasScale}
	RasterTriangle(&Triangle{A: t0, B: t1, C: t2}, cost, r)
	RasterTriangle(&Triangle{A: t0, B: t2, C: t3}, cost, r)
}

// RasterLine draws a thick line by expanding it to a rectangle before rasterizing.
func RasterLine(p0, p1 Vec2, width float64, cost Cost, scale float64, r domain.GridData) {

	dx := p1.X - p0.X
	dy := p1.Y - p0.Y
	length := math.Sqrt(dx*dx + dy*dy)
	if length == 0 {
		return
	}

	halfW := width * 0.5
	perpX := (-dy / length) * halfW
	perpY := (dx / length) * halfW

	tl := Vec2{X: p0.X + perpX, Y: p0.Y + perpY}
	tr := Vec2{X: p0.X - perpX, Y: p0.Y - perpY}
	bl := Vec2{X: p1.X + perpX, Y: p1.Y + perpY}
	br := Vec2{X: p1.X - perpX, Y: p1.Y - perpY}

	RasterRectangle(tl, bl, br, tr, cost, scale, r)
}

// RasterPNG rasterizes a PNG image onto a RegionGrid.
//
// pixelsPerCell is the number of source-image pixels that correspond to one
// grid cell side. The function box-samples every pixel whose centre falls
// inside each cell's footprint and writes the normalised redness [0,255]
// into the matching cell.
//
// Pixel (px, py) belongs to cell (cx, cy) when:
//
//	cx = floor(px / pixelsPerCell)
//	cy = floor(py / pixelsPerCell)
//
// Only cells that fall within both the image bounds and the grid bounds are
// written; out-of-range areas are silently skipped.
func RasterPNG(path string, pixelsPerCell float64, r domain.GridData) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	rRows, rCols := r.Dimensions()
	rCells := r.CellData()

	bounds := img.Bounds()

	// Accumulator and sample-count grids — allocate lazily as slices so we
	// don't touch the RegionGrid until we have a final value per cell.
	total := make([]float64, rRows*rCols)
	count := make([]int, rRows*rCols)
	zeroed := make([]bool, rRows*rCols)

	for py := bounds.Min.Y; py < bounds.Max.Y; py++ {
		// Pixel centre in image space.
		pyCentre := float64(py) + 0.5

		cy := int(math.Floor(pyCentre / pixelsPerCell))
		if cy < 0 || cy >= int(rRows) {
			continue
		}

		for px := bounds.Min.X; px < bounds.Max.X; px++ {
			pxCentre := float64(px) + 0.5

			cx := int(math.Floor(pxCentre / pixelsPerCell))
			if cx < 0 || cx >= int(rCols) {
				continue
			}

			idx := cy*int(rCols) + cx
			if zeroed[idx] {
				continue // cell is already condemned — no point accumulating
			}
			_, _, _, a := img.At(px, py).RGBA()
			if a == 0 {
				zeroed[idx] = true
				total[idx] = 0
				continue
			}
			total[idx] += redness(img.At(px, py))
			count[idx]++
		}
	}

	for i, n := range count {
		if n > 0 {
			if zeroed[i] {
				rCells[i] = 2
			} else if aggregate := total[i] / float64(n); aggregate > 128 {
				rCells[i] = 1
			}
		}
	}

	return nil
}

// redness returns the red channel value in a [0, 255] range
func redness(c color.Color) float64 {
	r, _, _, _ := c.RGBA() // returns values in [0, 65535]
	// Normalise to [0, 255]
	rf := float64(r) / 257.0
	return rf
}

// RasterPNG2 rasterizes a PNG and applies a distance-based border penalty.
func RasterPNG2(path string, pixelsPerCell float64, r domain.GridData, penaltyRadius int, floorThreshold Cost) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	rRows, rCols := r.Dimensions()
	rCells := r.CellData()

	bounds := img.Bounds()

	// Accumulator and sample-count grids — allocate lazily as slices so we
	// don't touch the RegionGrid until we have a final value per cell.
	floorCount := make([]int, rRows*rCols)
	count := make([]int, rRows*rCols)

	for py := bounds.Min.Y; py < bounds.Max.Y; py++ {
		// Pixel centre in image space.
		pyCentre := float64(py) + 0.5

		cy := int(math.Floor(pyCentre / pixelsPerCell))
		if cy < 0 || cy >= int(rRows) {
			continue
		}

		for px := bounds.Min.X; px < bounds.Max.X; px++ {
			pxCentre := float64(px) + 0.5

			cx := int(math.Floor(pxCentre / pixelsPerCell))
			if cx < 0 || cx >= int(rCols) {
				continue
			}

			idx := cy*int(rCols) + cx

			if redness(img.At(px, py)) > 128 {
				floorCount[idx]++
			}
			count[idx]++
		}
	}

	// Write averaged values into the grid
	for i, n := range count {
		if n > 0 && floorCount[i] > 0 {
			rCells[i] = Cost((2*n - floorCount[i] + n/2) / n)
		}
	}

	ApplyBorderPenalty(r, penaltyRadius, floorThreshold)

	return nil
}

// ApplyBorderPenalty increases traversable cell costs near wall cells.
func ApplyBorderPenalty(r domain.GridData, penaltyRadius int, floorThreshold Cost) {
	rRows, rCols := r.Dimensions()
	rCells := r.CellData()

	dist := make([]int, rRows*rCols)
	// initialize: floor cells get max distance, walls get 0
	for i, v := range rCells {
		if v <= floorThreshold {
			dist[i] = penaltyRadius + 1 // "far from wall"
		} else {
			dist[i] = 0
		}
	}

	// Forward pass (top-left to bottom-right)
	for y := 0; y < int(rRows); y++ {
		for x := 0; x < int(rCols); x++ {
			idx := y*int(rCols) + x
			if dist[idx] == 0 {
				continue
			}
			if x > 0 {
				dist[idx] = min(dist[idx], dist[idx-1]+1)
			}
			if y > 0 {
				dist[idx] = min(dist[idx], dist[idx-int(rCols)]+1)
			}
		}
	}
	// Backward pass (bottom-right to top-left)
	for y := int(rRows) - 1; y >= 0; y-- {
		for x := int(rCols) - 1; x >= 0; x-- {
			idx := y*int(rCols) + x
			if dist[idx] == 0 {
				continue
			}
			if x < int(rCols)-1 {
				dist[idx] = min(dist[idx], dist[idx+1]+1)
			}
			if y < int(rRows)-1 {
				dist[idx] = min(dist[idx], dist[idx+int(rCols)]+1)
			}
		}
	}

	// Apply falloff to floor cells within penaltyRadius of a wall
	for i, d := range dist {
		if d > 0 && d <= penaltyRadius {
			rCells[i] = rCells[i] * Cost(2*penaltyRadius-d) / Cost(penaltyRadius)
		}
	}
}

// applyWallAura adds a Chebyshev-distance penalty around blocked cells without
// cascading aura from cells that were only penalized by this pass.
func applyWallAura(r domain.GridData, auraRadius int, auraCost Cost) {
	if auraRadius <= 0 || auraCost <= 0 {
		return
	}

	rRows, rCols := r.Dimensions()
	rCells := r.CellData()
	walls := make([]bool, len(rCells))
	for i, cell := range rCells {
		walls[i] = cell == 0
	}

	rows := int(rRows)
	cols := int(rCols)
	for y := range rows {
		for x := range cols {
			idx := y*cols + x
			if walls[idx] || rCells[idx] <= 0 {
				continue
			}

			if hasWallWithinChebyshev(walls, rows, cols, x, y, auraRadius) && rCells[idx] < auraCost {
				rCells[idx] = auraCost
			}
		}
	}
}

func hasWallWithinChebyshev(walls []bool, rows, cols, x, y, radius int) bool {
	minY := max(0, y-radius)
	maxY := min(rows-1, y+radius)
	minX := max(0, x-radius)
	maxX := min(cols-1, x+radius)

	for ny := minY; ny <= maxY; ny++ {
		for nx := minX; nx <= maxX; nx++ {
			if nx == x && ny == y {
				continue
			}
			if walls[ny*cols+nx] {
				return true
			}
		}
	}
	return false
}

// rasterPNG3_core interprets pixels whose red channel passes the traversability
// threshold as traversable cells, then optionally applies wall aura.
func rasterPNG3_core(img image.Image, r domain.GridData, pixelsPerCell float64, traversableCost Cost, auraRadius int, auraCost Cost) {
	rRows, rCols := r.Dimensions()
	rCells := r.CellData()

	bounds := img.Bounds()

	// Accumulator and sample-count grids — allocate lazily as slices so we
	// don't touch the RegionGrid until we have a final value per cell.
	total := make([]float64, rRows*rCols)
	count := make([]int, rRows*rCols)
	zeroed := make([]bool, rRows*rCols)

	for py := bounds.Min.Y; py < bounds.Max.Y; py++ {
		// Pixel centre in image space.
		pyCentre := float64(py) + 0.5

		cy := int(math.Floor(pyCentre / pixelsPerCell))
		if cy < 0 || cy >= int(rRows) {
			continue
		}

		for px := bounds.Min.X; px < bounds.Max.X; px++ {
			pxCentre := float64(px) + 0.5

			cx := int(math.Floor(pxCentre / pixelsPerCell))
			if cx < 0 || cx >= int(rCols) {
				continue
			}

			idx := cy*int(rCols) + cx
			if zeroed[idx] {
				continue // cell is already condemned — no point accumulating
			}
			_, _, _, a := img.At(px, py).RGBA()
			if a == 0 {
				zeroed[idx] = true
				total[idx] = 0
				continue
			}
			total[idx] += redness(img.At(px, py))
			count[idx]++
		}
	}

	for i, n := range count {
		if n > 0 {
			if zeroed[i] {
				rCells[i] = 0
			} else if aggregate := total[i] / float64(n); aggregate > 128 {
				rCells[i] = traversableCost
			}
		}
	}

	applyWallAura(r, auraRadius, auraCost)
}

// RasterPNG3 rasterizes a generic image without wall aura.
func RasterPNG3(path string, pixelsPerCell float64, r domain.GridData, traversableCost Cost) error {
	return RasterPNG3WithAura(path, pixelsPerCell, r, traversableCost, 0)
}

// RasterPNG3WithAura is the generic-image version of the map rasterizer.
func RasterPNG3WithAura(path string, pixelsPerCell float64, r domain.GridData, traversableCost Cost, auraRadius int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	rasterPNG3_core(img, r, pixelsPerCell, traversableCost, auraRadius, traversableCost*2)
	return nil
}

// rasterPNG3_v2_core is the paletted-image specialization of rasterPNG3_core.
func rasterPNG3_v2_core(palettedImg *image.Paletted, r domain.GridData, pixelsPerCell float64, traversableCost Cost, auraRadius int, auraCost Cost) {
	rRows, rCols := r.Dimensions()
	rCells := r.CellData()

	bounds := palettedImg.Bounds()

	// Accumulator and sample-count grids — allocate lazily as slices so we
	// don't touch the RegionGrid until we have a final value per cell.
	total := make([]uint16, rRows*rCols)
	count := make([]uint16, rRows*rCols)
	zeroed := make([]bool, rRows*rCols)

	//Determine which palette index is for the color RED
	var redPaletteIdx uint8 = palettedImg.Pix[palettedImg.PixOffset(0, 0)]
	if r, _, _, _ := palettedImg.Palette[redPaletteIdx].RGBA(); r == 0 {
		redPaletteIdx = (redPaletteIdx + 1) % 2
	}

	for py := bounds.Min.Y; py < bounds.Max.Y; py++ {
		// Pixel centre in image space.
		pyCentre := float64(py) + 0.5

		cy := int(math.Floor(pyCentre / pixelsPerCell))
		if cy < 0 || cy >= int(rRows) {
			continue
		}

		for px := bounds.Min.X; px < bounds.Max.X; px++ {
			pxCentre := float64(px) + 0.5

			cx := int(math.Floor(pxCentre / pixelsPerCell))
			if cx < 0 || cx >= int(rCols) {
				continue
			}

			idx := cy*int(rCols) + cx

			if zeroed[idx] {
				continue // cell is already condemned — no point accumulating
			}

			pixIdx := palettedImg.PixOffset(px, py)
			paletteIdx := palettedImg.Pix[pixIdx]
			if paletteIdx != redPaletteIdx {
				zeroed[idx] = true
				total[idx] = 0
				continue
			}
			total[idx] += 2
			count[idx]++
		}
	}

	for i, n := range count {
		if n > 0 {
			if zeroed[i] {
				rCells[i] = 0
			} else if aggregate := total[i] / n; aggregate >= 1 {
				rCells[i] = traversableCost
			}
		}
	}

	applyWallAura(r, auraRadius, auraCost)
}

// RasterPNG3_v2 rasterizes a simple paletted image without wall aura.
func RasterPNG3_v2(path string, pixelsPerCell float64, r domain.GridData, traversableCost Cost) error {
	return RasterPNG3_v2WithAura(path, pixelsPerCell, r, traversableCost, 0)
}

// RasterPNG3_v2WithAura rasterizes a simple paletted image and applies wall aura.
func RasterPNG3_v2WithAura(path string, pixelsPerCell float64, r domain.GridData, traversableCost Cost, auraRadius int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	// Type assertion to check the concrete type
	palettedImg, ok := img.(*image.Paletted)
	if !ok || len(palettedImg.Palette) > 2 {
		return fmt.Errorf("image too complex for function 'RasterPNG3_v2': not paletted image or palette with more than 2 colors\n")
	}

	rasterPNG3_v2_core(palettedImg, r, pixelsPerCell, traversableCost, auraRadius, traversableCost*2)
	return nil
}

// RasterPNG4 rasterizes a PNG using the production selector without wall aura.
func RasterPNG4(path string, pixelsPerCell float64, r domain.GridData, traversableCost Cost) error {
	return RasterPNG4WithAura(path, pixelsPerCell, r, traversableCost, 0)
}

// RasterPNG4WithAura is the production map-image rasterizer used by grid.FromImg.
// It selects the paletted fast path when possible and falls back to generic PNG handling.
func RasterPNG4WithAura(path string, pixelsPerCell float64, r domain.GridData, traversableCost Cost, auraRadius int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	// Type assertion to check the concrete type
	palettedImg, ok := img.(*image.Paletted)
	if ok && len(palettedImg.Palette) <= 2 {
		rasterPNG3_v2_core(palettedImg, r, pixelsPerCell, traversableCost, auraRadius, traversableCost*2)

	} else {
		log.Warn("[RASTER] Image too complex for function 'RasterPNG3_v2': not paletted image or palette with more than 2 colors -> Fallback to less efficient function")
		rasterPNG3_core(img, r, pixelsPerCell, traversableCost, auraRadius, traversableCost*2)
	}

	return nil
}
