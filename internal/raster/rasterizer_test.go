package raster

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Kentalives/LifeRouter/internal/domain"
	"github.com/Kentalives/LifeRouter/internal/log"
	"github.com/Kentalives/LifeRouter/internal/mock"
)

// ---- helpers ----

func assertFilled(t *testing.T, r domain.GridData, x, y uint) {
	t.Helper()
	_, rCols := r.Dimensions()
	rCells := r.CellData()

	if rCells[y*rCols+x] == 0 {
		t.Errorf("expected cell (%d,%d) to be filled but it's empty", x, y)
	}
}

func assertEmpty(t *testing.T, r domain.GridData, x, y uint) {
	t.Helper()
	_, rCols := r.Dimensions()
	rCells := r.CellData()

	if rCells[y*rCols+x] != 0 {
		t.Errorf("expected cell (%d,%d) to be empty but has value %d", x, y, rCells[y*rCols+x])
	}
}

func assertNoDoubleCount(t *testing.T, r domain.GridData, maxVal int32) {
	t.Helper()
	rRows, rCols := r.Dimensions()
	rCells := r.CellData()

	for y := uint(0); y < rRows; y++ {
		for x := uint(0); x < rCols; x++ {
			v := rCells[y*rCols+x]

			if v > maxVal {
				t.Errorf("double counted cell at (%d,%d): value=%d", x, y, v)
			}
		}
	}
}

func TestApplyWallAura_ChebyshevRadiusOne(t *testing.T) {
	r := &mock.Grid{
		Rows: 5,
		Cols: 5,
		Cells: []int32{
			10, 10, 10, 10, 10,
			10, 10, 10, 10, 10,
			10, 10, 0, 10, 10,
			10, 10, 10, 10, 10,
			10, 10, 10, 10, 10,
		},
	}

	applyWallAura(r, 1, 20)

	expected := []int32{
		10, 10, 10, 10, 10,
		10, 20, 20, 20, 10,
		10, 20, 0, 20, 10,
		10, 20, 20, 20, 10,
		10, 10, 10, 10, 10,
	}
	for i, want := range expected {
		if r.Cells[i] != want {
			t.Fatalf("cell %d: expected %d, got %d\n%s", i, want, r.Cells[i], r)
		}
	}
}

func TestApplyWallAura_DoesNotCascade(t *testing.T) {
	r := &mock.Grid{
		Rows:  1,
		Cols:  5,
		Cells: []int32{0, 10, 10, 10, 10},
	}

	applyWallAura(r, 1, 20)

	expected := []int32{0, 20, 10, 10, 10}
	for i, want := range expected {
		if r.Cells[i] != want {
			t.Fatalf("cell %d: expected %d, got %d", i, want, r.Cells[i])
		}
	}
}

// ---- flat top ----

func TestRasterFlatTop_Basic(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	triangle := Triangle{
		A: Vec2{X: 4, Y: 4},
		B: Vec2{X: 14, Y: 4},
		C: Vec2{X: 9, Y: 14},
	}

	RasterTriangle(&triangle, 1, r)
	t.Log(r)

	assertFilled(t, r, 9, 8)
	assertFilled(t, r, 7, 6)
	assertFilled(t, r, 11, 6)
	assertEmpty(t, r, 0, 0)
	assertEmpty(t, r, 19, 19)
	assertEmpty(t, r, 0, 19)
	assertEmpty(t, r, 19, 0)
}

func TestRasterFlatTop_Thin(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	triangle := Triangle{
		A: Vec2{X: 8, Y: 4},
		B: Vec2{X: 10, Y: 4},
		C: Vec2{X: 9, Y: 14},
	}
	RasterTriangle(&triangle, 1, r)
	t.Log(r)

	assertFilled(t, r, 9, 8)
	assertEmpty(t, r, 0, 0)
	assertEmpty(t, r, 19, 19)
}

func TestRasterFlatTop_Wide(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	triangle := Triangle{
		A: Vec2{X: 1, Y: 2},
		B: Vec2{X: 18, Y: 2},
		C: Vec2{X: 9, Y: 18},
	}
	RasterTriangle(&triangle, 1, r)
	t.Log(r)

	assertFilled(t, r, 9, 10)
	assertFilled(t, r, 5, 5)
	assertFilled(t, r, 13, 5)
	assertEmpty(t, r, 0, 0)
	assertEmpty(t, r, 19, 19)
}

// ---- flat bottom ----

func TestRasterFlatBottom_Basic(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	triangle := Triangle{
		A: Vec2{X: 9, Y: 4},
		B: Vec2{X: 4, Y: 14},
		C: Vec2{X: 14, Y: 14},
	}
	RasterTriangle(&triangle, 1, r)
	t.Log(r)

	assertFilled(t, r, 9, 9)
	assertFilled(t, r, 7, 11)
	assertFilled(t, r, 11, 11)
	assertEmpty(t, r, 0, 0)
	assertEmpty(t, r, 19, 19)
}

func TestRasterFlatBottom_Thin(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	triangle := Triangle{
		A: Vec2{X: 9, Y: 4},
		B: Vec2{X: 8, Y: 14},
		C: Vec2{X: 10, Y: 14},
	}
	RasterTriangle(&triangle, 1, r)
	t.Log(r)

	assertFilled(t, r, 9, 9)
	assertEmpty(t, r, 0, 0)
	assertEmpty(t, r, 19, 19)
}

func TestRasterFlatBottom_Wide(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	triangle := Triangle{
		A: Vec2{X: 9, Y: 2},
		B: Vec2{X: 1, Y: 18},
		C: Vec2{X: 18, Y: 18},
	}
	RasterTriangle(&triangle, 1, r)
	t.Log(r)

	assertFilled(t, r, 9, 10)
	assertFilled(t, r, 5, 14)
	assertFilled(t, r, 13, 14)
	assertEmpty(t, r, 0, 0)
	assertEmpty(t, r, 19, 0)
}

// ---- general triangle ----

func TestRasterGeneral_MajorRight(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	triangle := Triangle{
		A: Vec2{X: 2, Y: 2},
		B: Vec2{X: 4, Y: 10},
		C: Vec2{X: 14, Y: 18},
	}
	RasterTriangle(&triangle, 1, r)
	t.Log(r)

	assertFilled(t, r, 5, 10)
	assertEmpty(t, r, 0, 0)
	assertEmpty(t, r, 19, 0)
	assertEmpty(t, r, 19, 19)
}

func TestRasterGeneral_MajorLeft(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	triangle := Triangle{
		A: Vec2{X: 14, Y: 2},
		B: Vec2{X: 12, Y: 10},
		C: Vec2{X: 2, Y: 18},
	}
	RasterTriangle(&triangle, 1, r)
	t.Log(r)

	assertFilled(t, r, 10, 10)
	assertEmpty(t, r, 0, 0)
	assertEmpty(t, r, 19, 0)
	assertEmpty(t, r, 19, 19)
}

func TestRasterGeneral_Diagonal(t *testing.T) {
	r := &mock.Grid{
		Rows:  30,
		Cols:  30,
		Cells: make([]int32, 30*30),
	}
	triangle := Triangle{
		A: Vec2{X: 1, Y: 1},
		B: Vec2{X: 28, Y: 14},
		C: Vec2{X: 1, Y: 28},
	}
	RasterTriangle(&triangle, 1, r)
	t.Log(r)

	assertFilled(t, r, 5, 10)
	assertFilled(t, r, 5, 18)
	assertEmpty(t, r, 29, 29)
	assertEmpty(t, r, 29, 0)
}

func TestRasterGeneral_SmallTriangle(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	triangle := Triangle{
		A: Vec2{X: 8, Y: 8},
		B: Vec2{X: 11, Y: 8},
		C: Vec2{X: 9, Y: 11},
	}
	RasterTriangle(&triangle, 1, r)
	t.Log(r)

	assertFilled(t, r, 9, 9)
	assertEmpty(t, r, 0, 0)
	assertEmpty(t, r, 19, 19)
}

func TestRasterGeneral_NearlyFlat(t *testing.T) {
	// triangle with very small Y difference between vertices â€” precision stress test
	r := &mock.Grid{
		Rows:  30,
		Cols:  30,
		Cells: make([]int32, 30*30),
	}
	triangle := Triangle{
		A: Vec2{X: 2, Y: 10},
		B: Vec2{X: 15, Y: 10.01},
		C: Vec2{X: 8, Y: 20},
	}
	RasterTriangle(&triangle, 1, r)
	t.Log(r)

	assertFilled(t, r, 8, 15)
	assertEmpty(t, r, 0, 0)
	assertEmpty(t, r, 29, 29)
}

func TestRasterGeneral_MajorRightPartlyOutside(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	triangle := Triangle{
		A: Vec2{X: 2, Y: 2},
		B: Vec2{X: 4, Y: 24},
		C: Vec2{X: 22, Y: 20},
	}
	RasterTriangle(&triangle, 1, r)
	t.Log(r)

	assertFilled(t, r, 3, 3)
	assertFilled(t, r, 4, 19)
	assertEmpty(t, r, 3, 19)
	assertEmpty(t, r, 0, 0)
	assertEmpty(t, r, 19, 0)
	assertFilled(t, r, 19, 19)
	assertFilled(t, r, 19, 18)
	assertEmpty(t, r, 19, 17)
}

func TestRaster_RectanglePartlyOutsideLeftDoesNotPanic(t *testing.T) {
	r := &mock.Grid{
		Rows:  21,
		Cols:  21,
		Cells: make([]int32, 21*21),
	}

	RasterRectangle(
		Vec2{X: -4, Y: 4},
		Vec2{X: -1, Y: 4},
		Vec2{X: -1, Y: 17},
		Vec2{X: -4, Y: 17},
		1,
		1,
		r,
	)

	t.Log(r)

	for i, v := range r.Cells {
		if v != 0 {
			t.Fatalf("expected rectangle fully left of region to leave grid empty; cell %d = %d", i, v)
		}
	}
}

func TestRaster_RectanglePartlyClippedToRegion(t *testing.T) {
	r := &mock.Grid{
		Rows:  21,
		Cols:  21,
		Cells: make([]int32, 21*21),
	}

	RasterRectangle(
		Vec2{X: -2, Y: 4},
		Vec2{X: 6, Y: 4},
		Vec2{X: 6, Y: 10},
		Vec2{X: -2, Y: 10},
		1,
		1,
		r,
	)

	t.Log(r)

	assertFilled(t, r, 0, 5)
	assertFilled(t, r, 5, 5)
	assertEmpty(t, r, 7, 5)
}

// ---- shared edge / top-left rule ----

func TestRasterSharedEdge_Horizontal(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	t1 := Triangle{
		A: Vec2{X: 2, Y: 2},
		B: Vec2{X: 16, Y: 2},
		C: Vec2{X: 9, Y: 10},
	}
	t2 := Triangle{
		A: Vec2{X: 2, Y: 2},
		B: Vec2{X: 9, Y: 10},
		C: Vec2{X: 2, Y: 16},
	}
	RasterTriangle(&t1, 1, r)
	RasterTriangle(&t2, 1, r)
	t.Log(r)

	assertNoDoubleCount(t, r, 1)
}

func TestRasterSharedEdge_Diagonal(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	t1 := Triangle{
		A: Vec2{X: 2, Y: 2},
		B: Vec2{X: 16, Y: 2},
		C: Vec2{X: 16, Y: 16},
	}
	t2 := Triangle{
		A: Vec2{X: 2, Y: 2},
		B: Vec2{X: 16, Y: 16},
		C: Vec2{X: 2, Y: 16},
	}
	RasterTriangle(&t1, 1, r)
	RasterTriangle(&t2, 1, r)
	t.Log(r)

	assertNoDoubleCount(t, r, 1)

	for y := uint(3); y < 15; y++ {
		for x := uint(3); x < 15; x++ {
			assertFilled(t, r, x, y)
		}
	}
}

func TestRasterSharedEdge_FourTriangles(t *testing.T) {
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	cx, cy := 10.0, 10.0
	triangles := []Triangle{
		{A: Vec2{X: 2, Y: 2}, B: Vec2{X: 18, Y: 2}, C: Vec2{X: cx, Y: cy}},
		{A: Vec2{X: 18, Y: 2}, B: Vec2{X: 18, Y: 18}, C: Vec2{X: cx, Y: cy}},
		{A: Vec2{X: 18, Y: 18}, B: Vec2{X: 2, Y: 18}, C: Vec2{X: cx, Y: cy}},
		{A: Vec2{X: 2, Y: 18}, B: Vec2{X: 2, Y: 2}, C: Vec2{X: cx, Y: cy}},
	}
	for i := range triangles {
		RasterTriangle(&triangles[i], 1, r)
	}
	t.Log(r)

	assertNoDoubleCount(t, r, 1)
}

func TestRasterSharedEdge_FullCoverage(t *testing.T) {
	// two triangles forming a rectangle â€” every interior cell should be filled exactly once
	r := &mock.Grid{
		Rows:  20,
		Cols:  20,
		Cells: make([]int32, 20*20),
	}
	t1 := Triangle{
		A: Vec2{X: 2, Y: 2},
		B: Vec2{X: 16, Y: 2},
		C: Vec2{X: 16, Y: 16},
	}
	t2 := Triangle{
		A: Vec2{X: 2, Y: 2},
		B: Vec2{X: 16, Y: 16},
		C: Vec2{X: 2, Y: 16},
	}
	RasterTriangle(&t1, 1, r)
	RasterTriangle(&t2, 1, r)
	t.Log(r)

	assertNoDoubleCount(t, r, 1)

	rRows, rCols := r.Dimensions()
	rCells := r.CellData()

	// count total filled cells â€” should equal roughly the rectangle area
	filled := 0
	for y := uint(0); y < rRows; y++ {
		for x := uint(0); x < rCols; x++ {
			if rCells[y*rCols+x] > 0 {
				filled++
			}
		}
	}
	// rectangle is 14x14 = 196 cells, allow some boundary variation
	if filled < 180 || filled > 210 {
		t.Errorf("expected ~196 filled cells for rectangle, got %d", filled)
	}
}

func TestRasterImage(t *testing.T) {
	r := &mock.Grid{
		Rows:  30,
		Cols:  110,
		Cells: make([]int32, 30*110),
	}
	path := "../../data/test4.png"

	//t.Log(r)
	if err := RasterPNG(path, 4.2286, r); err != nil {
		t.Errorf("ERROR: \n%s\n%s\n", err, r)
	} else {
		rCells := r.CellData()
		t.Logf("%s\n(x:4, y:2): %d\t(x:8, y:5): %d\n", r, rCells[2*60+4], rCells[5*60+8])
	}

}

func TestRasterImage2(t *testing.T) {
	r := &mock.Grid{
		Rows:  30,
		Cols:  110,
		Cells: make([]int32, 30*110),
	}
	path := "../../data/test4.png"

	//t.Log(r)
	if err := RasterPNG2(path, 4.2286, r, 2, 1); err != nil {
		t.Errorf("ERROR: \n%s\n%s\n", err, r)
	} else {
		rCells := r.CellData()
		t.Logf("%s\n(x:4, y:2): %d\t(x:8, y:5): %d\n", r, rCells[2*60+4], rCells[5*60+8])
	}

}

func TestRasterImage3(t *testing.T) {
	r := &mock.Grid{
		Rows:  30,
		Cols:  110,
		Cells: make([]int32, 30*110),
	}
	path := "../../data/realisticMap.png" //"./test4.png"

	//t.Log(r)
	if err := RasterPNG3(path, 4.2286, r, 1); err != nil {
		t.Errorf("ERROR: \n%s\n%s\n", err, r)
	} else {
		rCells := r.CellData()
		t.Logf("%s\n(x:4, y:2): %d\t(x:8, y:5): %d\n", r, rCells[2*60+4], rCells[5*60+8])
	}

}

func TestRasterImage3v2(t *testing.T) {
	r := &mock.Grid{
		Rows:  30,
		Cols:  110,
		Cells: make([]int32, 30*110),
	}
	path := "../../data/realisticMap.png" //"./test4.png"

	//t.Log(r)
	if err := RasterPNG3_v2(path, 4.2286, r, 1); err != nil {
		t.Errorf("ERROR: \n%s\n%s\n", err, r)
	} else {
		rCells := r.CellData()
		t.Logf("%s\n(x:4, y:2): %d\t(x:8, y:5): %d\n", r, rCells[2*60+4], rCells[5*60+8])
	}
}

func TestRasterImage3v2_Equivalence(t *testing.T) {
	r1 := &mock.Grid{
		Rows:  30,
		Cols:  110,
		Cells: make([]int32, 30*110),
	}
	r2 := &mock.Grid{
		Rows:  30,
		Cols:  110,
		Cells: make([]int32, 30*110),
	}
	path := "../../data/realisticMap.png" //"./test4.png"

	if err := RasterPNG3(path, 4.2286, r1, 1); err != nil {
		t.Errorf("ERROR: \n%s\n%s\n", err, r1)
	}
	if err := RasterPNG3_v2(path, 4.2286, r2, 1); err != nil {
		t.Errorf("ERROR: \n%s\n%s\n", err, r2)
	}

	t.Logf("NORMAL:\n%s\nV2:\n%s", r1, r2)
	for i := range r1.Cells {
		if v1, v2 := r1.Cells[i], r2.Cells[i]; v1 != v2 {
			y := i / int(r1.Cols)
			x := i % int(r1.Cols)
			t.Errorf("RasterPNG3_v2 NOT equivalent to first version (X: %d, Y: %d) -> BASE: %d, V2: %d\n", x, y, v1, v2)
		}
	}
}

func TestRasterImage3WithAura_v2Equivalence(t *testing.T) {
	r1 := &mock.Grid{
		Rows:  30,
		Cols:  110,
		Cells: make([]int32, 30*110),
	}
	r2 := &mock.Grid{
		Rows:  30,
		Cols:  110,
		Cells: make([]int32, 30*110),
	}
	path := "../../data/realisticMap.png"

	if err := RasterPNG3WithAura(path, 4.2286, r1, 10, 1); err != nil {
		t.Errorf("ERROR: \n%s\n%s\n", err, r1)
	}
	if err := RasterPNG3_v2WithAura(path, 4.2286, r2, 10, 1); err != nil {
		t.Errorf("ERROR: \n%s\n%s\n", err, r2)
	}

	t.Logf("NORMAL:\n%s\nV2:\n%s", r1, r2)
	for i := range r1.Cells {
		if v1, v2 := r1.Cells[i], r2.Cells[i]; v1 != v2 {
			y := i / int(r1.Cols)
			x := i % int(r1.Cols)
			t.Errorf("RasterPNG3_v2WithAura NOT equivalent to first version (X: %d, Y: %d) -> BASE: %d, V2: %d\n", x, y, v1, v2)
		}
	}
}

func TestRasterImage4_Selection(t *testing.T) {

	t.Run("SimpleImage", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)
		defer log.SetOutput(os.Stderr)

		r := &mock.Grid{
			Rows:  30,
			Cols:  110,
			Cells: make([]int32, 30*110),
		}
		path := "../../data/realisticMap.png" //"./test4.png"

		if err := RasterPNG4(path, 4.2286, r, 1); err != nil {
			t.Errorf("ERROR: \n%s\n", err)
		}
		if strings.Contains(buf.String(), "[RASTER] Image too complex") {
			t.Fatal("Expected to select optimized function, but didn`t")
		}
		t.Log(r)
	})

	t.Run("ComplexImage", func(t *testing.T) {
		var buf bytes.Buffer
		log.SetOutput(&buf)
		defer log.SetOutput(os.Stderr)

		r := &mock.Grid{
			Rows:  30,
			Cols:  110,
			Cells: make([]int32, 30*110),
		}
		path := "../../data/test4.png"

		if err := RasterPNG4(path, 4.2286, r, 1); err != nil {
			t.Errorf("ERROR: \n%s\n", err)
		}
		if !strings.Contains(buf.String(), "[RASTER] Image too complex") {
			t.Fatal("Expected to select general function, but went with the optimized one instead")
		}
		t.Log(r)
	})

}
