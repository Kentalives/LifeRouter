package domain

import (
	"context"

	"github.com/midtxwn/geotruth/pkg/natspublish"
	"github.com/midtxwn/geotruth/pkg/natsquery"
)

// ExternalSystem provides domain data that is not owned by pathfinding, such as
// object traversal costs, floor heights, and emergency signaling endpoints.
type ExternalSystem interface {
	// ObjectTraversalCost returns the extra movement cost added by an object.
	// Implementations should use the exported Cost constants as their scale.
	ObjectTraversalCost(ctx context.Context, id string, objType string) (Cost, error)

	// PointFloorHeight returns the floor height below a real-world point in meters.
	PointFloorHeight(ctx context.Context, x, y, z float64) (float64, error)

	// HeightAtFloorPoint returns the z coordinate for a floor region at x/y meters.
	// The current model assumes one floor height per region and x/y position.
	HeightAtFloorPoint(ctx context.Context, x, y float64, region string) (float64, error)

	////

	// SignalingSetDirection updates an emergency signaling node direction.
	SignalingSetDirection(ctx context.Context, nodeId string, dir Direction) error

	// SignalingDirection reads the current direction of an emergency signaling node.
	SignalingDirection(ctx context.Context, nodeId string) (Direction, error)

	// LightNodeRegex returns the geotruth query pattern for emergency signal nodes.
	LightNodeRegex(ctx context.Context) (string, error)
}

// GeoQuery is the read-side geospatial API required by pathfinding.
type GeoQuery interface {

	// NearbyObjectsOf returns oriented objects within radiusMeters of objectID.
	NearbyObjectsOf(ctx context.Context, objectID string, radiusMeters float64, regex *string) ([]natsquery.ObjectOriented, error)

	// ObjectData returns the current geotruth data for objectID.
	ObjectData(ctx context.Context, objectID string) (*natsquery.Object, error)

	// AllObjectsOriented returns all matching objects with rotations applied.
	AllObjectsOriented(ctx context.Context, regex *string) (natsquery.AllObjectsOrientedResp, error)

	// RegionFromPoint returns the floor/region name that contains an x/y/z point.
	RegionFromPoint(ctx context.Context, x, y, z float64) (string, error)
}

// GeoPublish is the write-side geospatial API used to move tracked objects.
type GeoPublish interface {
	// UpdateObjectPosition publishes a real-world object position in meters.
	UpdateObjectPosition(ctx context.Context, objectID string, x, y, z, rotY float64) (natspublish.CommitAck, error)
}
