package mock

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/Kentalives/LifeRouter/pkg/domain"
)

// AgentRegex is the ID prefix treated as an agent by the mock external system.
var AgentRegex = "Agent_"

// LightNodeRegex is the ID prefix treated as a signal node by the mock external system.
var LightNodeRegex = "Node_"

// ExternalSystem implements domain.ExternalSystem for tests and local runs.
type ExternalSystem struct {
	nodes        []*Node
	layerNames   []string
	layerHeights []float64
}

func fuzzyEq(a, b float64) bool {
	return math.Abs(a-b) < 0.0001
}

// Aux functions

// NewExternalSystem creates a mock external system with named floor heights.
func NewExternalSystem(layerNames []string, layerHeights []float64, nodes []*Node) (*ExternalSystem, error) {
	if len(layerNames) != len(layerHeights) {
		return nil, fmt.Errorf("No matching names (%d) and heights (%d)", len(layerNames), len(layerHeights))
	} else if len(layerNames) < 1 {
		return nil, fmt.Errorf("No layers were created")
	}

	return &ExternalSystem{
		nodes:        nodes,
		layerNames:   layerNames,
		layerHeights: layerHeights,
	}, nil
}

func (e ExternalSystem) layerIdx(name string) int {
	for i, l := range e.layerNames {
		if l == name {
			return i
		}
	}
	return -1
}

func (e ExternalSystem) findNode(nodeId string) *Node {
	for _, n := range e.nodes {
		if n.Id() == nodeId {
			return n
		}
	}

	return nil
}

// Main interface
// ObjectTraversalCost returns a deterministic traversal cost from the object ID prefix.
func (e ExternalSystem) ObjectTraversalCost(ctx context.Context, id string, objType string) (domain.Cost, error) {

	if strings.HasPrefix(id, AgentRegex) {
		return domain.COST_AGENT, nil

	} else if strings.HasPrefix(id, LightNodeRegex) {
		return domain.COST_LIGHT_OBJECT, nil
	}

	return domain.COST_UNREACHABLE, nil
}

// PointFloorHeight returns the configured floor height below or at z.
func (e ExternalSystem) PointFloorHeight(ctx context.Context, x, y, z float64) (float64, error) {

	layerName, err := e.FloorFromPoint(x, y, z)
	if err != nil {
		return 0, err
	}
	idx := e.layerIdx(layerName)
	if idx == -1 {
		return 0, fmt.Errorf("Layer not found")
	}

	return e.layerHeights[idx], nil
}

// HeightAtFloorPoint returns the configured height for region.
func (e ExternalSystem) HeightAtFloorPoint(ctx context.Context, x, y float64, region string) (float64, error) {
	idx := e.layerIdx(region)
	if idx == -1 {
		return 0, fmt.Errorf("Layer not found")
	}

	return e.layerHeights[idx], nil
}

// FloorFromPoint resolves z to the highest configured floor not above it.
func (e ExternalSystem) FloorFromPoint(x, y, z float64) (string, error) {
	minIdx := 0
	minHeight := math.Inf(-1)
	for i, height := range e.layerHeights {
		if (height < z || fuzzyEq(height, z)) && height > minHeight {
			minIdx = i
			minHeight = height
		}
	}

	return e.layerNames[minIdx], nil
}

// SignalingSetDirection stores dir in the named mock node.
func (e *ExternalSystem) SignalingSetDirection(ctx context.Context, nodeId string, dir domain.Direction) error {
	n := e.findNode(nodeId)
	if n == nil {
		return fmt.Errorf("Node - %s - not found", nodeId)
	}

	n.SetDirection(dir)
	return nil
}

// SignalingDirection returns the direction stored in the named mock node.
func (e ExternalSystem) SignalingDirection(ctx context.Context, nodeId string) (domain.Direction, error) {
	n := e.findNode(nodeId)
	if n == nil {
		return domain.DIR_UNKNOWN, fmt.Errorf("Node - %s - not found", nodeId)
	}

	return n.Direction(), nil
}

// LightNodeRegex returns the mock light-node ID prefix.
func (e ExternalSystem) LightNodeRegex(ctx context.Context) (string, error) {

	return LightNodeRegex, nil
}
