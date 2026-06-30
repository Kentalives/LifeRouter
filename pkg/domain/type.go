package domain

import (
	"github.com/google/uuid"
)

// GraphWaypoint is a waypoint in a preferred emergency route graph. X, Y, and Z
// are real-world meter coordinates; Floor is the corresponding pathfinding layer.
type GraphWaypoint struct {
	// Id uniquely identifies the waypoint inside its route graph.
	Id *uuid.UUID

	// X, Y, and Z are real-world coordinates in meters.
	X, Y, Z float64

	// Floor is the pathfinding layer index that contains the waypoint.
	Floor int
}

// GraphEdge links two waypoints in a preferred route graph. Weight is optional;
// nil weights default to 1.
type GraphEdge struct {
	// FromWaypoint is the UUID of the edge origin waypoint.
	FromWaypoint *uuid.UUID

	// ToWaypoint is the UUID of the edge destination waypoint.
	ToWaypoint *uuid.UUID

	// Weight scales the preference applied along this edge. Nil defaults to 1.
	Weight *uint16
}

// RouteGraph contains preferred emergency paths for one floor.
type RouteGraph struct {
	// Floor is the pathfinding layer index this graph applies to.
	Floor int

	// Waypoints are the graph vertices expressed in real-world coordinates.
	Waypoints []GraphWaypoint

	// Edges are directed preferred-route links between waypoints.
	Edges []GraphEdge
}
