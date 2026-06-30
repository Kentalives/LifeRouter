package domain

import (
	"math"

	"github.com/paulmach/orb"
)

// Point is a 2D point alias used by raster geometry.
type Point Vec2

// Vec2 is a two-dimensional vector in real-world or raster space.
type Vec2 struct{ X, Y float64 }

// FromPoint converts an orb point to Vec2.
func FromPoint(p orb.Point) Vec2 { return Vec2{p[0], p[1]} }

// ToPoint converts v to an orb point.
func (v Vec2) ToPoint() orb.Point { return orb.Point{v.X, v.Y} }

// Add returns the vector sum a+b.
func (a Vec2) Add(b Vec2) Vec2 { return Vec2{a.X + b.X, a.Y + b.Y} }

// Sub returns the vector difference a-b.
func (a Vec2) Sub(b Vec2) Vec2 { return Vec2{a.X - b.X, a.Y - b.Y} }

// Scale returns a multiplied by s.
func (a Vec2) Scale(s float64) Vec2 { return Vec2{a.X * s, a.Y * s} }

// Dot returns the dot product of a and b.
func (a Vec2) Dot(b Vec2) float64 { return a.X*b.X + a.Y*b.Y }

// Cross returns the 2D scalar cross product of a and b.
func (a Vec2) Cross(b Vec2) float64 { return a.X*b.Y - a.Y*b.X }

// Length returns the Euclidean length of a.
func (a Vec2) Length() float64 { return math.Sqrt(a.Dot(a)) }

// Normalize returns a unit vector in the same direction as a.
func (a Vec2) Normalize() Vec2 { return a.Scale(1 / a.Length()) }
