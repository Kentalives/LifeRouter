package mock

import (
	"sync"

	"github.com/midtxwn/geotruth/pkg/domain"

	mydomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

// Element is an in-memory oriented object used by mock geotruth.
type Element struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Z      float64 `json:"z"`
	RotY   float64 `json:"roty"`
	IdName string  `json:"idname"`
	mu     sync.RWMutex
	Width  float64
	Height float64
}

// Move updates the element position and rotation.
func (e *Element) Move(x, y, z, rotY float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.X = x
	e.Y = y
	e.Z = z
	e.RotY = rotY
}

// Position returns the element position and rotation.
func (e *Element) Position() (x, y, z, rotY float64) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.X, e.Y, e.Z, e.RotY
}

// Dims returns the element dimensions.
func (e *Element) Dims() domain.ObjectDimensions {
	return domain.ObjectDimensions{Width: e.Width, Height: e.Height}
}

// Id returns the element identifier.
func (e *Element) Id() string {
	return e.IdName
}

// Node is a mock emergency signaling node.
type Node struct {
	Element
	Dir mydomain.Direction
}

// Direction returns the currently stored signal direction.
func (n *Node) Direction() mydomain.Direction {
	return n.Dir
}

// SetDirection stores the signal direction.
func (n *Node) SetDirection(d mydomain.Direction) {
	n.Dir = d
}

// Position returns the node position and derived floor index.
func (n *Node) Position() (x, y, z float64, floor int) {
	x, y, z, _ = n.Element.Position()

	return x, y, z, int(z) / 10
}
