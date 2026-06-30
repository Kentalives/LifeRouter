package mock

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sync"

	"github.com/midtxwn/geotruth/pkg/domain"
	"github.com/midtxwn/geotruth/pkg/natspublish"
	"github.com/midtxwn/geotruth/pkg/natsquery"

	"github.com/Kentalives/LifeRouter/internal/log"
)

// ElementToGeoTruthObject converts e to the geotruth object shape.
func (g *GeoTruth) ElementToGeoTruthObject(e *Element) (*natsquery.Object, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	floorName, err := g.ex.FloorFromPoint(e.X, e.Y, e.Z)
	if err != nil {
		return nil, err
	}

	return &natsquery.Object{
		ID:     e.Id(),
		Region: &floorName,
		X:      e.X,
		Y:      e.Y,
		Z:      e.Z,
		RotY:   e.RotY,
	}, nil
}

func boundsFromElement(e *Element) natsquery.OrientedBounds {
	center := domain.Point{X: e.X, Y: e.Y}
	d := e.Dims()
	d.Width /= 2
	d.Height /= 2

	corners := []domain.Point{
		{X: -d.Width, Y: -d.Height}, //TL
		{X: +d.Width, Y: -d.Height}, //TR
		{X: -d.Width, Y: +d.Height}, //BL
		{X: +d.Width, Y: +d.Height}, //BR
	}

	for i, c := range corners {
		corners[i].X = c.X*math.Cos(e.RotY) - c.Y*math.Sin(e.RotY)
		corners[i].Y = c.X*math.Sin(e.RotY) + c.Y*math.Cos(e.RotY)
		corners[i] = corners[i].Add(center)
	}

	bounds := natsquery.OrientedBounds{
		TL: corners[0],
		TR: corners[1],
		BL: corners[2],
		BR: corners[3],
	}

	return bounds
}

// ToGeoTruthOrientedObject converts e to an oriented geotruth object.
func (e *Element) ToGeoTruthOrientedObject() natsquery.ObjectOriented {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return natsquery.ObjectOriented{
		ID: e.Id(),
		Position: natsquery.Position3D{
			X: e.X,
			Y: e.Y,
			Z: e.Z,
		},
		Bounds: boundsFromElement(e),
	}
}

// GeoTruth is an in-memory geospatial query and publisher implementation.
type GeoTruth struct {
	storage map[string]*Element
	mu      sync.RWMutex
	ex      *ExternalSystem
}

// NewGeoTruth creates an empty mock geotruth store.
func NewGeoTruth(numElems int, ex *ExternalSystem) *GeoTruth {
	return &GeoTruth{
		storage: make(map[string]*Element, numElems),
		ex:      ex,
	}
}

// AddObject registers e in the mock geotruth store.
func (g *GeoTruth) AddObject(e *Element) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, ok := g.storage[e.Id()]; ok {
		return fmt.Errorf("[MOCK-GEOTRUTH] object already registered: %s", e.Id())
	}

	g.storage[e.Id()] = e
	return nil
}

// ObjectData returns the current data for objectID.
func (g *GeoTruth) ObjectData(ctx context.Context, objectID string) (*natsquery.Object, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	e, ok := g.storage[objectID]
	if !ok {
		return nil, fmt.Errorf("[MOCK-GEOTRUTH] object not registered: %s", objectID)
	}

	return g.ElementToGeoTruthObject(e)
}

// NearbyObjectsOf returns same-floor oriented objects within radiusMeters.
func (g *GeoTruth) NearbyObjectsOf(ctx context.Context, objectID string, radiusMeters float64, regex *string) ([]natsquery.ObjectOriented, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	source, ok := g.storage[objectID]
	if !ok {
		return nil, fmt.Errorf("[MOCK-GEOTRUTH] object not registered: %s", objectID)
	}

	sourceX, sourceY, sourceZ, _ := source.Position()
	sourceFloor, err := g.ex.FloorFromPoint(sourceX, sourceY, sourceZ)
	if err != nil {
		return nil, err
	}
	radiusSquared := radiusMeters * radiusMeters

	ids := make([]string, 0, len(g.storage))
	for id := range g.storage {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	resp := make([]natsquery.ObjectOriented, 0, len(g.storage)-1)
	for _, id := range ids {
		if id == objectID {
			continue
		}

		elem := g.storage[id]
		x, y, z, _ := elem.Position()
		floor, err := g.ex.FloorFromPoint(x, y, z)
		if err != nil {
			return nil, err
		}
		if floor != sourceFloor {
			continue
		}

		dx := x - sourceX
		dy := y - sourceY
		if dx*dx+dy*dy > radiusSquared {
			continue
		}

		resp = append(resp, elem.ToGeoTruthOrientedObject())
	}

	return resp, nil
}

// AllObjectsOriented groups all stored objects by resolved floor name.
func (g *GeoTruth) AllObjectsOriented(ctx context.Context, regex *string) (natsquery.AllObjectsOrientedResp, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	resp := natsquery.AllObjectsOrientedResp{
		Regions: make(map[string][]natsquery.ObjectOriented),
	}

	for _, elem := range g.storage {
		floor, err := g.ex.FloorFromPoint(elem.X, elem.Y, elem.Z)
		if err != nil {
			log.Errorf("[MOCK-GEOTRUTH] Floor from point: %s\n", err)
			continue
		}
		resp.Regions[floor] = append(resp.Regions[floor], elem.ToGeoTruthOrientedObject())
	}

	return resp, nil
}

// RegionFromPoint resolves a point to a mock floor name.
func (g *GeoTruth) RegionFromPoint(ctx context.Context, x, y, z float64) (string, error) {
	return g.ex.FloorFromPoint(x, y, z)
}

// UpdateObjectPosition updates a stored object's position.
func (g *GeoTruth) UpdateObjectPosition(ctx context.Context, objectID string, x, y, z, rotY float64) (natspublish.CommitAck, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	c := natspublish.CommitAck{
		InstanceID: "",
		CommitSeq:  0,
	}

	e, ok := g.storage[objectID]
	if !ok {
		return c, fmt.Errorf("[MOCK-GEOTRUTH] object not registered: %s", objectID)
	}

	e.Move(x, y, z, rotY)

	return c, nil
}
