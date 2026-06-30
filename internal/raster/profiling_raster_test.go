package raster

import (
	"math"
	"testing"

	"github.com/Kentalives/LifeRouter/internal/mock"
)

type lineCase struct {
	a, b Vec2
}

func benchmarkGrid(rows, cols uint) *mock.Grid {
	return &mock.Grid{
		Rows:  rows,
		Cols:  cols,
		Cells: make([]int32, rows*cols),
	}
}

func benchmarkLineCorners(p1, p2 Vec2, thickness float64) [4]Vec2 {
	dx := p2.X - p1.X
	dy := p2.Y - p1.Y
	length := math.Hypot(dx, dy)
	if length == 0 {
		return [4]Vec2{}
	}

	offX := -dy / length * thickness / 2
	offY := dx / length * thickness / 2
	return [4]Vec2{
		{X: p1.X + offX, Y: p1.Y + offY},
		{X: p2.X + offX, Y: p2.Y + offY},
		{X: p2.X - offX, Y: p2.Y - offY},
		{X: p1.X - offX, Y: p1.Y - offY},
	}
}

var (
	flatTopTriangleCases = [][3]Vec2{
		{{X: 8, Y: 5}, {X: 42, Y: 5}, {X: 25, Y: 45}},
		{{X: 3, Y: 12}, {X: 49, Y: 12}, {X: 35, Y: 48}},
		{{X: 15, Y: 4}, {X: 38, Y: 4}, {X: 9, Y: 41}},
	}
	flatBottomTriangleCases = [][3]Vec2{
		{{X: 25, Y: 4}, {X: 6, Y: 46}, {X: 45, Y: 46}},
		{{X: 8, Y: 8}, {X: 4, Y: 40}, {X: 50, Y: 40}},
		{{X: 42, Y: 2}, {X: 11, Y: 48}, {X: 39, Y: 48}},
	}
	generalTriangleCases = []Triangle{
		{A: Vec2{X: 4, Y: 3}, B: Vec2{X: 58, Y: 21}, C: Vec2{X: 17, Y: 70}},
		{A: Vec2{X: 67, Y: 9}, B: Vec2{X: 13, Y: 37}, C: Vec2{X: 48, Y: 74}},
		{A: Vec2{X: 31, Y: 5}, B: Vec2{X: 72, Y: 62}, C: Vec2{X: 7, Y: 54}},
	}
	lineCases = []lineCase{
		{a: Vec2{X: 5, Y: 5}, b: Vec2{X: 70, Y: 70}},
		{a: Vec2{X: 70, Y: 8}, b: Vec2{X: 10, Y: 63}},
		{a: Vec2{X: 12, Y: 38}, b: Vec2{X: 72, Y: 42}},
	}
)

func BenchmarkRaster_FlatTopTriangle(b *testing.B) {
	r := benchmarkGrid(50, 50)

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		tc := flatTopTriangleCases[i%len(flatTopTriangleCases)]
		rasterFlatTopTriangle(tc[0], tc[1], tc[2], 3, r)
		i++
	}
	b.ReportMetric(float64(r.Rows*r.Cols), "cells")
}

func BenchmarkRaster_FlatBottomTriangle(b *testing.B) {
	r := benchmarkGrid(50, 50)

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		tc := flatBottomTriangleCases[i%len(flatBottomTriangleCases)]
		rasterFlatBottomTriangle(tc[0], tc[1], tc[2], 3, r)
		i++
	}
	b.ReportMetric(float64(r.Rows*r.Cols), "cells")
}

func BenchmarkRaster_GeneralTriangle(b *testing.B) {
	r := benchmarkGrid(77, 77)

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		t := generalTriangleCases[i%len(generalTriangleCases)]
		RasterTriangle(&t, 3, r)
		i++
	}
	b.ReportMetric(float64(r.Rows*r.Cols), "cells")
}

func BenchmarkRaster_Line(b *testing.B) {
	b.Run("RectangleExpansion", func(b *testing.B) {
		r := benchmarkGrid(77, 77)

		b.ReportAllocs()
		i := 0
		for b.Loop() {
			tc := lineCases[i%len(lineCases)]
			corners := benchmarkLineCorners(tc.a, tc.b, 3)
			RasterRectangle(corners[0], corners[1], corners[2], corners[3], 2, 1, r)
			i++
		}
	})

	b.Run("Native", func(b *testing.B) {
		r := benchmarkGrid(77, 77)

		b.ReportAllocs()
		i := 0
		for b.Loop() {
			tc := lineCases[i%len(lineCases)]
			RasterLine(tc.a, tc.b, 3, 2, 1, r)
			i++
		}
	})
}

func BenchmarkRaster_PNG(b *testing.B) {
	cases := []struct {
		name          string
		path          string
		rows, cols    uint
		pixelsPerCell float64
	}{
		{name: "BasicMap", path: "../../data/map.png", rows: 61, cols: 114, pixelsPerCell: 10},
		{name: "RealisticMap", path: "../../data/realisticMap.png", rows: 30, cols: 110, pixelsPerCell: 4.2286},
	}

	for _, tc := range cases {
		b.Run(tc.name+"/PNG3", func(b *testing.B) {
			r := benchmarkGrid(tc.rows, tc.cols)

			b.ReportAllocs()
			for b.Loop() {
				if err := RasterPNG3(tc.path, tc.pixelsPerCell, r, 1); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(tc.name+"/PNG3_v2", func(b *testing.B) {
			r := benchmarkGrid(tc.rows, tc.cols)

			b.ReportAllocs()
			for b.Loop() {
				if err := RasterPNG3_v2(tc.path, tc.pixelsPerCell, r, 1); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(tc.name+"/PNG4WithAura", func(b *testing.B) {
			r := benchmarkGrid(tc.rows, tc.cols)

			b.ReportAllocs()
			for b.Loop() {
				if err := RasterPNG4WithAura(tc.path, tc.pixelsPerCell, r, 1, 1); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
