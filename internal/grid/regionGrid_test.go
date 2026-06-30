package grid

import (
	"testing"
)

func TestRegionGrid_Clipping(t *testing.T) {
	g := newGrid(10, 10)

	r := &RegionGrid{
		Origin: Coords{X: 11, Y: 11},
		Rows:   3,
		Cols:   5,
		Cells:  nil,
	}

	//Top left corner
	r.ClipToGrid(g, 3, 3, Coords{X: 0, Y: 0})
	expectedOrigin, expectedRows, expectedCols := Coords{X: 0, Y: 0}, 4, 4
	if r.Origin != expectedOrigin || r.Rows != uint(expectedRows) || r.Cols != uint(expectedCols) {
		t.Errorf("Wrong clipping (top-left)! Expected #Origin (%v), got (%v); #Rows (%d), got (%d); #Cols (%d), got (%d)\n", expectedOrigin, r.Origin, expectedRows, r.Rows, expectedCols, r.Cols)
	} else {
		t.Log("Correct cliping (top-left)!")
	}

	//Top right corner
	r.ClipToGrid(g, 3, 3, Coords{X: 8, Y: 0})
	expectedOrigin, expectedRows, expectedCols = Coords{X: 5, Y: 0}, 4, 5
	if r.Origin != expectedOrigin || r.Rows != uint(expectedRows) || r.Cols != uint(expectedCols) {
		t.Errorf("Wrong clipping (top-right)! Expected #Origin (%v), got (%v); #Rows (%d), got (%d); #Cols (%d), got (%d)\n", expectedOrigin, r.Origin, expectedRows, r.Rows, expectedCols, r.Cols)
	} else {
		t.Log("Correct cliping (top-right)!")
	}

	//Full grid
	r.ClipToGrid(g, 100, 100, Coords{X: 8, Y: 6})
	expectedOrigin, expectedRows, expectedCols = Coords{X: 0, Y: 0}, 10, 10
	if r.Origin != expectedOrigin || r.Rows != uint(expectedRows) || r.Cols != uint(expectedCols) {
		t.Errorf("Wrong clipping (full-grid)! Expected #Origin (%v), got (%v); #Rows (%d), got (%d); #Cols (%d), got (%d)\n", expectedOrigin, r.Origin, expectedRows, r.Rows, expectedCols, r.Cols)
	} else {
		t.Log("Correct cliping (full-grid)!")
	}

	//Bottom left corner
	r.ClipToGrid(g, 4, 3, Coords{X: 1, Y: 8})
	expectedOrigin, expectedRows, expectedCols = Coords{X: 0, Y: 5}, 5, 6
	if r.Origin != expectedOrigin || r.Rows != uint(expectedRows) || r.Cols != uint(expectedCols) {
		t.Errorf("Wrong clipping (bottom-left)! Expected #Origin (%v), got (%v); #Rows (%d), got (%d); #Cols (%d), got (%d)\n", expectedOrigin, r.Origin, expectedRows, r.Rows, expectedCols, r.Cols)
	} else {
		t.Log("Correct cliping (bottom-left)!")
	}

	//Bottom right corner
	r.ClipToGrid(g, 2, 5, Coords{X: 9, Y: 9})
	expectedOrigin, expectedRows, expectedCols = Coords{X: 7, Y: 4}, 6, 3
	if r.Origin != expectedOrigin || r.Rows != uint(expectedRows) || r.Cols != uint(expectedCols) {
		t.Errorf("Wrong clipping (bottom-right)! Expected #Origin (%v), got (%v); #Rows (%d), got (%d); #Cols (%d), got (%d)\n", expectedOrigin, r.Origin, expectedRows, r.Rows, expectedCols, r.Cols)
	} else {
		t.Log("Correct cliping (bottom-right)!")
	}

	//Fully inside
	r.ClipToGrid(g, 2, 3, Coords{X: 4, Y: 5})
	expectedOrigin, expectedRows, expectedCols = Coords{X: 2, Y: 2}, 7, 5
	if r.Origin != expectedOrigin || r.Rows != uint(expectedRows) || r.Cols != uint(expectedCols) {
		t.Errorf("Wrong clipping (regular)! Expected #Origin (%v), got (%v); #Rows (%d), got (%d); #Cols (%d), got (%d)\n", expectedOrigin, r.Origin, expectedRows, r.Rows, expectedCols, r.Cols)
	} else {
		t.Log("Correct cliping (regular)!")
	}

	//Fully outside
	r.ClipToGrid(g, 2, 3, Coords{X: 20, Y: 5})
	expectedOrigin, expectedRows, expectedCols = Coords{X: 18, Y: 2}, 7, 0
	if r.Origin != expectedOrigin || r.Rows != uint(expectedRows) || r.Cols != uint(expectedCols) {
		t.Errorf("Wrong clipping (outside)! Expected #Origin (%v), got (%v); #Rows (%d), got (%d); #Cols (%d), got (%d)\n", expectedOrigin, r.Origin, expectedRows, r.Rows, expectedCols, r.Cols)
	} else {
		t.Log("Correct cliping (outside)!")
	}

}
