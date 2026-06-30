package emergency

// Node is a fixed emergency signaling node position.
type Node struct {
	id      string
	x, y, z float64
	floor   int
}

// NewNode creates a signaling node at a layer-qualified position.
func NewNode(id string, x, y, z float64, floor int) Node {
	return Node{
		id:    id,
		x:     x,
		y:     y,
		z:     z,
		floor: floor,
	}
}

// Id returns the signaling node identifier.
func (n Node) Id() string {
	return n.id
}

// Position returns the node position and floor index.
func (n Node) Position() (x, y, z float64, floor int) {
	return n.x, n.y, n.z, n.floor
}
