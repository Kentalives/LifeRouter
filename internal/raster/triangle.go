package raster

import (
	"math"

	"github.com/Kentalives/LifeRouter/internal/domain"

	"github.com/paulmach/orb"
)

// FromPoint converts orb points to the raster vector type.
var FromPoint = domain.FromPoint

const epsilon = 1e-10

// Triangle is the raster primitive used after polygons or rectangles are split
// into three-point shapes.
type Triangle struct {
	A, B, C Vec2
}

func collinear(a, b, c Vec2) bool {
	area := a.X*(b.Y-c.Y) + b.X*(c.Y-a.Y) + c.X*(a.Y-b.Y)
	return math.Abs(area) < epsilon
}

// ValidTriangle accepts one closed ring with three non-collinear points.
func ValidTriangle(polygon *orb.Polygon) bool {
	if len(*polygon) != 1 {
		return false
	}

	ring := (*polygon)[0]
	if len(ring) != 4 {
		return false
	}

	if ring[0] != ring[3] {
		return false
	}

	a, b, c := FromPoint(ring[0]), FromPoint(ring[1]), FromPoint(ring[2])
	if collinear(a, b, c) {
		return false
	}

	return true
}

// NewTriangle converts a valid three-point polygon into a raster triangle.
func NewTriangle(polygon *orb.Polygon) Triangle {
	ring := (*polygon)[0]
	return Triangle{
		A: FromPoint(ring[0]),
		B: FromPoint(ring[1]),
		C: FromPoint(ring[2]),
	}
}
